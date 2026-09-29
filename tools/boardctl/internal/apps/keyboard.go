package apps

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/client"
	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/protocol"
)

type terminalInput struct {
	char byte // ASCII, zero for an extended key
	scan byte // PC-style scan code; BIOS translates ASCII when zero
}

// Decode terminal sequences on the PC. The BIOS sees AX-style keys, not ANSI.
type terminalDecoder struct {
	escape  byte
	params  string
	pending *terminalInput
}

func (d *terminalDecoder) feed(b byte) (terminalInput, bool) {
	if d.escape == 1 {
		if b == '[' || b == 'O' {
			d.escape, d.params = 2, ""
			return terminalInput{}, false
		}
		d.escape = 0
		if next, ready := d.feed(b); ready {
			d.pending = &next
		}
		return terminalInput{char: 27, scan: 1}, true
	}
	if d.escape == 2 {
		if b < 0x40 || b > 0x7e {
			if len(d.params) < 16 {
				d.params += string(b)
			}
			return terminalInput{}, false
		}
		d.escape = 0
		var scan byte
		switch b {
		case 'A':
			scan = 0x48
		case 'B':
			scan = 0x50
		case 'C':
			scan = 0x4d
		case 'D':
			scan = 0x4b
		case 'H':
			scan = 0x47
		case 'F':
			scan = 0x4f
		case 'P':
			scan = 0x3b
		case 'Q':
			scan = 0x3c
		case 'R':
			scan = 0x3d
		case 'S':
			scan = 0x3e
		case '~':
			switch strings.Split(d.params, ";")[0] {
			case "1", "7":
				scan = 0x47
			case "2":
				scan = 0x52
			case "3":
				scan = 0x53
			case "4", "8":
				scan = 0x4f
			case "5":
				scan = 0x49
			case "6":
				scan = 0x51
			case "11":
				scan = 0x3b
			case "12":
				scan = 0x3c
			case "13":
				scan = 0x3d
			case "14":
				scan = 0x3e
			case "15":
				scan = 0x3f
			case "17":
				scan = 0x40
			case "18":
				scan = 0x41
			case "19":
				scan = 0x42
			case "20":
				scan = 0x43
			case "21":
				scan = 0x44
			}
		}
		return terminalInput{scan: scan}, scan != 0
	}
	switch b {
	case 27:
		d.escape = 1
		return terminalInput{}, false
	case '\r', '\n':
		return terminalInput{char: 13, scan: 0x1c}, true
	case 8, 127:
		return terminalInput{char: 8, scan: 0x0e}, true
	case 9:
		return terminalInput{char: 9, scan: 0x0f}, true
	default:
		return terminalInput{char: b}, true
	}
}

func (d *terminalDecoder) idle() (terminalInput, bool) {
	back := d.escape == 1
	d.escape, d.params = 0, ""
	return terminalInput{char: 27, scan: 1}, back
}

func ttyCommand(args ...string) *exec.Cmd {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = os.Stdin
	return cmd
}

func keyboardExchange(c *client.Client, command byte, data []byte, attempts int) (bool, error) {
	return keyboardExchangeContext(context.Background(), c, command, data, attempts)
}

func keyboardExchangeContext(ctx context.Context, c *client.Client, command byte, data []byte, attempts int) (bool, error) {
	f, err := c.TransactAttemptsContext(ctx, protocol.Frame{Command: command, Data: data}, attempts)
	if err := checkResponse(f, err); err != nil {
		return false, err
	}
	if len(f.Data) != 2 || f.Data[1]&^protocol.KeyboardFlagRunning != 0 {
		return false, fmt.Errorf("invalid keyboard response to command %#02x: data=% x (length %d); expected status=00 and flags=00 or 01", command, f.Data, len(f.Data))
	}
	return f.Data[1]&protocol.KeyboardFlagRunning != 0, nil
}

