package apps

import (
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeUploadFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInvalidCommandDoesNotOpenSerial(t *testing.T) {
	empty := writeUploadFile(t, "empty.bin", nil)
	large := writeUploadFile(t, "large.bin", make([]byte, maxPayloadSize+1))
	missing := filepath.Join(t.TempDir(), "missing.bin")
	existingDump := writeUploadFile(t, "existing.bin", []byte{1})
	for _, test := range []struct {
		args []string
		want string
	}{
		{nil, "usage:"},
		{[]string{"unknown"}, "unknown command"},
		{[]string{"-unknown"}, "flag provided but not defined"},
		{[]string{"-port"}, "flag needs an argument"},
		{[]string{"run"}, "usage:"},
		{[]string{"run", "1", "2"}, "usage:"},
		{[]string{"run", "0"}, "invalid program ID"},
		{[]string{"run", "5"}, "invalid program ID"},
		{[]string{"run", "-1"}, "invalid program ID"},
		{[]string{"run", "foo"}, "invalid program ID"},
		{[]string{"delete"}, "usage:"},
		{[]string{"delete", "1", "2"}, "usage:"},
		{[]string{"delete", "0"}, "invalid program ID"},
		{[]string{"rename", "1"}, "usage:"},
		{[]string{"rename", "5", "Name"}, "invalid program ID"},
		{[]string{"rename", "1", ""}, "program name"},
		{[]string{"rename", "1", "   "}, "program name"},
		{[]string{"rename", "1", "abcdefghijklmnop"}, "program name"},
		{[]string{"rename", "1", "Name\n"}, "program name"},
		{[]string{"upload"}, "usage:"},
		{[]string{"upload", "one.bin", "two.bin"}, "usage:"},
		{[]string{"upload", "--unknown"}, "flag provided but not defined"},
		{[]string{"upload", "--name"}, "flag needs an argument"},
		{[]string{"upload", "--run=invalid"}, "invalid boolean value"},
		{[]string{"upload", "--name", "abcdefghijklmnop", missing}, "program name"},
		{[]string{"upload", missing}, "read upload file"},
		{[]string{"upload", empty}, "upload size"},
		{[]string{"upload", large}, "upload size"},
		{[]string{"dump"}, "usage:"},
		{[]string{"dump", "--addr", "0"}, "usage:"},
		{[]string{"dump", "--size", "1"}, "usage:"},
		{[]string{"dump", "--addr", "0", "--size", "1", "extra"}, "usage:"},
		{[]string{"dump", "--addr", "-1", "--size", "1"}, "invalid dump address"},
		{[]string{"dump", "--addr", "0x100000000", "--size", "1"}, "invalid dump address"},
		{[]string{"dump", "--addr", "0", "--size", "x"}, "invalid dump size"},
		{[]string{"dump", "--addr", "0", "--size", "0"}, "dump range"},
		{[]string{"dump", "--addr", "0x1ffff", "--size", "2"}, "dump range"},
		{[]string{"dump", "--addr", "0xf8000", "--size", "32768"}, "dump range"},
		{[]string{"dump", "--addr", "0xfffff", "--size", "1"}, "dump range"},
		{[]string{"dump", "--addr", "0xfffff", "--size", "2"}, "dump range"},
		{[]string{"dump", "--addr", "0", "--size", "1", "--output", existingDump}, "already exists"},
		{[]string{"write"}, "usage:"},
		{[]string{"write", existingDump}, "usage:"},
		{[]string{"write", "--addr", "0x9000"}, "usage:"},
		{[]string{"write", "--addr", "x", existingDump}, "invalid write address"},
		{[]string{"write", "--addr", "0xf8000", existingDump}, "write range"},
		{[]string{"write", "--addr", "0x8500", existingDump}, "write range"},
		{[]string{"write", "--addr", "0x9000", empty}, "write range"},
		{[]string{"write", "--addr", "0x9000", missing}, "read memory write file"},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			err := runWithSerial(test.args, func(string) (io.ReadWriteCloser, error) {
				t.Fatal("serial opened before validation")
				return nil, nil
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
	for _, name := range []string{"keyboard", "console", "ping", "info", "programs", "reset"} {
		t.Run(name, func(t *testing.T) {
			err := runWithSerial([]string{name, "extra"}, func(string) (io.ReadWriteCloser, error) {
				t.Fatal("serial opened with extra arguments")
				return nil, nil
			})
			if err == nil || !strings.Contains(err.Error(), "usage:") {
				t.Fatalf("error = %v, want usage error", err)
			}
		})
	}
}

func TestHelpDoesNotOpenSerial(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"--help"}, {"upload", "-h"}, {"dump", "-h"}, {"write", "-h"},
		{"bios-flasher-mode", "-h"}, {"bios-read", "-h"}, {"bios-write", "-h"}} {
		err := runWithSerial(args, func(string) (io.ReadWriteCloser, error) {
			t.Fatal("serial opened for help")
			return nil, nil
		})
		if !errors.Is(err, flag.ErrHelp) {
			t.Fatalf("error = %v, want flag.ErrHelp", err)
		}
	}
}

func TestPrepareCommand(t *testing.T) {
	for _, args := range [][]string{{"delete", "0x2"}, {"rename", "2", "My program"}} {
		cmd, err := prepareCommand(args)
		if err != nil || cmd.programID != 2 || (cmd.name == "rename" && cmd.programName != "My program") {
			t.Fatalf("registry command = %+v, error = %v", cmd, err)
		}
	}
	for _, addr := range []string{"0", "0x8800", "0x19000"} {
		cmd, err := prepareCommand([]string{"-port", "test-port", "dump", "--addr", addr, "--size", "256"})
		if err != nil || cmd.port != "test-port" || cmd.name != "dump" || cmd.dump.size != 256 {
			t.Fatalf("dump command = %+v, error = %v", cmd, err)
		}
	}
	for _, id := range []string{"1", "4", "0x2"} {
		cmd, err := prepareCommand([]string{"-port", "test-port", "run", id})
		if err != nil || cmd.port != "test-port" || cmd.name != "run" || cmd.programID < 1 || cmd.programID > 4 {
			t.Fatalf("command = %+v, error = %v", cmd, err)
		}
	}
	path := writeUploadFile(t, "demo.bin", []byte{1, 2, 3})
	for _, name := range []string{"", "Custom"} {
		args := []string{"upload", "--run"}
		if name != "" {
			args = append(args, "--name", name)
		} else {
			name = "demo"
		}
		cmd, err := prepareCommand(append(args, path))
		if err != nil || cmd.upload.name != name || !cmd.upload.runAfter || string(cmd.upload.data) != "\x01\x02\x03" {
			t.Fatalf("command = %+v, error = %v", cmd, err)
		}
	}
}

type failingSerial struct {
	err    error
	closed int
}

func (s *failingSerial) Read([]byte) (int, error)  { return 0, s.err }
func (s *failingSerial) Write([]byte) (int, error) { return 0, s.err }
func (s *failingSerial) Close() error {
	s.closed++
	return nil
}

func TestCommandErrorClosesSerial(t *testing.T) {
	path := writeUploadFile(t, "demo.bin", []byte{1})
	want := errors.New("serial write failed")
	for _, args := range [][]string{{"info"}, {"programs"}, {"reset"}, {"run", "1"}, {"delete", "1"}, {"rename", "1", "Name"}, {"keyboard"}, {"upload", path}, {"dump", "--addr", "0", "--size", "1"}, {"write", "--addr", "0x9000", path}} {
		t.Run(args[0], func(t *testing.T) {
			serial := &failingSerial{err: want}
			err := runWithSerial(args, func(string) (io.ReadWriteCloser, error) {
				return serial, nil
			})
			if !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
			if serial.closed != 1 {
				t.Fatalf("serial closed %d times, want 1", serial.closed)
			}
		})
	}
}
