# 8088 Homebrew Computer

An open-source modular computer built around the Intel 8088 CPU, using
through-hole DIP components and a custom expansion architecture. Development
proceeds incrementally from a minimal working system toward additional hardware
and software capabilities.

## Mainboard Rev. 2.0

| Front | Left-side assembly view |
| --- | --- |
| [![Front render](docs/renders/rev2.0-front.png?1)](docs/renders/rev2.0-front.png?1) | [![Left-side assembly render](docs/renders/rev2.0-left.png?1)](docs/renders/rev2.0-left.png?1) |

## Firmware and host tools

The repository includes a [32 KiB ROM BIOS](firmware/8088-mainboard/README.md),
[boardctl](tools/boardctl/README.md), and two supported operating systems:

| Operating system | Build from the repository root | Disk image |
| --- | --- | --- |
| [HOME8088 OS](firmware/8088-mainboard/os/home8088/README.md) | `make -C firmware/8088-mainboard os` | `home8088.img` |
| [Microsoft MS-DOS 2.0](firmware/8088-mainboard/os/msdos/README.md) | `make -C firmware/8088-mainboard msdos` | `msdos20.img` |

BIOS performs POST, offers a 20×4 LCD menu for boot/diagnostics/info, and exposes
PC-style hardware services. Both operating systems boot from FAT12
floppy images served by boardctl over UART. Images are writable by default.
Generated base images live in `firmware/8088-mainboard/build/`; keep persistent
DOS files and saves in working copies under `firmware/8088-mainboard/disks/`.

For example, build and boot HOME8088 OS from the repository root:

```sh
make -C firmware/8088-mainboard bios os BIOS_VERSION=dev-disk
go -C tools/boardctl build -o boardctl ./cmd
tools/boardctl/boardctl -port /dev/cu.usbserial-XXXX info
tools/boardctl/boardctl -port /dev/cu.usbserial-XXXX terminal \
  --disk firmware/8088-mainboard/build/home8088.img --boot
```

Install the built BIOS before booting the OS. HOME8088 applications run with
RUN CITY, RUN TIMER or RUN TERMINAL; MS-DOS starts DOS programs by filename.
BIOS/boardctl do not manage application uploads or resident slots. Normal host operations require
[UART protocol 2.0](docs/uart-protocol.md); EEPROM operations negotiate the
installed upgrade transport separately so an older BIOS can still be updated.
The independent updater lives in `firmware/8088-mainboard/bios/programs/flasher`.

Rev. 2 uses on-board UART at 3F8..3FF, 9600 8N1, optional PIC/PIT, and an optional
20×4 LCD with JP1 bridged (80/81 ports). POST detects installed SRAM chips across
00000..9FFFF; the monitor/updater need one working chip anywhere. OS boot requires
RAM0 and a verified timer, reserving the upper 32 KiB of contiguous RAM for BIOS.
The PC timer runs at about 18.2065 Hz. Both operating systems use BIOS disk,
keyboard, console and time services.

Microsoft MS-DOS uses the original, unmodified Microsoft 2.00 kernel and COMMAND
2.02 with the board's OEM I/O layer. The image includes DEBUG and a DOS example;
`make -C firmware/8088-mainboard msdos-zork` adds Zork I in `msdos20-zork.img`.
Prepare a working copy after building DOS, then boot it through the same BIOS
and boardctl (from the repository root):

```sh
mkdir -p firmware/8088-mainboard/disks
cp -n firmware/8088-mainboard/build/msdos20.img firmware/8088-mainboard/disks/msdos20.img
tools/boardctl/boardctl -port /dev/cu.usbserial-XXXX terminal \
  --disk firmware/8088-mainboard/disks/msdos20.img --boot
```

The supported DOS configuration uses the UART console and writable disk images.
Add `--disk-readonly` to protect an image from guest writes.
See the [DOS guide](firmware/8088-mainboard/os/msdos/README.md) for tested programs
and current hardware and storage limits.

## Architecture

