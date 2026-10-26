# Zork I on Microsoft MS-DOS

Build a separate disk with Zork, keeping the basic DOS image available.
From the repository root:

```sh
make -C firmware/8088-mainboard bios msdos-zork BIOS_VERSION=dev-disk
go -C tools/boardctl build -o boardctl ./cmd
```

This explicit target downloads SHA-256-verified, pinned inputs from
`upstream.json`: the unmodified Zork I story (release 119, serial 880429)
and MIT license from [Historical Source](https://github.com/historicalsource/zork1),
Infocom's [generic MS-DOS 2.0 interpreter version G](https://github.com/erkyrath/infocom-zcode-terps/tree/master/msdos),
and the [JWasm](https://github.com/JWasm/JWasm) source archive. Release 119 is the
archived final development version, rather than the commercial release 88.
The interpreter is compiled with the 8086 instruction set restriction. The build
requires NASM, Python 3, make, Go for boardctl, and a host C compiler
(Xcode command-line tools on macOS,
or a C toolchain on Linux); `CC` can override the compiler. Downloads, toolchain,
generated assembly and build logs stay in ignored `build/zork-upstream/`.
Once cached, the target can build offline. No game/interpreter binaries are
committed to the project; Zork's license is included as ZORKLIC.TXT on the disk.

Two adaptations are applied to the historical interpreter source: its fixed
WITNESS filenames become ZORK1, and its uninitialized game segment is moved
last so DOS allocates 56 KiB of working memory without loading zeros from disk.
Its VM and DOS I/O routines are retained. SETUP.INF selects 25 rows, 80 columns
and plain ASCII output, avoiding ANSI status-line redraws on the UART console.

Use the board with **256 KiB RAM or more**, including RAM0, and working PIC/PIT.
Install the BIOS with disk-write support (`dev-disk`) when updating firmware.
From the repository root, prepare a working image for persistent saves:

```sh
mkdir -p firmware/8088-mainboard/disks
cp -n firmware/8088-mainboard/build/msdos20-zork.img firmware/8088-mainboard/disks/msdos20-zork.img
```

`cp -n` preserves existing saves in an existing working image. After exiting the
current terminal and pressing Reset, run from `tools/boardctl`:

```sh
./boardctl -port /dev/cu.usbserial-0001 terminal \
  --disk ../../firmware/8088-mainboard/disks/msdos20-zork.img --boot
```

At `A>`, enter `ZORK1`. Loading DOS and then the game involves UART disk reads;
allow time for the opening description and the game's `>` prompt. Try:

```text
look
open mailbox
read leaflet
north
inventory
```

`quit`, followed by `y`, returns to DOS. The game uses the UART terminal; it
does not render on the 20x4 LCD.

The working image is writable by default. At the game prompt, enter `save` and
press Enter to accept ZORK1.SAV. `restore` and Enter reload that file; saved
positions also survive reboot. Reconnect with the same working image, omitting
`--boot`. Add `--disk-readonly` for a protected session without writes; existing
saves can still be restored. See [writable disk instructions](../README.md#writable-disks).
Rebuilding the base image replaces it; the working image in `disks/` survives
both rebuilds and `make clean`.

The optional `tests/test_zork.py` boots the image in the board emulator, loads
the MZ executable through Microsoft DOS, exercises FCB game-file paging, reads
the leaflet, moves north, quits and verifies DOS still responds to VER. This
validates the software path; physical hardware timing remains to be checked.
An additional test saves a position, moves, restores it, and restores the same
position after reboot from the modified image.

