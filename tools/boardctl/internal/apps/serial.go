package apps

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// os.File.Read may wait indefinitely in Go's netpoller after EAGAIN,
// regardless of the terminal's VMIN/VTIME. Return periodically so protocol
// deadlines and cancellation are checked, even when the board sends nothing.
type serialPort struct {
	file *os.File
	raw  syscall.RawConn
}

const serialPollInterval = 10 * time.Millisecond

func (s *serialPort) Read(p []byte) (int, error) {
	var n int
	var readErr error
	err := s.raw.Read(func(fd uintptr) bool {
		n, readErr = syscall.Read(int(fd), p)
		return true // never enter Go's unbounded readiness wait
	})
	if err != nil {
		return 0, err
	}
	if errors.Is(readErr, syscall.EAGAIN) || errors.Is(readErr, syscall.EINTR) {
		time.Sleep(serialPollInterval)
		return 0, nil
	}
	if n == 0 && readErr == nil && len(p) != 0 {
		time.Sleep(serialPollInterval)
	}
	return n, readErr
}

func (s *serialPort) Write(p []byte) (int, error) {
	deadline := time.Now().Add(time.Second)
	written := 0
	for written < len(p) {
		var n int
		var writeErr error
		err := s.raw.Write(func(fd uintptr) bool {
			n, writeErr = syscall.Write(int(fd), p[written:])
			return true
		})
		if err != nil {
			return written, err
		}
		if n > 0 {
			written += n
		}
		if writeErr != nil && !errors.Is(writeErr, syscall.EAGAIN) && !errors.Is(writeErr, syscall.EINTR) {
			return written, writeErr
		}
		if written == len(p) {
			return written, nil
		}
		if time.Now().After(deadline) {
			return written, os.ErrDeadlineExceeded
		}
		time.Sleep(serialPollInterval)
	}
	return written, nil
}

func (s *serialPort) Close() error { return s.file.Close() }

var _ io.ReadWriteCloser = (*serialPort)(nil)

func openSerial(port string) (*serialPort, error) {
	file, err := os.OpenFile(port, os.O_RDWR|syscall.O_NOCTTY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	// Keep the descriptor open while configuring it. Some tty drivers reset
	// settings on the next open after the last descriptor has been closed.
	if err := exec.Command("stty", "-f", port, "9600", "raw", "-echo", "min", "0", "time", "1").Run(); err != nil {
		_ = file.Close()
		return nil, err
	}
	raw, err := file.SyscallConn()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return &serialPort{file: file, raw: raw}, nil
}
