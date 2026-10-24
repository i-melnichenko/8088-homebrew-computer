package apps

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/client"
	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/protocol"
)

type diskPeer struct {
	reader bytes.Reader
	poll   []byte
	chunks []protocol.Frame
	cancel context.CancelFunc
	status byte
}

func (p *diskPeer) Read(b []byte) (int, error) { return p.reader.Read(b) }
func (p *diskPeer) Write(b []byte) (int, error) {
	f, err := protocol.Decode(b)
	if err != nil {
		return 0, err
	}
	data := []byte{p.status}
	if f.Command == protocol.DiskPoll {
		data = p.poll
	} else {
		p.chunks = append(p.chunks, f)
		if p.cancel != nil {
			p.cancel()
		}
	}
	reply, err := protocol.Encode(protocol.Frame{Command: f.Command | 0x80, Sequence: f.Sequence, Data: data})
	if err != nil {
		return 0, err
	}
	p.reader.Reset(reply)
	return len(b), nil
}
func diskPoll(pending byte, lba, offset uint16) []byte {
	p := []byte{0, pending, 0x34, 0x12, 0, 0, 0, 0}
	binary.LittleEndian.PutUint16(p[4:], lba)
	binary.LittleEndian.PutUint16(p[6:], offset)
	return p
}
func TestDiskSectorTransferAndResume(t *testing.T) {
	image := make([]byte, diskImageSize)
	for i := range image {
		image[i] = byte(i*7 + 3)
	}
	for _, offset := range []uint16{0, 120, 480} {
		peer := &diskPeer{poll: diskPoll(1, 2879, offset)}
		if err := serviceDisk(context.Background(), client.New(peer, nil), image); err != nil {
			t.Fatal(err)
		}
		var result []byte
		next := offset
		for _, f := range peer.chunks {
			if f.Command != protocol.DiskData || len(f.Data) < 6 || len(f.Data) > 125 || binary.LittleEndian.Uint16(f.Data) != 0x1234 || binary.LittleEndian.Uint16(f.Data[2:]) != next || f.Data[4] != 0 {
				t.Fatalf("invalid chunk: %+v", f)
			}
			result = append(result, f.Data[5:]...)
			next += uint16(len(f.Data) - 5)
		}
		if next != 512 || !bytes.Equal(result, image[2879*512+int(offset):]) {
			t.Fatal("sector content changed")
		}
	}
}
func TestDiskRejectsMalformedRequestsAndCancels(t *testing.T) {
	image := make([]byte, diskImageSize)
	for _, poll := range [][]byte{{0}, {0, 2, 0, 0, 0, 0, 0, 0}, diskPoll(1, 2880, 0), diskPoll(1, 0, 512)} {
		peer := &diskPeer{poll: poll}
		if err := serviceDisk(context.Background(), client.New(peer, nil), image); err == nil {
			t.Fatalf("accepted %x", poll)
		}
		if len(peer.chunks) != 0 {
			t.Fatal("sent data for invalid request")
		}
	}
	peer := &diskPeer{poll: diskPoll(0, 0, 0)}
	if err := serviceDisk(context.Background(), client.New(peer, nil), image); err != nil || len(peer.chunks) != 0 {
		t.Fatalf("idle poll: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	peer = &diskPeer{poll: diskPoll(1, 0, 0), cancel: cancel}
	if err := serviceDisk(ctx, client.New(peer, nil), image); err == nil {
		t.Fatal("ignored cancellation")
	}
	if len(peer.chunks) != 1 {
		t.Fatal("continued transfer after cancellation")
	}
	peer = &diskPeer{poll: diskPoll(1, 0, 0), status: 18}
	if err := serviceDisk(context.Background(), client.New(peer, nil), image); err == nil {
		t.Fatal("ignored BIOS rejection")
	}
}
func TestDiskArgumentsValidateBeforeSerialAndKeepImageReadonly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disk.img")
	image := make([]byte, diskImageSize)
	image[0] = 42
	if err := os.WriteFile(path, image, 0600); err != nil {
		t.Fatal(err)
	}
	cmd, err := prepareCommand([]string{"terminal", "--disk", path, "--boot"})
	if err != nil || !cmd.terminal.bootDisk || !bytes.Equal(cmd.terminal.diskImage, image) {
		t.Fatalf("prepare: %v", err)
	}
	cmd.terminal.diskImage[0] = 99
	original, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, image) {
		t.Fatal("changed image on disk")
	}
	for _, args := range [][]string{
		{"terminal", "--boot"}, {"keyboard", "--disk", path},
		{"terminal", "--disk", path, "--run"}, {"terminal", "--disk", path, "--upload", path},
		{"terminal", "--disk", filepath.Join(t.TempDir(), "missing.img")},
	} {
		if err := runWithSerial(args, func(string) (io.ReadWriteCloser, error) {
			t.Fatal("opened serial with invalid disk options")
			return nil, nil
		}); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	short := writeTestFile(t, "short.img", []byte{0})
	if _, err := prepareCommand([]string{"terminal", "--disk", short}); err == nil {
		t.Fatal("accepted short disk image")
	}
}

func TestInfoTimerFrequencyMatchesFirmwareProtocol(t *testing.T) {
	for _, pair := range [][2]string{{"2.0", "18.2 Hz"}} {
		output := formatInfo("protocol " + pair[0] + ";IRQ 3")
		if !strings.Contains(output, "IRQ0 / "+pair[1]) {
			t.Fatal(output)
		}
	}
}
