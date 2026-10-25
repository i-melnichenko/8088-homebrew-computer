package apps

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/client"
	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/protocol"
)

type ramSerial struct {
	requests  []protocol.Frame
	packet    []byte
	respond   func(protocol.Frame) []byte
	dropStart bool
}

func (s *ramSerial) Read(p []byte) (int, error) {
	if len(s.packet) == 0 {
		return 0, io.EOF
	}
	n := copy(p, s.packet)
	s.packet = s.packet[n:]
	return n, nil
}
func (s *ramSerial) Write(p []byte) (int, error) {
	f, err := protocol.Decode(p)
	if err != nil {
		return 0, err
	}
	s.requests = append(s.requests, f)
	if s.dropStart && f.Command == protocol.RAMTest {
		s.dropStart = false
		return len(p), nil
	}
	s.packet, err = protocol.Encode(protocol.Frame{Command: f.Command | 0x80, Sequence: f.Sequence, Data: s.respond(f)})
	return len(p), err
}

func ramMapReply() []byte {
	b := make([]byte, 11)
	b[1], b[2], b[3] = 3, 0, protocol.RAMFlagHeartbeat
	binary.LittleEndian.PutUint32(b[4:], 256*1024)
	binary.LittleEndian.PutUint16(b[8:], 256)
	return b
}
func ramStatusReply(state byte) []byte {
	b := make([]byte, 31)
	b[1], b[2], b[3] = state, 2, 1
	binary.LittleEndian.PutUint32(b[17:], 655360)
	if state != 0 {
		b[4] = 2
		binary.LittleEndian.PutUint32(b[5:], 0x20010)
		binary.LittleEndian.PutUint32(b[9:], 131072)
		binary.LittleEndian.PutUint32(b[27:], 20480)
	}
	if state == 2 {
		b[4] = 10
		binary.LittleEndian.PutUint32(b[5:], 0x3fff0)
		binary.LittleEndian.PutUint32(b[9:], 655360)
		binary.LittleEndian.PutUint32(b[27:], 81920)
	}
	return b
}

// Exercise internal wire operations independently of the single CLI workflow.
func exchangeAndPrintRAMTest(c *client.Client, action byte, out io.Writer) error {
	status, err := exchangeRAMTest(context.Background(), c, action, 10)
	if err != nil {
		return err
	}
	return printRAMTestStatus(status, out)
}

