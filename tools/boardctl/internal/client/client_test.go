package client

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/protocol"
)

type cancelledTransport struct {
	cancel context.CancelFunc
	writes int
}

func (s *cancelledTransport) Write(p []byte) (int, error) {
	s.writes++
	return len(p), nil
}

func (s *cancelledTransport) Read(p []byte) (int, error) {
	s.cancel()
	return 0, io.EOF
}

func TestTransactionCancellationWhileWaitingForReply(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &cancelledTransport{cancel: cancel}
	_, err := New(s, nil).TransactAttemptsContext(ctx, protocol.Frame{Command: protocol.KeyboardMode}, 10)
	if !errors.Is(err, context.Canceled) || s.writes != 1 {
		t.Fatalf("error=%v writes=%d; expected cancellation without retry", err, s.writes)
	}
}

type idleThenPacket struct {
	packet []byte
	idle   bool
}

func (r *idleThenPacket) Read(dst []byte) (int, error) {
	if !r.idle {
		r.idle = true
		return 0, io.EOF
	}
	if len(r.packet) == 0 {
		return 0, io.EOF
	}
	dst[0] = r.packet[0]
	r.packet = r.packet[1:]
	return 1, nil
}

func TestReadPacketAfterIdle(t *testing.T) {
	want, err := protocol.Encode(protocol.Frame{Command: protocol.Ping, Sequence: 7})
	if err != nil {
		t.Fatal(err)
	}
	got, err := readPacket(&idleThenPacket{packet: want}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("packet = %x, want %x", got, want)
	}
}

type lostUploadAck struct {
	writes [][]byte
	packet []byte
}

func (s *lostUploadAck) Write(p []byte) (int, error) {
	s.writes = append(s.writes, append([]byte(nil), p...))
	f, err := protocol.Decode(p)
	if err != nil {
		return 0, err
	}
	if len(s.writes) != 1 {
		s.packet, err = protocol.Encode(protocol.Frame{Command: f.Command | 0x80, Sequence: f.Sequence, Data: []byte{0}})
	}
	return len(p), err
}

func (s *lostUploadAck) Read(p []byte) (int, error) {
	if len(s.packet) == 0 {
		return 0, errors.New("simulated lost ACK")
	}
	n := copy(p, s.packet)
	s.packet = s.packet[n:]
	return n, nil
}

func TestUploadDataRetryReusesSequenceAndBytes(t *testing.T) {
	s := &lostUploadAck{}
	c := New(s, nil)
	data := make([]byte, protocol.MaxUploadDataLength)
	for i := range data {
		data[i] = byte(i)
	}
	if _, err := c.TransactAttempts(protocol.Frame{Command: protocol.ProgramUploadData, Data: data}, 2); err != nil {
		t.Fatal(err)
	}
	if len(s.writes) != 2 || string(s.writes[0]) != string(s.writes[1]) {
		t.Fatalf("retry changed packet: % x", s.writes)
	}
	if _, err := c.Transact(protocol.Frame{Command: protocol.ProgramUploadData, Data: []byte{1}}); err != nil {
		t.Fatal(err)
	}
	f, err := protocol.Decode(s.writes[2])
	if err != nil || f.Sequence != 1 || len(f.Data) != 1 || f.Data[0] != 1 {
		t.Fatalf("next block: %+v, %v", f, err)
	}
}

type slowEEPROMReply struct {
	packet  []byte
	delayed bool
	writes  int
}

func (s *slowEEPROMReply) Write(packet []byte) (int, error) {
	s.writes++
	f, err := protocol.Decode(packet)
	if err != nil {
		return 0, err
	}
	s.packet, err = protocol.Encode(protocol.Frame{Command: f.Command | 0x80, Sequence: f.Sequence, Data: []byte{0}})
	return len(packet), err
}

func (s *slowEEPROMReply) Read(dst []byte) (int, error) {
	if !s.delayed {
		s.delayed = true
		time.Sleep(1100 * time.Millisecond)
		return 0, nil
	}
	n := copy(dst, s.packet)
	s.packet = s.packet[n:]
	return n, nil
}

func TestEEPROMTimeoutAllowsProgrammingWithoutRetry(t *testing.T) {
	s := &slowEEPROMReply{}
	_, err := New(s, nil).TransactTimeout(protocol.Frame{Command: protocol.BIOSWrite, Data: []byte{0, 0, 0, 0, 1}}, 2*time.Second)
	if err != nil || s.writes != 1 {
		t.Fatalf("err=%v writes=%d", err, s.writes)
	}
}

func TestInvalidEEPROMTimeoutDoesNotSend(t *testing.T) {
	s := &slowEEPROMReply{}
	if _, err := New(s, nil).TransactTimeout(protocol.Frame{Command: protocol.BIOSWrite}, 0); err == nil || s.writes != 0 {
		t.Fatalf("err=%v writes=%d", err, s.writes)
	}
}
