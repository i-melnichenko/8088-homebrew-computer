# ROM BIOS and LCD Status Demo

The default firmware is a minimal ROM BIOS for the 8088 Homebrew Computer. It
initializes an HD44780-compatible 20x4 LCD attached to `Exp1`, reports its
progress, copies an embedded program into SRAM, and passes control to it.
The RAM program then replaces the status screen and animates a heartbeat.

## Requirements

- 8088 mainboard with a 28C256 EEPROM
- HD44780-compatible 20x4 display module connected to `Exp1`
- [NASM](https://www.nasm.us/)

The display module uses the `C0h` and `C1h` I/O ports: `C0h` writes LCD
commands and `C1h` writes character data.

## Build

From this directory, run:

```sh
make
```

This produces `bios.bin`, a complete 32 KiB EEPROM image. Program the image
into the 28C256, install it in the mainboard, connect the display to `Exp1`,
and reset the system.

`bios.asm` copies `ram-program.bin` to `0800:0200` (physical address `08200h`)
and executes a far jump to that address. `ram-program.asm` is assembled with
`ORG 0200h`, so labels inside the payload remain valid after copying. Replace
it with another flat 16-bit program assembled for `0800:0200` to change what
the BIOS loads. Keep it below the stack at physical `0FFFEh`.

To program the EEPROM, you can use the
[DIY EEPROM Programmer](https://github.com/erikvanzijst/eeprom) project.

The source assumes standard 20x4 HD44780 DDRAM line offsets. For a typical
16x4 display, set `LINE3_CMD` to `090h` and `LINE4_CMD` to `0D0h` in
`demo.asm` before building.

The original standalone LCD program is still available: run `make demo` to
build `demo.bin`. Build artifacts are intentionally not stored in Git; run
`make clean` to remove them.
