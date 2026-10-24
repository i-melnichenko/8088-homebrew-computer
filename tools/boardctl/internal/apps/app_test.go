package apps

import (
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func writeTestFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestRemovedCommandsDoNotOpenSerial(t *testing.T) {
	for _, args := range [][]string{{"upload", "test.bin"}, {"programs"}, {"run", "1"}, {"delete", "1"}, {"rename", "1", "x"}, {"terminal", "--upload", "test.bin"}, {"terminal", "--run"}, {"keyboard", "--upload", "test.bin"}, {"terminal", "test.bin"}, {"ram", "extra"}, {"dump"}, {"write"}} {
		if err := runWithSerial(args, func(string) (io.ReadWriteCloser, error) {
			t.Fatal("opened serial for invalid command")
			return nil, nil
		}); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
func TestHelpDoesNotOpenSerial(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"terminal", "-h"}, {"keyboard", "-h"}, {"bios-write", "-h"}, {"dump", "-h"}} {
		if err := runWithSerial(args, func(string) (io.ReadWriteCloser, error) { t.Fatal("opened UART for help"); return nil, nil }); !errors.Is(err, flag.ErrHelp) {
			t.Fatal(err)
		}
	}
}
