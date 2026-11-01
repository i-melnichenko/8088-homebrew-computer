# CITY

Native COM application for [HOME8088 OS](../../README.md), using BIOS services.
Build the FAT12 image with `make os` from `firmware/8088-mainboard`.

After booting the image through `boardctl terminal --disk IMAGE --boot`, run:

```text
RUN CITY
```

Esc returns to the OS shell. The application owns its OS-provided segment and
stack and terminates with INT 20h. BIOS/boardctl application uploads are not used.

Citylights animates only the LCD. UART carries plain startup/help and exit
messages, without ANSI screen drawing or animation frames. Keyboard-only mode
suppresses UART messages. It uses the BIOS hardware timer when available and
approximate software delays otherwise. LCD presence is not hardware-detected.
