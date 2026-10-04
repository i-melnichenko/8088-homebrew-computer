package apps

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/client"
	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/protocol"
)

const defaultBIOSWriteTimeout = 10 * time.Second
const biosWriteBlockSize = 64 // aligned AT28C256 pages; below the wire limit

type biosReadOptions struct {
	output   string
	expected []byte
}

type biosWriteOptions struct {
	data       []byte
	resetAfter bool
	timeout    time.Duration
}

func validateBIOSImage(data []byte) error {
	if len(data) != protocol.BIOSImageSize {
		return fmt.Errorf("BIOS image must be exactly %d bytes, including padding and reset vector", protocol.BIOSImageSize)
	}
	return nil
}

func validateBIOSRange(address uint32, size int) error {
	if size <= 0 || address < protocol.BIOSAddress ||
		uint64(address)+uint64(size) > protocol.BIOSAddress+protocol.BIOSImageSize {
		return errors.New("BIOS range must lie entirely in EEPROM 0xf8000..0xfffff")
	}
	return nil
}

func readBIOSFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read BIOS file: %w", err)
	}
	return data, validateBIOSImage(data)
}

func checkStatusReply(frame protocol.Frame, err error) error {
	if err := checkResponse(frame, err); err != nil {
		return err
	}
	if len(frame.Data) != 1 {
		return errors.New("invalid status-only BIOS response")
	}
	return nil
}

func enterBIOSFlasherMode(c *client.Client) error {
	if err := checkStatusReply(c.Transact(protocol.Frame{Command: protocol.BIOSFlasherMode})); err != nil {
		return fmt.Errorf("enter EEPROM mode: %w", err)
	}
	return nil
}

func prepareBIOSFlasherMode(c *client.Client, allowExisting bool) error {
	// A new host process cannot distinguish its ENTER from an old lost-ACK retry
	// using the wrapping sequence alone. Probe READ before entering: it is only
	// supported in RAM mode. Never adopt an existing session for a new write.
	request := make([]byte, 6)
	binary.LittleEndian.PutUint32(request, protocol.BIOSAddress)
	binary.LittleEndian.PutUint16(request[4:], 1)
	response, err := c.Transact(protocol.Frame{Command: protocol.BIOSRead, Data: request})
	if err != nil {
		return fmt.Errorf("check EEPROM mode: %w", err)
	}
	if len(response.Data) == 2 && response.Data[0] == 0 {
		if allowExisting {
			return nil
		}
		return errors.New("EEPROM mode is already active; use bios-read to inspect it and reset to return to BIOS before a new flash")
	}
	if len(response.Data) == 1 && response.Data[0] == 17 {
		return enterBIOSFlasherMode(c)
	}
	if err := checkResponse(response, nil); err != nil {
		return fmt.Errorf("check EEPROM mode: %w", err)
	}
	return errors.New("invalid EEPROM mode probe response")
}

func readEEPROM(c *client.Client) ([]byte, error) {
	data := make([]byte, 0, protocol.BIOSImageSize)
	for len(data) < protocol.BIOSImageSize {
		n := min(protocol.MaxBIOSReadLength, protocol.BIOSImageSize-len(data))
		payload := make([]byte, 6)
		address := uint32(protocol.BIOSAddress + len(data))
		if err := validateBIOSRange(address, n); err != nil {
			return nil, err
		}
		binary.LittleEndian.PutUint32(payload, address)
		binary.LittleEndian.PutUint16(payload[4:], uint16(n))
		response, err := c.Transact(protocol.Frame{Command: protocol.BIOSRead, Data: payload})
		if err := checkResponse(response, err); err != nil {
			return nil, fmt.Errorf("read EEPROM at %#x: %w", address, err)
		}
		if len(response.Data) != n+1 {
			return nil, fmt.Errorf("invalid EEPROM response at %#x: got %d bytes, expected %d", address, len(response.Data)-1, n)
		}
		data = append(data, response.Data[1:]...)
	}
	return data, nil
}

func verifyBIOS(data, expected []byte) error {
	actualHash, expectedHash := sha256.Sum256(data), sha256.Sum256(expected)
	if actualHash != expectedHash {
		return fmt.Errorf("EEPROM SHA-256 mismatch: PC=%x EEPROM=%x", expectedHash, actualHash)
	}
	return nil
}

func readBIOS(c *client.Client, options biosReadOptions) ([]byte, error) {
	if options.expected != nil {
		if err := validateBIOSImage(options.expected); err != nil {
			return nil, err
		}
	}
	if err := prepareBIOSFlasherMode(c, true); err != nil {
		return nil, err
	}
	data, err := readEEPROM(c)
	if err != nil {
		return nil, err
	}
	if options.expected != nil {
		if err := verifyBIOS(data, options.expected); err != nil {
			return nil, err
		}
	}
	if options.output != "" {
		file, err := os.OpenFile(options.output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, fmt.Errorf("create BIOS output: %w", err)
		}
		_, writeErr := file.Write(data)
		if err := errors.Join(writeErr, file.Close()); err != nil {
			return nil, fmt.Errorf("save BIOS output: %w", err)
		}
	}
	return data, nil
}

func writeBIOS(c *client.Client, options biosWriteOptions, progress func(int, int)) error {
	if err := validateBIOSImage(options.data); err != nil {
		return err
	}
	if options.timeout <= 0 {
		return errors.New("BIOS flash timeout must be positive")
	}
	if err := prepareBIOSFlasherMode(c, false); err != nil {
		return err
	}
	for offset := 0; offset < len(options.data); {
		n := min(biosWriteBlockSize, len(options.data)-offset)
		payload := make([]byte, 4+n)
		address := uint32(protocol.BIOSAddress + offset)
		if err := validateBIOSRange(address, n); err != nil {
			return err
		}
		binary.LittleEndian.PutUint32(payload, address)
		copy(payload[4:], options.data[offset:offset+n])
		err := checkStatusReply(c.TransactTimeout(protocol.Frame{Command: protocol.BIOSWrite, Data: payload}, options.timeout))
		if err != nil {
			return fmt.Errorf("flash EEPROM at %#x: %w; board remains in EEPROM mode, RESET was not sent", address, err)
		}
		offset += n
		if progress != nil {
			progress(offset, len(options.data))
		}
	}
	readback, err := readEEPROM(c)
	if err != nil {
		return fmt.Errorf("verify EEPROM: %w; RESET was not sent", err)
	}
	if err := verifyBIOS(readback, options.data); err != nil {
		return fmt.Errorf("%w; RESET was not sent", err)
	}
	if options.resetAfter {
		if err := checkStatusReply(c.Transact(protocol.Frame{Command: protocol.Reset})); err != nil {
			return fmt.Errorf("reset after EEPROM verification: %w", err)
		}
	}
	return nil
}
