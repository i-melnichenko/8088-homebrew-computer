package apps

import (
	"bytes"
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

type biosSerial struct {
	image         []byte
	mode          bool
	writable      bool
	requests      []protocol.Frame
	packet        []byte
	flashStatus   byte
	loadStatus    byte
	enterStatus   byte
	shortLoad     bool
	extraEnterACK bool
	corruptRead   bool
	dropCommand   byte
	dropped       bool
	lastWrite     []byte
	lastReply     []byte
	programmed    int
	resetCount    int
}

func newBIOSSerial() *biosSerial {
	return &biosSerial{image: memoryPattern(0, protocol.BIOSImageSize)}
}

func (s *biosSerial) Write(packet []byte) (int, error) {
	f, err := protocol.Decode(packet)
	if err != nil {
		return 0, err
	}
	s.requests = append(s.requests, f)
	if bytes.Equal(packet, s.lastWrite) {
		s.packet = append([]byte(nil), s.lastReply...)
		return len(packet), nil
	}
	data := []byte{0}
	switch f.Command {
	case protocol.BIOSFlasherMode:
		if len(f.Data) != 0 {
			return 0, errors.New("FLASHER_MODE must have an empty payload")
		}
		if s.enterStatus != 0 {
			data[0] = s.enterStatus
		} else if s.mode {
			data[0] = 20
		} else {
			s.mode = true
			if s.extraEnterACK {
				data = append(data, 0)
			}
		}
	case protocol.BIOSRead:
		if len(f.Data) != 6 {
			return 0, errors.New("READ must contain address and size")
		}
		if !s.mode {
			data[0] = 17
			break
		}
		if s.loadStatus != 0 {
			data[0] = s.loadStatus
			break
		}
		address, size := binary.LittleEndian.Uint32(f.Data), int(binary.LittleEndian.Uint16(f.Data[4:]))
		if size < 1 || size > protocol.MaxBIOSReadLength || address < 0xf8000 || uint64(address)+uint64(size) > 0x100000 {
			return 0, errors.New("invalid READ range")
		}
		offset := int(address - 0xf8000)
		data = append(data, s.image[offset:offset+size]...)
		if s.corruptRead && size > 1 {
			data[1] ^= 1
		}
		if s.shortLoad {
			data = data[:len(data)-1]
		}
	case protocol.BIOSWrite:
		if !s.mode {
			data[0] = 17
			break
		}
		if s.flashStatus != 0 {
			data[0] = s.flashStatus
			break
		}
		if len(f.Data) < 5 || len(f.Data) > 4+protocol.MaxBIOSWriteLength {
			return 0, errors.New("invalid WRITE size")
		}
		address := binary.LittleEndian.Uint32(f.Data)
		if address < 0xf8000 || uint64(address)+uint64(len(f.Data)-4) > 0x100000 {
			return 0, errors.New("invalid WRITE range")
		}
		if !s.writable {
			data[0] = 17
			break
		}
		offset := int(address - 0xf8000)
		if offset != s.programmed || offset%64 != 0 || len(f.Data[4:]) != 64 {
			return 0, errors.New("WRITE blocks must be consecutive aligned pages")
		}
		copy(s.image[offset:], f.Data[4:])
		s.programmed += len(f.Data[4:])
	case protocol.Reset:
		s.resetCount++
	case protocol.Ping:
	default:
		return 0, errors.New("unexpected command in EEPROM transaction")
	}
	s.packet, err = protocol.Encode(protocol.Frame{Command: f.Command | 0x80, Sequence: f.Sequence, Data: data})
	s.lastWrite = append([]byte(nil), packet...)
	s.lastReply = append([]byte(nil), s.packet...)
	if f.Command == s.dropCommand && !s.dropped {
		s.dropped = true
		s.packet = nil
	}
	return len(packet), err
}

func (s *biosSerial) Read(dst []byte) (int, error) {
	if len(s.packet) == 0 {
		return 0, errors.New("simulated lost ACK")
	}
	n := copy(dst, s.packet)
	s.packet = s.packet[n:]
	return n, nil
}

func (s *biosSerial) Close() error { return nil }

func TestBIOSModeEntryEmptyPayloadAndLostACK(t *testing.T) {
	s := newBIOSSerial()
	s.dropCommand = protocol.BIOSFlasherMode
	if err := enterBIOSFlasherMode(client.New(s, nil)); err != nil {
		t.Fatal(err)
	}
	if !s.mode || len(s.requests) != 2 || s.requests[0].Sequence != s.requests[1].Sequence ||
		len(s.requests[0].Data) != 0 || !bytes.Equal(s.requests[0].Data, s.requests[1].Data) {
		t.Fatalf("entry/retry: mode=%v requests=%+v", s.mode, s.requests)
	}
}

func TestBIOSPhysicalRanges(t *testing.T) {
	for _, r := range []struct {
		address uint32
		size    int
	}{
		{0xf8000, 32768}, {0xf8000, 1}, {0xfffff, 1}, {0xfff81, 127},
	} {
		if err := validateBIOSRange(r.address, r.size); err != nil {
			t.Fatalf("valid range %+v: %v", r, err)
		}
	}
	for _, r := range []struct {
		address uint32
		size    int
	}{
		{0, 1}, {0x8800, 1}, {0xe0000, 1}, {0xf0000, 1}, {0xf7fff, 2},
		{0xf8000, 0}, {0xf8000, -1}, {0xf8000, 32769}, {0xfffff, 2},
		{0x100000, 1}, {0x1f8000, 1}, {0xffffffff, 2},
	} {
		if err := validateBIOSRange(r.address, r.size); err == nil {
			t.Fatalf("accepted range %+v", r)
		}
	}
}

func TestBIOSReadFileHashAndExistingMode(t *testing.T) {
	for _, active := range []bool{false, true} {
		s := newBIOSSerial()
		s.mode = active
		output := filepath.Join(t.TempDir(), "bios.bin")
		data, err := readBIOS(client.New(s, nil), biosReadOptions{output: output, expected: s.image})
		if err != nil || !bytes.Equal(data, s.image) {
			t.Fatalf("active=%v data=%d err=%v", active, len(data), err)
		}
		file, err := os.ReadFile(output)
		if err != nil || !bytes.Equal(file, s.image) || s.resetCount != 0 {
			t.Fatalf("readback file/reset: %v", err)
		}
		for _, f := range s.requests {
			if active && f.Command == protocol.BIOSFlasherMode {
				t.Fatal("existing mode was reentered")
			}
		}
	}
}

func TestBIOSReadFailuresDoNotSaveOutput(t *testing.T) {
	for _, scenario := range []string{"hash", "read", "short", "enter", "extra_ack"} {
		t.Run(scenario, func(t *testing.T) {
			s := newBIOSSerial()
			options := biosReadOptions{output: filepath.Join(t.TempDir(), "failed.bin"), expected: append([]byte(nil), s.image...)}
			switch scenario {
			case "hash":
				s.corruptRead = true
			case "read":
				s.loadStatus = 19
			case "short":
				s.shortLoad = true
			case "enter":
				s.enterStatus = 15
			case "extra_ack":
				s.extraEnterACK = true
			}
			if _, err := readBIOS(client.New(s, nil), options); err == nil {
				t.Fatal("accepted failed BIOS load")
			}
			if _, err := os.Stat(options.output); !errors.Is(err, os.ErrNotExist) || s.resetCount != 0 {
				t.Fatalf("failure created output/reset: %v", err)
			}
		})
	}
}

func TestBIOSWritePagesRetriesHashAndReset(t *testing.T) {
	for _, reset := range []bool{false, true} {
		s := newBIOSSerial()
		s.writable, s.dropCommand = true, protocol.BIOSWrite
		image := bytes.Repeat([]byte{0x5a}, protocol.BIOSImageSize)
		progress := 0
		err := writeBIOS(client.New(s, nil), biosWriteOptions{data: image, resetAfter: reset, timeout: defaultBIOSWriteTimeout}, func(done, total int) {
			if done != progress+64 || total != len(image) {
				t.Errorf("progress=%d/%d", done, total)
			}
			progress = done
		})
		if err != nil || progress != len(image) || s.programmed != len(image) || !bytes.Equal(s.image, image) {
			t.Fatalf("flash: progress=%d programmed=%d err=%v", progress, s.programmed, err)
		}
		wantReset := 0
		if reset {
			wantReset = 1
		}
		if s.resetCount != wantReset {
			t.Fatalf("resets=%d", s.resetCount)
		}
		var flashes []protocol.Frame
		for _, f := range s.requests {
			if f.Command == protocol.BIOSWrite {
				flashes = append(flashes, f)
			}
		}
		if len(flashes) != 513 || flashes[0].Sequence != flashes[1].Sequence || !bytes.Equal(flashes[0].Data, flashes[1].Data) {
			t.Fatalf("WRITE retry changed packet or wrote twice: %d requests", len(flashes))
		}
		// 512 writes wrap the sequence twice; offsets, not sequence, order pages.
		if flashes[257].Sequence != flashes[0].Sequence {
			t.Fatal("sequence did not wrap")
		}
		if reset && s.requests[len(s.requests)-1].Command != protocol.Reset {
			t.Fatal("reset preceded verification")
		}
	}
}

func TestBIOSWriteFailureNeverResets(t *testing.T) {
	for _, scenario := range []string{"rev1", "write", "hash", "read", "short", "active", "enter", "extra_ack"} {
		t.Run(scenario, func(t *testing.T) {
			s := newBIOSSerial()
			s.writable = true
			switch scenario {
			case "rev1":
				s.writable = false
			case "write":
				s.flashStatus = 19
			case "hash":
				s.corruptRead = true
			case "read":
				s.loadStatus = 19
			case "short":
				s.shortLoad = true
			case "active":
				s.mode = true
			case "enter":
				s.enterStatus = 10
			case "extra_ack":
				s.extraEnterACK = true
			}
			err := writeBIOS(client.New(s, nil), biosWriteOptions{data: s.image, resetAfter: true, timeout: defaultBIOSWriteTimeout}, nil)
			if err == nil || s.resetCount != 0 {
				t.Fatalf("err=%v resets=%d", err, s.resetCount)
			}
			if scenario == "active" && (len(s.requests) != 1 || !strings.Contains(err.Error(), "already active")) {
				t.Fatalf("active session was modified: %v", err)
			}
			if scenario == "rev1" && (!strings.Contains(err.Error(), "unsupported") || s.programmed != 0) {
				t.Fatalf("rev1 result: %v", err)
			}
		})
	}
}

func TestBIOSCommandsValidateBeforeOpeningSerial(t *testing.T) {
	image := writeUploadFile(t, "bios.bin", make([]byte, protocol.BIOSImageSize))
	short := writeUploadFile(t, "short.bin", []byte{0})
	for _, args := range [][]string{
		{"bios-flasher-mode", "--crc16", "0x1234"}, {"bios-flasher-mode", "extra"},
		{"bios-read", "extra"}, {"bios-read", "--verify", short}, {"bios-read", "--output", image},
		{"bios-write"}, {"bios-write", short}, {"bios-write", "--timeout", "0s", image},
		{"bios-write", "--timeout", "-1s", image}, {"bios-write", "--timeout", "invalid", image},
	} {
		if err := runWithSerial(args, func(string) (io.ReadWriteCloser, error) {
			t.Fatal("invalid BIOS command opened serial")
			return nil, nil
		}); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	cmd, err := prepareCommand([]string{"bios-write", "--reset", "--timeout", "30s", image})
	if err != nil || !cmd.biosWrite.resetAfter || cmd.biosWrite.timeout.String() != "30s" || len(cmd.biosWrite.data) != protocol.BIOSImageSize {
		t.Fatalf("flash options: %+v, %v", cmd.biosWrite, err)
	}
	for _, args := range [][]string{{"bios-flasher-mode"}, {"bios-read"}, {"bios-read", "--verify", image}} {
		if _, err := prepareCommand(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
}

func TestBIOSReadAndWriteCLI(t *testing.T) {
	image := writeUploadFile(t, "bios.bin", memoryPattern(0, protocol.BIOSImageSize))
	output := filepath.Join(t.TempDir(), "readback.bin")
	s := newBIOSSerial()
	if err := runWithSerial([]string{"bios-read", "--output", output, "--verify", image}, func(string) (io.ReadWriteCloser, error) { return s, nil }); err != nil {
		t.Fatal(err)
	}
	s = newBIOSSerial()
	s.writable = true
	if err := runWithSerial([]string{"bios-write", "--reset", image}, func(string) (io.ReadWriteCloser, error) { return s, nil }); err != nil {
		t.Fatal(err)
	}
	if s.resetCount != 1 {
		t.Fatal("CLI did not reset after verification")
	}
}
