package apps

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/client"
	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/protocol"
)

const defaultPort = "/dev/cu.usbserial-110"
const scratchLimit = 0xfc00
const scratchBase = 0x8800

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
	if cmd.name == "ram-test" {
		confirmed, err := confirmRAMTest(os.Stdin, os.Stdout)
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}
	serial, err := open(cmd.port)
	if err != nil {
		return fmt.Errorf("open serial port: %w", err)
	}
	defer serial.Close()
	var diagnostics io.Writer = os.Stderr
	if cmd.name == "terminal" || cmd.name == "keyboard" {
		diagnostics = terminalDiagnostics{os.Stderr}
	}
	c := client.New(serial, diagnostics)
	if cmd.name == "bios-write" || cmd.name == "bios-read" || cmd.name == "bios-flasher-mode" {
		c, err = client.NewFlasher(serial, diagnostics)
		if err != nil {
			return err
		}
	}

	switch cmd.name {
	case "irq-test":
		return runIRQTest(c, os.Stdout)
	case "ram":
		return showRAM(c, os.Stdout)
	case "ram-test":
		return runRAMTest(c, os.Stdout)
	case "ping":
		if err := checkStatusReply(c.Transact(protocol.Frame{Command: protocol.Ping})); err != nil {
			return err
		}
		fmt.Println("PONG")
	case "bios-flasher-mode":
		if err := enterBIOSFlasherMode(c); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "boardctl: EEPROM mode ready; runtime and keyboard sharing stopped")
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
	case "terminal":
		return terminal(c, cmd.terminal)
	case "keyboard":
		if err := keyboard(c, cmd.terminal); err != nil {
			return fmt.Errorf("keyboard: %w", err)
		}
	case "console":
		if err := checkStatusReply(c.Transact(protocol.Frame{Command: protocol.Ping})); err != nil {
			return err
		}
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
		fmt.Print(formatInfo(string(frame.Data[1:])))
	case "dump":
		return dumpMemory(c, cmd.dump, os.Stdout)
	case "write":
		if err := writeMemory(c, cmd.write.address, cmd.write.data); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "boardctl: wrote %d bytes at %#x; RAM SHA-256 verified\n", len(cmd.write.data), cmd.write.address)
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

func checkResponse(f protocol.Frame, err error) error {
	if err != nil {
		return err
	}
	if len(f.Data) == 0 {
		return errors.New("missing status in BIOS response")
	}
	if f.Data[0] != 0 {
		descriptions := map[byte]string{
			10: "memory check busy", 11: "operating system busy",
			17: "unsupported operation", 18: "invalid BIOS request/range/state",
			19: "EEPROM programming or readback failure", 20: "EEPROM mode active",
		}
		if description := descriptions[f.Data[0]]; description != "" {
			return fmt.Errorf("BIOS status %d (%s)", f.Data[0], description)
		}
		return fmt.Errorf("BIOS status %d", f.Data[0])
	}
	return nil
}
