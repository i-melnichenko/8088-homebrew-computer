# Operating systems

HOME8088 OS and Microsoft MS-DOS 2.0 are supported operating systems for this PC.
Both boot through the board's PC BIOS and use the UART floppy provided by
`boardctl terminal --disk IMAGE --boot`. Images are writable by default;
protect them with `--disk-readonly`.

| Target | Sources | Build | Disk image |
| --- | --- | --- | --- |
| HOME8088 OS | [home8088/](home8088/README.md) | `make os` | `build/home8088.img` |
| Microsoft MS-DOS 2.0 | [msdos/](msdos/README.md) | `make msdos` | `build/msdos20.img` |
| MS-DOS with Zork I | [msdos/zork/](msdos/zork/README.md) | `make msdos-zork` | `build/msdos20-zork.img` |

Run these commands from `firmware/8088-mainboard`. `make bios` builds the shared
ROM. The DOS targets explicitly fetch pinned upstream files; the default build
stays offline.

HOME8088 provides a small native shell and the CITY, TIMER and TERMINAL
applications. MS-DOS provides the original Microsoft DOS kernel and command
interpreter with the board's OEM I/O layer; DEBUG and Zork I are validated DOS
applications. The Zork image is an MS-DOS configuration with an additional game.

Each OS keeps its applications in its own `programs/` directory. HOME8088's
native COM contract and the DOS API are different; applications are built for
their respective OS. Zork's package-specific preparation lives in `msdos/zork/`;
common image-building scripts remain in `../scripts/`.

Generated images and caches live in `build/` and are removed by `make clean`.
Keep persistent DOS files and game saves in working copies under ignored
`disks/`, which survives cleaning. HOME8088's kernel only reads its filesystem;
MS-DOS supports file and directory writes through the shared BIOS.
