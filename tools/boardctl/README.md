# boardctl

Serial tool for the revision-2 8088 board. Normal commands require UART protocol
**2.0** (frame byte 02h), at 9600 baud, 8N1. There is no legacy fallback for
normal diagnostics, terminal or disk commands. EEPROM operations negotiate the
installed upgrade transport independently, including version 1.

From `tools/boardctl`:

```sh
go build -o boardctl ./cmd
./boardctl -port /dev/cu.usbserial-0001 info
```

Only one process should own the port. A successful initial probe is silent;
Reset guidance appears after an unanswered PING. Ctrl-C cancels interactive work.

## Commands

| Command | Purpose |
| --- | --- |
| `info` | Firmware, board, SRAM banks, PIC/PIT and active IRQs |
| `ping` | Monitor connectivity |
| `ram` | Installed banks, total and contiguous memory, diagnostic state |
| `ram-test` | Confirm, run and report a destructive full RAM test |
| `irq-test` | Verify PIT channel 0 → PIC IRQ0 → CPU |
| `keyboard` | Forward keys while using the board LCD |
| `terminal [--disk IMAGE [--disk-readonly] [--boot]]` | UART console and optional floppy (writable by default) |
| `console` | Raw serial console after a protocol-2.0 probe |
| `dump --addr ADDRESS --size BYTES [--output FILE]` | Read installed RAM |
| `write --addr ADDRESS FILE` | Write/verify monitor scratch RAM |
| `bios-flasher-mode` | Enter the independent RAM EEPROM updater |
| `bios-read [--output FILE] [--verify FILE]` | Read the full EEPROM |
| `bios-write [--reset] [--timeout DURATION] FILE` | Write and verify a 32-KiB BIOS |
| `reset` | Restart POST from the idle monitor |

Applications are managed by the OS. There are no upload, programs, run, delete
or rename commands, startup upload flags, or terminal upload/run escape commands.
Keyboard and terminal forward Ctrl-] as a normal key; Ctrl-C is the local exit.

## Boot the native OS

Build BIOS and image from the repository root:

```sh
make -C firmware/8088-mainboard bios os BIOS_VERSION=dev-disk
```

After installing this BIOS, from `tools/boardctl`:

```sh
./boardctl -port /dev/cu.usbserial-0001 terminal \
  --disk ../../firmware/8088-mainboard/build/home8088.img --boot
```

`--disk` opens a raw 1.44-MiB image read/write by default and persists guest
changes. Add `--disk-readonly` to serve an immutable snapshot without modifying
the file. The terminal reports the selected mode on every connection. Failure
to open or lock a writable image is an error; there is no automatic read-only
fallback. Terminal, keyboard and sector transfers share one connection.
Without `--boot`, select Default boot in the BIOS menu. Boot requires RAM0 and
a verified PIC/PIT timer. One RAM0 chip is sufficient; 256 KiB installed gives
224 KiB conventional RAM after the BIOS reservation.

For MS-DOS, use a working copy with `terminal --disk IMAGE --boot`; see the
[DOS setup guide](../../firmware/8088-mainboard/os/msdos/README.md). The BIOS with
write support (`dev-disk`) and rebuilt OEM layer are required. boardctl opens the existing image read/write,
holds an exclusive advisory lock, and writes, syncs and verifies each sector
before acknowledging success to the guest. Reconnect to the same image and mode
without `--boot`; mount permissions change only after Reset. Use `--disk-readonly` again when reconnecting a protected image.
An interrupted FAT update can leave an incomplete filesystem change. Rebuilding
an image replaces it, so keep user files and saves in a separate working copy under
`firmware/8088-mainboard/disks/`, which survives `make clean`.

The shell provides HELP, MEM, DIR, RUN CITY, RUN REACTOR, RUN TERMINAL and REBOOT.
Esc exits the bundled apps. At 9600 baud loading takes time; the initial kernel
transfer is roughly 12–20 seconds. See [the OS guide](../../firmware/8088-mainboard/os/home8088/README.md).

Ctrl-C detaches and cancels host transfers, without resetting the OS. Reconnect
with the same `terminal --disk IMAGE` and mount mode, omitting `--boot`. Repeat
`--disk-readonly` if used initially. Missing disk service
times out in BIOS. Reset/REBOOT clears mounting; reconnect to mount again.

## Diagnostics and memory

`ram-test` asks for confirmation before opening UART. It erases RAM contents,
reports progress and the first failure, and cancels on Ctrl-C or host loss.
The BIOS preserves live state and stack during reserved-byte probes. There are
no separate CLI status/cancel subcommands. `irq-test` restores the runtime after
its temporary test configuration.

Dump addresses are physical SRAM addresses in 00000..9FFFF; BIOS rejects absent
banks. Scratch writes must remain within selected-bank offsets 8800..FBFF and
are read back for SHA-256 verification. They do not load or execute applications.
Commands requiring the monitor return busy while an OS runs; reboot first.

## EEPROM upgrade exception

The service ABI retains PING/RESET/FLASHER_MODE/WRITE/READ command IDs 01..06.
EEPROM commands probe framing versions 2 and 1, then use the detected transport
for entry, writes, readback and Reset. They can update an installed protocol-1.x
BIOS to 2.0. A valid matching response may select another installed service-frame
version; normal commands never enable this behavior.

ROM write SW1 must permit writes. The updater runs from RAM, checks EEPROM
readback, and blocks reset after an incomplete write. Host `bios-write` verifies
the full EEPROM SHA-256 before honoring `--reset`. Input must be exactly 32768
bytes. Output files use exclusive creation and are not overwritten. `bios-read`
can save a backup or compare against a local image.

After upgrading, reconnect with the current boardctl for protocol-2.0 operations.
