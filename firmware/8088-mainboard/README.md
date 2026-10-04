# 8088 mainboard firmware

There is one active ROM BIOS. It initializes both available console devices:
the 20×4 HD44780-compatible LCD on Exp1 and the GM16C550 UART module on Exp0.
The monitor runs directly from ROM at `F800:0000`; uploaded programs execute
from paragraph-aligned SRAM addresses allocated by BIOS.

BIOS sources are grouped by responsibility:

- `bios/uart/`: wire protocol constants, framing, command dispatch and handlers
  for programs, memory, keyboard events, Info and flasher mode.
- `bios/`: initialization, the cooperative monitor loop, shared SRAM state,
  keyboard queue, program allocation, LCD menu and interrupt API.
- `bios/utils/`: self-contained BIOS utilities such as the RAM diagnostic.
- `lib/`: shared hardware drivers and the program BIOS API definitions.
- `programs/flasher/`: the standalone RAM flasher, sharing UART framing and
  protocol constants with BIOS.

## Build

Run these commands from `firmware/8088-mainboard`. NASM is required for
local builds; Go is required for the UART tools.

```sh
make        # build/bios.bin, a complete 32 KiB 28C256 image
make citylights # build/citylights.bin, UART-loaded city animation
make terminal  # build/terminal.bin, UART-to-LCD terminal
make clean
```

BIOS builds print occupied ROM bytes, percentage and free space. The report
includes code/data and the reset vector, excluding `0xFF` padding.

The build supplies the BIOS version: `dev` by default, or an explicit short
identifier for CI/release builds:

```sh
make BIOS_VERSION=v1.0.0
```

Use 1–9 ASCII characters (letters, digits, dots, hyphens or underscores) so
the version remains readable in the LCD header and Info list. The same version
appears in the menu, LCD Info and UART `INFO` reply. BIOS is rebuilt on each
invocation to apply version changes without `make clean`; example programs
still build incrementally. Board revision, UART protocol version and BIOS API
revision are independent. `make test BIOS_VERSION=ci-test` also passes the
version to the Docker build.

## UART and menu control

```sh
make serial-ports
make keyboard SERIAL_PORT=/dev/cu.usbserial-XXXX
make serial SERIAL_PORT=/dev/cu.usbserial-XXXX
```

`make serial` opens the UART console through `boardctl`, without `cu` lock
files or `sudo`; use `Ctrl-C` to leave it. The UART uses `A0h` through `A7h` and is configured for 9600 8N1 from the
module's 1.8432 MHz crystal. The LCD remains on `C0h` (commands) and `C1h`
(data). See the [boardctl guide](../../tools/boardctl/README.md) for CLI commands.

### Boot menu

After boot, the ROM BIOS shows a menu: **Default boot**, **Custom programs**,
**Memory check**, and **Info**. Default boot currently reports `NO BOOT TARGET`;
there is no countdown or DOS loader yet. All menu/monitor code executes from
EEPROM, not copied to RAM. RAM holds only buffers, state, stack and explicit
user uploads; the diagnostic writes RAM only after confirmation.

The first LCD row is the current section header, except for the two-line
Memory check warning dialog. Progress, success, failure and cancellation retain the `MEMORY CHECK`
header; changing status uses rows 2–4. `INFO` is a scrollable list with three
visible rows: BIOS version, board revision, CPU, physical RAM/ROM sizes, LCD,
UART, protocol/API revisions, committed program count and address ranges.
Up/Down scroll one row without wrapping, Enter does nothing, Esc returns to
the selected root item. The rightmost column shows the same `^`/`v` rail as
the menu, without a selection cursor. Reopening Info starts at the top.
The UART `INFO` command still returns its compact ASCII summary.
While launching a payload,
the program name stays on row 1 and `RUNNING PROGRAM` appears on row 2.
The payload then owns the LCD and may use its own layout.

`boardctl keyboard` shares the PC terminal keyboard; the menu is shown only
on the board LCD. Use Up/Down, Enter and Esc to navigate. Ordinary letters,
including `U` and `Q`, belong to the program, not the host utility.
Press `Ctrl-]`, then `u` for a binary path prompt; `Ctrl-C` disconnects directly
and restores the PC terminal, including from the prompt.
Uploads are available only while the ROM menu is active; the new slot is
selected without starting it. Enter opens its details; Enter again starts it.
The submenu always contains four slots, initially Empty.
The last LCD column shows `|` with `^`/`v` for hidden items above/below.
The selected `>` blinks in the root and Custom programs menus, and becomes
visible again on navigation. Only that LCD cell is updated; UART polling
continues without a blocking delay. Revision 1 counts idle-loop iterations,
so the blink rate depends on CPU speed and monitor traffic, not a hardware
timer. Info and other non-selectable pages do not blink.
Use one serial client at a time; do not run a
separate `upload` process while `keyboard` owns the port. Ordinary `upload`
works from any menu page except during an active RAM test.