func keyboard(c *client.Client) error {
	// Install signal handling before the first UART exchange, not after attach.
	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stopSignals()
	ctx, cancel := context.WithCancel(signalCtx)
	defer cancel()
	// An interrupted/invalid attach ACK can still mean BIOS accepted MODE=1.
	// Try detaching even if attach fails; use a fresh context for cleanup.
	defer func() {
		if _, err := keyboardExchange(c, protocol.KeyboardMode, []byte{0}, 1); err != nil {
			fmt.Fprintln(os.Stderr, "boardctl: detach not acknowledged; Reset restores raw UART mode")
		}
	}()
	fmt.Fprintln(os.Stderr, "boardctl: connecting keyboard; wait for 'Keyboard connected' before typing; Ctrl-C cancels (press Reset if needed)")
	if _, err := keyboardExchangeContext(ctx, c, protocol.KeyboardMode, []byte{1}, 10); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("attach keyboard (press Reset if needed): %w", err)
	}
	settings, err := ttyCommand("-g").Output()
	if err != nil {
		return errors.New("keyboard requires an interactive terminal")
	}
	if err := ttyCommand("raw", "-echo", "min", "0", "time", "1").Run(); err != nil {
		return err
	}
	defer func() { _ = ttyCommand(strings.TrimSpace(string(settings))).Run() }()
	stop, done := make(chan struct{}), make(chan struct{})
	inputs := make(chan terminalInput, 64)
	go func() {
		defer close(done)
		var decoder terminalDecoder
		buf := make([]byte, 1)
		for {
			select {
			case <-stop:
				return
			default:
			}
			n, err := os.Stdin.Read(buf)
			if n == 0 && err != nil && !errors.Is(err, io.EOF) {
				return
			}
			var input terminalInput
			var ready bool
			if n > 0 {
				if buf[0] == 3 {
					// Raw mode emits a byte, not SIGINT. Cancel in the reader so
					// Ctrl-C also interrupts an outstanding UART exchange.
					cancel()
					return
				}
				input, ready = decoder.feed(buf[0])
			} else {
				input, ready = decoder.idle()
			}
			for ready {
				select {
				case inputs <- input:
				case <-stop:
					return
				}
				ready = decoder.pending != nil
				if ready {
					input = *decoder.pending
					decoder.pending = nil
				}
			}
		}
	}()
	defer func() { close(stop); <-done }()
	fmt.Fprint(os.Stderr, "Keyboard connected; use the board LCD. Ctrl-C: quit. Ctrl-] then u: upload, ]: send Ctrl-].\r\n")
	return keyboardLoop(ctx, c, inputs, done)
}

func keyboardLoop(ctx context.Context, c *client.Client, inputs <-chan terminalInput, done <-chan struct{}) error {
	var prefix, prompt bool
	var path string
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-done:
			if ctx.Err() != nil {
				return nil
			}
			return errors.New("terminal input stopped")
		case input := <-inputs:
			// Raw mode disables terminal signals: handle Ctrl-C as a local exit,
			// never a BIOS key, in every input state. Real SIGINT is handled above.
			if input.char == 3 {
				fmt.Fprint(os.Stderr, "\r\nDisconnecting keyboard.\r\n")
				return nil
			}
			if prompt {
				switch input.char {
				case 27:
					prompt, path = false, ""
					fmt.Fprint(os.Stderr, "\r\nUpload cancelled.\r\n")
				case 13:
					fmt.Fprint(os.Stderr, "\r\n")
					data, err := os.ReadFile(path)
					if err == nil {
						_, err = uploadData(c, defaultProgramName(path), data, func(done, total int) {
							fmt.Fprintf(os.Stderr, "\rUploading %d/%d bytes", done, total)
							if done == total {
								fmt.Fprint(os.Stderr, "\r\nVerifying RAM SHA-256...\r\n")
							}
						})
					}
					if err != nil {
						fmt.Fprintf(os.Stderr, "\r\nUpload failed: %v\r\n", err)
					} else {
						fmt.Fprint(os.Stderr, "\r\nUploaded and SHA-256 verified; select the program on the LCD.\r\n")
					}
					prompt, path = false, ""
				case 8:
					if len(path) > 0 {
						_, size := utf8.DecodeLastRuneInString(path)
						path = path[:len(path)-size]
						fmt.Fprint(os.Stderr, "\b \b")
					}
				default:
					if input.char >= ' ' {
						text := string([]byte{input.char})
						path += text
						fmt.Fprint(os.Stderr, text)
					}
				}
				continue
			}
			if prefix {
				prefix = false
				switch input.char {
				case 'q', 'Q':
					return nil
				case 'u', 'U':
					running, err := keyboardExchangeContext(ctx, c, protocol.KeyboardMode, []byte{1}, 1)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Board not responding (program may not poll INT 16h): %v\r\n", err)
						continue
					}
					if running {
						fmt.Fprint(os.Stderr, "Return to the BIOS menu before uploading.\r\n")
						continue
					}
					prompt, path = true, ""
					fmt.Fprint(os.Stderr, "Binary path (Enter: upload, Esc: cancel): ")
					continue
				case ']':
					input = terminalInput{char: 29}
				default:
					fmt.Fprint(os.Stderr, "Ctrl-C: quit. Ctrl-] commands: u upload, ] send Ctrl-].\r\n")
					continue
				}
			} else if input.char == 29 {
				prefix = true
				continue
			}
			if input.char >= 128 || input == (terminalInput{}) {
				continue
			}
			if _, err := keyboardExchangeContext(ctx, c, protocol.KeyboardEvent, []byte{input.char, input.scan}, 3); err != nil {
				fmt.Fprintf(os.Stderr, "Key not acknowledged (program must poll INT 16h): %v\r\n", err)
			}
		}
	}
}
