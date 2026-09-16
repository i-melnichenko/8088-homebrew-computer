# Smart I/O Module configuration

This document records the expansion-bus resource selection for the Smart I/O
Module PCB in `hardware/kicad/smart-io-module/`.

## Design files

[KiCad project](../hardware/kicad/smart-io-module/smart-io-module.kicad_pro)

## Bill of materials

The table below is derived from the KiCad PCB. It includes both through-hole
and SMD components.

| Designators | Qty. | Part / value | Footprint / notes |
| --- | ---: | --- | --- |
| U1 | 1 | ESP32-S3-DevKitC | DevKitC module footprint |
| U2 | 1 | 74HC595 | DIP-16, 7.62 mm socket footprint |
| U3 | 1 | 74LS02 | DIP-14, 7.62 mm socket footprint |
| U4, U8 | 2 | 74LS373 | DIP-20, 7.62 mm socket footprint |
| U5–U7 | 3 | SN74LVC245AN | DIP-20, 7.62 mm socket footprint |
| R1, R5–R7 | 4 | 10 kΩ | 0805 SMD |
| R2 | 1 | 330 Ω | 0805 SMD |
| R3, R4, R9–R16 | 10 | 33 Ω | 0805 SMD |
| R8 | 1 | 1 kΩ | 0805 SMD |
| C1–C7 | 7 | 100 nF ceramic | 0805 SMD |
| C8 | 1 | 220 µF electrolytic | Radial, 2.00 mm pitch |
| D1 | 1 | SS14 | SMA Schottky diode |
| D2–D4 | 3 | BAT54WS | SOD-323 Schottky diode |
| JP1–JP4 | 4 | Open solder jumper | 2-pad, 1.3 mm pitch; CS and IRQ selection |
| J1 | 1 | TFT connector | Right-angle 1×10 male header, 2.54 mm pitch |
| J2 | 1 | SD connector | Right-angle 1×6 male header, 2.54 mm pitch |
| J3 | 1 | Expansion connector A | Right-angle 2×20 male header, 2.54 mm pitch |
| J4 | 1 | Expansion connector B | Right-angle 2×10 male header, 2.54 mm pitch |

## TFT display and microSD module

The board is designed to connect a **2.8-inch, 240×320 SPI TFT module based on
the ILI9341**, with an integrated microSD-card socket.
The pictured module is marked `2.8 TFT SPI 240*320 V1.2` and exposes the TFT
and microSD interfaces on the same SPI bus.

`J1` connects the TFT interface; `J2` connects the microSD interface. The
devices share the SPI clock and data lines, and use independent chip-select
signals, `TFT_CS` and `SD_CS`. The TFT additionally uses `TFT_RST` and
`TFT_DC`.

Both connectors provide **3.3 V** power. Do not connect a 5 V-only TFT or SD
module directly to `J1` or `J2`.

## I/O chip-select

The PCB has no factory-selected I/O window: `JP1` and `JP2` are open solder
jumpers. Before installing the module, bridge exactly one jumper.

| Jumper to bridge | Selected signal | I/O window |
| --- | --- | --- |
| `JP1` | `EXP_IO_CS5#` | `A0h`–`BFh` |
| `JP2` | `EXP_IO_CS6#` | `C0h`–`DFh` |

Do not bridge both jumpers. The selected window must not be assigned to any
other installed module.

## Interrupt request

The PCB has no factory-selected interrupt: `JP3` and `JP4` are open solder
jumpers. If interrupt operation is enabled, bridge exactly one jumper and
configure firmware for the matching 8259A request mode.

| Jumper to bridge | Selected signal |
| --- | --- |
| `JP3` | `EXP_IRQ5` |
| `JP4` | `EXP_IRQ6` |

Leave both jumpers open when the module does not request interrupts. Never
bridge both jumpers.

## Assembly record

Record the actual resource allocation for each assembled system:

| Field | Value |
| --- | --- |
| Bridged I/O chip-select jumper | _to be recorded at assembly_ |
| Selected I/O window | _derived from jumper_ |
| Bridged IRQ jumper, or `none` | _to be recorded at assembly_ |
| Internal I/O-port assignments | _to be recorded by firmware configuration_ |
