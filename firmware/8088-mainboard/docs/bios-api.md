# BIOS software-interrupt API

The BIOS provides PC-style services and board extensions on the revision-2 8088.
Board API revision is 1; UART protocol revision is 2.0. These are separate APIs.
Applications are COM files loaded by the OS. ROM has no application registry,
upload service, shared application stack, or RETF-to-menu launch contract.

## Execution and memory

RAM0 is required for IVT/BDA and OS boot. The ROM monitor and EEPROM updater
can use another working bank without software interrupts. OS boot requires a
verified timer and reserves the upper 32 KiB of contiguous RAM from physical zero.
BDA 0040:0013 and INT 12h exclude this reservation. Later banks beyond a hole are
not conventional memory. With 256 KiB contiguous RAM, DOS/native OS sees 224 KiB.

The BIOS bank segment is stored at physical 04F0, signature B105h at 04F2. All
persistent state and IRQ access is independent of caller DS/ES/SS. Before boot,
the selected working bank holds the same offsets without relocation. A read-only
BDA signature probe selects the monitor-only fallback when RAM0 is absent.

| BIOS-bank offsets | Purpose |
| --- | --- |
| 8000..81FF | Timer/UART state, disk metadata, RX ring |
| 8200..82FF | Exact disk-block retry cache |
| 8300..84FF | Disk sector buffer |
| 8500..86FF | Monitor buffers and diagnostic state |
| 8700..87FF | LCD/console/transport state |
| 8800..FBFF | Flasher and diagnostic scratch |
| FC00..FFFF | BIOS stack reservation |

The IVT and standard BDA remain at physical zero after relocation. COM memory,
PSP, entry registers, stack and INT 20h termination belong to the OS; see the
[OS contract](../os/README.md). No BIOS routine implements DOS INT 21h.

Unless specified, software INT services preserve AX/BX/CX/DX/SI/DI/BP/DS/ES
and caller flags. They use IRET, clear DF internally and may enable verified IRQs
while servicing calls, restoring caller IF/DF on return. Unsupported board/API
functions return CF=1/AH=86h. Caller buffers must remain valid; there is no memory
isolation. Keep IF enabled for background timer/UART service.

## PC/XT interrupts

| INT | Functions |
| --- | --- |
| 08h | Hardware timer ISR: monotonic ticks, BDA time and INT 1Ch hook |
| 0Ch | UART IRQ4 ISR: bounded RX FIFO drain into private ring |
| 10h | OS-mode UART console, below |
| 11h | Equipment word from BDA; one floppy after boot |
| 12h | AX = conventional RAM in KiB |
| 13h | Read-only virtual floppy, below |
| 14h | UART/terminal, below |
| 16h | Keyboard queue, below |
| 19h | Boot mounted floppy at 0000:7C00; does not return |
| 1Ah | AH=00h read PC time; AH=01h set PC time |
| 1Ch | Timer hook, initially IRET |
| 60h | Board/LCD/time extensions, below |

IRQ0 uses PIT channel 0 divisor 65536, approximately 18.2065 Hz. INT 1Ah/AH=00h
returns CX:DX ticks since midnight and AL rollover count, clearing the count.
AH=01h sets CX:DX below 1800B0h and clears rollover. Both clear CF; invalid
functions/ranges return CF=1/AH=86h. BDA 0040:006C stores the clock and 0070
rollovers. Setting PC time does not change the monotonic INT 60h clock. No RTC
or retained date is provided.

Timer/UART ISRs preserve registers and never parse frames or write LCD. INT 1Ch
hooks must remain short and must not reenter cooperative UART processing.
Programs must not reprogram BIOS-owned PIC/PIT/UART or IVT hardware-IRQ entries.

### INT 10h — UART text console in OS mode

Mode 3 has a logical 80×25 page zero. Output is transmitted through UART;
there is no B800:0000 video memory, complete CGA API or attribute framebuffer.
LCD is a separate board display and shows OS/disk status by default.

| AH | Inputs | Result |
| --- | --- | --- |
| 00h | AL=3 | Clear/home UART terminal; restore LCD OS status |
| 01h | cursor shape | Accepted, no physical cursor-shape emulation |
| 02h | BH=0, DH=row 0..24, DL=column 0..79 | Set cursor using ANSI |
| 03h | page zero | DX=logical cursor, CX=0607h |
| 0Eh | AL=character | Teletype output, track CR/LF/BS/wrap |
| 0Fh | none | AX=5003h, BH=0 |

Before OS boot, the BIOS monitor's LCD text backend is 20×4 custom mode 7Fh.
Applications use INT 60h LCD functions independently of the UART console.
This implementation is not yet a claim that stock DOS boots unmodified.

### INT 13h — read-only floppy

Drive DL=0, 80 cylinders, 2 heads, 18 sectors per track, 512-byte sectors.

| AH | Inputs | Result |
| --- | --- | --- |
| 00h | DL=0 | Reset/status success if mounted |
| 01h | DL=0 | Last disk status |
| 02h | AL=1..18, CH=cylinder, CL=sector, DH=head, ES:BX=buffer | Read sectors; AL=count on success |
| 03h | write | CF=1, AH=03h (write protected) |
| 08h | DL=0 | CH=79, CL=18, DH=1, DL=1, BL=4 |
| 15h | DL=0 | AH=1 floppy type |

