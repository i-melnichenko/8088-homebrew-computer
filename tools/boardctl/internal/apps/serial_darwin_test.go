package apps

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/client"
	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/protocol"
)

// Exercise actual Darwin tty I/O, not a mock that always returns immediately.
func newTestPTY(t *testing.T) (*serialPort, string) {
	t.Helper()
	fd, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(fd), "test pty")
	t.Cleanup(func() { _ = file.Close() })
	for _, operation := range []uintptr{syscall.TIOCPTYGRANT, syscall.TIOCPTYUNLK} {
		if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), operation, 0); errno != 0 {
			t.Fatal(errno)
		}
	}
	var name [128]byte
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TIOCPTYGNAME, uintptr(unsafe.Pointer(&name[0]))); errno != 0 {
		t.Fatal(errno)
	}
	raw, err := file.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	return &serialPort{file: file, raw: raw}, strings.TrimRight(string(name[:]), "\x00")
}

func TestSerialIdleReadReturnsAndProtocolTimesOut(t *testing.T) {
	_, name := newTestPTY(t)
	port, err := openSerial(name)
	if err != nil {
		t.Fatal(err)
	}
	defer port.Close()
	result := make(chan error, 1)
	go func() {
		var buf [1]byte
		n, err := port.Read(buf[:])
		if err == nil && n != 0 {
			err = fmt.Errorf("idle read returned %d bytes", n)
		}
		result <- err
	}()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("idle serial read blocked instead of returning to the protocol loop")
	}
	start := time.Now()
	_, err = keyboardExchangeContext(t.Context(), client.New(port, nil), protocol.KeyboardMode, []byte{1}, 1)
	if err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("missing reply did not time out: %v after %v", err, time.Since(start))
	}
}

// Run the real keyboard session in a child with its own controlling terminal.
func TestKeyboardPTYProcess(t *testing.T) {
	if os.Getenv("BOARDCTL_TEST_KEYBOARD_PROCESS") != "1" {
		return
	}
	if err := Run([]string{"-port", os.Getenv("BOARDCTL_TEST_SERIAL_PORT"), "keyboard"}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func startKeyboardPTY(t *testing.T) (*serialPort, *serialPort, <-chan error) {
	t.Helper()
	board, serialName := newTestPTY(t)
	terminal, terminalName := newTestPTY(t)
	input, err := os.OpenFile(terminalName, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close() })
	cmd := exec.Command(os.Args[0], "-test.run=^TestKeyboardPTYProcess$")
	cmd.Env = append(os.Environ(), "BOARDCTL_TEST_KEYBOARD_PROCESS=1", "BOARDCTL_TEST_SERIAL_PORT="+serialName)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = input, input, input
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	return board, terminal, done
}

func waitPTYText(t *testing.T, port *serialPort, want string) {
	t.Helper()
	var output []byte
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		var buf [512]byte
		n, err := port.Read(buf[:])
		if err != nil {
			t.Fatalf("terminal read: %v; output=%q", err, output)
		}
		output = append(output, buf[:n]...)
		if bytes.Contains(output, []byte(want)) {
			return
		}
	}
	t.Fatalf("did not see %q; output=%q", want, output)
}

func readPTYFrame(t *testing.T, port *serialPort) protocol.Frame {
	t.Helper()
	var packet []byte
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		var buf [1]byte
		n, err := port.Read(buf[:])
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			continue
		}
		packet = append(packet, buf[0])
		if buf[0] == 0 && len(packet) > 1 {
			frame, err := protocol.Decode(packet)
			if err != nil {
				t.Fatalf("%v: packet=% x", err, packet)
			}
			return frame
		}
	}
	t.Fatal("timed out reading request from keyboard process")
	return protocol.Frame{}
}

func replyPTYFrame(t *testing.T, port *serialPort, request protocol.Frame) {
	t.Helper()
	packet, err := protocol.Encode(protocol.Frame{Command: request.Command | 0x80, Sequence: request.Sequence, Data: []byte{0, 0}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := port.Write(packet); err != nil {
		t.Fatal(err)
	}
}

func waitKeyboardExit(t *testing.T, done <-chan error, terminal *serialPort) {
	t.Helper()
	var output []byte
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("keyboard exit: %v; output=%q", err, output)
			}
			return
		default:
		}
		// Like a terminal emulator, drain output while waiting. Darwin tty
		// close can wait for its output queue to drain before process exit.
		var buf [512]byte
		n, _ := terminal.Read(buf[:])
		output = append(output, buf[:n]...)
	}
	t.Fatalf("Ctrl-C did not stop keyboard; output=%q", output)
}

func TestKeyboardPTYCtrlCDuringSilentAttach(t *testing.T) {
	_, terminal, done := startKeyboardPTY(t)
	// No simulated BIOS response: real UART read must permit retry and SIGINT.
	waitPTYText(t, terminal, fmt.Sprintf("retrying command %#02x", protocol.KeyboardMode))
	if _, err := terminal.Write([]byte{3}); err != nil {
		t.Fatal(err)
	}
	waitKeyboardExit(t, done, terminal)
}

func TestKeyboardPTYArrowAndRawCtrlC(t *testing.T) {
	board, terminal, done := startKeyboardPTY(t)
	original := ptySettings(t, terminal)
	attach := readPTYFrame(t, board)
	if attach.Command != protocol.KeyboardMode || !bytes.Equal(attach.Data, []byte{1}) {
		t.Fatalf("invalid attach: %+v", attach)
	}
	replyPTYFrame(t, board, attach)
	waitPTYText(t, terminal, "Keyboard connected; use")
	if _, err := terminal.Write([]byte("\x1b[B")); err != nil {
		t.Fatal(err)
	}
	key := readPTYFrame(t, board)
	if key.Command != protocol.KeyboardEvent || !bytes.Equal(key.Data, []byte{0, 0x50}) {
		t.Fatalf("down arrow was not sent: %+v", key)
	}
	// Do not ACK the key: raw Ctrl-C must cancel this read too.
	if _, err := terminal.Write([]byte{3}); err != nil {
		t.Fatal(err)
	}
	detach := readPTYFrame(t, board)
	if detach.Command != protocol.KeyboardMode || !bytes.Equal(detach.Data, []byte{0}) {
		t.Fatalf("expected detach, not Ctrl-C key: %+v", detach)
	}
	replyPTYFrame(t, board, detach)
	waitKeyboardExit(t, done, terminal)
	// The child restored the tty, rather than leaving the shell in raw mode.
	if restored := ptySettings(t, terminal); restored != original {
		t.Fatalf("terminal not restored: got %+v, want %+v", restored, original)
	}
}

func ptySettings(t *testing.T, port *serialPort) syscall.Termios {
	t.Helper()
	var settings syscall.Termios
	var ioctlErr syscall.Errno
	err := port.raw.Read(func(fd uintptr) bool {
		_, _, ioctlErr = syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGETA, uintptr(unsafe.Pointer(&settings)))
		return true
	})
	if err != nil || ioctlErr != 0 {
		t.Fatalf("read terminal settings: %v %v", err, ioctlErr)
	}
	return settings
}
