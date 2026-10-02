package apps

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
)

const usage = "usage: boardctl [-port DEVICE] keyboard | console | ping | info | programs | upload [--name NAME] [--run] FILE | run ID | delete ID | rename ID NAME | dump --addr ADDRESS --size BYTES [--output FILE] | write --addr ADDRESS FILE | bios-flasher-mode | bios-read [--output FILE] [--verify FILE] | bios-write [--reset] [--timeout DURATION] FILE | reset"
const uploadUsage = "usage: boardctl upload [--name NAME] [--run] FILE"
const dumpUsage = "usage: boardctl dump --addr ADDRESS --size BYTES [--output FILE]"
const writeUsage = "usage: boardctl write --addr ADDRESS FILE"
const biosFlasherModeUsage = "usage: boardctl bios-flasher-mode"
const biosReadUsage = "usage: boardctl bios-read [--output FILE] [--verify FILE]"
const biosWriteUsage = "usage: boardctl bios-write [--reset] [--timeout DURATION] FILE"

type command struct {
	port        string
	name        string
	programID   uint32
	programName string
	upload      uploadOptions
	dump        dumpOptions
	write       writeOptions
	biosRead    biosReadOptions
	biosWrite   biosWriteOptions
}

type uploadOptions struct {
	name     string
	data     []byte
	runAfter bool
}

func parseFlags(fs *flag.FlagSet, args []string, usage string) error {
	fs.SetOutput(io.Discard)
	err := fs.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, usage)
		fs.SetOutput(os.Stderr)
		fs.PrintDefaults()
	}
	return err
}

