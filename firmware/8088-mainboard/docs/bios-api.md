# ROM BIOS software-interrupt API

API revision **1** runs on the revision-1 8088 board using real software
interrupts. It is independent of the UART wire protocol version (also 1).
This is a documented subset of PC-style BIOS services plus board extensions,
not a complete IBM PC BIOS or a claim that stock MS-DOS can boot.

## Memory and execution

The current decoder selects RAM for physical 00000h..1FFFFh. Its 32-KiB
SRAM uses A0..A14 only: addresses separated by 8000h refer to the same bytes.
They are aliases, not additional memory. BIOS keeps its existing upper-alias
execution convention, reserving the corresponding low-memory structures:

| Canonical SRAM bytes | Upper alias | Purpose |
| --- | --- | --- |
| 0000..03FF | 8000..83FF | 256 interrupt vectors, offset:segment |
| 0400..04FF | 8400..84FF | BIOS Data Area, partial implementation |
| 0500..06FF | 8500..86FF | monitor buffers, screen, registry |
| 0700..07FF | 8700..87FF | API LCD framebuffer, cursor, keyboard and EEPROM mode parameters |
| 0800..7BFF | 8800..FBFF | program allocations, 29,696 bytes minus alignment gaps |
| 7C00..7DFF | FC00..FDFF | program stack |
| 7E00..7FFF | FE00..FFFF | BIOS stack |

The vectors for 10h, 14h, 16h and 60h point into ROM F800:xxxx. Other vectors
currently lead to the unsupported-service handler. This is not exception
recovery: keep hardware interrupts disabled on revision 1. Software INT
works with IF=0 and does not require a PIC.

Programs remain flat 8086-compatible ORG-0 images. BIOS far-calls their
allocated segment at offset 0, with DS=CS, IF=0 and DF=0. It sets SP=FDFE
before CALL; the program sees SP=FDFA because the far return address uses
four bytes. Preserve SS/SP and use RETF to return to the menu.

Each INT uses the caller's stack and returns with IRET. Unless specified
below, AX/BX/CX/DX/SI/DI/BP/DS/ES and the caller's flags are preserved.
Handlers clear DF internally, then restore the caller's DF/IF through IRET.
Invalid function numbers/parameters return CF=1 and AH=86h; this error
convention is a board extension, not a claim about unspecified PC BIOS cases.
Never access service internals or depend on handler addresses.

The BDA currently initializes COM1 base (0040:0000 = A0h), physical memory
size (0040:0013 = 32 KiB), keyboard ring head/tail (001A/001C) and buffer
(001E..003D, 16 word slots with at most 15 queued keys), display mode
(0049 = custom 7Fh), columns (004A = 20),
framebuffer byte count (004C = 80), page-0 cursor (0050), and last row
(0084 = 3). Other fields are reserved, not implemented PC services.
There is no PC video memory at B800:0000 and no attribute framebuffer.

## INT 10h — LCD text console

Only page BH=0 is supported, except AH=06h where BH is the ignored attribute.
Coordinates are zero-based, row 0..3 and column 0..19.

| AH | Inputs | Operation / outputs |
| --- | --- | --- |
| 02h | BH=0, DH=row, DL=column | Set logical cursor and LCD DDRAM position |
| 03h | BH=0 | Return DX=row:column, CX=2000h (hidden cursor) |
| 06h | AL=lines, BH=attribute, CH/CL=top/left, DH/DL=bottom/right | Scroll the inclusive rectangle upward; AL=0 or AL>=height clears it |
| 0Eh | BH=0, AL=character | Write character, advance cursor and scroll when necessary |

Attributes/colors are ignored on this monochrome character LCD. Teletype
interprets CR (column zero), LF (next row, retaining column), BS (move left,
without erasing or crossing the row boundary), and BEL (no speaker, no-op).
Writing the final column wraps; reaching past row 3 scrolls the whole screen.
Valid video calls preserve FLAGS. This 20x4 custom mode is not PC mode 3.

## INT 14h — serial port

DX is the BIOS port index, **0** for the sole UART, not the A0h I/O address.

| AH | Inputs | Outputs / operation |
| --- | --- | --- |
| 00h | DX=0, AL=E3h | Initialize 9600 8N1, reset FIFOs and keyboard queue; return status as AH=03h |
| 01h | DX=0, AL=byte | Transmit; AL retained, AH=line status or timeout |
| 02h | DX=0 | Receive; AL=byte on success, AH=line status or timeout |
| 03h | DX=0 | AH=line status, AL=modem status |

Initialization configurations other than E3h are not supported yet.
Transmit and receive poll at most 65,535 iterations. On timeout AH bit 7 is
set; do not use the receive AL then. The timeout is CPU-speed dependent,
not a millisecond clock. AH bits 0..6 come from the UART LSR; bit 7 is the
BIOS timeout indicator, not the 16550 FIFO-error-summary bit. Flags and
registers other than AX are preserved on valid calls. No UART IRQ is enabled.
While host keyboard sharing is enabled, AH=02h returns CF=1/AH=86h because
framed keyboard traffic owns UART RX. TX/status remain available; initialization
resets the FIFO and keyboard queue but does not disable sharing. UART text
output is not displayed by the host keyboard command.

## INT 16h — polled keyboard

