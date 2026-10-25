# REACTOR

Native COM application for [HOME8088 OS](../../os/README.md), using BIOS services.
Build the FAT12 image with `make os` from `firmware/8088-mainboard`.

After booting the image through `boardctl terminal --disk IMAGE --boot`, run:

```text
RUN REACTOR
```

Esc returns to the OS shell. The application owns its OS-provided segment and
stack and terminates with INT 20h. BIOS/boardctl application uploads are not used.

Reactor is a three-minute resource game displayed on LCD and/or ANSI terminal.
Enter starts; +/- adjusts power; C cools; V vents; P pauses; R restarts.
Arrow keys/A/D select controls, Enter activates. Keys 1/2/3 select UART/LCD/both.
The game uses the 18.2065-Hz PIT clock with fractional accumulation; without
hardware time it uses an approximate training clock.
