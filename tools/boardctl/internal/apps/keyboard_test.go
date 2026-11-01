package apps

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/client"
	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/protocol"
)

type keyboardTransport struct {
	reader   bytes.Reader
	requests []protocol.Frame
	data     []byte
	respond  func(protocol.Frame) []byte
}

func (s *keyboardTransport) Read(p []byte) (int, error) { return s.reader.Read(p) }
func (s *keyboardTransport) Write(p []byte) (int, error) {
	f, err := protocol.Decode(p)
	if err != nil {
		return 0, err
	}
	s.requests = append(s.requests, f)
	data := s.data
	if s.respond != nil {
		data = s.respond(f)
	}
	reply, err := protocol.Encode(protocol.Frame{Command: f.Command | 0x80, Sequence: f.Sequence, Data: data})
	if err != nil {
		return 0, err
	}
	s.reader.Reset(append([]byte("program UART text\r\n"), reply...))
	return len(p), nil
}

func TestTerminalQueueFullKeepsKeyAndServicesGuest(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	var accepted []byte
	diskPolled, outputPolled, rejected := false, false, false
	s := &keyboardTransport{respond: func(f protocol.Frame) []byte {
		switch f.Command {
		case protocol.KeyboardEvent:
			if !rejected {
				rejected = true
				return []byte{13}
			}
			if !diskPolled || !outputPolled {
				t.Fatal("retried input before servicing disk and output")
			}
			accepted = append(accepted, f.Data[0])
			return []byte{0, 1}
		case protocol.DiskPoll:
			diskPolled = true
			return make([]byte, 8)
		case protocol.TerminalPoll:
			outputPolled = true
			return []byte{0, 1, 'O', 'K'}
		default:
			t.Fatalf("unexpected command: %x", f.Command)
			return nil
		}
	}}
	inputs := make(chan terminalInput, 3)
	inputs <- terminalInput{char: 'A'}
	inputs <- terminalInput{char: 'B'}
	inputs <- terminalInput{char: 3}
	var out bytes.Buffer
	err := interactiveLoop(ctx, client.New(s, nil), inputs, make(chan struct{}), true, &out, &diskService{image: make([]byte, diskImageSize)})
	if err != nil || string(accepted) != "AB" || !strings.Contains(out.String(), "OK") {
		t.Fatalf("err=%v accepted=%q output=%q", err, accepted, out.String())
	}
}

func TestKeyboardQueueFullCanBeCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	attempts := 0
	s := &keyboardTransport{respond: func(f protocol.Frame) []byte {
		attempts++
		if attempts == 2 {
			cancel()
		}
		return []byte{13}
	}}
	inputs := make(chan terminalInput, 1)
	inputs <- terminalInput{char: 'A'}
	if err := keyboardLoop(ctx, client.New(s, nil), inputs, make(chan struct{})); err != nil || attempts != 2 {
		t.Fatalf("err=%v attempts=%d", err, attempts)
	}
}

func TestKeyboardQueueFullOnlyAppliesToEvents(t *testing.T) {
	for _, command := range []byte{protocol.KeyboardEvent, protocol.KeyboardMode} {
		s := &keyboardTransport{data: []byte{13}}
		_, err := keyboardExchange(client.New(s, nil), command, []byte{1}, 1)
		if err == nil || errors.Is(err, errKeyboardQueueFull) != (command == protocol.KeyboardEvent) {
			t.Fatalf("command=%x err=%v", command, err)
		}
	}
}

func TestKeyboardExchangeNoScreenRequests(t *testing.T) {
	s := &keyboardTransport{data: []byte{0, 0}}
	c := client.New(s, nil)
	if running, err := keyboardExchange(c, protocol.KeyboardMode, []byte{1}, 1); err != nil || running {
		t.Fatalf("attach: %v %v", running, err)
	}
	s.data = []byte{0, 1}
	for _, key := range []terminalInput{{char: 'Q'}, {char: 'U'}, {scan: 0x48}, {char: 13, scan: 0x1c}} {
		if running, err := keyboardExchange(c, protocol.KeyboardEvent, []byte{key.char, key.scan}, 1); err != nil || !running {
			t.Fatalf("event: %v %v", running, err)
		}
	}
	for _, request := range s.requests {
		if request.Command != protocol.KeyboardMode && request.Command != protocol.KeyboardEvent {
			t.Fatalf("unexpected command %x", request.Command)
		}
	}
	for _, data := range [][]byte{{}, {0}, {0, 2}, {13}} {
		s.data = data
		if _, err := keyboardExchange(c, protocol.KeyboardEvent, []byte{'A', 0}, 1); err == nil {
			t.Fatalf("accepted invalid reply %x", data)
		}
	}
}