The mainboard is built around an Intel 8088 in minimum mode. It exposes a
20-bit address bus (`A0…A19`) and an 8-bit data bus (`D0…D7`); two 74LS373
latches demultiplex `AD0…AD7`, and a 74LS245 buffers the data bus.

![8088 Homebrew Computer architecture](docs/architecture-overview.svg)

## Memory map

| Address range | Function |
| --- | --- |
| `00000h–9FFFFh` | 640 KiB SRAM (five 128 KiB banks) |
| `A0000h–BFFFFh` | `EXP_MEM_CS5#` expansion-memory window, including the conventional `B8000h` video-memory area; available on both Exp0 and Exp1 |
| `C0000h–DFFFFh` | `EXP_MEM_CS6#` expansion-memory / option-ROM window; available on both Exp0 and Exp1 |
| `E0000h–FFFFFh` | 28C256 boot ROM (32 KiB mirrored in the window) |

## I/O map

| Device | Primary ports | Notes |
| --- | --- | --- |
| 8259A PIC | `20h`, `21h` | Interrupt controller |
| 8254 PIT | `40h–43h` | System timer |
| GM16C550 UART | `3F8h–3FFh` | COM1-compatible; mirrored because decoding is partial |
| `EXP_IO_CS4#` | `80h–9Fh` | Expansion I/O window; available on both Exp0 and Exp1 |
| `EXP_IO_CS5#` | `A0h–BFh` | Expansion I/O window; available on both Exp0 and Exp1 |
| `EXP_IO_CS6#` | `C0h–DFh` | Expansion I/O window; available on both Exp0 and Exp1 |

## UART console

The GM16C550 UART provides a COM1-compatible console at `3F8h–3FFh`. Its
1.8432 MHz crystal supports common serial rates; the current BIOS and
`boardctl` use 9600 8N1.

`J9` is a 3-pin TTL UART header (`GND`, `TXD`, `RXD`). To connect the computer
to a USB host, use an external **CP2102 USB-to-TTL adapter**: connect ground
and cross `TXD` with `RXD`. The CP2102 is powered by USB; do not connect its
power pin to the mainboard through `J9`. Verify that the selected CP2102 board
accepts the mainboard's 5 V TTL `TXD` level. This is not an RS-232 interface;
use a MAX232-compatible level converter before connecting an RS-232 port.

## Interrupts and timer

The 8259A PIC operates as a single 8086/8088-mode master and drives the
processor `INTR` input. The 8254 PIT provides the system tick: its channel 0
receives a 1.1931817 MHz clock from a 74LS74 divider and, when programmed with
a divisor of 65,536 in mode 2, produces an 18.2065 Hz interrupt on `IR0`.

| PIC input | PCB connection |
| --- | --- |
| `IR0` | `PIT_IRQ` from the 8254 system timer |
| `IR1` | `EXP_IRQ1` expansion line |
| `IR2` | `EXP_IRQ2` expansion line |
| `IR3` | `EXP_IRQ3` expansion line |
| `IR4` | `UART_IRQ` from the GM16C550 UART |
| `IR5` | `EXP_IRQ5` expansion line |
| `IR6` | `EXP_IRQ6` expansion line |
| `IR7` | `EXP_IRQ7` expansion line |

Each PIC request input has a 10 kΩ pull-down, keeping it inactive when no
device drives the line. Firmware must initialize the PIC and PIT before
enabling interrupts.

## Expansion modules

The mechanical and electrical contract for daughterboards is defined in the
[Expansion Module Standard Rev.2](docs/expansion-module-standard.md).

| Module | Purpose | Documentation |
| --- | --- | --- |
| Display Module | HD44780-compatible character LCD interface | [Configuration and BOM](docs/display-module.md) |
| Smart I/O Module | ESP32-based smart I/O, TFT, and SD interface | [Configuration and BOM](docs/smart-io-module.md) |

## Bill of materials

The table below is derived from [`hardware/kicad`](hardware/kicad) for Rev. 2.0.
It includes both through-hole and SMD components.