Revision 1 has no hardware interrupts. The ROM loop polls keyboard packets;
RAM programs must call INT 16h/AH=01h regularly, or wait with AH=00h, to poll
the transport. AH=01h does not consume a key. A program that does not poll
cannot accept keyboard packets; host timeouts and UART overflow are possible.
The BDA holds a 15-key queue shared by the menu and INT 16h. Keyboard sharing
owns UART RX: raw INT 14h/AH=02h is rejected until the host detaches.
`console` remains a separate raw UART mode. Attach keyboard sharing from the
ROM menu before launching a program. Reset disables sharing; reconnect the
host session afterward. If detach cannot be acknowledged, Reset restores raw mode.

## Uploaded programs

For example, build the city animation with `make citylights`, then upload
and start it from this directory:

```sh
go -C ../../tools/boardctl run ./cmd -port /dev/cu.usbserial-XXXX upload \
  --name "City Lights" --run ../../firmware/8088-mainboard/build/citylights.bin
```

Omit `--run` to add the program to the menu without starting it. The animation
shows `CITY LIGHTS` over the skyline for about two seconds, then alternates
between night and day. Stars twinkle between a bright shape and a dim dot at
night; clouds drift left during the day. One pedestrian paces on the left,
while two small buildings remain on the right.
The moon and sun stay in place. Each scene lasts about five seconds before a
right-to-left column wipe reveals the next one. The lower-right `ESC:BACK`
hint appears for about three seconds, hides for
about ten, and repeats; Esc always returns to BIOS. Timing is approximate on
rev1 because it has no hardware timer.

The `terminal` example displays keyboard input on the LCD and echoes it over
UART. Its header and controls stay on the top two rows; Esc returns to BIOS.

The host sends a name, image size and CRC; BIOS assigns an ID and a
paragraph-aligned address. Up to four programs
may coexist. First-fit allocation reuses deleted ranges without relocating
resident programs; allocations remain paragraph-aligned. `PROGRAM_DELETE`
frees a slot/range and shifts following IDs; `PROGRAM_RENAME` changes only its
name. Both operations refresh the LCD and require no active upload/test/program.
The current upload area is `0x8800..0xFBFF` (29,696 bytes total, less alignment gaps).
The first 2 KiB of physical SRAM are reserved for IVT, BDA and BIOS state;
`0xFC00..0xFFFF` is reserved for program and BIOS stacks. The decoder mirrors
the 32-KiB SRAM every 32 KiB within `0x00000..0x1FFFF`.
BIOS maintains the sequential upload cursor; DATA packets contain only program
bytes, 128 per block except the final remainder. Uploads must be
fully received with matching image CRC before a slot becomes runnable. Retried PROGRAM_UPLOAD_DATA packets
are acknowledged without counting their data twice. The LCD shows transfer
progress as a 20-character `[###    ]`-style bar with 18 cells between the
brackets. See [protocol v1](../../docs/uart-protocol-v1.md) for the wire format and
commands. Protocol version remains 1 during development; update BIOS and
boardctl together.

