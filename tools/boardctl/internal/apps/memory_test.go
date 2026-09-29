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

type memorySerial struct {
	requests []protocol.Frame
	packet   []byte
	response func(uint32, int) []byte
}

func (s *memorySerial) Write(p []byte) (int, error) {
	f, err := protocol.Decode(p)
	if err != nil {
		return 0, err
	}
	if f.Command != protocol.MemoryRead || len(f.Data) != 6 {
		return 0, errors.New("invalid MEMORY_READ request")
	}
	addr, n := binary.LittleEndian.Uint32(f.Data), int(binary.LittleEndian.Uint16(f.Data[4:]))
	if n < 1 || n > protocol.MaxMemoryReadLength {
		return 0, errors.New("invalid MEMORY_READ size")
	}
	s.requests = append(s.requests, f)
	data := append([]byte{0}, memoryPattern(addr, n)...)
	if s.response != nil {
		data = s.response(addr, n)
	}
	s.packet, err = protocol.Encode(protocol.Frame{Command: f.Command | 0x80, Sequence: f.Sequence, Data: data})
	return len(p), err
}

func (s *memorySerial) Read(p []byte) (int, error) {
	if len(s.packet) == 0 {
		return 0, io.EOF
	}
	n := copy(p, s.packet)
	s.packet = s.packet[n:]
	return n, nil
}

func (s *memorySerial) Close() error { return nil }

func memoryPattern(address uint32, size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(address + uint32(i))
	}
	return data
}

func TestReadMemoryChunks(t *testing.T) {
	for _, address := range []uint32{0, 0x8800, 0x19000} {
		s := &memorySerial{}
		data, err := readMemory(client.New(s, nil), address, 256)
		if err != nil || !bytes.Equal(data, memoryPattern(address, 256)) {
			t.Fatalf("dump at %#x: %x, %v", address, data, err)
		}
		if len(s.requests) != 3 {
			t.Fatalf("got %d chunks, expected 3", len(s.requests))
		}
		for i, n := range []uint16{127, 127, 2} {
			f := s.requests[i]
			if binary.LittleEndian.Uint32(f.Data) != address+uint32(i*127) || binary.LittleEndian.Uint16(f.Data[4:]) != n {
				t.Fatalf("chunk %d: %x", i, f.Data)
			}
		}
	}
}

func TestReadMemoryRangesAndReplies(t *testing.T) {
	for _, r := range [][2]uint32{{0, 0x20000}, {0x1ffff, 1}, {0x18000, 0x8000}} {
		if err := validateDumpRange(r[0], r[1]); err != nil {
			t.Fatalf("valid range %x: %v", r, err)
		}
	}
	for _, r := range [][2]uint32{{0, 0}, {0x1ffff, 2}, {0x20000, 1}, {0xf7fff, 2},
		{0xf8000, 0x8000}, {0xffff0, 16}, {0xfffff, 1}, {0xfffff, 2}, {0xffffffff, 2}} {
		s := &memorySerial{}
		if _, err := readMemory(client.New(s, nil), r[0], r[1]); err == nil || len(s.requests) != 0 {
			t.Fatalf("invalid range %x: %v", r, err)
		}
	}
	for _, reply := range [][]byte{nil, {14}, {10}, {11}, {0}, {0, 1, 2}} {
		s := &memorySerial{response: func(uint32, int) []byte { return reply }}
		if _, err := readMemory(client.New(s, nil), 0x8800, 1); err == nil {
			t.Fatalf("accepted invalid response %x", reply)
		}
	}
}

func TestDumpFileAndHex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ram.bin")
	s := &memorySerial{}
	options := dumpOptions{address: 0x8800, size: 256, output: path}
	if err := dumpMemory(client.New(s, nil), options, io.Discard); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, memoryPattern(options.address, 256)) {
		t.Fatalf("binary dump: %x, %v", data, err)
	}
	if err := dumpMemory(client.New(s, nil), options, io.Discard); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing file: %v", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, data) {
		t.Fatal("existing dump was overwritten")
	}
	options.output = filepath.Join(t.TempDir(), "failed.bin")
	s.response = func(uint32, int) []byte { return []byte{14} }
	if err := dumpMemory(client.New(s, nil), options, io.Discard); err == nil {
		t.Fatal("failed read accepted")
	}
	if _, err := os.Stat(options.output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed read created a dump: %v", err)
	}
	var out bytes.Buffer
	s = &memorySerial{}
	if err := dumpMemory(client.New(s, nil), dumpOptions{address: 0x8800, size: 17}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "08800  00 01 02") || !strings.Contains(out.String(), "08810  10") {
		t.Fatalf("hex addresses: %q", out.String())
	}
	out.Reset()
	if err := writeHexDump(&out, 0xffff0, []byte{'A', 0, 255, '~'}); err != nil || !strings.Contains(out.String(), "|A..~|") {
		t.Fatalf("hex ASCII: %q, %v", out.String(), err)
	}
}

func TestDumpCommandWritesBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ram.bin")
	s := &memorySerial{}
	err := runWithSerial([]string{"dump", "--addr", "0x19000", "--size", "16", "--output", path},
		func(string) (io.ReadWriteCloser, error) { return s, nil })
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, memoryPattern(0x19000, 16)) {
		t.Fatalf("CLI dump: %x, %v", data, err)
	}
}

type writeSerial struct {
	memorySerial
	writes        []protocol.Frame
	ram           map[uint32]byte
	writeResponse []byte
	corrupt       bool
}

func newWriteSerial() *writeSerial {
	s := &writeSerial{ram: make(map[uint32]byte)}
	s.response = func(address uint32, size int) []byte {
		data := make([]byte, size+1)
		for i := 0; i < size; i++ {
			data[i+1] = s.ram[(address+uint32(i))&0x7fff]
		}
		if s.corrupt {
			data[1] ^= 1
		}
		return data
	}
	return s
}

func (s *writeSerial) Write(p []byte) (int, error) {
	f, err := protocol.Decode(p)
	if err != nil {
		return 0, err
	}
	if f.Command == protocol.MemoryRead {
		return s.memorySerial.Write(p)
	}
	if f.Command != protocol.MemoryWrite || len(f.Data) < 5 || len(f.Data) > 128 {
		return 0, errors.New("invalid MEMORY_WRITE request")
	}
	address := binary.LittleEndian.Uint32(f.Data)
	if err := validateWriteRange(address, len(f.Data)-4); err != nil {
		return 0, err
	}
	s.writes = append(s.writes, f)
	for i, b := range f.Data[4:] {
		s.ram[(address+uint32(i))&0x7fff] = b
	}
	status := []byte{0}
	if s.writeResponse != nil {
		status = s.writeResponse
	}
	s.packet, err = protocol.Encode(protocol.Frame{Command: f.Command | 0x80, Sequence: f.Sequence, Data: status})
	return len(p), err
}

func TestWriteMemoryChunksAliasesAndVerification(t *testing.T) {
	for _, address := range []uint32{0x1000, 0x9000, 0x11000, 0x19000} {
		s := newWriteSerial()
		data := memoryPattern(address, 257)
		if err := writeMemory(client.New(s, nil), address, data); err != nil {
			t.Fatal(err)
		}
		if len(s.writes) != 3 || len(s.requests) != 3 {
			t.Fatalf("writes=%d reads=%d", len(s.writes), len(s.requests))
		}
		for i, n := range []int{124, 124, 9} {
			f := s.writes[i]
			if binary.LittleEndian.Uint32(f.Data) != address+uint32(i*124) || !bytes.Equal(f.Data[4:], data[i*124:i*124+n]) {
				t.Fatalf("write chunk %d: %x", i, f.Data)
			}
		}
	}
}

func TestWriteMemoryProtectsRangesAndHandlesFailures(t *testing.T) {
	for _, base := range []uint32{0, 0x8000, 0x10000, 0x18000} {
		for _, offset := range []uint32{0, 0x400, 0x500, 0x700, 0x7ff, 0x7bff, 0x7c00, 0x7fff} {
			s := newWriteSerial()
			if err := writeMemory(client.New(s, nil), base+offset, []byte{1, 2}); err == nil || len(s.writes) != 0 {
				t.Fatalf("unprotected RAM alias %#x: %v", base+offset, err)
			}
		}
	}
	for _, address := range []uint32{0x20000, 0xf8000, 0xfffff, 0xffffffff} {
		s := newWriteSerial()
		if err := writeMemory(client.New(s, nil), address, []byte{1}); err == nil || len(s.writes) != 0 {
			t.Fatalf("non-RAM write %#x: %v", address, err)
		}
	}
	if err := validateWriteRange(0x9000, 0); err == nil {
		t.Fatal("empty write accepted")
	}
	for _, response := range [][]byte{{14}, {10}, {11}, {15}, {}, {0, 0}} {
		s := newWriteSerial()
		s.writeResponse = response
		if err := writeMemory(client.New(s, nil), 0x9000, []byte{1}); err == nil || len(s.requests) != 0 {
			t.Fatalf("accepted write response %x: %v", response, err)
		}
	}
	s := newWriteSerial()
	s.corrupt = true
	if err := writeMemory(client.New(s, nil), 0x9000, []byte{1, 2}); err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("write corruption accepted: %v", err)
	}
}

func TestWriteCommand(t *testing.T) {
	data := []byte{1, 2, 3}
	path := writeUploadFile(t, "patch.bin", data)
	s := newWriteSerial()
	if err := runWithSerial([]string{"write", "--addr", "0x9000", path},
		func(string) (io.ReadWriteCloser, error) { return s, nil }); err != nil {
		t.Fatal(err)
	}
	if len(s.writes) != 1 || !bytes.Equal(s.writes[0].Data[4:], data) {
		t.Fatal("CLI did not write patch")
	}
}