func TestTerminalKeys(t *testing.T) {
	for _, test := range []struct {
		input string
		want  terminalInput
	}{
		{"\x1b[A", terminalInput{scan: 0x48}},
		{"\x1b[B", terminalInput{scan: 0x50}},
		{"\x1b[C", terminalInput{scan: 0x4d}},
		{"\x1b[D", terminalInput{scan: 0x4b}},
		{"\x1bOA", terminalInput{scan: 0x48}},
		{"\x1b[1;5B", terminalInput{scan: 0x50}},
		{"\x1b[3~", terminalInput{scan: 0x53}},
		{"\x1bOP", terminalInput{scan: 0x3b}},
		{"\r", terminalInput{char: 13, scan: 0x1c}},
		{"\n", terminalInput{char: 13, scan: 0x1c}},
		{"\x7f", terminalInput{char: 8, scan: 0x0e}},
		{"\t", terminalInput{char: 9, scan: 0x0f}},
		{"U", terminalInput{char: 'U'}},
		{"q", terminalInput{char: 'q'}},
		{"\x03", terminalInput{char: 3}},
		{"\x1a", terminalInput{char: 26}},
	} {
		t.Run(test.input, func(t *testing.T) {
			var d terminalDecoder
			var got terminalInput
			for _, b := range []byte(test.input) {
				if input, ready := d.feed(b); ready {
					got = input
				}
			}
			if got != test.want {
				t.Fatalf("key = %+v, want %+v", got, test.want)
			}
		})
	}
	var d terminalDecoder
	d.feed(27)
	if input, ready := d.idle(); !ready || input != (terminalInput{char: 27, scan: 1}) {
		t.Fatal("standalone Escape must produce Esc")
	}
	d.feed(27)
	if input, ready := d.feed('x'); !ready || input.char != 27 || d.pending == nil || d.pending.char != 'x' {
		t.Fatal("Esc followed quickly by a letter must retain both keys")
	}
}

func TestKeyboardCtrlCExitsInEveryInputState(t *testing.T) {
	for _, test := range []struct {
		name  string
		chars []byte
	}{
		{"normal", []byte{3}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := &keyboardTransport{data: []byte{0, 0}}
			inputs := make(chan terminalInput, len(test.chars))
			for _, char := range test.chars {
				inputs <- terminalInput{char: char}
			}
			if err := keyboardLoop(context.Background(), client.New(s, nil), inputs, make(chan struct{})); err != nil {
				t.Fatal(err)
			}
			for _, request := range s.requests {
				if request.Command == protocol.KeyboardEvent {
					t.Fatal("Ctrl-C must not be forwarded to BIOS")
				}
			}
			if _, err := keyboardExchange(client.New(s, nil), protocol.KeyboardMode, []byte{0}, 1); err != nil {
				t.Fatalf("detach: %v", err)
			}
			last := s.requests[len(s.requests)-1]
			if last.Command != protocol.KeyboardMode || !bytes.Equal(last.Data, []byte{0}) {
				t.Fatal("invalid detach request")
			}
		})
	}
}

func TestKeyboardCtrlZIsForwarded(t *testing.T) {
	s := &keyboardTransport{data: []byte{0, 0}}
	inputs := make(chan terminalInput, 2)
	inputs <- terminalInput{char: 26}
	inputs <- terminalInput{char: 3}
	if err := keyboardLoop(context.Background(), client.New(s, nil), inputs, make(chan struct{})); err != nil {
		t.Fatal(err)
	}
	if len(s.requests) != 1 || s.requests[0].Command != protocol.KeyboardEvent ||
		!bytes.Equal(s.requests[0].Data, []byte{26, 0}) {
		t.Fatalf("expected one Ctrl-Z keyboard event, got %+v", s.requests)
	}
}

func TestKeyboardInvalidResponseReportsActualBytes(t *testing.T) {
	for _, test := range []struct {
		data []byte
		want string
	}{
		{[]byte{0}, "data=00 (length 1)"},
		{[]byte{0, 2}, "data=00 02 (length 2)"},
		{[]byte{0, 255}, "data=00 ff (length 2)"},
	} {
		s := &keyboardTransport{data: test.data}
		_, err := keyboardExchange(client.New(s, nil), protocol.KeyboardMode, []byte{1}, 1)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("error = %v, want %q", err, test.want)
		}
	}
}

func TestKeyboardCancellationIsNormalExit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &keyboardTransport{data: []byte{0, 0}}
	if err := keyboardLoop(ctx, client.New(s, nil), make(chan terminalInput), make(chan struct{})); err != nil {
		t.Fatal(err)
	}
	if len(s.requests) != 0 {
		t.Fatal("cancelled session sent a request")
	}
}

func TestTerminalOutput(t *testing.T) {
	for _, data := range [][]byte{{0, 0}, {0, 1, 27, '[', '3', '1', 'm', 'A'}} {
		s := &keyboardTransport{data: data}
		var out bytes.Buffer
		running, err := pollTerminal(context.Background(), client.New(s, nil), &out)
		if err != nil || running != (data[1] == 1) || !bytes.Equal(out.Bytes(), data[2:]) {
			t.Fatalf("output %x: %v %v %q", data, running, err, out.String())
		}
		if len(s.requests) != 1 || s.requests[0].Command != protocol.TerminalPoll {
			t.Fatal(s.requests)
		}
	}
	for _, data := range [][]byte{{}, {0}, {0, 2}, {12}, append([]byte{0, 0}, make([]byte, 65)...)} {
		s := &keyboardTransport{data: data}
		if _, err := pollTerminal(context.Background(), client.New(s, nil), io.Discard); err == nil {
			t.Fatalf("accepted %x", data)
		}
	}
}

func TestTerminalCommand(t *testing.T) {
	cmd, err := prepareCommand([]string{"-port", "test", "terminal"})
	if err != nil || cmd.name != "terminal" || cmd.port != "test" {
		t.Fatalf("%+v %v", cmd, err)
	}
	if _, err := prepareCommand([]string{"terminal", "extra"}); err == nil {
		t.Fatal("accepted arguments")
	}
}
