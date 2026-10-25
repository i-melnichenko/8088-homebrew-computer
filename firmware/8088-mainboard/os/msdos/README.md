# Microsoft MS-DOS 2.0 on the 8088 board

Microsoft MS-DOS 2.0 is a supported operating system for the revision-2 board,
alongside HOME8088 OS. It boots the original Microsoft `MSDOS.SYS` (version 2.00) and
`COMMAND.COM` (command interpreter 2.02) from the
[official Microsoft archive](https://github.com/microsoft/MS-DOS/tree/main/v2.0/bin).
Their bytes remain unchanged. The board's OEM I/O layer connects DOS devices to
our existing PC BIOS interrupts. The archive does not contain a ready-made
IBM-PC `IO.SYS` for this kernel.

Use the protocol-2.0 BIOS (`dev-disk`) and updated boardctl for writable disks.
The Microsoft kernel and COMMAND.COM remain unmodified.

## Build

Requirements: NASM, make, Python 3 and Go for boardctl.
From the repository root:

```sh
make -C firmware/8088-mainboard bios msdos BIOS_VERSION=dev-disk
go -C tools/boardctl build -o boardctl ./cmd
```

The explicit `msdos` target fetches four files from pinned Microsoft commit
`2d04cacc5322951f187bb17e017c12920ac8ebe2`: MSDOS.SYS, COMMAND.COM, DEBUG.COM and LICENSE.
`upstream.json` records and verifies their SHA-256 digests. Cached files live in
ignored `build/msdos-upstream/`. A mismatching file fails the build.
The default `make`/`make bios os` does not download Microsoft files.

For an offline build, supply a checkout containing the same pinned files:

```sh
make -C firmware/8088-mainboard msdos MSDOS_SOURCE_DIR=/path/to/MS-DOS
```

Output: `firmware/8088-mainboard/build/msdos20.img`, a raw 1.44-MiB FAT12 floppy.
The Microsoft MIT license is included as LICENSE.TXT on the disk. Third-party
binaries are downloaded into build output and are not committed to this project.

## Run on the board

Working RAM0, PIC and PIT are required, as for HOME8088 OS. One 128-KiB RAM0
chip is sufficient; BIOS reserves the upper 32 KiB. The initial DOS date is
1980-01-01; CLOCK$ uses PIT time and supports DOS DATE/TIME changes in RAM.
There is no battery-backed clock.

Install `build/bios.bin` using the normal EEPROM update procedure when updating
firmware. From the repository root, prepare a persistent working copy:

```sh
mkdir -p firmware/8088-mainboard/disks
cp -n firmware/8088-mainboard/build/msdos20.img firmware/8088-mainboard/disks/msdos20.img
```

`cp -n` preserves an existing working image. Exit the current boardctl session
with Ctrl-C and press Reset, then from `tools/boardctl` run:

```sh
./boardctl -port /dev/cu.usbserial-0001 terminal \
  --disk ../../firmware/8088-mainboard/disks/msdos20.img --boot
```

At 9600 baud, initial loading can take roughly one to two minutes. The terminal
shows the loader, Microsoft DOS banner, COMMAND banner and an `A>` prompt.
AUTOEXEC.BAT disables batch echo and runs VER.

Try:

```text
VER
DIR
TYPE HELLO.TXT
HELLO
TIME
```

HELLO.COM prints through DOS INT 21h/AH=09h and exits through AH=4Ch. This is a
DOS application, separate from the native HOME8088 OS application contract.
The existing CITY/REACTOR/TERMINAL apps are not included: they terminate through
the native OS and have not been validated as DOS applications.

## Microsoft DEBUG

The image includes the unmodified DEBUG.COM from the same pinned Microsoft
archive (binary header `Vers 2.10`, SHA-256 verified). It uses the text console.
At the DOS prompt, load the example executable:

```text
DEBUG HELLO.COM
```

At DEBUG's `-` prompt, enter one command per line:

```text
R
U 100 10B
T
G
Q
```

`R` displays registers, initially IP=0100. `U` disassembles HELLO's instructions.
`T` executes its first instruction, advancing IP to 0103. `G` runs the remaining
DOS program and prints its message. `Q` returns to DOS; `VER` should still work.
These example operations modify only the debugged program's RAM. DEBUG file
writes use DOS disk services and follow the selected mount permissions;
`--disk-readonly` reports write protection.

## Zork I

`make msdos-zork` builds a separate `build/msdos20-zork.img` with the game and
Infocom's DOS console interpreter. See [Zork build and play instructions](zork/README.md).
The package's build script and pinned inputs are in `zork/`.

## Writable disks

The run command above opens the working image read/write by default. Add
`--disk-readonly` for a protected session. Writable open/lock failures are errors;
boardctl does not fall back silently to read-only.

Try file creation, copying, renaming, directories, batch execution and deletion:

```text
ECHO HELLO>NOTE.TXT
TYPE NOTE.TXT
COPY NOTE.TXT COPY.TXT
REN COPY.TXT RENAMED.TXT
MD TESTDIR
CD TESTDIR
CD .
CD \
ECHO ECHO BATCH_OK>CHECK.BAT
CHECK
DEL RENAMED.TXT
DIR
```

`CD .` is tested inside a subdirectory; the original DOS 2.00 kernel can reject
it in the root directory. Use `CD \` to select the root explicitly.

Working images in ignored `disks/` survive `make clean`; `build/` holds only
regenerable images and caches. boardctl locks the working image against another
cooperating writer. Each sector is written, synced and verified by readback
before BIOS returns success.
FAT file operations span multiple sectors; an interrupted operation is not
transactional. Keep the working copy separate: `make msdos` regenerates the
base image and does not preserve changes made in that base image.

With `--disk-readonly`, DOS reports write protection. Mount permissions are
fixed until Reset. Ctrl-C detaches boardctl; reconnect with the same image and
mode, omitting `--boot`. Reset returns to the ROM monitor. A writable open or
lock failure is an error, without falling back to read-only.

## Layout and interfaces

`os/msdos/boot.asm` loads the OEM layer from the 16 reserved sectors after the boot
sector into 0070:0000. The layer reads the pinned kernel from contiguous FAT12
data sectors beginning at LBA 49 and enters it at 0300:0000. Root bootstrap PSP
is at 0060:0000; driver and bootstrap stacks are separate and live in the OEM
segment. Keeping DOS low leaves enough space with one RAM0 chip.

The kernel initializes a chain of CON, AUX, PRN, CLOCK$ and floppy devices.
The OEM code normalizes standard handles using the sequence from Microsoft's
SYSINIT. Main device callbacks switch away from DOS's shallow stack before
calling ROM and preserve caller registers and FLAGS. INT 10h/13h/16h/1Ah supply
console, disk, keyboard and time. CLOCK$ converts PIT clocks with 8086 integer
arithmetic; time is quantized to hardware ticks. AUX/PRN are stub devices.

The FAT12 disk has 17 reserved sectors, two 9-sector FATs, 224 root entries,
512-byte clusters, 80 cylinders, 2 heads and 18 sectors/track. The kernel is the
first file/data extent, followed by COMMAND and the example files. The bootstrap
requires the pinned kernel size and layout; it is not a general DOS installer.

## Supported configuration and limits

The supported configuration boots Microsoft DOS through PC BIOS services, uses
the UART terminal for console input/output, and reads files from the FAT12
floppy served by boardctl. Writes are enabled by default and disabled by
`--disk-readonly`. Validated functionality includes shell commands,
COM/EXE loading, Microsoft DEBUG and Zork I. Hardware runs have confirmed the
DOS shell, HELLO.COM, and DEBUG register inspection, single-step and execution;
the Zork gameplay test runs in the board emulator.

The board has no CGA video memory or physical PC keyboard/printer devices;
AUX/PRN are stubs. Programs requiring those devices or additional BIOS functions
need further support. Compatibility with arbitrary DOS software depends on
each program's hardware requirements.

## Tests

After `make msdos`, the optional `tests/test_msdos.py` integration test boots the
real Microsoft binaries in the board emulator and exercises VER, DIR, TYPE,
a DOS COM executable, TIME, one-chip RAM0 operation, and original Microsoft
DEBUG loading/disassembly/single-step/execution/exit. Writable tests exercise
files, directories, batch execution and persistence after reboot. Clock
arithmetic tests also run without downloading Microsoft files. The default
Docker `make test` rebuilds only BIOS/HOME8088 and excludes host build output,
so DOS/Zork boot tests skip there. See the [full local test command](../../README.md#verification)
to include generated DOS/Zork images. Emulator results do not
certify physical wiring or timing; the image must also be tried on the board.
