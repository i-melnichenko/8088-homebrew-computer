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
	serial       io.ReadWriter
	diagnostics  io.Writer
	sequence     byte
	monitorReady bool
	version      byte
	flashing     bool
}

// New creates a client. The caller owns and closes the transport.
// A nil diagnostics writer disables progress messages.
func New(serial io.ReadWriter, diagnostics io.Writer) *Client {
	if diagnostics == nil {
		diagnostics = io.Discard
	}
	return &Client{serial: serial, diagnostics: diagnostics, version: protocol.Version}
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
	if c.flashing && request.Command != protocol.Ping && request.Command != protocol.Reset && request.Command != protocol.BIOSFlasherMode && request.Command != protocol.BIOSRead && request.Command != protocol.BIOSWrite {
		return protocol.Frame{}, errors.New("upgrade transport permits EEPROM operations only")
	}
	packet, err := protocol.EncodeVersion(request, c.version)
	if err != nil {
		return protocol.Frame{}, err
	}
	for attempt := 0; attempt < attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return protocol.Frame{}, err
		}
		if attempt > 0 {
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
			frame, version, err := protocol.DecodeAnyVersion(response)
			if err == nil && version != c.version {
				if !c.flashing && frame.Command == request.Command|0x80 && frame.Sequence == request.Sequence {
					return protocol.Frame{}, protocol.ErrVersion
				}
				if frame.Command == request.Command|0x80 && frame.Sequence == request.Sequence {
					c.version = version
				}
			}
			if err == nil && frame.Command == request.Command|0x80 && frame.Sequence == request.Sequence {
				c.monitorReady = true
				return frame, nil
			}
		}
		// A successful initial probe is silent. Offer Reset only after the
		// first PING really timed out, never on cancellation or an I/O error.
		if attempt == 0 && request.Command == protocol.Ping && !c.monitorReady &&
			ctx.Err() == nil && !time.Now().Before(deadline) {
			fmt.Fprintln(c.diagnostics, "boardctl: waiting for board monitor; press Reset if needed")
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

// NewFlasher negotiates the installed EEPROM monitor independently of normal
// protocol 2.0 operations. Service command IDs 01..06 share the upgrade ABI.
func NewFlasher(serial io.ReadWriter, diagnostics io.Writer) (*Client, error) {
	c := New(serial, nil)
	c.flashing = true
	for _, version := range []byte{protocol.Version, 1} {
		c.version = version
		f, err := c.TransactAttempts(protocol.Frame{Command: protocol.Ping}, 1)
		if err == nil && len(f.Data) == 1 && f.Data[0] == 0 {
			if diagnostics != nil {
				c.diagnostics = diagnostics
			}
			return c, nil
		}
	}
	return nil, errors.New("no EEPROM upgrade monitor answered protocol probes (2, 1); press Reset and retry")
}