func TestRAMCLIConfirmationBeforeOpeningPort(t *testing.T) {
	for _, args := range [][]string{{"ram", "extra"}, {"ram-test", "start"}, {"ram-test", "start", "--yes"},
		{"ram-test", "status"}, {"ram-test", "cancel"}, {"ram-test", "--yes"}, {"ram-test", "invalid"}} {
		err := runWithSerial(args, func(string) (io.ReadWriteCloser, error) {
			t.Fatal("opened UART with invalid arguments")
			return nil, nil
		})
		if err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	for _, args := range [][]string{{"ram"}, {"ram-test"}} {
		if _, err := prepareCommand(args); err != nil {
			t.Fatalf("valid %v: %v", args, err)
		}
	}
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	_, _ = writer.Write([]byte("n\n"))
	writer.Close()
	previous := os.Stdin
	os.Stdin = input
	defer func() { os.Stdin = previous }()
	if err := runWithSerial([]string{"ram-test"}, func(string) (io.ReadWriteCloser, error) {
		t.Fatal("opened UART after confirmation was declined")
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}

}

func TestRAMMapOutputAndReservations(t *testing.T) {
	s := &ramSerial{respond: func(f protocol.Frame) []byte {
		if f.Command != protocol.RAMInfo || len(f.Data) != 0 {
			t.Fatalf("request %+v", f)
		}
		return ramMapReply()
	}}
	var out bytes.Buffer
	if err := showRAM(client.New(s, nil), &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"RAM0  00000-1FFFF", "RAM1  20000-3FFFF", "RAM4  80000-9FFFF", "FAIL/ABSENT", "BIOS", "Detected RAM: 256 KiB", "Conventional RAM: 256 KiB", "RAM test: IDLE"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in %s", want, &out)
		}
	}
}

func TestRAMTestStartRetryAndExplicitCancel(t *testing.T) {
	s := &ramSerial{dropStart: true, respond: func(f protocol.Frame) []byte {
		if f.Command == protocol.Ping {
			return []byte{0}
		}
		if !bytes.Equal(f.Data, []byte{1, protocol.RAMTestConfirm}) {
			t.Fatalf("START lacks confirmation: %x", f.Data)
		}
		return ramStatusReply(1)
	}}
	var out bytes.Buffer
	if err := exchangeAndPrintRAMTest(client.New(s, nil), 1, &out); err != nil {
		t.Fatal(err)
	}
	if len(s.requests) != 3 || s.requests[0].Command != protocol.Ping ||
		s.requests[1].Sequence != s.requests[2].Sequence || !bytes.Equal(s.requests[1].Data, s.requests[2].Data) {
		t.Fatalf("retry changed START: %+v", s.requests)
	}
	if !strings.Contains(out.String(), "RUNNING 25.0%") || !strings.Contains(out.String(), "RAM1 20010-2001F") {
		t.Fatal(out.String())
	}
	s.respond = func(f protocol.Frame) []byte {
		if f.Command != protocol.RAMTest || !bytes.Equal(f.Data, []byte{2}) {
			t.Fatalf("cancel: %+v", f)
		}
		return ramStatusReply(3)
	}
	out.Reset()
	if err := exchangeAndPrintRAMTest(client.New(s, nil), 2, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "CANCELLED 25.0%") {
		t.Fatal(out.String())
	}
}

func TestRAMTestResultsAndFirstError(t *testing.T) {
	for _, state := range []byte{0, 1, 2, 3} {
		b := ramStatusReply(state)
		s := &ramSerial{respond: func(f protocol.Frame) []byte {
			if !bytes.Equal(f.Data, []byte{0}) {
				t.Fatalf("status mutates RAM: %x", f.Data)
			}
			return b
		}}
		var out bytes.Buffer
		if err := exchangeAndPrintRAMTest(client.New(s, nil), 0, &out); err != nil {
			t.Fatal(err)
		}
		if state == 2 && !strings.Contains(out.String(), "PASSED 100.0%") {
			t.Fatal(out.String())
		}
	}
	b := ramStatusReply(2)
	binary.LittleEndian.PutUint32(b[9:], 655359)
	binary.LittleEndian.PutUint32(b[13:], 1)
	binary.LittleEndian.PutUint32(b[21:], 0x21234)
	b[25], b[26] = 0xaa, 0x55
	s := &ramSerial{respond: func(protocol.Frame) []byte { return b }}
	var out bytes.Buffer
	if err := exchangeAndPrintRAMTest(client.New(s, nil), 0, &out); err == nil {
		t.Fatal("completed RAM failure must fail the command")
	}
	if !strings.Contains(out.String(), "FAILED 100.0%") || !strings.Contains(out.String(), "RAM1 21234 expected=AA actual=55") {
		t.Fatal(out.String())
	}
}

func TestRAMRejectsMalformedRepliesAndOldFirmware(t *testing.T) {
	for _, mutate := range []func([]byte) []byte{
		func(b []byte) []byte { return b[:10] }, func(b []byte) []byte { b[1] = 0; return b },
		func(b []byte) []byte { b[2] = 4; return b }, func(b []byte) []byte { b[4] ^= 1; return b },
		func(b []byte) []byte { b[8] ^= 1; return b }, func(b []byte) []byte { b[10] = 5; return b },
		func(b []byte) []byte { b[3] = 1; return b }, func(b []byte) []byte { b[9] = 0xff; return b },
	} {
		if _, err := decodeRAMMap(mutate(ramMapReply())); err == nil {
			t.Fatal("accepted malformed map")
		}
	}
	for _, mutate := range []func([]byte) []byte{
		func(b []byte) []byte { return b[:30] }, func(b []byte) []byte { b[1] = 4; return b },
		func(b []byte) []byte { b[2] = 0; return b }, func(b []byte) []byte { b[4] = 10; return b },
		func(b []byte) []byte { b[17] ^= 1; return b }, func(b []byte) []byte { b[27] = 0xff; b[29] = 0xff; return b },
		func(b []byte) []byte { b[5] = 0xff; return b },
		func(b []byte) []byte { b[1] = 2; b[4] = 10; return b },
		func(b []byte) []byte { b[1] = 0; return b },
	} {
		if _, err := decodeRAMTestStatus(mutate(ramStatusReply(1))); err == nil {
			t.Fatal("accepted malformed status")
		}
	}
	s := &ramSerial{respond: func(protocol.Frame) []byte { return []byte{1} }}
	if err := showRAM(client.New(s, nil), io.Discard); err == nil {
		t.Fatal("old firmware accepted")
	}
	if err := exchangeAndPrintRAMTest(client.New(s, nil), 0, io.Discard); err == nil {
		t.Fatal("old firmware accepted")
	}
}

func TestRAMTestConfirmation(t *testing.T) {
	for _, answer := range []string{"", "\n", "n\n", "no\n", "anything\n", "y\n", "yes\n", " YES \n"} {
		var out bytes.Buffer
		yes, err := confirmRAMTest(strings.NewReader(answer), &out)
		if err != nil {
			t.Fatal(err)
		}
		expected := strings.EqualFold(strings.TrimSpace(answer), "y") || strings.EqualFold(strings.TrimSpace(answer), "yes")
		if yes != expected || !strings.Contains(out.String(), "erases RAM contents") {
			t.Fatalf("%q: %v %s", answer, yes, &out)
		}
	}
}

func TestRAMTestSingleCommandWatchesToCompletion(t *testing.T) {
	polls := 0
	started := false
	s := &ramSerial{respond: func(f protocol.Frame) []byte {
		if f.Command == protocol.RAMInfo {
			return ramMapReply()
		}
		if f.Command == protocol.Ping {
			return []byte{0}
		}
		switch f.Data[0] {
		case 1:
			started = true
			return ramStatusReply(1)
		case 0:
			if !started {
				return ramStatusReply(0)
			}
			polls++
			if polls == 3 {
				return ramStatusReply(2)
			}
			return ramStatusReply(1)
		default:
			t.Fatal("unexpected cancellation")
			return nil
		}
	}}
	var out bytes.Buffer
	if err := watchRAMTest(context.Background(), client.New(s, nil), &out, 0); err != nil {
		t.Fatal(err)
	}
	if polls != 3 || !strings.Contains(out.String(), "RUNNING 25.0%") || !strings.Contains(out.String(), "PASSED 100.0%") {
		t.Fatal(out.String())
	}
}

type ramCancelWriter struct {
	bytes.Buffer
	cancel context.CancelFunc
}

func (w *ramCancelWriter) Write(b []byte) (int, error) {
	if w.cancel != nil && bytes.Contains(b, []byte("\rRAM TEST: RUNNING")) {
		w.cancel()
		w.cancel = nil
	}
	return w.Buffer.Write(b)
}

func TestRAMTestInterruptSendsCancelWithFreshContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &ramSerial{respond: func(f protocol.Frame) []byte {
		if f.Command == protocol.RAMInfo {
			return ramMapReply()
		}
		if f.Command == protocol.Ping {
			return []byte{0}
		}
		switch f.Data[0] {
		case 0:
			return ramStatusReply(0)
		case 1:
			return ramStatusReply(1)
		case 2:
			return ramStatusReply(3)
		}
		return nil
	}}
	out := &ramCancelWriter{cancel: cancel}
	if err := watchRAMTest(ctx, client.New(s, nil), out, time.Hour); err != nil {
		t.Fatal(err)
	}
	last := s.requests[len(s.requests)-1]
	if !bytes.Equal(last.Data, []byte{2}) || !strings.Contains(out.String(), "CANCELLED 25.0%") {
		t.Fatalf("%+v %s", s.requests, out.String())
	}
}

