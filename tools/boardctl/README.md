# boardctl

`boardctl` is the host-side command-line tool for the 8088 mainboard monitor
protocol. It communicates over the GM16C550 UART at 9600 8N1 and loads flat
16-bit binaries into SRAM.

The wire format is documented in [uart-protocol-v1.md](../../docs/uart-protocol-v1.md).

## Build

Go 1.26 or later is required.

```sh
go build -o boardctl ./cmd
```

The tool uses the macOS built-in `stty` command to configure the serial device.

## Monitor status

The monitor executes directly from ROM at `F800:0000`. It implements `keyboard`, `info`,
`upload`, `programs`, `run`, `delete`, `rename`, `dump`, `write`, `ping`, and `reset`;
uploaded programs execute from SRAM.
`bios-flasher-mode`, `bios-read` and `bios-write` use the built-in RAM flasher.

The `console` command is usable now. The default device is
`/dev/cu.usbserial-110`; use `-port` to select another one.

```sh
./boardctl -port /dev/cu.usbserial-XXXX info
```

## Commands

```sh
# Open a plain UART console. Ctrl-C exits.
./boardctl -port /dev/cu.usbserial-XXXX console

# Share the PC keyboard; menu and program output stay on the board LCD.
./boardctl -port /dev/cu.usbserial-XXXX keyboard

# Print BIOS/board revision, memory map and UART settings.
./boardctl -port /dev/cu.usbserial-XXXX info

# Check the connection in either the ROM menu or RAM flasher.
./boardctl -port /dev/cu.usbserial-XXXX ping

# Name a program, let BIOS allocate RAM, and immediately execute it.
./boardctl -port /dev/cu.usbserial-XXXX upload \
  --name "City Lights" --run ../../firmware/8088-mainboard/build/citylights.bin

# List resident program IDs, names, addresses and sizes.
./boardctl -port /dev/cu.usbserial-XXXX programs

# Execute a binary that was previously uploaded.
./boardctl -port /dev/cu.usbserial-XXXX run 1

# Rename a resident without reloading it, or delete it and free its RAM.
./boardctl -port /dev/cu.usbserial-XXXX rename 1 Penguins
./boardctl -port /dev/cu.usbserial-XXXX delete 1

# Return execution to ROM reset handling.
./boardctl -port /dev/cu.usbserial-XXXX reset

# Print 256 bytes of RAM as an address/hex/ASCII dump.
./boardctl -port /dev/cu.usbserial-XXXX dump --addr 0x8800 --size 256

# Save the 32-KiB RAM bank to a new binary file.
./boardctl -port /dev/cu.usbserial-XXXX dump \
  --addr 0 --size 32768 --output ram-dump.bin

# Write raw bytes into payload RAM, then verify SHA-256 by readback.
./boardctl -port /dev/cu.usbserial-XXXX write --addr 0x9000 patch.bin

# Enter the RAM flasher and save the complete EEPROM image (works on Rev.1).
./boardctl -port /dev/cu.usbserial-XXXX bios-read --output bios-dump.bin

# Read EEPROM and compare its SHA-256 with a local 32-KiB BIOS image.
./boardctl -port /dev/cu.usbserial-XXXX bios-read \
  --verify ../../firmware/8088-mainboard/build/bios.bin

# Flash on writing hardware, verify SHA-256, then reset to the new BIOS.
./boardctl -port /dev/cu.usbserial-XXXX bios-write --reset \
  ../../firmware/8088-mainboard/build/bios.bin
```

`upload` sends name, size and image CRC in `PROGRAM_UPLOAD_BEGIN`, receives an ID and
address, sends `PROGRAM_UPLOAD_DATA` payloads containing only program bytes
(128 per block, except the final remainder). Before `PROGRAM_UPLOAD_COMMIT`,
boardctl reads the entire assigned range back through `MEMORY_READ` and compares
its SHA-256 with the original binary on the PC. Only a matching hash permits
COMMIT with the one-byte program ID. Read failures or mismatched hashes trigger
ABORT where possible; the program is not committed or run. Hash mismatches
report both PC and RAM hashes. This check also applies to uploads from keyboard
sharing. Readback adds roughly another transfer's worth of UART time.
BIOS still independently verifies the image CRC16 during COMMIT.
BIOS advances its own write cursor; boardctl
waits for each ACK and retries the same sequence on timeout.
The LCD displays `RECEIVING...` and a 20-cell progress bar during the
transfer. The current SRAM upload range is `0x8800` through `0xFBFF`
(29,696 bytes); the upper 1 KiB is reserved for the stack.
Up to four named programs may coexist. BIOS assigns paragraph-aligned addresses
starting at `0x8800`; `--addr` is no longer supported. Programs use `ORG 0`,
DS=CS, preserve SS/SP and return with RETF. Old ORG-0200 binaries must be rebuilt.
The INT API reserves IVT/BDA and state below 8800h; update boardctl and BIOS
together. Programs may use the [BIOS API](../../firmware/8088-mainboard/docs/bios-api.md) rather than
including hardware drivers. Both examples have been migrated.
Name defaults to the file stem; `--name` overrides it. Names must be unique,
1–15 printable ASCII characters and not all spaces. Failed uploads are aborted
where possible; already committed programs remain available.