| Designators | Qty. | Part / value | Footprint / notes |
| --- | ---: | --- | --- |
| U1 | 1 | Intel 8088, minimum mode | DIP-40, 15.24 mm socket footprint |
| U2, U3 | 2 | 74LS373 | DIP-20, 7.62 mm socket footprint |
| U4 | 1 | 74LS245 | DIP-20, 7.62 mm socket footprint |
| U5 | 1 | 8284 clock generator / driver | DIP-18, 7.62 mm socket footprint |
| U6, U7 | 2 | 74LS138 | DIP-16, 7.62 mm socket footprint |
| U8 | 1 | 28C256 EEPROM | DIP-28 socket footprint; 3M 228-1277-00-0602J |
| U9–U13 | 5 | AS6C1008-55PCN SRAM | DIP-32, 15.24 mm socket footprint |
| U14 | 1 | GM16C550 UART | DIP-40, 15.24 mm socket footprint |
| U15 | 1 | 8259A-2 programmable interrupt controller | DIP-28, 15.24 mm socket footprint |
| U16 | 1 | 8254 programmable interval timer | DIP-24, 15.24 mm socket footprint |
| U17 | 1 | 74LS74 | DIP-14, 7.62 mm socket footprint |
| Q1 | 1 | NDP6020P P-channel MOSFET | TO-220-3, horizontal tab-down |
| Y1 | 1 | 14.31818 MHz crystal | HC-49/U, vertical |
| Y2 | 1 | 1.8432 MHz crystal | HC-49/U, vertical |
| R1–R3, R5–R7, R9–R10, R12, R15–R18, R21–R28 | 21 | 10 kΩ | 0805 SMD |
| R8, R19 | 2 | 1 kΩ | 0805 SMD |
| R11, R20 | 2 | 2 kΩ | 0805 SMD |
| R4 | 1 | 680 Ω | 0805 SMD |
| R13 | 1 | 1 MΩ | 0805 SMD |
| R14 | 1 | 1.5 kΩ | 0805 SMD |
| R29–R44 | 16 | 33 Ω | 0805 SMD; series isolation of the `D0…D7` branches for Exp0 and Exp1 |
| C1–C4, C6–C14, C18–C21 | 17 | 100 nF ceramic | 0805 SMD |
| C5 | 1 | 10 µF electrolytic | Radial, 1.50 mm pitch |
| C15 | 1 | 220 µF electrolytic | Radial, 2.00 mm pitch |
| C16 | 1 | 22 pF ceramic | 0805 SMD |
| C17 | 1 | 47 pF ceramic | 0805 SMD |
| D1 | 1 | Reset LED | 0805 SMD |
| D2 | 1 | Power LED | 0805 SMD |
| SW1 | 1 | 3-position DIP switch | SPST ×3, 2.54 mm pitch |
| SW2 | 1 | Reset pushbutton | 6 mm THT tactile switch |
| J1 | 1 | 10-pin connector | 1×10 pin header, 2.54 mm pitch |
| J2 | 1 | External reset connector | 1×2 pin header, 2.54 mm pitch |
| J3 | 1 | External clock connector | 1×2 pin header, 2.54 mm pitch |
| J4, J6 | 2 | Expansion connectors (Exp0-A, Exp1-A) | 2×20 pin socket, 2.54 mm pitch |
| J5, J7 | 2 | Expansion connectors (Exp0-B, Exp1-B) | 2×10 pin socket, 2.54 mm pitch |
| J8 | 1 | 5 V power input | 2-pin Phoenix MKDS-1,5, 5.08 mm pitch |
| J9 | 1 | CP2102 module connector | 1×3 pin header, 2.54 mm pitch |
| J10, J11 | 2 | 4-pin auxiliary connectors | 1×4 pin header, 2.54 mm pitch |

For ICs, sockets are recommended; the PCB footprints already provide for them.

## Links

- [DIY EEPROM Programmer](https://github.com/erikvanzijst/eeprom)
