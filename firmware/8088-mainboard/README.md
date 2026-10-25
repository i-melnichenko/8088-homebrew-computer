# Revision-2 8088 mainboard firmware

ROM BIOS performs POST, provides hardware services, runs diagnostics, loads an
operating system and updates EEPROM. Applications are loaded and run by the OS.
There is no BIOS program registry, application upload command or resident-app menu.
The board supports HOME8088 OS and Microsoft MS-DOS 2.0 through the same BIOS.

## Build

Requirements: NASM, make, Python 3; Go for boardctl.

```sh
make bios os BIOS_VERSION=dev-disk
```

`build/bios.bin` is the single 32-KiB image for the 28C256 at physical
F8000..FFFFF. `build/home8088.img` is the FAT12 floppy containing
HOME8088 OS and CITY.COM, REACTOR.COM, TERMINAL.COM. Examples build only as COM
applications. HOME8088 only reads its filesystem; boardctl opens disk images
read/write unless `--disk-readonly` is supplied. See
[OS build, commands and memory layout](os/home8088/README.md).

To use Microsoft MS-DOS 2.0, build `make bios msdos BIOS_VERSION=dev-disk` and boot
`build/msdos20.img` instead. Both OS images use the same UART disk transport;
see the [operating systems index](os/README.md) to choose an image.

## Source layout

- `bios/`: ROM BIOS, PC services, UART protocol, diagnostics and RAM flasher.
- `lib/`: shared hardware drivers, memory definitions and BIOS API constants.
- `os/home8088/`: native OS and its `programs/` applications.
- `os/msdos/`: Microsoft DOS OEM layer, `programs/hello/` and optional `zork/` package.
- `scripts/`: general image-building tools and ROM size reporting.
- `tests/`: board emulator and firmware/OS integration tests.
- `docs/`: BIOS API documentation.
- `build/`: generated binaries, disk images and upstream caches.
- `disks/`: ignored writable working images, preserved by `make clean`.

See the [operating systems index](os/README.md) for build targets and images.

## Hardware and POST

- Intel 8088, 8086 instructions, 20-bit real-mode address space.
- Five optional, distinct 128-KiB SRAM chips at 00000..9FFFF. At least one is
  required; the monitor and flasher can use any working bank. OS boot requires RAM0.
- GM16C550/16550 at 3F8..3FF, 9600 baud, 8N1.
- Optional PIC 8259A at 20/21 and PIT 8254 at 40..43. POST checks presence and
  independently verifies timer IRQ0 and UART IRQ4. Failed checks select polling.
- Optional HD44780 LCD2004 at 80/81 with display JP1 bridged. The single BIOS
  always writes it with bounded delays. R/W is grounded, so presence is not read.

The PC timer uses divisor 65536: approximately 18.2065 Hz. IRQ0 is vector 08h,
IRQ4 is 0Ch. The timer updates the BDA clock and calls INT 1Ch; UART IRQ drains
bytes into a bounded ring. Framing, console requests and disk service remain
foreground work. Long-running code must call BIOS regularly. ISR hooks must
remain short and must not reenter cooperative transport processing.

## Monitor

The LCD menu contains Default boot, Memory check and Info. Default boot enters
the mounted UART floppy; without a mounted image it reports NO BOOT TARGET.
There is no automatic countdown. Info lists board, firmware and protocol data.
The RAM test explicitly confirms erasure and preserves live BIOS state/stack
while probing reserved bytes. UART tests stop when host heartbeats disappear.

```sh
# From tools/boardctl, after building its current binary:
./boardctl -port /dev/cu.usbserial-0001 info
./boardctl -port /dev/cu.usbserial-0001 ram
./boardctl -port /dev/cu.usbserial-0001 irq-test
./boardctl -port /dev/cu.usbserial-0001 ram-test
./boardctl -port /dev/cu.usbserial-0001 terminal \
  --disk ../../firmware/8088-mainboard/build/home8088.img --boot
```

The normal UART protocol is **2.0**, frame version byte 02h. Normal boardctl
commands require it. EEPROM operations negotiate the installed upgrade transport
separately, including framing version 1, so older installed BIOS can be updated.
See [boardctl](../../tools/boardctl/README.md) and the
[wire specification](../../docs/uart-protocol.md).

## BIOS and OS boundary

[The BIOS API](docs/bios-api.md) supplies standard PC-style interrupts plus
LCD/timing/UART extensions. Before OS boot, data and stack live in the selected
working bank. Boot moves the live upper half-bank into the upper 32 KiB of
contiguous RAM and reduces INT 12h/BDA conventional memory accordingly. BIOS
services and IRQ handlers locate their data independently of application SS.

The OS accesses a virtual floppy through INT 13h and owns application memory
and stacks. The current BIOS is an incremental PC/XT implementation. HOME8088 loads native COM files; Microsoft DOS loads DOS COM/EXE
programs through its OEM I/O layer. The Microsoft DOS target is tested;
arbitrary PC software compatibility is not implied. There is no RTC, PC video
RAM or physical PC keyboard controller.

## EEPROM updater

`bios/programs/flasher/` contains the independent RAM updater embedded in ROM.
Entering it stops PIT/UART IRQs, masks PIC, clears host sharing and copies the
updater into working RAM. It uses polling and makes no calls into ROM while
writing EEPROM. The normal ROM monitor cannot program EEPROM directly.

Host writes verify the complete EEPROM with SHA-256 before optional Reset.
ROM write SW1 must permit writing. `bios-read` can save or compare a complete
32-KiB image. Do not run another serial client during an update.

## Verification

`make test` builds images and executes the real assembly in a Docker/Unicorn
emulator. Tests cover protocol 2.0 rejection of other frame versions, optional
RAM/chips, diagnostics, EEPROM transfer/readback/reset, BIOS calls on independent
stacks, PC clock services, boot sectors, FAT12, fragmented COM files and OS return.
Go tests cover the host transport, PTYs, cancellation and legacy EEPROM upgrades.
The Docker build excludes host `build/` and `disks/` directories and rebuilds
BIOS/HOME8088 images only. DOS and Zork boot tests therefore skip in the default
`make test` run. To include both OSes and Zork, install the packages from
`tests/requirements.txt` in a local Python environment, then from the repository
root run:

```sh
make -C firmware/8088-mainboard bios os msdos msdos-zork BIOS_VERSION=dev-disk
BIOS_VERSION=dev-disk python3 -m unittest discover -s firmware/8088-mainboard/tests -v
```

Set the test `BIOS_VERSION` to the same label used for assembly.
Emulation checks behavior, not physical timing or wiring.

## Microsoft MS-DOS 2.0

`make msdos` builds an image with the pinned, unmodified Microsoft MS-DOS 2.00
kernel and COMMAND 2.02, plus a small OEM I/O layer using our PC BIOS services.
MS-DOS is a supported OS option with a UART console and a FAT12 disk.
boardctl mounts are writable by default; `--disk-readonly` protects the image.
Validated applications include Microsoft DEBUG and Zork I.
See [build and run instructions](os/msdos/README.md). This explicit target downloads
verified Microsoft binaries into build output; the normal build stays offline.
`make msdos-zork` builds an additional `build/msdos20-zork.img` with Zork I and
Infocom's generic DOS console interpreter for the UART terminal (256 KiB RAM).
Use the updated `dev-disk` BIOS and boardctl for write support. The Microsoft
kernel and COMMAND.COM remain unmodified.
