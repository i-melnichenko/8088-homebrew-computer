# Expansion Module Standard Rev.2

This document defines the mechanical and electrical contract for daughterboards
used with the `Exp0` and `Exp1` slots of the 8088 Homebrew Computer. It is
derived from the matching geometry of the existing Display and Smart I/O
modules. New modules should follow this standard.

Module-specific resource-selection records:

- [Display Module configuration](display-module.md)
- [Smart I/O Module configuration](smart-io-module.md)

## Mechanical specification
![Mechanical drawing with dimensions in mils](./docs/expansion-module-mechanical.svg?3)

- Board outline: **4800 × 2900 mil** (**121.92 × 73.66 mm**; 48 × 29 pitches of 2.54 mm).
- Corners: 4 mm radius.
- Connector side: top (`F.Cu`). Connector A and Connector B must be installed
  on this side in the orientation specified below. Other components may be
  placed on either side of the PCB.
- Coordinate origin: the top-left outline corner when viewing the connector
  side; X increases to the right and Y increases downward.

| Feature | Purpose / type | Coordinates, mm |
| --- | --- | --- |
| Connector A, pad 1 | System bus; right-angle 2×20, 2.54 mm pitch, male header | X=101.60, Y=68.58 |
| Connector B, pad 1 | Arbitration / CS / IRQ; right-angle 2×10, 2.54 mm pitch, male header | X=43.18, Y=68.58 |
| H1 centre | M3 mounting hole; 3.2 mm drill, 6.4 mm plated GND pad | X=116.84, Y=68.58 |

The mainboard carries the mating female sockets. A module uses right-angle male
headers and sits above the mainboard; its header tails extend past the lower
PCB edge. Pad 1 must be rectangular and visibly marked on silkscreen.

H1 is a plated mounting hole connected to GND. Keep the 6.9 mm courtyard clear
of components and traces. If conductive hardware is used, it will be grounded;
use an insulating washer only if that is not desired.

## Connector orientation

Connector-side view with connectors at the lower edge: pad 1 is the
rectangular **upper-right** pad. Odd pins are on the upper row; even pins are
on the lower row; numbering runs right to left.

```text
A (2×20):  upper  39 ... 1     lower  40 ... 2
B (2×10):  upper  19 ... 1     lower  20 ... 2
```

## Electrical interface

The interface uses **+5 V TTL** power and logic levels. It is not hot-plug
capable: only connect or disconnect a module with power off. A module must
share ground, must not drive `VCC`, and must not actively drive `D0…D7` outside
an authorised bus cycle.

### Data-bus rules

Use a **33 Ω series resistor** on each `D0…D7` branch that a module can drive;
place the resistors close to the connector or the module bus driver. They damp
edges and reduce fault current during accidental contention. A module that only
reads `D0…D7` and cannot drive them may omit its local resistors: the mainboard
already includes 33 Ω series resistors on each slot data branch. Additional
33 Ω resistors on a module are acceptable where further isolation is desired.

Series resistors do **not** provide bus arbitration. At most one module may
drive `D0…D7` at a time. In particular, two modules must never write data or
respond to the same read cycle simultaneously. Assign every module an unambiguous
chip-select and ensure its bus transceiver or output-enable is inactive outside
its authorised bus cycle.

### Resource selection

New modules should preferably provide configuration jumpers (for example,
solder jumpers or a DIP switch) to select their chip-select. An
interrupt-capable module should similarly provide a way to select its
`EXP_IRQ*` line or disable interrupt requests. A finished assembly must select
at most one chip-select and, when interrupts are enabled, at most one IRQ line;
record the selected resources and occupied address range in the module's
configuration document.

### Connector A — system bus, 2×20

`#` denotes an active-low signal. `N/A` means that the pin is power, ground, or
a binary data/address signal rather than an asserted control signal.