`MEMORY_READ` lets boardctl dump RAM (including BIOS state and mirrors)
in blocks of up to 127 bytes. It is available in the ROM menu, not during a
memory check or program execution. ROM, unmapped and out-of-range reads are rejected;
live BIOS state is not an atomic snapshot. See the boardctl
[dump examples](../../tools/boardctl/README.md#commands).

`MEMORY_WRITE` permits raw writes only to payload SRAM and its aliases, never
ROM, IVT/BDA, BIOS state or stacks. Writes are rejected while an upload, memory
check or program is active. It does not register programs; boardctl `write`
verifies written bytes by SHA-256 readback.

Normal RETF preserves uploaded programs; Reset, power-off or jumping to
`F800:0000` clears the registry. There is no memory isolation between programs
and BIOS.

## BIOS flasher mode

`BIOS_FLASHER_MODE()` copies the built-in EEPROM monitor to RAM and
transfers control to it. The command has no payload; the full EEPROM
image size is fixed at 32 KiB. Entry works on revision 1 and clears the
resident program registry and keyboard sharing because the monitor occupies
payload RAM. Entry during a program upload, memory test or running program
is rejected. The RAM monitor's ACK confirms it is ready.

Only `BIOS_WRITE`, `BIOS_READ`, `RESET` and `PING` work in this mode; other
requests return status 20. An identical entry retry repeats its ACK. On
revision 1, a valid WRITE returns status 17 (unsupported), READ reads EEPROM in blocks
of 1–127 bytes, and RESET returns to the unchanged ROM BIOS. In the ordinary
ROM menu, READ and WRITE return status 17; enter EEPROM mode to read.
Both commands carry physical addresses and validate the entire block within
`0xF8000..0xFFFFF` in the RAM flasher; invalid requests return status 18.
The UART configuration/FIFO are preserved during
entry; hardware interrupts stay disabled and all monitor code runs from RAM.

The LCD switches to a `BIOS FLASHER` status screen in the same four-row style
as BIOS: section header, operation result, EEPROM address range and
`HOST RESET TO EXIT`. Entry shows `READY - READ ONLY`; successful reads show
`READ OK` and the inclusive physical range of the block. Revision-1 writes
show `WRITE UNSUPPORTED`, while invalid requests display an error. PING and
identical entry retries preserve the latest operation result. LCD updates
run one short operation per UART polling iteration, entirely from RAM.
RESET returns to the BIOS menu; keyboard navigation is disabled in this mode.

The revision-2 contract uses the same mode and commands, adding EEPROM writes
in WRITE; the write driver is not implemented yet. Blocks are written and
verified immediately. The host can read the
complete image with READ and compare SHA-256 before RESET; after a write,
RESET requires all 32 KiB to have been written and verified by readback.
See the [wire contract](../../docs/uart-protocol-v1.md#bios-flasher-mode).
The flasher lives in [`programs/flasher/main.asm`](programs/flasher/main.asm);
EEPROM hardware drivers belong in `lib/`. The flasher is assembled separately
with `ORG 0` so it can execute from RAM without calling ROM services.
`make bios` builds the normal 32-KiB EEPROM image containing the RAM monitor;
it does not build a second BIOS image. boardctl exposes `bios-flasher-mode`, `bios-read`
and `bios-write`; see its [EEPROM guide](../../tools/boardctl/README.md#bios-flasher-mode).

## Memory check

Memory check asks for confirmation because it destroys all uploaded programs.
The dialog shows `ALL RAM PROGRAMS` and `WILL BE DELETED!` on rows 1–2,
a blank row 3 and `YES:ENTER  NO:ESC` on row 4. Enter starts the destructive
test; Esc returns to the menu without changing programs or their RAM contents.
It tests the complete 32-KiB SRAM through its `0x8000..0xFFFF` alias. The
program area (`0x8800..0xFBFF`) uses destructive write/read passes; the IVT,
BDA, monitor/API state and stacks are checked one byte at a time and restored
after each probe. The five test values are `00`, `FF`, `55`, `AA`, and
address-dependent data. Row 2 shows overall progress across
all ten passes with the same bracketed 18-cell bar; row 3 shows the current
inclusive 16-byte RAM range,
and row 4 shows six-digit decimal `OK`/`BAD` counters. These count byte
verifications, not unique addresses: each of 32,768 bytes is checked five
times, so a successful full test reports `OK:163840 BAD:000000`. Writes do
not increment these counters. Testing continues after mismatches and retains
the first failing address, expected and actual byte in BIOS state. After
completion row 3 shows `DONE FFF0-FFFF`, or `FAIL xxxx-yyyy` for the block
containing the first mismatch. After cancellation it shows `STOP xxxx-yyyy`
for the last processed block. Completion keeps a full bar and the
totals, while cancellation keeps partial progress and counters.
Esc cancels. Cancellation also invalidates
all old programs. This is a basic diagnostic, not an exhaustive
RAM certification test. UART requests remain serviceable throughout the test.

## Program BIOS API

Programs call real software interrupts: INT 10h for text/cursor/scrolling,
INT 14h for UART, INT 16h for UART-backed keyboard, and INT 60h for board
LCD services, including custom CGRAM glyphs. Include `lib/bios_api.inc`;
the examples no longer include their own copies of the hardware drivers.
Programs use `ORG 0`, start with `DS=CS`, preserve `SS/SP`, and use `RETF`
to return to the menu. Handlers live in ROM and return with IRET, preserving caller segments,
stack and non-output registers/flags. Revision 1 needs no PIC for software INT.
See [bios-api.md](docs/bios-api.md) for exact registers, flags, supported
functions, memory aliases and limitations. This is not a full PC/MS-DOS BIOS.

## Software verification

The Docker tests execute the actual ROM image with Unicorn and emulated
UART/LCD. They cover keyboard navigation, LCD output, upload retries and bounds,
RAM pass/fail/cancel, program return, SRAM mirrors and INT API contracts.
The emulator implements CPU IVT dispatch; real ROM handlers and IRET execute.
It does not verify hardware timing.

Run the emulator checks in Docker (Docker must be running):

```sh
make test
```

The image installs NASM/Make and the pinned Unicorn dependency, rebuilds the
ROM inside Linux, then runs the tests. It does not access the EEPROM, serial
devices, or write host build files. This is a test runner, not an interactive
virtual board. `EMULATOR_IMAGE` overrides the local Docker image name.

## Current scope

There is no automatic boot countdown, bootable storage or MS-DOS loader yet.
Revision 1 polls UART and does not use hardware interrupts. The implemented
software INT services are a starting point, not a complete PC-compatible BIOS.
Revision 2 can extend these services, add hardware interrupt support, memory
discovery and storage boot while keeping one BIOS implementation.
