package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func TestPayloadOnlyHeader(t *testing.T) {
	packet, err := Encode(Frame{Command: ProgramExec, Sequence: 42, Data: []byte{2}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := cobsDecode(packet[1 : len(packet)-1])
	if err != nil || len(raw) != 8 || !bytes.Equal(raw[:6], []byte{1, 0x0e, 42, 1, 0, 2}) {
		t.Fatalf("unexpected frame: % x; error=%v", raw, err)
	}
}

func TestPayloadLengthBounds(t *testing.T) {
	for _, size := range []int{0, 1, 127, 128} {
		want := bytes.Repeat([]byte{0xff}, size)
		packet, err := Encode(Frame{Command: ProgramUploadData, Data: want})
		if err != nil {
			t.Fatal(err)
		}
		got, err := Decode(packet)
		if err != nil || !bytes.Equal(got.Data, want) {
			t.Fatalf("size %d: %v", size, err)
		}
	}
	if _, err := Encode(Frame{Data: make([]byte, 129)}); !errors.Is(err, ErrLength) {
		t.Fatalf("oversized payload: %v", err)
	}
	// A valid CRC must not excuse an incorrect payload length.
	raw := []byte{1, 1, 0, 1, 0, 0, 0}
	binary.LittleEndian.PutUint16(raw[5:], CRC16(raw[:5]))
	packet := append(append([]byte{0}, cobsEncode(raw)...), 0)
	if _, err := Decode(packet); !errors.Is(err, ErrLength) {
		t.Fatalf("invalid length: %v", err)
	}
}

func TestCommandValues(t *testing.T) {
	commands := []byte{
		Ping, Info, Reset,
		BIOSFlasherMode, BIOSWrite, BIOSRead,
		MemoryRead, MemoryWrite,
		ProgramList, ProgramUploadBegin, ProgramUploadData, ProgramUploadCommit, ProgramUploadAbort, ProgramExec, ProgramDelete, ProgramRename,
		KeyboardMode, KeyboardEvent,
	}
	for i, command := range commands {
		if want := byte(i + 1); command != want {
			t.Fatalf("command index %d: got %#02x, want %#02x", i, command, want)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	want := Frame{Command: ProgramUploadData, Sequence: 7, Data: []byte{0, 1, 2, 0, 255}}
	packet, err := Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(packet)
	if err != nil {
		t.Fatal(err)
	}
	if got.Command != want.Command || got.Sequence != want.Sequence || string(got.Data) != string(want.Data) {
		t.Fatalf("round trip mismatch: %#v", got)
	}
}

func TestCRC(t *testing.T) {
	if got := CRC16([]byte("123456789")); got != 0x29b1 {
		t.Fatalf("CRC16 = %#x", got)
	}
}
