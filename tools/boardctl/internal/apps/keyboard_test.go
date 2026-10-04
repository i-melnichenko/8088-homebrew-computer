package apps

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/client"
	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/protocol"
)

type keyboardTransport struct {
	reader   bytes.Reader
	requests []protocol.Frame
	data     []byte
}

func (s *keyboardTransport) Read(p []byte) (int, error) { return s.reader.Read(p) }
func (s *keyboardTransport) Write(p []byte) (int, error) {
	f, err := protocol.Decode(p)
	if err != nil {
		return 0, err
	}
	s.requests = append(s.requests, f)
	reply, err := protocol.Encode(protocol.Frame{Command: f.Command | 0x80, Sequence: f.Sequence, Data: s.data})
	if err != nil {
		return 0, err
	}
	s.reader.Reset(append([]byte("program UART text\r\n"), reply...))
	return len(p), nil
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
		{"prefix", []byte{29, 3}},
		{"upload prompt", []byte{29, 'u', 3}},
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

func TestUploadBounds(t *testing.T) {
	for _, test := range []struct {
		name string
		size int
		ok   bool
	}{
		{"hello", 1, true}, {"penguins", maxPayloadSize, true},
		{"abcdefghijklmnop", 1, false}, {"test", maxPayloadSize + 1, false},
		{"test", 0, false}, {"", 1, false}, {"   ", 2, false},
		{"тест", 1, false}, {"a\nb", 1, false},
	} {
		if err := validateUpload(test.name, test.size); (err == nil) != test.ok {
			t.Errorf("%q + %d: %v (valid=%v)", test.name, test.size, err, test.ok)
		}
	}
}
