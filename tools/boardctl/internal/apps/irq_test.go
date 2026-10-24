package apps

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/client"
	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/protocol"
)

func TestIRQResultsAndErrors(t *testing.T) {
	for _, test := range []struct {
		data   []byte
		want   string
		output string
	}{
		{[]byte{0, 0, 8, 0, 8}, "", "PASS"},
		{[]byte{0, 1, 0, 0, 8}, "timed out", "TIMEOUT"},
		{[]byte{0, 1, 1, 0, 8}, "timed out", "1 / 8"},
		{[]byte{21}, "PIC 8259A not detected", ""},
		{[]byte{22}, "PIT 8254 not detected", ""},
		{[]byte{23}, "RAM0", ""},
		{[]byte{24}, "CLK/GATE", ""},
		{[]byte{1}, "IRQ_TEST support", ""},
		{[]byte{0}, "invalid", ""},
		{[]byte{0, 0, 0, 0, 8}, "inconsistent", ""},
	} {
		t.Run(test.want+test.output, func(t *testing.T) {
			serial := &ramSerial{respond: func(f protocol.Frame) []byte {
				if f.Command == protocol.Ping {
					return []byte{0}
				}
				if f.Command != protocol.IRQTest {
					t.Fatalf("unexpected command %x", f.Command)
				}
				return test.data
			}}
			var output bytes.Buffer
			err := runIRQTest(client.New(serial, io.Discard), &output)
			if test.want == "" && err != nil || test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
			if test.output != "" && !strings.Contains(output.String(), test.output) {
				t.Fatal(output.String())
			}
			if len(serial.requests) != 2 {
				t.Fatalf("requests = %d", len(serial.requests))
			}
		})
	}
}