| AH | Outputs / operation |
| --- | --- |
| 00h | Wait for a key and consume it; AL=ASCII, AH=scan code |
| 01h | ZF=1 when empty (AX unchanged); otherwise ZF=0, AX=key, without consuming |
| 02h | AL=0 modifier state; no physical keyboard modifier tracking yet |

ASCII letters, digits, basic punctuation, Esc, Tab, Backspace and CR have
PC-style scan codes. Uppercase letters use the same scan code as lowercase.
Other raw received bytes have scan code zero. In raw mode CR and LF are
distinct bytes; terminal arrow sequences are not decoded by BIOS.
Only AH=01h changes ZF; other FLAGS are retained.

`boardctl keyboard` enables framed keyboard sharing before program launch.
The host translates terminal arrows, Home/End, Insert/Delete, Page Up/Down
and F1..F10 into keys with AL=0 and a PC-style scan code in AH. Enter becomes
0D/1C, Backspace 08/0E and Esc 1B/01. No modifier/release events or PS/2
decoding are implemented. The BIOS menu consumes the same BDA queue as programs.

There are **no hardware interrupts on revision 1**. Both AH=00h and AH=01h
pump the UART frame decoder, at most one complete request per call. AH=00h
continues polling until a key is available; AH=01h is suitable for cooperative
program loops. Long work without these calls may cause UART overflow or host
timeouts. The 15-key ring cannot prevent hardware FIFO overflow before polling.
Software INT does not make input asynchronous.

For a program doing other work, poll between bounded work slices:

~~~asm
.loop:
    bios_key_peek                 ; INT 16h/AH=01h: pump UART, leave key queued
    jz .work
    bios_key_read                 ; INT 16h/AH=00h: consume, AX=scan:ASCII
    ; Handle AX here.
.work:
    ; Do a short slice of work; do not HLT waiting for a nonexistent IRQ.
    jmp .loop
~~~

In sharing mode, the program-side poll accepts keyboard mode/events and PING;
BIOS_WRITE receives status 17 (unsupported) on revision 1; other valid monitor
commands, including BIOS_FLASHER_MODE and BIOS_READ, receive status 11 (program
busy). Uploads remain
available only in the ROM menu. Sharing survives RETF, but Reset disables it.
Attach from the menu, not while an existing raw-UART program is running.
Raw UART and keyboard input share one port: in sharing mode INT 14h receive
is rejected; in raw mode it cannot consume keys already queued by INT 16h.
Do not mix the two raw input paths while a key is pending.

## INT 60h — board LCD extensions

These are board-specific functions, not replacements for DOS INT 21h.
Success clears CF; failure sets CF and AH=86h. Other flags are retained.

| AH | Inputs | Outputs / operation |
| --- | --- | --- |
| 00h | none | AX=1 revision, BX=B105h signature, CH=4 rows, CL=20 columns, DX=001Fh capabilities |
| 01h | none | Clear LCD/framebuffer and reset cursor; retain pending keyboard input |
| 02h | DH=row, DS:SI=ASCIZ | Replace one line, truncate/pad to 20 cells; cursor retained |
| 03h | AL=glyph 0..7, DS:SI=8 bytes | Load 5x8 CGRAM glyph; only low five bits of each byte used |
| 04h | DH=row, DL=column, AL=raw byte | Write one cell without interpreting controls, advancing cursor or scrolling |
| 05h | DS:SI=ASCIZ | Print string using console teletype rules |

Capabilities bits: 0 text console, 1 UART, 2 keyboard, 3 LCD extensions,
4 host keyboard sharing (cooperative polling, not an IRQ-driven keyboard).
Line/string/glyph functions preserve DS, SI and all other non-output
registers. Strings and glyph buffers belong to the caller; these calls
perform no memory isolation or bounds validation against the program image.
ASCIZ printing requires a valid terminator. After CGRAM operations BIOS
restores the LCD's DDRAM cursor.

## SDK and example

Include firmware/8088-mainboard/lib/bios_api.inc when assembling with
NASM -I lib/. It contains constants and lightweight INT macros, no LCD/UART
drivers. A macro may set argument registers before the INT; register
preservation applies to the interrupt itself, not those setup instructions.

For example:

~~~asm
BITS 16
CPU 8086
ORG 0
%include "bios_api.inc"

    bios_lcd_clear
    bios_print message
    bios_key_read
    retf

message: db 'Hello from INT!', 0
~~~

Both bundled examples use this API. The city animation loads CGRAM glyphs
through INT 60h; the UART terminal reads keys through INT 16h, echoes through
INT 14h and writes LCD cells through INT 60h.
The animation also polls INT 16h between short delay slices; Esc ends its loop.

## Verification and compatibility direction

`make test` from `firmware/8088-mainboard` runs only inside Docker. The emulator maps all four SRAM aliases
to one backing store and emulates CPU INT vector dispatch; the actual ROM
handler and IRET instructions execute in Unicorn. Tests cover vectors/BDA,
upload and memory-test isolation, caller segments/registers/flags/stack,
cursor/scrolling/CGRAM, UART status/timeout, keyboard peek/get and return.
They do not certify the board's wiring or hardware timings.

Future PC compatibility must extend the documented standard services,
memory/BDA organization, disk boot and timer/IRQ handling. This API provides
reusable implementations; it does not emulate an 80x25 display, DOS kernel
calls, PC keyboard hardware or direct PC I/O/video-memory accesses.
Standard reference: [Ralf Brown's Interrupt List](https://www.cs.cmu.edu/~ralf/files.html).
