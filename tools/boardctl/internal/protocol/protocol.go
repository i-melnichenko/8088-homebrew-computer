package protocol

import (
	"encoding/binary"
	"errors"
)

const (
	Version              = 2 // binary framing major
	Revision             = "2.0"
	MaxPayloadLength     = 128
	MaxMemoryReadLength  = MaxPayloadLength - 1 // response starts with status
	MaxMemoryWriteLength = MaxPayloadLength - 4 // request starts with address
	BIOSImageSize        = 32768
	BIOSAddress          = 0xf8000 // canonical EEPROM window on both revisions
	MaxBIOSReadLength    = MaxPayloadLength - 1
	MaxBIOSWriteLength   = MaxPayloadLength - 4

	Ping  byte = 0x01
	Info  byte = 0x02
	Reset byte = 0x03

	BIOSFlasherMode byte = 0x04
	BIOSWrite       byte = 0x05
	BIOSRead        byte = 0x06

	MemoryRead  byte = 0x07
	MemoryWrite byte = 0x08

	KeyboardMode        byte = 0x09
	KeyboardEvent       byte = 0x0a
	KeyboardFlagRunning byte = 1

	TerminalPoll         byte = 0x0d
	IRQTest              byte = 0x0e
	StatusPICMissing     byte = 21
	StatusPITMissing     byte = 22
	StatusRAM0Required   byte = 23
	StatusPITNotCounting byte = 24

	RAMInfo          byte = 0x0b
	RAMTest          byte = 0x0c
	RAMTestConfirm   byte = 0xa5
	RAMFlagHeartbeat byte = 2
)

var (
	ErrVersion = errors.New("unsupported UART protocol; boardctl requires 2.0")
	ErrFrame   = errors.New("invalid protocol frame")
	ErrCRC     = errors.New("protocol CRC mismatch")
	ErrLength  = errors.New("invalid protocol data length")
)

type Frame struct {
	Command  byte
	Sequence byte
	Data     []byte
}

func Encode(f Frame) ([]byte, error) { return EncodeVersion(f, Version) }

// EncodeVersion is used only by the EEPROM upgrade transport.
func EncodeVersion(f Frame, version byte) ([]byte, error) {
	if len(f.Data) > MaxPayloadLength {
		return nil, ErrLength
	}
	raw := make([]byte, 5+len(f.Data)+2)
	raw[0], raw[1], raw[2] = version, f.Command, f.Sequence
	binary.LittleEndian.PutUint16(raw[3:5], uint16(len(f.Data)))
	copy(raw[5:], f.Data)
	binary.LittleEndian.PutUint16(raw[len(raw)-2:], CRC16(raw[:len(raw)-2]))
	encoded := cobsEncode(raw)
	return append(append([]byte{0}, encoded...), 0), nil
}

func Decode(packet []byte) (Frame, error) {
	f, version, err := DecodeAnyVersion(packet)
	if err != nil {
		return Frame{}, err
	}
	if version != Version {
		return Frame{}, ErrVersion
	}
	return f, nil
}

// DecodeAnyVersion validates framing, bounds and CRC before exposing a version.
// Only the EEPROM upgrade transport may accept a different major version.
func DecodeAnyVersion(packet []byte) (Frame, byte, error) {
	if len(packet) < 2 || packet[0] != 0 || packet[len(packet)-1] != 0 {
		return Frame{}, 0, ErrFrame
	}
	raw, err := cobsDecode(packet[1 : len(packet)-1])
	if err != nil || len(raw) < 7 {
		return Frame{}, 0, ErrFrame
	}
	n := int(binary.LittleEndian.Uint16(raw[3:5]))
	if n > MaxPayloadLength || len(raw) != 5+n+2 {
		return Frame{}, 0, ErrLength
	}
	if CRC16(raw[:len(raw)-2]) != binary.LittleEndian.Uint16(raw[len(raw)-2:]) {
		return Frame{}, 0, ErrCRC
	}
	return Frame{Command: raw[1], Sequence: raw[2], Data: append([]byte(nil), raw[5:len(raw)-2]...)}, raw[0], nil
}

func CRC16(data []byte) uint16 {
	crc := uint16(0xffff)
	for _, b := range data {
		crc ^= uint16(b) << 8
		for bit := 0; bit < 8; bit++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func cobsEncode(in []byte) []byte {
	out := make([]byte, 1, len(in)+len(in)/254+1)
	codePos, code := 0, byte(1)
	for _, b := range in {
		if b == 0 {
			out[codePos] = code
			codePos = len(out)
			out = append(out, 0)
			code = 1
			continue
		}
		out = append(out, b)
		code++
		if code == 0xff {
			out[codePos] = code
			codePos = len(out)
			out = append(out, 0)
			code = 1
		}
	}
	out[codePos] = code
	return out
}

func cobsDecode(in []byte) ([]byte, error) {
	out := make([]byte, 0, len(in))
	for i := 0; i < len(in); {
		code := int(in[i])
		i++
		if code == 0 || i+code-1 > len(in) {
			return nil, ErrFrame
		}
		out = append(out, in[i:i+code-1]...)
		i += code - 1
		if code != 0xff && i < len(in) {
			out = append(out, 0)
		}
	}
	return out, nil
}

const (
	DiskMount     byte = 0x0f
	DiskBoot      byte = 0x10
	DiskPoll      byte = 0x11
	DiskData      byte = 0x12
	DiskWriteData byte = 0x13
	DiskWriteAck  byte = 0x14
)
