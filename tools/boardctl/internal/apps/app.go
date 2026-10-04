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

const defaultPort = "/dev/cu.usbserial-110"
const payloadLimit = 0xfc00
const payloadBase = 0x8800
const maxPayloadSize = payloadLimit - payloadBase

// Run validates command-line arguments and executes the requested board command.
func Run(args []string) error {
	return runWithSerial(args, func(port string) (io.ReadWriteCloser, error) {
		return openSerial(port)
	})
}

func runWithSerial(args []string, open func(string) (io.ReadWriteCloser, error)) error {
	cmd, err := prepareCommand(args)
	if err != nil {
		return err
	}
	serial, err := open(cmd.port)
	if err != nil {
		return fmt.Errorf("open serial port: %w", err)
	}
	defer serial.Close()
	c := client.New(serial, os.Stderr)

	switch cmd.name {
	case "ping":
		if err := checkStatusReply(c.Transact(protocol.Frame{Command: protocol.Ping})); err != nil {
			return err
		}
		fmt.Println("PONG")
	case "bios-flasher-mode":
		if err := enterBIOSFlasherMode(c); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "boardctl: EEPROM mode ready; resident programs and keyboard sharing cleared")
	case "bios-read":
		data, err := readBIOS(c, cmd.biosRead)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "boardctl: read %d EEPROM bytes; SHA-256=%x\n", len(data), sha256.Sum256(data))
		if cmd.biosRead.expected != nil {
			fmt.Fprintln(os.Stderr, "boardctl: EEPROM SHA-256 verified")
		}
	case "bios-write":
		if err := writeBIOS(c, cmd.biosWrite, func(done, total int) {
			if done%1024 == 0 || done == total {
				fmt.Fprintf(os.Stderr, "boardctl: flashed %d/%d bytes\n", done, total)
			}
		}); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "boardctl: EEPROM SHA-256 verified")
		if cmd.biosWrite.resetAfter {
			fmt.Fprintln(os.Stderr, "boardctl: reset acknowledged")
		}
	case "keyboard":
		if err := keyboard(c); err != nil {
			return fmt.Errorf("keyboard: %w", err)
		}
	case "console":
		return console(serial)
	case "info":
		frame, err := c.Transact(protocol.Frame{Command: protocol.Ping})
		if err := checkResponse(frame, err); err != nil {
			return fmt.Errorf("PING: %w", err)
		}
		frame, err = c.Transact(protocol.Frame{Command: protocol.Info})
		if err := checkResponse(frame, err); err != nil {
			return fmt.Errorf("INFO requires the menu BIOS; update firmware: %w", err)
		}
		fmt.Println(string(frame.Data[1:]))
	case "upload":
		return upload(c, cmd.upload)
	case "dump":
		return dumpMemory(c, cmd.dump, os.Stdout)
	case "write":
		if err := writeMemory(c, cmd.write.address, cmd.write.data); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "boardctl: wrote %d bytes at %#x; RAM SHA-256 verified\n", len(cmd.write.data), cmd.write.address)
	case "programs":
		frame, err := c.Transact(protocol.Frame{Command: protocol.ProgramList})
		if err != nil {
			return err
		}
		programs, err := decodePrograms(frame.Data)
		if err != nil {
			return err
		}
		for _, p := range programs {
			fmt.Printf("%d  %-15s  %#04x  %d bytes\n", p.ID, p.Name, p.Address, p.Size)
		}
		if len(programs) == 0 {
			fmt.Println("No programs loaded")
		}
	case "run":
		return run(c, cmd.programID)
	case "delete":
		if err := deleteProgram(c, cmd.programID); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "boardctl: program deleted; use programs to refresh IDs")
	case "rename":
		if err := renameProgram(c, cmd.programID, cmd.programName); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "boardctl: program %d renamed to %q\n", cmd.programID, cmd.programName)
	case "reset":
		return checkResponse(c.Transact(protocol.Frame{Command: protocol.Reset}))
	}
	return nil
}

func console(serial io.ReadWriter) error {
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := serial.Read(buf)
			if n > 0 {
				_, _ = os.Stdout.Write(buf[:n])
			}
			if err != nil && !errors.Is(err, io.EOF) {
				return
			}
		}
	}()
	_, err := io.Copy(serial, os.Stdin)
	return err
}