`rename ID NAME` preserves the program's address, size and bytes; the new name
follows the same 1–15 ASCII and uniqueness rules as upload. `delete ID` frees
the slot and allocation but does not erase RAM or relocate other programs.
BIOS uses first-fit allocation to reuse deleted ranges. Following program IDs
shift after deletion: run `programs` again before using saved IDs. Both commands
require the ROM menu and no pending upload or memory check. Deleting switches
the LCD to Custom programs. Retries cannot delete the next ID a second time.

`dump` uses `MEMORY_READ` in chunks of at most 127 bytes. Both `--addr` and
`--size` are required; decimal and `0x`-prefixed hex are supported. The range
must lie entirely in RAM `0x00000..0x1FFFF` (four mirrors of 32 KiB).
ROM reads are rejected. Protected BIOS RAM is readable, but live state and buffers
are not atomic snapshots. Use the ROM menu: memory checks reject reads, and
running programs cannot be dumped. With `--output`, the file is created only
after a successful full read; existing files are never overwritten.

`write --addr ADDRESS FILE` uses `MEMORY_WRITE` in chunks of at most 124 bytes.
Both boardctl and BIOS check the entire range: only payload RAM `0x8800..0xFBFF`
and its aliases (`0x00800..0x07BFF`, `0x10800..0x17BFF`, `0x18800..0x1FBFF`) are
writable. ROM, unmapped memory, IVT/BDA, BIOS state and stacks are protected.
After writing, boardctl reads the entire range back and checks SHA-256. Writes
are rejected during uploads, memory checks and program execution. Raw writes
do not create a menu slot: use `upload` for programs. Existing program bytes
can be overwritten, and a failed multi-block write may leave a partial patch;
there is no rollback.

## BIOS flasher mode

`bios-flasher-mode` sends `BIOS_FLASHER_MODE` with an empty payload. Entry copies
the BIOS's embedded flasher to RAM and clears resident programs and keyboard
sharing. The EEPROM
image is always 32,768 bytes. This command works on Rev.1. In EEPROM mode only
`BIOS_WRITE`, `BIOS_READ`, `PING` and `RESET` are available, plus an identical
lost-ACK entry retry. Use `ping` to check the connection and `reset` to return
to the ROM BIOS when firmware permits it.

`bios-read [--output FILE] [--verify FILE]` reads all 32 KiB using `BIOS_READ`
blocks of up to 127 bytes and prints the EEPROM SHA-256. Requests carry physical
addresses in `0xF8000..0xFFFFF`; the host and flasher validate the entire range.
RAM, EEPROM mirrors and addresses outside this window are rejected. It probes the mode:
from the ROM menu it enters the RAM flasher automatically;
if already in the flasher it reads without reentering. `--verify` compares
SHA-256 with an exact 32-KiB file, including padding
and reset vector. `--output` saves a new binary file only after all reads and
any requested verification succeed; existing files are never overwritten.
The command leaves the board in EEPROM mode and does not reset it.

`bios-write [--reset] [--timeout DURATION] FILE` requires an exact 32-KiB image
and a board in its ordinary ROM menu. It probes the mode before entering the RAM
flasher, then sends consecutive 64-byte EEPROM pages through
`BIOS_WRITE`. Each request is acknowledged before the next page; retries keep
the same sequence, physical address and bytes, including across sequence wrap. The RAM
flasher performs per-block write/readback checks. boardctl then reads
the entire EEPROM through `BIOS_READ` and compares SHA-256 with the source.
`--reset` sends RESET only after that verification succeeds; otherwise the
board remains in the RAM flasher. Any write/readback error stops the transfer
without sending RESET. The per-WRITE-attempt timeout defaults to 10 seconds
and can be increased, for example `--timeout 30s`, for EEPROM programming and
readback verification.

Rev.1 supports entry and readback; a valid WRITE returns status 17
(`unsupported operation`) without writing. Invalid requests in the RAM flasher
return status 18. The Rev.2 EEPROM write driver is not implemented in the
current firmware; the host flash command implements its
protocol contract. An already active EEPROM session is rejected for a new
flash. After a SHA-256 mismatch, do not reset into the unverified BIOS.
The protocol permits a new full transfer from the active flasher after
completion, starting with WRITE at `0xF8000`; the current CLI does not expose
that recovery operation. The host does not reset or restart a write automatically.

