package apps

import (
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/client"
	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/protocol"
)

type program struct {
	ID      byte
	Address uint32
	Size    uint16
	Name    string
}

func validateName(name string) error {
	if len(name) < 1 || len(name) > 15 || strings.TrimSpace(name) == "" {
		return errors.New("program name must contain 1..15 printable ASCII characters")
	}
	for _, b := range []byte(name) {
		if b < ' ' || b > '~' {
			return errors.New("program name must contain only printable ASCII characters")
		}
	}
	return nil
}

func defaultProgramName(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func validateProgram(p program) error {
	if p.ID < 1 || p.ID > 4 || p.Address < payloadBase || p.Address%16 != 0 || p.Size == 0 || uint64(p.Address)+uint64(p.Size) > payloadLimit {
		return errors.New("invalid BIOS program allocation")
	}
	return validateName(p.Name)
}

func decodePrograms(data []byte) ([]program, error) {
	if len(data) < 2 || data[0] != 0 || data[1] > 4 || len(data) != 2+int(data[1])*23 {
		return nil, errors.New("invalid BIOS program list")
	}
	result := make([]program, 0, int(data[1]))
	for offset := 2; offset < len(data); offset += 23 {
		rec := data[offset : offset+23]
		zero := -1
		for i, b := range rec[7:] {
			if b == 0 {
				zero = i
				break
			}
		}
		if zero < 0 {
			return nil, errors.New("unterminated BIOS program name")
		}
		p := program{ID: rec[0], Address: binary.LittleEndian.Uint32(rec[1:5]), Size: binary.LittleEndian.Uint16(rec[5:7]), Name: string(rec[7 : 7+zero])}
		if err := validateProgram(p); err != nil {
			return nil, err
		}
		if int(p.ID) != len(result)+1 {
			return nil, errors.New("invalid BIOS program order")
		}
		// First-fit allocation may place a newer ID into a deleted low-address
		// hole. Validate overlap rather than requiring address ordering.
		for _, other := range result {
			if p.Address < other.Address+uint32(other.Size) && other.Address < p.Address+uint32(p.Size) {
				return nil, errors.New("overlapping BIOS program allocations")
			}
		}
		result = append(result, p)
	}
	return result, nil
}

func deleteProgram(c *client.Client, id uint32) error {
	if id < 1 || id > 4 {
		return errors.New("program ID must be 1..4")
	}
	// A separate CLI invocation is a new deletion, not a retry with a reused
	// sequence. PING clears the BIOS's immediate-delete retry cache.
	if err := checkResponse(c.Transact(protocol.Frame{Command: protocol.Ping})); err != nil {
		return err
	}
	return registryCommand(c, protocol.ProgramDelete, []byte{byte(id)})
}

func renameProgram(c *client.Client, id uint32, name string) error {
	if id < 1 || id > 4 {
		return errors.New("program ID must be 1..4")
	}
	if err := validateName(name); err != nil {
		return err
	}
	return registryCommand(c, protocol.ProgramRename, append([]byte{byte(id)}, name...))
}

func registryCommand(c *client.Client, command byte, payload []byte) error {
	response, err := c.Transact(protocol.Frame{Command: command, Data: payload})
	if err := checkResponse(response, err); err != nil {
		return fmt.Errorf("program registry command %#02x: %w", command, err)
	}
	if len(response.Data) != 1 {
		return errors.New("invalid program registry response")
	}
	return nil
}