func upload(c *client.Client, options uploadOptions) error {
	p, err := uploadData(c, options.name, options.data, func(done, total int) {
		fmt.Fprintf(os.Stderr, "boardctl: uploaded %d/%d bytes\n", done, total)
		if done == total {
			fmt.Fprintln(os.Stderr, "boardctl: verifying RAM SHA-256...")
		}
	})
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "boardctl: RAM SHA-256 verified")
	fmt.Fprintf(os.Stderr, "boardctl: program %d %q at %#x\n", p.ID, p.Name, p.Address)
	if options.runAfter {
		fmt.Fprintln(os.Stderr, "boardctl: starting program")
		if err := run(c, uint32(p.ID)); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "boardctl: program started")
	}
	return nil
}

func validateUpload(name string, size int) error {
	if size <= 0 || size > maxPayloadSize {
		return fmt.Errorf("upload size must be 1 through %d bytes", maxPayloadSize)
	}
	return validateName(name)
}

func uploadData(c *client.Client, name string, data []byte, progress func(int, int)) (p program, err error) {
	if err := validateUpload(name, len(data)); err != nil {
		return p, err
	}
	total := len(data)
	expectedHash := sha256.Sum256(data)
	size := make([]byte, 6)
	binary.LittleEndian.PutUint32(size, uint32(len(data)))
	binary.LittleEndian.PutUint16(size[4:], protocol.CRC16(data))
	response, err := c.Transact(protocol.Frame{Command: protocol.ProgramUploadBegin, Data: append(size, name...)})
	if err := checkResponse(response, err); err != nil {
		return p, fmt.Errorf("begin upload: %w", err)
	}
	if len(response.Data) != 6 {
		return p, errors.New("invalid allocation response")
	}
	p = program{ID: response.Data[1], Address: binary.LittleEndian.Uint32(response.Data[2:]), Size: uint16(total), Name: name}
	if err := validateProgram(p); err != nil {
		return p, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = c.TransactAttempts(protocol.Frame{Command: protocol.ProgramUploadAbort, Data: []byte{p.ID}}, 1)
		}
	}()
	for len(data) > 0 {
		n := len(data)
		if n > protocol.MaxUploadDataLength {
			n = protocol.MaxUploadDataLength
		}
		response, err := c.Transact(protocol.Frame{Command: protocol.ProgramUploadData, Data: data[:n]})
		if err := checkResponse(response, err); err != nil {
			return p, fmt.Errorf("upload block at offset %d: %w", total-len(data), err)
		}
		data = data[n:]
		if progress != nil {
			progress(total-len(data), total)
		}
	}
	// Read back the complete BIOS-assigned range before publishing the slot.
	// Both CLI uploads and keyboard sharing use this same verification path.
	ram, err := readMemory(c, p.Address, uint32(total))
	if err != nil {
		return p, fmt.Errorf("verify upload: %w", err)
	}
	actualHash := sha256.Sum256(ram)
	if actualHash != expectedHash {
		return p, fmt.Errorf("RAM SHA-256 mismatch: PC=%x RAM=%x", expectedHash, actualHash)
	}
	response, err = c.Transact(protocol.Frame{Command: protocol.ProgramUploadCommit, Data: []byte{p.ID}})
	if err := checkResponse(response, err); err != nil {
		return p, fmt.Errorf("commit upload: %w", err)
	}
	committed = true
	return p, nil
}

func checkResponse(f protocol.Frame, err error) error {
	if err != nil {
		return err
	}
	if len(f.Data) == 0 {
		return errors.New("missing status in BIOS response")
	}
	if f.Data[0] != 0 {
		descriptions := map[byte]string{
			9: "image CRC mismatch", 10: "memory check busy", 11: "program busy",
			15: "upload busy", 17: "unsupported operation", 18: "invalid BIOS request/range/state",
			19: "EEPROM programming or readback failure", 20: "EEPROM mode active",
		}
		if description := descriptions[f.Data[0]]; description != "" {
			return fmt.Errorf("BIOS status %d (%s)", f.Data[0], description)
		}
		return fmt.Errorf("BIOS status %d", f.Data[0])
	}
	return nil
}

func run(c *client.Client, id uint32) error {
	if id < 1 || id > 4 {
		return errors.New("program ID must be 1..4")
	}
	// A new invocation is not a retransmission of an earlier PROGRAM_EXEC.
	response, err := c.Transact(protocol.Frame{Command: protocol.Ping})
	if err := checkResponse(response, err); err != nil {
		return err
	}
	response, err = c.Transact(protocol.Frame{Command: protocol.ProgramExec, Data: []byte{byte(id)}})
	if err := checkResponse(response, err); err != nil {
		return fmt.Errorf("board rejected PROGRAM_EXEC: %w", err)
	}
	return nil
}
