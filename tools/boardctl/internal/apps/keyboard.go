package apps

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

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

var errKeyboardQueueFull = errors.New("BIOS keyboard queue full")

func keyboardExchangeContext(ctx context.Context, c *client.Client, command byte, data []byte, attempts int) (bool, error) {
	f, err := c.TransactAttemptsContext(ctx, protocol.Frame{Command: command, Data: data}, attempts)
	if err == nil && command == protocol.KeyboardEvent && len(f.Data) == 1 && f.Data[0] == 13 {
		return false, errKeyboardQueueFull
	}
	if err := checkResponse(f, err); err != nil {
		return false, err
	}
	if len(f.Data) != 2 || f.Data[1]&^protocol.KeyboardFlagRunning != 0 {
		return false, fmt.Errorf("invalid keyboard response to command %#02x: data=% x (length %d); expected status=00 and flags=00 or 01", command, f.Data, len(f.Data))
	}
	return f.Data[1]&protocol.KeyboardFlagRunning != 0, nil
}

func keyboard(c *client.Client, startup terminalOptions) error {
	return interactive(c, false, startup)
}

func terminal(c *client.Client, startup terminalOptions) error {
	return interactive(c, true, startup)
}

func interactive(c *client.Client, terminalMode bool, startup terminalOptions) error {
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
	if terminalMode {
		fmt.Fprintln(os.Stderr, "boardctl: connecting terminal; Ctrl-C cancels (press Reset if needed)")
	} else {
		fmt.Fprintln(os.Stderr, "boardctl: connecting keyboard; wait for 'Keyboard connected' before typing; Ctrl-C cancels (press Reset if needed)")
	}
	mode := byte(1)
	if terminalMode {
		mode = 2
	}
	running, err := keyboardExchangeContext(ctx, c, protocol.KeyboardMode, []byte{mode}, 10)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		if terminalMode {
			return fmt.Errorf("attach terminal (requires BIOS with terminal support; press Reset if needed): %w", err)
		}
		return fmt.Errorf("attach keyboard (press Reset if needed): %w", err)
	}
	if startup.bootDisk && running {
		return errors.New("an operating system is already running; reboot before booting another image")
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
	if startup.diskImage != nil && !running {
		var mount []byte
		if !startup.diskReadonly {
			mount = []byte{1}
		}
		if err := checkStatusReply(c.TransactAttemptsContext(ctx, protocol.Frame{Command: protocol.DiskMount, Data: mount}, 3)); err != nil {
			return fmt.Errorf("mount disk: %w", err)
		}
		if startup.bootDisk {
			if err := checkStatusReply(c.TransactAttemptsContext(ctx, protocol.Frame{Command: protocol.DiskBoot}, 3)); err != nil {
				return fmt.Errorf("boot disk: %w", err)
			}
			if !startup.diskReadonly {
				fmt.Fprint(os.Stderr, "Booting writable floppy A:.\r\n")
			} else {
				fmt.Fprint(os.Stderr, "Booting read-only floppy A:.\r\n")
			}
		}
	}
	if startup.diskImage != nil {
		if startup.diskReadonly {
			fmt.Fprint(os.Stderr, "Floppy A: read-only.\r\n")
		} else {
			fmt.Fprint(os.Stderr, "Floppy A: read/write (changes saved to image).\r\n")
		}
	}
	if terminalMode {
		fmt.Fprint(os.Stderr, "Terminal connected. Ctrl-C: quit.\r\n")
		defer fmt.Fprint(os.Stdout, "\x1b[0m\x1b[?25h\r\n")
		disk := &diskService{image: startup.diskImage}
		if startup.diskFile != nil {
			disk.writer = startup.diskFile
		}
		return interactiveLoop(ctx, c, inputs, done, true, os.Stdout, disk)
	}
	fmt.Fprint(os.Stderr, "Keyboard connected; use the board LCD. Ctrl-C: quit.\r\n")
	return keyboardLoop(ctx, c, inputs, done)
}

func keyboardLoop(ctx context.Context, c *client.Client, inputs <-chan terminalInput, done <-chan struct{}) error {
	return interactiveLoop(ctx, c, inputs, done, false, io.Discard, nil)
}

func interactiveLoop(ctx context.Context, c *client.Client, inputs <-chan terminalInput, done <-chan struct{}, terminalMode bool, out io.Writer, disk *diskService) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	var pending *terminalInput
	for {
		// Keep the rejected key ahead of later input. BIOS status 13 means
		// nothing was accepted; retry only after allowing the guest to progress.
		nextInput := inputs
		if pending != nil {
			nextInput = nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-done:
			if ctx.Err() != nil {
				return nil
			}
			return errors.New("terminal input stopped")
		case <-ticker.C:
			if disk != nil && disk.image != nil {
				if err := serviceDisk(ctx, c, disk.image, disk.writer); err != nil {
					if ctx.Err() != nil {
						return nil
					}
					return fmt.Errorf("virtual disk: %w", err)
				}
			}
			if terminalMode {
				if _, err := pollTerminal(ctx, c, out); err != nil {
					if ctx.Err() != nil {
						return nil
					}
					return fmt.Errorf("terminal output: %w", err)
				}
			}
		case input := <-nextInput:
			if input.char == 3 {
				return nil
			}
			if input.char >= 128 || input == (terminalInput{}) {
				continue
			}
			pending = &input
		}
		if pending != nil {
			if _, err := keyboardExchangeContext(ctx, c, protocol.KeyboardEvent, []byte{pending.char, pending.scan}, 3); err != nil {
				if errors.Is(err, errKeyboardQueueFull) {
					continue
				}
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("keyboard input: %w", err)
			}
			pending = nil
		}
	}
}

// Only framed output is printed: protocol ACKs never reach the terminal.
func pollTerminal(ctx context.Context, c *client.Client, out io.Writer) (bool, error) {
	f, err := c.TransactAttemptsContext(ctx, protocol.Frame{Command: protocol.TerminalPoll}, 3)
	if err := checkResponse(f, err); err != nil {
		return false, err
	}
	if len(f.Data) < 2 || len(f.Data) > 66 || f.Data[1]&^protocol.KeyboardFlagRunning != 0 {
		return false, errors.New("invalid terminal output response")
	}
	if _, err := out.Write(f.Data[2:]); err != nil {
		return false, err
	}
	return f.Data[1]&protocol.KeyboardFlagRunning != 0, nil
}

// Client diagnostics use LF; raw tty output requires CRLF to return to column 1.
// This wrapper is only for host diagnostics, never the program's ANSI stream.
type terminalDiagnostics struct{ io.Writer }

func (w terminalDiagnostics) Write(p []byte) (int, error) {
	encoded := bytes.ReplaceAll(p, []byte("\n"), []byte("\r\n"))
	n, err := w.Writer.Write(encoded)
	if n != len(encoded) && err == nil {
		err = io.ErrShortWrite
	}
	if err != nil {
		return 0, err
	}
	return len(p), nil
}
