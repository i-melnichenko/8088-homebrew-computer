package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/protocol"
)

const monitorAttempts = 10

// Client exchanges monitor protocol frames over a transport.
// Calls must be serialized, and transport reads must return periodically
// so response deadlines can be checked.
type Client struct {
	serial      io.ReadWriter
	diagnostics io.Writer
	sequence    byte
}

// New creates a client. The caller owns and closes the transport.
// A nil diagnostics writer disables progress messages.
func New(serial io.ReadWriter, diagnostics io.Writer) *Client {
	if diagnostics == nil {
		diagnostics = io.Discard
	}
	return &Client{serial: serial, diagnostics: diagnostics}
}

// Transact sends a request with the default retry count.
func (c *Client) Transact(request protocol.Frame) (protocol.Frame, error) {
	return c.TransactAttempts(request, monitorAttempts)
}

// TransactTimeout allows EEPROM programming and readback verification to take
// longer than an ordinary monitor request. Retries retain the same packet.
func (c *Client) TransactTimeout(request protocol.Frame, timeout time.Duration) (protocol.Frame, error) {
	return c.transact(context.Background(), request, monitorAttempts, timeout)
}

// TransactAttempts sends a request, reusing its sequence number on retries.
func (c *Client) TransactAttempts(request protocol.Frame, attempts int) (protocol.Frame, error) {
	return c.TransactAttemptsContext(context.Background(), request, attempts)
}

// TransactAttemptsContext permits cancellation between transport operations.
// Reads must still return periodically, as required by Client.
func (c *Client) TransactAttemptsContext(ctx context.Context, request protocol.Frame, attempts int) (protocol.Frame, error) {
	return c.transact(ctx, request, attempts, time.Second)
}

func (c *Client) transact(ctx context.Context, request protocol.Frame, attempts int, timeout time.Duration) (protocol.Frame, error) {
	if timeout <= 0 {
		return protocol.Frame{}, errors.New("response timeout must be positive")
	}
	request.Sequence = c.sequence
	c.sequence++
	packet, err := protocol.Encode(request)
	if err != nil {
		return protocol.Frame{}, err
	}
	for attempt := 0; attempt < attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return protocol.Frame{}, err
		}
		if attempt == 0 && request.Command == protocol.Ping {
			fmt.Fprintln(c.diagnostics, "boardctl: waiting for board monitor; press Reset if needed")
		} else if attempt > 0 {
			fmt.Fprintf(c.diagnostics, "boardctl: retrying command %#02x (%d/%d)\n", request.Command, attempt+1, attempts)
		}
		if _, err = c.serial.Write(packet); err != nil {
			return protocol.Frame{}, err
		}
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			response, err := readPacketContext(ctx, c.serial, deadline)
			if err != nil {
				break
			}
			frame, err := protocol.Decode(response)
			if err == nil && frame.Command == request.Command|0x80 && frame.Sequence == request.Sequence {
				return frame, nil
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return protocol.Frame{}, err
	}
	return protocol.Frame{}, fmt.Errorf("no response to command %#02x from board monitor after %d attempts", request.Command, attempts)
}

func readPacket(file io.Reader, deadline time.Time) ([]byte, error) {
	return readPacketContext(context.Background(), file, deadline)
}

func readPacketContext(ctx context.Context, file io.Reader, deadline time.Time) ([]byte, error) {
	packet := make([]byte, 0, 160)
	one := []byte{0}
	started := false
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, err := file.Read(one)
		// With VMIN=0 and VTIME=1, an idle serial read may be reported
		// by Go as io.EOF. It means "no byte yet", not a closed port.
		if errors.Is(err, io.EOF) && n == 0 {
			continue
		}
		if err != nil {
			return nil, err
		}
		if n == 0 {
			continue
		}
		if one[0] == 0 {
			if started {
				return append(packet, 0), nil
			}
			started = true
			packet = append(packet, 0)
			continue
		}
		if started {
			packet = append(packet, one[0])
		}
	}
	return nil, errors.New("timeout")
}
