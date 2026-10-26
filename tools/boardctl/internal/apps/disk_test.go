package apps

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
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
	for _, poll := range [][]byte{{0}, {0, 3, 0, 0, 0, 0, 0, 0}, diskPoll(1, 2880, 0), diskPoll(1, 0, 512)} {
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
func TestDiskArgumentsValidateBeforeSerialAndKeepReadonlySnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disk.img")
	image := make([]byte, diskImageSize)
	image[0] = 42
	if err := os.WriteFile(path, image, 0600); err != nil {
		t.Fatal(err)
	}
	cmd, err := prepareCommand([]string{"terminal", "--disk", path, "--disk-readonly", "--boot"})
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

// A peer with an immutable pending write; retransmitted fetches/ACKs are safe.
type writePeer struct {
	reader    bytes.Reader
	sector    []byte
	ack       []byte
	writer    *testSectorWriter
	malformed bool
	loseAck   bool
	requests  int
}

func (p *writePeer) Read(b []byte) (int, error) { return p.reader.Read(b) }
func (p *writePeer) Write(b []byte) (int, error) {
	f, err := protocol.Decode(b)
	if err != nil {
		return 0, err
	}
	data := []byte{0}
	switch f.Command {
	case protocol.DiskPoll:
		data = diskPoll(2, 2879, 0)
	case protocol.DiskWriteData:
		p.requests++
		if len(f.Data) != 4 {
			return 0, io.ErrUnexpectedEOF
		}
		offset := int(binary.LittleEndian.Uint16(f.Data[2:]))
		data = append(data, f.Data...)
		data = append(data, p.sector[offset:offset+min(120, 512-offset)]...)
		if p.malformed {
			data[1] ^= 1
		}
	case protocol.DiskWriteAck:
		// Success must only be sent after persistence and readback.
		if f.Data[2] == 0 && (p.writer != nil && (!p.writer.synced || !p.writer.verified)) {
			return 0, errors.New("premature write acknowledgement")
		}
		p.ack = append([]byte(nil), f.Data...)
		if p.loseAck {
			p.loseAck = false
			p.reader.Reset(nil)
			return len(b), nil
		}
	default:
		return 0, errors.New("unexpected write command")
	}
	reply, err := protocol.Encode(protocol.Frame{Command: f.Command | 0x80, Sequence: f.Sequence, Data: data})
	if err != nil {
		return 0, err
	}
	p.reader.Reset(reply)
	return len(b), nil
}

type testSectorWriter struct {
	data             []byte
	synced, verified bool
	fail             bool
	short            bool
	mismatch         bool
	writes           int
}

func (w *testSectorWriter) WriteAt(b []byte, off int64) (int, error) {
	w.writes++
	copy(w.data[int(off):], b)
	if w.short {
		return 12, nil
	}
	return len(b), nil
}
func (w *testSectorWriter) Sync() error {
	if w.fail {
		return errors.New("sync failure")
	}
	w.synced = true
	return nil
}
func (w *testSectorWriter) ReadAt(b []byte, off int64) (int, error) {
	copy(b, w.data[int(off):])
	if w.mismatch {
		b[0] ^= 1
	}
	w.verified = true
	return len(b), nil
}
func TestDiskWritesCommitBeforeAckAndPreserveReadonlyDefault(t *testing.T) {
	for _, mode := range []string{"success", "readonly", "sync-error", "short-write", "mismatch", "bad-reply", "lost-ack"} {
		t.Run(mode, func(t *testing.T) {
			image := make([]byte, diskImageSize)
			w := &testSectorWriter{data: make([]byte, diskImageSize), fail: mode == "sync-error", short: mode == "short-write", mismatch: mode == "mismatch"}
			peer := &writePeer{sector: bytes.Repeat([]byte{0xA5}, 512), writer: w, malformed: mode == "bad-reply", loseAck: mode == "lost-ack"}
			var writer sectorWriter = w
			if mode == "readonly" {
				writer = nil
			}
			err := serviceDisk(context.Background(), client.New(peer, nil), image, writer)
			if mode == "bad-reply" {
				if err == nil || len(peer.ack) != 0 || w.writes != 0 {
					t.Fatal("accepted malformed data")
				}
				return
			}
			status := byte(0)
			if mode == "readonly" {
				status = 3
			}
			if w.fail || w.short || w.mismatch {
				status = 0x20
				if err == nil {
					t.Fatal("ignored storage error")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(peer.ack, []byte{0x34, 0x12, status}) {
				t.Fatalf("ack %x", peer.ack)
			}
			if status == 0 {
				if !bytes.Equal(image[2879*512:], peer.sector) || w.writes != 1 {
					t.Fatal("write not persisted or applied twice")
				}
			} else if image[2879*512] != 0 {
				t.Fatal("published uncommitted bytes")
			}
			if mode == "readonly" && (peer.requests != 0 || w.writes != 0) {
				t.Fatal("readonly host touched disk")
			}
		})
	}
}
func TestWritableDiskLockSizeAndPersistence(t *testing.T) {
	path := writeTestFile(t, "disk.img", make([]byte, diskImageSize))
	image := make([]byte, diskImageSize)
	f, err := openWritableDisk(path, image)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if other, err := openWritableDisk(path, image); err == nil {
		other.Close()
		t.Fatal("allowed concurrent writers")
	}
	defaultCmd, err := prepareCommand([]string{"terminal", "--disk", path})
	if err != nil || defaultCmd.terminal.diskReadonly {
		t.Fatal("disk is not writable by default")
	}
	if err := runWithSerial([]string{"terminal", "--disk", path}, func(string) (io.ReadWriteCloser, error) {
		t.Fatal("opened serial after writable lock failure")
		return nil, nil
	}); err == nil {
		t.Fatal("silently fell back to read-only")
	}
	data := bytes.Repeat([]byte{0x47}, 512)
	peer := &writePeer{sector: data}
	if err := serviceDisk(context.Background(), client.New(peer, nil), image, f); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(saved[2879*512:], data) {
		t.Fatal("file not persisted")
	}
	for _, args := range [][]string{{"terminal", "--disk-readonly"}, {"keyboard", "--disk-readonly"}, {"terminal", "--disk", path, "--disk-write"}} {
		if _, err := prepareCommand(args); err == nil {
			t.Fatal("accepted invalid write options")
		}
	}
	cmd, err := prepareCommand([]string{"terminal", "--disk", path, "--disk-readonly", "--boot"})
	if err != nil || !cmd.terminal.diskReadonly {
		t.Fatal(err)
	}
}
