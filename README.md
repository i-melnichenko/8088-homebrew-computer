# 8088 Homebrew Computer

An open-source modular computer built around the Intel 8088 CPU.

## Overview

This project explores the design of a modular 8088-based computer using
through-hole DIP components and a custom expansion architecture.

The project is developed incrementally, starting with a minimal working
system and gradually adding new hardware and software capabilities.

## Firmware

The default firmware is a [minimal ROM BIOS](firmware/8088-mainboard/bios.asm).
It builds a 32 KiB EEPROM image, initializes an HD44780-compatible 20×4 LCD
on `Exp1`, copies the bundled program to SRAM, and transfers control to it.

## Mainboard Rev. 2.0

| Front |
| --- |
| [![Front render](hardware/kicad/renders/rev2.0-front.png)](hardware/kicad/renders/rev2.0-front.png) |

### Bill of materials

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

### Display module

The table below is derived from the
[`display-module` KiCad project](hardware/kicad/display-module/display-module.kicad_pro).

| Designators | Qty. | Part / value               | Footprint / notes |
| --- | ---: |----------------------------| --- |
| U1 | 1 | 74LS373                    | DIP-20, 7.62 mm socket footprint |
| U2 | 1 | 74LS00                     | DIP-14, 7.62 mm socket footprint |
| DS1 | 1 | WC1604A LCD                | 1×16, 2.54 mm right-angle pin header |
| C1 | 1 | 220 µF electrolytic        | Radial, 2.00 mm pitch |
| C2, C3 | 2 | 100 nF ceramic             | Disc capacitor, 5.00 mm pitch |
| R1 | 1 | 650 Ω                      | DIN0207 axial, 7.62 mm pitch |
| RV1 | 1 | 10 kΩ potentiometer        | Runtron RM-065, vertical |
| J1 | 1 | Expansion connector (Exp0-A) | 2×20 pin header, 2.54 mm pitch |
| J2 | 1 | Expansion connector (Exp0-B) | 2×10 pin header, 2.54 mm pitch |

## Links

- [DIY EEPROM Programmer](https://github.com/erikvanzijst/eeprom)
