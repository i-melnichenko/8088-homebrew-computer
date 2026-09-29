package apps

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/client"
	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/protocol"
)

type dumpOptions struct {
	address uint32
	size    uint32
	output  string
}

type writeOptions struct {
	address uint32
	data    []byte
}

func validateWriteRange(address uint32, size int) error {
	// A15/A16 are not connected to SRAM: validate the backing bytes, not just
	// the apparent physical address, to protect all mirrors of BIOS state.
	start := uint64(address & 0x7fff)
	if size <= 0 || address >= 0x20000 || start < payloadBase&0x7fff ||
		start+uint64(size) > payloadLimit&0x7fff {
		return errors.New("write range must lie entirely in payload RAM 0x8800..0xfbff or its aliases; ROM, BIOS RAM and stacks are protected")
	}
	return nil
}

func writeMemory(c *client.Client, address uint32, data []byte) error {
	if err := validateWriteRange(address, len(data)); err != nil {
		return err
	}
	expectedHash := sha256.Sum256(data)
	for offset := 0; offset < len(data); {
		n := min(protocol.MaxMemoryWriteLength, len(data)-offset)
		payload := make([]byte, 4+n)
		binary.LittleEndian.PutUint32(payload, address+uint32(offset))
		copy(payload[4:], data[offset:offset+n])
		response, err := c.Transact(protocol.Frame{Command: protocol.MemoryWrite, Data: payload})
		if err := checkResponse(response, err); err != nil {
			return fmt.Errorf("write memory at %#x: %w", address+uint32(offset), err)
		}
		if len(response.Data) != 1 {
			return errors.New("invalid memory write response")
		}
		offset += n
	}
	ram, err := readMemory(c, address, uint32(len(data)))
	if err != nil {
		return fmt.Errorf("verify memory write: %w", err)
	}
	actualHash := sha256.Sum256(ram)
	if actualHash != expectedHash {
		return fmt.Errorf("RAM SHA-256 mismatch: PC=%x RAM=%x", expectedHash, actualHash)
	}
	return nil
}

func validateDumpRange(address, size uint32) error {
	end := uint64(address) + uint64(size)
	if size == 0 || address >= 0x20000 || end > 0x20000 {
		return errors.New("dump range must lie entirely in RAM 0x00000..0x1ffff")
	}
	return nil
}

func readMemory(c *client.Client, address, size uint32) ([]byte, error) {
	if err := validateDumpRange(address, size); err != nil {
		return nil, err
	}
	data := make([]byte, 0, int(size))
	for uint32(len(data)) < size {
		n := min(uint32(protocol.MaxMemoryReadLength), size-uint32(len(data)))
		addr := address + uint32(len(data))
		payload := make([]byte, 6)
		binary.LittleEndian.PutUint32(payload, addr)
		binary.LittleEndian.PutUint16(payload[4:], uint16(n))
		response, err := c.Transact(protocol.Frame{Command: protocol.MemoryRead, Data: payload})
		if err := checkResponse(response, err); err != nil {
			return nil, fmt.Errorf("read memory at %#x: %w", addr, err)
		}
		if len(response.Data) != int(n)+1 {
			return nil, fmt.Errorf("invalid memory response at %#x: got %d bytes, expected %d", addr, len(response.Data)-1, n)
		}
		data = append(data, response.Data[1:]...)
	}
	return data, nil
}

func dumpMemory(c *client.Client, options dumpOptions, out io.Writer) error {
	data, err := readMemory(c, options.address, options.size)
	if err != nil {
		return err
	}
	if options.output != "" {
		// Do not truncate an existing file, or create one for a failed read.
		file, err := os.OpenFile(options.output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return fmt.Errorf("create dump file: %w", err)
		}
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		return errors.Join(writeErr, closeErr)
	}
	return writeHexDump(out, options.address, data)
}

func writeHexDump(out io.Writer, address uint32, data []byte) error {
	for offset := 0; offset < len(data); offset += 16 {
		row := data[offset:min(offset+16, len(data))]
		line := fmt.Sprintf("%05x  ", address+uint32(offset))
		for i := 0; i < 16; i++ {
			if i < len(row) {
				line += fmt.Sprintf("%02x ", row[i])
			} else {
				line += "   "
			}
			if i == 7 {
				line += " "
			}
		}
		line += " |"
		for _, b := range row {
			if b < 0x20 || b > 0x7e {
				b = '.'
			}
			line += string(b)
		}
		if _, err := fmt.Fprintln(out, line+"|"); err != nil {
			return err
		}
	}
	return nil
}