func prepareCommand(args []string) (command, error) {
	var cmd command
	fs := flag.NewFlagSet("boardctl", flag.ContinueOnError)
	fs.StringVar(&cmd.port, "port", defaultPort, "serial device")
	if err := parseFlags(fs, args, usage); err != nil {
		return cmd, err
	}
	args = fs.Args()
	if len(args) == 0 {
		return cmd, errors.New(usage)
	}
	cmd.name = args[0]
	args = args[1:]
	switch cmd.name {
	case "keyboard", "console", "ping", "info", "programs", "reset":
		if len(args) != 0 {
			return cmd, fmt.Errorf("usage: boardctl %s", cmd.name)
		}
	case "run", "delete", "rename":
		n := 1
		commandUsage := fmt.Sprintf("usage: boardctl %s ID (1..4)", cmd.name)
		if cmd.name == "rename" {
			n = 2
			commandUsage += " NAME"
		}
		if len(args) != n {
			return cmd, errors.New(commandUsage)
		}
		id, err := strconv.ParseUint(args[0], 0, 32)
		if err != nil || id < 1 || id > 4 {
			return cmd, fmt.Errorf("invalid program ID %q: must be 1..4", args[0])
		}
		cmd.programID = uint32(id)
		if cmd.name == "rename" {
			cmd.programName = args[1]
			if err := validateName(cmd.programName); err != nil {
				return cmd, err
			}
		}
	case "upload":
		fs := flag.NewFlagSet("upload", flag.ContinueOnError)
		fs.StringVar(&cmd.upload.name, "name", "", "program name (1..15 ASCII characters; default: file name)")
		fs.BoolVar(&cmd.upload.runAfter, "run", false, "run after upload")
		if err := parseFlags(fs, args, uploadUsage); err != nil {
			return cmd, err
		}
		if fs.NArg() != 1 {
			return cmd, errors.New(uploadUsage)
		}
		path := fs.Arg(0)
		if cmd.upload.name == "" {
			cmd.upload.name = defaultProgramName(path)
		}
		if err := validateName(cmd.upload.name); err != nil {
			return cmd, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return cmd, fmt.Errorf("read upload file: %w", err)
		}
		if err := validateUpload(cmd.upload.name, len(data)); err != nil {
			return cmd, err
		}
		cmd.upload.data = data
	case "dump":
		fs := flag.NewFlagSet("dump", flag.ContinueOnError)
		var address, size string
		fs.StringVar(&address, "addr", "", "physical RAM address (decimal or 0x-prefixed hex)")
		fs.StringVar(&size, "size", "", "number of bytes to read")
		fs.StringVar(&cmd.dump.output, "output", "", "save binary to a new file instead of printing hex")
		if err := parseFlags(fs, args, dumpUsage); err != nil {
			return cmd, err
		}
		if fs.NArg() != 0 || address == "" || size == "" {
			return cmd, errors.New(dumpUsage)
		}
		addr, err := strconv.ParseUint(address, 0, 32)
		if err != nil {
			return cmd, fmt.Errorf("invalid dump address %q", address)
		}
		n, err := strconv.ParseUint(size, 0, 32)
		if err != nil {
			return cmd, fmt.Errorf("invalid dump size %q", size)
		}
		cmd.dump.address, cmd.dump.size = uint32(addr), uint32(n)
		if err := validateDumpRange(cmd.dump.address, cmd.dump.size); err != nil {
			return cmd, err
		}
		if cmd.dump.output != "" {
			if _, err := os.Lstat(cmd.dump.output); err == nil {
				return cmd, fmt.Errorf("dump output already exists: %s", cmd.dump.output)
			} else if !errors.Is(err, os.ErrNotExist) {
				return cmd, fmt.Errorf("check dump output: %w", err)
			}
		}
	case "write":
		fs := flag.NewFlagSet("write", flag.ContinueOnError)
		var address string
		fs.StringVar(&address, "addr", "", "physical payload RAM address (decimal or 0x-prefixed hex)")
		if err := parseFlags(fs, args, writeUsage); err != nil {
			return cmd, err
		}
		if fs.NArg() != 1 || address == "" {
			return cmd, errors.New(writeUsage)
		}
		addr, err := strconv.ParseUint(address, 0, 32)
		if err != nil {
			return cmd, fmt.Errorf("invalid write address %q", address)
		}
		data, err := os.ReadFile(fs.Arg(0))
		if err != nil {
			return cmd, fmt.Errorf("read memory write file: %w", err)
		}
		cmd.write = writeOptions{address: uint32(addr), data: data}
		if err := validateWriteRange(cmd.write.address, len(data)); err != nil {
			return cmd, err
		}
	case "bios-flasher-mode":
		fs := flag.NewFlagSet("bios-flasher-mode", flag.ContinueOnError)
		if err := parseFlags(fs, args, biosFlasherModeUsage); err != nil {
			return cmd, err
		}
		if fs.NArg() != 0 {
			return cmd, errors.New(biosFlasherModeUsage)
		}
	case "bios-read":
		fs := flag.NewFlagSet("bios-read", flag.ContinueOnError)
		var verify string
		fs.StringVar(&cmd.biosRead.output, "output", "", "save the complete 32-KiB EEPROM to a new file")
		fs.StringVar(&verify, "verify", "", "compare EEPROM SHA-256 with this 32-KiB image")
		if err := parseFlags(fs, args, biosReadUsage); err != nil {
			return cmd, err
		}
		if fs.NArg() != 0 {
			return cmd, errors.New(biosReadUsage)
		}
		if verify != "" {
			data, err := readBIOSFile(verify)
			if err != nil {
				return cmd, err
			}
			cmd.biosRead.expected = data
		}
		if path := cmd.biosRead.output; path != "" {
			if _, err := os.Lstat(path); err == nil {
				return cmd, fmt.Errorf("BIOS output already exists: %s", path)
			} else if !errors.Is(err, os.ErrNotExist) {
				return cmd, fmt.Errorf("check BIOS output: %w", err)
			}
		}
	case "bios-write":
		fs := flag.NewFlagSet("bios-write", flag.ContinueOnError)
		fs.BoolVar(&cmd.biosWrite.resetAfter, "reset", false, "reset only after complete EEPROM SHA-256 verification")
		fs.DurationVar(&cmd.biosWrite.timeout, "timeout", defaultBIOSWriteTimeout, "response timeout per BIOS_WRITE attempt")
		if err := parseFlags(fs, args, biosWriteUsage); err != nil {
			return cmd, err
		}
		if fs.NArg() != 1 {
			return cmd, errors.New(biosWriteUsage)
		}
		if cmd.biosWrite.timeout <= 0 {
			return cmd, errors.New("BIOS flash timeout must be positive")
		}
		data, err := readBIOSFile(fs.Arg(0))
		if err != nil {
			return cmd, err
		}
		cmd.biosWrite.data = data
	default:
		return cmd, fmt.Errorf("unknown command: %s", cmd.name)
	}
	return cmd, nil
}
