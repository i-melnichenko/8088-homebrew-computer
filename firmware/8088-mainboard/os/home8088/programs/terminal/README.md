# TERMINAL

Native COM application for [HOME8088 OS](../../README.md), using BIOS services.
Build the FAT12 image with `make os` from `firmware/8088-mainboard`.

After booting the image through `boardctl terminal --disk IMAGE --boot`, run:

```text
RUN TERMINAL
```

Esc returns to the OS shell. The application owns its OS-provided segment and
stack and terminates with INT 20h. BIOS/boardctl application uploads are not used.

Type to echo UART input onto the LCD. Esc exits. Input and output use the shared
BIOS keyboard/terminal transport.
