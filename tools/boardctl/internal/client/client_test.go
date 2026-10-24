package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
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
	data := make([]byte, protocol.MaxPayloadLength)
	for i := range data {
		data[i] = byte(i)
	}
	if _, err := c.TransactAttempts(protocol.Frame{Command: protocol.DiskData, Data: data}, 2); err != nil {
		t.Fatal(err)
	}
	if len(s.writes) != 2 || string(s.writes[0]) != string(s.writes[1]) {
		t.Fatalf("retry changed packet: % x", s.writes)
	}
	if _, err := c.Transact(protocol.Frame{Command: protocol.DiskData, Data: []byte{1}}); err != nil {
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

type pingDiagnosticsTransport struct {
	diagnostics *bytes.Buffer
	packet      []byte
	writes      [][]byte
	delayFirst  bool
	failRead    bool
}

func (s *pingDiagnosticsTransport) Write(packet []byte) (int, error) {
	if len(s.writes) == 0 && s.diagnostics.Len() != 0 {
		return 0, errors.New("diagnostic printed before first request")
	}
	s.writes = append(s.writes, append([]byte(nil), packet...))
	frame, err := protocol.Decode(packet)
	if err != nil {
		return 0, err
	}
	s.packet, err = protocol.Encode(protocol.Frame{Command: frame.Command | 0x80, Sequence: frame.Sequence, Data: []byte{0}})
	return len(packet), err
}
func (s *pingDiagnosticsTransport) Read(dst []byte) (int, error) {
	if s.failRead {
		return 0, errors.New("transport read failed")
	}
	if s.delayFirst {
		s.delayFirst = false
		time.Sleep(15 * time.Millisecond)
		return 0, nil
	}
	n := copy(dst, s.packet)
	s.packet = s.packet[n:]
	return n, nil
}

func TestInitialPingSuccessHasNoDiagnostics(t *testing.T) {
	var diagnostics bytes.Buffer
	s := &pingDiagnosticsTransport{diagnostics: &diagnostics}
	_, err := New(s, &diagnostics).transact(context.Background(), protocol.Frame{Command: protocol.Ping}, 2, time.Second)
	if err != nil || diagnostics.Len() != 0 || len(s.writes) != 1 {
		t.Fatalf("err=%v writes=%d diagnostics=%q", err, len(s.writes), diagnostics.String())
	}
}

func TestInitialPingResetHintOnlyAfterTimeout(t *testing.T) {
	for _, attempts := range []int{1, 2} {
		var diagnostics bytes.Buffer
		s := &pingDiagnosticsTransport{diagnostics: &diagnostics, delayFirst: true}
		_, err := New(s, &diagnostics).transact(context.Background(), protocol.Frame{Command: protocol.Ping}, attempts, 10*time.Millisecond)
		if (err != nil) != (attempts == 1) {
			t.Fatalf("attempts=%d err=%v", attempts, err)
		}
		if strings.Count(diagnostics.String(), "press Reset if needed") != 1 {
			t.Fatalf("diagnostics=%q", diagnostics.String())
		}
		if attempts == 2 && !bytes.Equal(s.writes[0], s.writes[1]) {
			t.Fatal("retry changed packet")
		}
	}
}

func TestInitialPingIOErrorHasNoResetHint(t *testing.T) {
	var diagnostics bytes.Buffer
	s := &pingDiagnosticsTransport{diagnostics: &diagnostics, failRead: true}
	_, err := New(s, &diagnostics).transact(context.Background(), protocol.Frame{Command: protocol.Ping}, 1, time.Second)
	if err == nil || diagnostics.Len() != 0 {
		t.Fatalf("err=%v diagnostics=%q", err, diagnostics.String())
	}
}

type upgradeTransport struct {
	installed  byte
	forceReply byte
	requests   []protocol.Frame
	versions   []byte
	reader     bytes.Reader
}

func (s *upgradeTransport) Read(p []byte) (int, error) { return s.reader.Read(p) }
func (s *upgradeTransport) Write(p []byte) (int, error) {
	f, version, err := protocol.DecodeAnyVersion(p)
	if err != nil {
		return 0, err
	}
	s.requests = append(s.requests, f)
	s.versions = append(s.versions, version)
	if version != s.installed && s.forceReply == 0 {
		return len(p), nil
	}
	replyVersion := s.installed
	if s.forceReply != 0 {
		replyVersion = s.forceReply
	}
	packet, err := protocol.EncodeVersion(protocol.Frame{Command: f.Command | 0x80, Sequence: f.Sequence, Data: []byte{0}}, replyVersion)
	s.reader.Reset(packet)
	return len(p), err
}
func TestEEPROMUpgradeNegotiatesIndependentlyOfNormalProtocol(t *testing.T) {
	for _, version := range []byte{1, 2} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			peer := &upgradeTransport{installed: version}
			c, err := NewFlasher(peer, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, command := range []byte{protocol.BIOSFlasherMode, protocol.BIOSWrite, protocol.BIOSRead, protocol.Reset} {
				if _, err := c.Transact(protocol.Frame{Command: command}); err != nil {
					t.Fatal(err)
				}
				if peer.versions[len(peer.versions)-1] != version {
					t.Fatal("wrong EEPROM wire version")
				}
			}
			before := len(peer.requests)
			if _, err := c.Transact(protocol.Frame{Command: protocol.DiskMount}); err == nil || len(peer.requests) != before {
				t.Fatal("upgrade transport accepted a normal command")
			}
		})
	}
}
func TestNormalClientRejectsOtherVersions(t *testing.T) {
	peer := &upgradeTransport{installed: 1, forceReply: 1}
	if _, err := New(peer, nil).Transact(protocol.Frame{Command: protocol.Ping}); !errors.Is(err, protocol.ErrVersion) {
		t.Fatalf("expected protocol 2.0 requirement, got %v", err)
	}
	if len(peer.requests) != 1 || peer.versions[0] != 2 {
		t.Fatal("normal client fell back")
	}
}