CF clears on success, sets on error. Errors: 01h unsupported, 03h write
protected, 04h invalid CHS/count, 09h invalid destination, 80h unavailable/timeout.
Reads require OS mode, may not wrap a 64-KiB offset, touch IVT/BDA below 0500h,
or extend above conventional RAM. ES:DI disk-parameter tables are not implemented.
Sector transfer is cooperative over boardctl's mounted image; requests have a
bounded timeout. INT 19h checks the sector signature, then enters 0000:7C00 with
DL=0, DS=ES=SS=0, SP=7C00, DF=0 and IF=1.

## INT 14h — serial/terminal

DX is port index 0, not I/O address 3F8h.

| AH | Inputs | Outputs |
| --- | --- | --- |
| 00h | AL=E3h, DX=0 | Initialize 9600 8N1, reset FIFO/keys; AH=03h |
| 01h | AL=byte, DX=0 | Transmit, AL retained, AH=line status or timeout |
| 02h | DX=0 | Receive AL, AH=line status or timeout |
| 03h | DX=0 | AH=line status, AL=modem status |

Only AX changes on valid calls; flags are preserved. Raw TX/RX poll boundedly;
AH bit 7 reports timeout. Raw RX is unavailable while framed keyboard sharing
owns UART. Verified UART IRQ reception uses the private ring.

In terminal mode 2, TX enters a 64-byte framed-output queue. It processes host
requests while waiting for space; AH=60h success, 80h timeout/detach. Accepted
bytes may still be queued. In keyboard-only mode 1 raw text is not displayed by
boardctl; applications should disable UART animation in that mode.

## INT 16h — keyboard

| AH | Result |
| --- | --- |
| 00h | Wait/consume; AL=ASCII, AH=PC-style scan |
| 01h | Peek; ZF=1 empty (AX unchanged), otherwise ZF=0 and AX=key |
| 02h | AL=0, no modifier tracking |

Only AH=01h changes ZF. The standard BDA ring holds fifteen keys. boardctl
translates ANSI arrows/function keys into scan codes. Raw bytes have translated
ASCII scan codes where known; raw ANSI sequences are not decoded by BIOS.
Both blocking/peek calls process framed transport requests. IRQ4 collects bytes
but does not parse commands. Long computation must call BIOS between slices.

## INT 60h — board services

Success clears CF, failure sets CF/AH=86h, except AH=09h uses its own errors.
Other flags/registers are preserved except documented outputs.

| AH | Inputs | Outputs/operation |
| --- | --- | --- |
| 00h | none | AX=1, BX=B105h, CH=4 rows, CL=20 columns, DX=00FFh capabilities |
| 01h | none | Clear LCD/framebuffer/cursor, retain keys |
| 02h | DH=row, DS:SI=ASCIZ | Replace/pad one 20-cell LCD row, retain cursor |
| 03h | AL=glyph 0..7, DS:SI=8 bytes | Load 5×8 LCD CGRAM glyph |
| 04h | DH=row, DL=column, AL=raw byte | Write LCD cell without cursor movement/control parsing |
| 05h | DS:SI=ASCIZ | Print through the LCD text backend |
| 06h | none | AX=0, DX=runtime flags, CX=18 integer rate hint or 0 without timer |
| 07h | none | DX:AX=32-bit monotonic hardware ticks, CF=1/AH=86h unavailable |
| 08h | CX=milliseconds | Wait/pump host; AX=0 hardware, 1 approximate software |
| 09h | DS:SI=buffer, CX=count | AX=accepted bytes, DX=0 success/1 timeout-detach/2 offset wrap; CF=error |
| 0Ah | none | AX=host mode 0/1/2, DX=1 LCD support (not detected presence) |

LCD coordinates are zero based, row 0..3, column 0..19. Strings truncate/pad
rows; glyphs use the low five bits. The fixed-delay write-only LCD driver works
with an absent module; R/W is grounded. LCD calls pump frames between writes.

Capabilities: bit 0 text console, 1 UART, 2 keyboard, 3 LCD extensions, 4 host
sharing, 5 time functions, 6 binary UART block output, 7 console query. Runtime
flags: bit 0 verified timer, bit 1 verified UART RX IRQ.

The monotonic counter wraps modulo 2^32 and pauses in diagnostics/EEPROM mode.
Delay rounds up to whole hardware ticks plus initial phase; zero is immediate.
Without a verified timer, or after a stalled-clock watchdog, waiting uses
approximate CPU work. Applications must inspect availability where exact time
matters. Reactor accumulates PIT-clock fractions instead of assuming 18 exact
hardware ticks per second.

Block output preserves DS/SI/CX and other non-output registers; only AX/DX/CF
change. Count zero succeeds. Ending at offset 10000h is allowed; crossing it is
rejected before output. On partial failure retry from SI+AX with remaining
length. Caller memory must be readable; this API provides no isolation.

## SDK

Include `lib/bios_api.inc` with `nasm -I lib/`. Macros only wrap interrupts;
argument setup may itself change registers before the call. A native COM app:

```asm
BITS 16
CPU 8086
ORG 100h
%include "bios_api.inc"
    bios_lcd_clear
    bios_print message
    bios_key_read
    int 20h
message: db 'Hello from OS!', 0
```

Tests execute assembled ROM and native OS code in Unicorn, checking stack,
segment, transport and memory behavior. They do not certify board wiring/timing.
