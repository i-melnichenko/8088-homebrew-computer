# Display Module configuration

This document records the expansion-bus resource selection for the Display
Module PCB in `hardware/kicad/display-module/`.

## Design files

[KiCad project](../hardware/kicad/display-module/display-module.kicad_pro)

## Bill of materials

The table below is derived from the KiCad PCB. It includes both through-hole
and SMD components.

| Designators | Qty. | Part / value | Footprint / notes |
| --- | ---: | --- | --- |
| DS1 | 1 | WC1604A LCD | 1×16, 2.54 mm horizontal header footprint |
| U1 | 1 | 74LS373 | DIP-20, 7.62 mm socket footprint |
| U2 | 1 | 74LS02 | DIP-14, 7.62 mm socket footprint |
| R1 | 1 | 650 Ω | THT axial, DIN0207, 7.62 mm pitch |
| RV1 | 1 | 10 kΩ | Runtron RM-065 vertical trimmer |
| C1 | 1 | 220 µF electrolytic | Radial, 2.00 mm pitch |
| C2, C3 | 2 | 100 nF ceramic | THT disc, 5.00 mm pitch |
| J1 | 1 | Expansion connector A | Right-angle 2×20 male header, 2.54 mm pitch |
| J2 | 1 | Expansion connector B | Right-angle 2×10 male header, 2.54 mm pitch |

## I/O chip-select

The PCB has no factory-selected I/O window: `JP1` and `JP2` are open solder
jumpers. Before installing the module, bridge exactly one jumper.

| Jumper to bridge | Selected signal | I/O window |
| --- | --- | --- |
| `JP1` | `EXP_IO_CS4#` | `80h`–`9Fh` |
| `JP2` | `EXP_IO_CS5#` | `A0h`–`BFh` |

Do not bridge both jumpers. The selected window must not be assigned to any
other installed module.

## Interrupts

The Display Module does not use an `EXP_IRQ*` line. Its `NMI` connector pin is
also not used by the display logic.

## Assembly record

Record the actual bridge and the display register addresses used by software
for each assembled system:

| Field | Value |
| --- | --- |
| Bridged jumper | _to be recorded at assembly_ |
| Selected chip-select | _derived from jumper_ |
| Display command-register address | _to be recorded by software configuration_ |
| Display data-register address | _to be recorded by software configuration_ |