## Keyboard sharing

Run from this directory without building a binary:

```sh
go run ./cmd -port /dev/cu.usbserial-0001 keyboard
```

The entry point is `cmd/main.go`; application logic lives in `internal/apps`.
`internal/client` handles monitor requests, response matching, and retries;
`internal/protocol` encodes and decodes wire frames.
`keyboard` translates terminal input into ASCII/PC-style scan-code events.
It does not fetch, redraw or mirror the BIOS menu on the PC.
It restores the terminal settings on normal exit and handled signals.

Connect while the board is in its ROM menu, then use Up/Down, Enter and Esc
to navigate on the LCD. Wait for `Keyboard connected` before typing: while
attaching, the terminal is still in its normal mode. Ctrl-C cancels attachment
as well as an active session, including while a key acknowledgement is pending.
Failed/interrupted attachment also attempts to detach, since BIOS may have
accepted the request before its acknowledgement was received. A malformed
keyboard acknowledgement reports its actual payload bytes and length.
Programs read the same input through INT 16h.
UART reads are nonblocking and return periodically even without incoming
bytes; write waits are bounded. This lets retry deadlines and Ctrl-C run on
macOS instead of getting stuck inside Go's file readiness wait. The serial
descriptor stays open while `stty` configures it, so closing the configuration
command does not reset settings before the UART exchange.
Letters, including `U` and `Q`, and Ctrl-Z are forwarded to the program.
Arrows, Home/End, Insert/Delete, Page Up/Down and F1..F10 are translated;
modifier state, key releases and Unicode text are not supported.

Press `Ctrl-]`, then `u` to open a path prompt. Paste/type a binary path, press Enter to upload to
an address assigned by BIOS. Its file stem becomes the program name. After
upload the new Custom programs slot is selected: Enter opens details, then
Enter runs it. Paths may contain
spaces; shell quote characters and `~` are not expanded. Esc cancels the prompt.
Uploads are allowed only after returning to the BIOS menu. `Ctrl-C` detaches
and quits directly, including from the upload prompt. It is intercepted in
raw mode and not forwarded to BIOS. Terminal
settings are restored on exit. `Ctrl-]`, then `q` remains an exit alias;
`Ctrl-]`, then `]` forwards a literal Ctrl-].

Revision 1 has **no hardware interrupts**. In RAM programs, INT 16h/AH=00h
waits while polling input; AH=01h polls and peeks without consuming a key.
Programs doing other work must call AH=01h regularly. A program that does not
poll cannot acknowledge keyboard events; long delays can overflow the UART
FIFO. The BIOS ring holds up to 15 keys after packets have been received.
The host reports failed acknowledgements/queue errors; it does not promise
unlimited buffering or asynchronous input. Raw INT 14h receive is unavailable
while sharing owns RX. UART TX/status still work, but this mode does not
display program UART output. Use `console` separately for raw UART programs.

Sharing survives ordinary RETF. Reset disables sharing, so restart the host
session after Reset. If a non-polling program prevents detach, Reset restores
raw UART mode. Do not attach sharing to an already-running raw-input program.

Only one process may own the serial port. Use the built-in upload action while
sharing, or close the session before using the separate `upload` command.
Custom programs has four slots, initially Empty. The rightmost LCD column
shows `^`/`v` when more items exist above/below. Reset and confirmed memory
check clear the registry; ordinary program return preserves it.
Menu memory check erases the payload only after confirmation; an active test
rejects uploads and can be cancelled with Esc. Info is a scrollable LCD list:
Up/Down scroll, Esc returns, Enter has no action. The host `info` command
continues to print a compact summary rather than the LCD list. Default boot is a placeholder;
there is no automatic countdown, persistent program selection, or DOS boot yet.

## Current scope

Protocol v1 implements `PING`, `INFO`, `RESET`, `BIOS_FLASHER_MODE`, `BIOS_WRITE`,
`BIOS_READ`, `MEMORY_READ`, `MEMORY_WRITE`; `PROGRAM_UPLOAD_BEGIN`, `PROGRAM_UPLOAD_DATA`,
`PROGRAM_UPLOAD_COMMIT`, `PROGRAM_UPLOAD_ABORT`; `PROGRAM_LIST`, `PROGRAM_EXEC`,
`PROGRAM_DELETE`, `PROGRAM_RENAME`; and
`KEYBOARD_MODE`, `KEYBOARD_EVENT`. Version stays 1 during development; update BIOS and boardctl
together. Each packet uses COBS framing, CRC-16/CCITT-FALSE, sequence numbers,
and host retries. Firmware status 17–20 is reported with a description.
Execution debugging is not implemented.