| Pin | Signal | Direction (module) | Active level | Description |
| ---: | --- | --- | --- | --- |
| 1 | GND | Ground | N/A | Logic and power return. |
| 2 | VCC (+5 V) | Power in | N/A | +5 V TTL supply. A module must not drive this pin. |
| 3 | IOM | Input | High = I/O | Cycle type from the CPU: High selects I/O space; Low selects memory space. |
| 4 | WR# | Input | Low | Write strobe. A selected device captures `D0…D7` while this signal is asserted. |
| 5 | RD# | Input | Low | Read strobe. A selected device may drive `D0…D7` only while this signal is asserted. |
| 6 | RDY | Bidirectional | High = ready | Wait-state control: High completes the bus cycle; Low extends it with wait states. |
| 7 | RESET | Input | High | System reset input to the module. |
| 8 | NMI | Output | Rising edge | Non-maskable interrupt request to the CPU. |
| 9 | ALE | Input | High pulse | Address-latch enable; marks a valid multiplexed address phase. |
| 10 | GND | Ground | N/A | Logic and power return. |
| 11 | D7 | Bidirectional | N/A | Bidirectional data bit 7. |
| 12 | D6 | Bidirectional | N/A | Bidirectional data bit 6. |
| 13 | D5 | Bidirectional | N/A | Bidirectional data bit 5. |
| 14 | D4 | Bidirectional | N/A | Bidirectional data bit 4. |
| 15 | D3 | Bidirectional | N/A | Bidirectional data bit 3. |
| 16 | D2 | Bidirectional | N/A | Bidirectional data bit 2. |
| 17 | D1 | Bidirectional | N/A | Bidirectional data bit 1. |
| 18 | D0 | Bidirectional | N/A | Bidirectional data bit 0. |
| 19 | GND | Ground | N/A | Logic and power return. |
| 20 | GND | Ground | N/A | Logic and power return. |
| 21 | A19 | Input | N/A | Address bit 19, CPU output; High represents binary 1. |
| 22 | A18 | Input | N/A | Address bit 18, CPU output; High represents binary 1. |
| 23 | A17 | Input | N/A | Address bit 17, CPU output; High represents binary 1. |
| 24 | A16 | Input | N/A | Address bit 16, CPU output; High represents binary 1. |
| 25 | A15 | Input | N/A | Address bit 15, CPU output; High represents binary 1. |
| 26 | A14 | Input | N/A | Address bit 14, CPU output; High represents binary 1. |
| 27 | A13 | Input | N/A | Address bit 13, CPU output; High represents binary 1. |
| 28 | A12 | Input | N/A | Address bit 12, CPU output; High represents binary 1. |
| 29 | A11 | Input | N/A | Address bit 11, CPU output; High represents binary 1. |
| 30 | A10 | Input | N/A | Address bit 10, CPU output; High represents binary 1. |
| 31 | A9 | Input | N/A | Address bit 9, CPU output; High represents binary 1. |
| 32 | A8 | Input | N/A | Address bit 8, CPU output; High represents binary 1. |
| 33 | A7 | Input | N/A | Address bit 7, CPU output; High represents binary 1. |
| 34 | A6 | Input | N/A | Address bit 6, CPU output; High represents binary 1. |
| 35 | A5 | Input | N/A | Address bit 5, CPU output; High represents binary 1. |
| 36 | A4 | Input | N/A | Address bit 4, CPU output; High represents binary 1. |
| 37 | A3 | Input | N/A | Address bit 3, CPU output; High represents binary 1. |
| 38 | A2 | Input | N/A | Address bit 2, CPU output; High represents binary 1. |
| 39 | A1 | Input | N/A | Address bit 1, CPU output; High represents binary 1. |
| 40 | A0 | Input | N/A | Address bit 0, CPU output; High represents binary 1. |

### Connector B — arbitration, device select, and interrupts, 2×10

`#` denotes an active-low signal. `N/A` means that the pin is power, ground, or
a timing signal rather than an asserted control signal.

| Pin | Signal | Direction (module) | Active level | Description |
| ---: | --- | --- | --- | --- |
| 1 | GND | Ground | N/A | Logic and power return. |
| 2 | CLK | Input | Rising edge | 8088 system clock; timing reference for synchronous module logic. |
| 3 | HOLD | Output | High | Bus-ownership request to the CPU. Assert only when the module requires bus-master access. |
| 4 | HLDA | Input | High | Hold acknowledge from the CPU. The CPU has released its local bus while asserted. |
| 5 | DT_R | Input | High = transmit | Data-transceiver direction: High means CPU-to-bus; Low means bus-to-CPU. |
| 6 | DEN# | Input | Low | Data-transceiver enable. Asserted during memory, I/O, and interrupt-acknowledge accesses. |
| 7 | EXP_IO_CS4# | Input | Low | Active-low I/O-space chip-select 4 for an external device. |
| 8 | EXP_IO_CS5# | Input | Low | Active-low I/O-space chip-select 5 for an external device. |
| 9 | EXP_IO_CS6# | Input | Low | Active-low I/O-space chip-select 6 for an external device. |
| 10 | GND | Ground | N/A | Logic and power return. |
| 11 | EXP_MEM_CS5# | Input | Low | Active-low memory-space chip-select 5 for an external device. |
| 12 | EXP_MEM_CS6# | Input | Low | Active-low memory-space chip-select 6 for an external device. |
| 13 | EXP_IRQ1 | Output | High / rising edge | External interrupt request to PIC IR1. The firmware must use a PIC mode compatible with the module. |
| 14 | EXP_IRQ2 | Output | High / rising edge | External interrupt request to PIC IR2. The firmware must use a PIC mode compatible with the module. |
| 15 | EXP_IRQ3 | Output | High / rising edge | External interrupt request to PIC IR3. The firmware must use a PIC mode compatible with the module. |
| 16 | EXP_IRQ5 | Output | High / rising edge | External interrupt request to PIC IR5. The firmware must use a PIC mode compatible with the module. |
| 17 | EXP_IRQ6 | Output | High / rising edge | External interrupt request to PIC IR6. The firmware must use a PIC mode compatible with the module. |
| 18 | EXP_IRQ7 | Output | High / rising edge | External interrupt request to PIC IR7. The firmware must use a PIC mode compatible with the module. |
| 19 | GND | Ground | N/A | Logic and power return. |
| 20 | GND | Ground | N/A | Logic and power return. |

Each module must use a unique `EXP_IO_CS*` or `EXP_MEM_CS*` signal for every
decoded device. An IRQ-capable module must use an unassigned `EXP_IRQ*` line and
configure its request style to match the 8259A PIC mode selected by firmware.

## New-module release checklist

- Verify the connector pad-1 and H1 coordinates against the mechanical table.
- Confirm pin 1 is marked and the module mechanically mates with the mainboard.
- Run ERC and DRC, then inspect Gerbers, including Edge.Cuts, the mounting-hole
  pad, and the protruding right-angle headers.
- Document the selected chip-select, IRQ, and occupied I/O or memory addresses.
