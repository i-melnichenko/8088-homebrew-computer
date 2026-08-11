# LCD Status Demo

This is a standalone ROM demo for the 8088 Homebrew Computer. It initializes
an HD44780-compatible 20x4 LCD attached to the `Exp1` connector, displays a
four-line status screen, and animates the final character as a heartbeat.

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

This produces `demo.bin`, a complete 32 KiB EEPROM image. Program the image
into the 28C256, install it in the mainboard, connect the display to `Exp1`,
and reset the system.

To program the EEPROM, you can use the
[DIY EEPROM Programmer](https://github.com/erikvanzijst/eeprom) project.

The source assumes standard 20x4 HD44780 DDRAM line offsets. For a typical
16x4 display, set `LINE3_CMD` to `090h` and `LINE4_CMD` to `0D0h` in
`demo.asm` before building.

`demo.bin` is a build artifact and is intentionally not stored in Git. Run
`make clean` to remove it.
