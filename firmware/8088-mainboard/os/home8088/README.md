# HOME8088 OS

A small native, single-task operating system for the revision-2 PC, supported
alongside [Microsoft MS-DOS 2.0](../msdos/README.md). It loads into RAM through
the PC BIOS boot path and provides a shell and COM applications using BIOS
services. The console uses UART; the LCD shows OS/disk status until an
application uses it.

HOME8088 reads FAT12 files and runs one application at a time. Its kernel has no
scheduler, filesystem write operations or DOS INT 21h API. BIOS disk writes are
available independently; the MS-DOS port uses them for writable filesystems.

## Build and start

From the repository root:

```sh
make -C firmware/8088-mainboard bios os BIOS_VERSION=dev-disk
go -C tools/boardctl build -o boardctl ./cmd
```

Use the current protocol-2.0 BIOS, shared by HOME8088 and MS-DOS. When updating
the firmware, install `build/bios.bin` using the normal EEPROM update procedure.
The image requires working RAM0, PIC and a verified PIT timer for OS boot. One
128-KiB RAM chip in bank 0 is sufficient. The normal monitor still works with
other bank arrangements and without PIC/PIT.

From `tools/boardctl`, using the updated host binary:

```sh
./boardctl -port /dev/cu.usbserial-0001 terminal \
  --disk ../../firmware/8088-mainboard/build/home8088.img --boot
```

The BIOS loads sector 0 at `0000:7C00`, checks `55 AA`, and enters it with
`DL=0`. The boot sector loads the kernel at `1000:0000`. At 9600 baud initial
loading takes roughly 12–20 seconds; this is a transfer estimate, not a timeout.
HOME8088 OS only reads the filesystem. boardctl opens images read/write by
default; add `--disk-readonly` to protect the host image from guest writes.

Without `--boot`, `terminal --disk IMAGE` mounts the image and lets you select
**Default boot** in the BIOS menu. Ctrl-C detaches the terminal; it does not
reboot the OS. Reconnect with `terminal --disk` and the same image, without
`--boot`. Keep `--disk-readonly` on reconnect if it was used at mount time;
mount permissions change only after Reset. A disk request times out if the
host remains absent. Reset/REBOOT returns to the ROM monitor; reconnect to
mount the disk again.

## Commands

| Command | Action |
| --- | --- |
| `HELP` | List commands |
| `MEM` | Conventional RAM available below the BIOS reservation |
| `DIR` | List files in the FAT12 root directory |
| `RUN CITY` | Start Citylights; Esc returns to the shell |
| `RUN TIMER` | Start the countdown timer; Esc returns to the shell |
| `RUN TERMINAL` | Start the UART/LCD example; Esc returns to the shell |
| `REBOOT` | Restart BIOS POST |

The [timer](programs/timer/README.md) starts at one minute. Enter starts or
pauses, +/- adjusts the duration by 30 seconds while stopped, R resets, and
Esc exits. It uses BIOS PIT ticks and displays on the LCD; UART carries service messages only.

Commands and names are case insensitive. Filenames use 8.3 notation; Citylights
is named `CITY.COM` on disk. The loader follows FAT12 cluster chains. Programs
must fit in 31,488 bytes; subdirectories and arguments are not implemented.

## Memory and program contract

BIOS reserves the upper 32 KiB of contiguous RAM starting at physical zero,
moves its live runtime/monitor state there, and reduces BDA memory size and
INT 12h accordingly. For example, 256 KiB installed gives 224 KiB conventional
RAM. Holes terminate conventional RAM; later installed banks are not advertised
to the OS as usable conventional memory.

| Physical range | Use |
| --- | --- |
| `00000..004FF` | IVT and BDA; BIOS bank pointer at `004F0`, signature at `004F2` |
| `02000..03BFF` | Root directory buffer |
| `04000..051FF` | FAT1 buffer |
| `06000..0DFFF` | Current application segment, PSP and stack |
| `10000..11FFF` | Kernel reservation: 16 disk sectors |
| `12000..17FFF` | Kernel workspace/stack |
| Upper 32 KiB of contiguous RAM | Resident BIOS data, sector buffer, monitor stack |

The kernel stack is `1000:7FFE`. COM programs start at `0600:0100` with
`DS=ES=SS=0600`, `SP=7FFC`, DF clear and verified hardware IRQs enabled. A zero
return word on the stack leads a near `RET` to the PSP's `INT 20h`. Explicit
`INT 20h` also exits; the kernel restores its own stack and console. This is a
small native COM contract, not a complete DOS PSP or DOS API.

Examples build only as OS `.com` files (`ORG 100h`, `INT 20h`).
BIOS and boardctl do not load or execute applications. The OS and programs do not write PIC/PIT registers directly.

## Disk layout and current compatibility

`home8088.img` is a deterministic 1.44-MiB FAT12 floppy: 512-byte sectors, one
sector per cluster, 17 reserved sectors (boot + kernel), two 9-sector FATs,
224 root entries, 18 sectors per track, 2 heads and 80 cylinders.
`scripts/make-os-image.py` constructs the filesystem without external formatting
tools. The kernel and applications live in `os/home8088/`, separately from the
MS-DOS port in `os/msdos/`.

The BIOS implements INT 11h/12h, INT 13h reset/status/read/write/parameters/type,
INT 19h boot, INT 1Ah tick read/set, and a basic UART INT 10h console in OS mode.
INT 13h writes require a writable mount; HOME8088's kernel only uses reads.
Timer IRQ0 is INT 08h; UART IRQ4 is INT 0Ch. INT 1Ch runs once per timer tick.
The existing INT 16h keyboard interface is shared with the shell and applications.

This image verifies boot and native programs. The separate [Microsoft DOS
target](../msdos/README.md) uses an OEM I/O layer with the same PC BIOS. There is
no CGA memory at B8000, RTC, physical PC keyboard controller, or compatibility
for programs accessing those directly.
