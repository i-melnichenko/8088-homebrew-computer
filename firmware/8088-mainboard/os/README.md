# HOME8088 OS

A small, single-task operating system loaded into RAM through the PC BIOS boot
path. It is a development target for the PC/XT BIOS, not an MS-DOS implementation.
The current console uses UART; the LCD shows OS/disk status until an application
uses it. No scheduler, writable filesystem or DOS INT 21h is implemented.

## Build and start

From the repository root:

```sh
make -C firmware/8088-mainboard bios os BIOS_VERSION=dev-os2
go -C tools/boardctl build -o boardctl ./cmd
```

Install the new `build/bios.bin` using the normal EEPROM update procedure.
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
The host serves a read-only snapshot of the image and never writes the file.

Without `--boot`, `terminal --disk IMAGE` mounts the image and lets you select
**Default boot** in the BIOS menu. Ctrl-C detaches the terminal; it does not
reboot the OS. Reconnect with `terminal --disk` and the same image, without
`--boot`. A disk request times out if the host remains absent. Reset/REBOOT
returns to the ROM monitor; reconnect to mount the disk again.

## Commands

| Command | Action |
| --- | --- |
| `HELP` | List commands |
| `MEM` | Conventional RAM available below the BIOS reservation |
| `DIR` | List files in the FAT12 root directory |
| `RUN CITY` | Start Citylights; Esc returns to the shell |
| `RUN REACTOR` | Start Reactor; Esc returns to the shell |
| `RUN TERMINAL` | Start the UART/LCD example; Esc returns to the shell |
| `REBOOT` | Restart BIOS POST |

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
`make-os-image.py` constructs the filesystem without external formatting tools.

The BIOS implements INT 11h/12h, INT 13h reset/status/read/parameters/type,
INT 19h boot, INT 1Ah tick read/set, and a basic UART INT 10h console in OS mode.
Timer IRQ0 is INT 08h; UART IRQ4 is INT 0Ch. INT 1Ch runs once per timer tick.
The existing INT 16h keyboard interface is shared with the shell and applications.

This image verifies boot and native programs. Stock DOS has not been validated:
more INT 10h functions, disk parameter-table conventions and DOS-specific boot
requirements remain to be checked. There is no CGA memory at B8000, RTC, physical
PC keyboard controller, or compatibility for programs accessing those directly.