func TestRAMTestLostStartReplyStillAttemptsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &ramSerial{respond: func(f protocol.Frame) []byte {
		if f.Command == protocol.RAMInfo {
			return ramMapReply()
		}
		if f.Command == protocol.Ping {
			return []byte{0}
		}
		switch f.Data[0] {
		case 0:
			return ramStatusReply(0)
		case 1:
			cancel()
			return ramStatusReply(1)
		case 2:
			return ramStatusReply(3)
		}
		return nil
	}}
	var out bytes.Buffer
	if err := watchRAMTest(ctx, client.New(s, nil), &out, 0); err != nil {
		t.Fatal(err)
	}
	if last := s.requests[len(s.requests)-1]; !bytes.Equal(last.Data, []byte{2}) {
		t.Fatal(s.requests)
	}
}

func TestRAMTestReattachesWithoutRestarting(t *testing.T) {
	polls := 0
	s := &ramSerial{respond: func(f protocol.Frame) []byte {
		if f.Command == protocol.RAMInfo {
			return ramMapReply()
		}
		if f.Command != protocol.RAMTest || !bytes.Equal(f.Data, []byte{0}) {
			t.Fatal("reattach restarted test")
		}
		polls++
		if polls == 1 {
			return ramStatusReply(1)
		}
		return ramStatusReply(2)
	}}
	var out bytes.Buffer
	if err := watchRAMTest(context.Background(), client.New(s, nil), &out, 0); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Following") || !strings.Contains(out.String(), "PASSED") {
		t.Fatal(out.String())
	}
}

func TestRAMTestLostConnectionReportsWatchdogFallback(t *testing.T) {
	running := false
	s := &ramSerial{respond: func(f protocol.Frame) []byte {
		if f.Command == protocol.RAMInfo {
			return ramMapReply()
		}
		if f.Command == protocol.Ping {
			return []byte{0}
		}
		if f.Data[0] == 1 {
			running = true
			return ramStatusReply(1)
		}
		if !running {
			return ramStatusReply(0)
		}
		return nil // no valid status/cancel acknowledgement after the link fails
	}}
	var out bytes.Buffer
	err := watchRAMTest(context.Background(), client.New(s, nil), &out, 0)
	if err == nil || !strings.Contains(err.Error(), "heartbeat budget expires") {
		t.Fatalf("lost link: %v", err)
	}
	if !bytes.Equal(s.requests[len(s.requests)-1].Data, []byte{2}) {
		t.Fatal("did not attempt cancellation")
	}
}

func TestRAMTestRequiresFirmwareWatchdogBeforeStarting(t *testing.T) {
	s := &ramSerial{respond: func(f protocol.Frame) []byte {
		if f.Command != protocol.RAMInfo {
			t.Fatal("started a test without a firmware watchdog")
		}
		b := ramMapReply()
		b[3] = 0
		return b
	}}
	err := watchRAMTest(context.Background(), client.New(s, nil), io.Discard, 0)
	if err == nil || !strings.Contains(err.Error(), "heartbeat watchdog") || len(s.requests) != 1 {
		t.Fatalf("%v %+v", err, s.requests)
	}
}
