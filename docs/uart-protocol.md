# 8088 board UART protocol 2.0

The filename is retained. This specification describes the current protocol.
UART is 9600 baud, 8N1; baud configuration is outside the protocol.
Normal boardctl operations accept only protocol 2.0. EEPROM upgrading has a
separate version-negotiation exception described below.

## Framing

Packets are `00 + COBS(raw) + 00`. All integers are little endian.

| Offset | Type | Meaning |
| --- | --- | --- |
| 0 | u8 | Frame version: 02h |
| 1 | u8 | Command; responses set bit 7 |
| 2 | u8 | Sequence number |
| 3 | u16 | Payload length, 0..128 |
| 5 | bytes | Payload only; no implicit address field |
| 5+length | u16 | CRC-16/CCITT-FALSE of header and payload |

CRC starts at FFFFh, polynomial 1021h, no reflection or final XOR. Test string
`123456789` gives 29B1h. Invalid COBS, CRC, lengths and version are discarded.
A zero delimiter resynchronizes reception. Requests and responses are serialized;
response command and sequence must match. Responses begin with a status byte.
Host retries repeat the exact command, sequence and payload.

## Commands

| Command | ID | Request | Success response including status |
| --- | --- | --- | --- |
| PING | 01h | empty | 0 |
| INFO | 02h | empty | 0, ASCII summary |
| RESET | 03h | empty | 0, then restart |
| BIOS_FLASHER_MODE | 04h | empty | 0 from the RAM updater |
| BIOS_WRITE | 05h | address:u32, data:1..124 | 0 |
| BIOS_READ | 06h | address:u32, length:u16 (1..127) | 0, bytes |
| MEMORY_READ | 07h | address:u32, length:u16 (1..127) | 0, bytes |
| MEMORY_WRITE | 08h | address:u32, data:1..124 | 0 |
| KEYBOARD_MODE | 09h | mode:u8 | 0, OS-running:u8 |
| KEYBOARD_EVENT | 0Ah | ASCII:u8, scan:u8 | 0, OS-running:u8 |
| RAM_INFO | 0Bh | empty | 11-byte snapshot |
| RAM_TEST | 0Ch | action:u8, optional confirmation | 31-byte snapshot |
| TERMINAL_POLL | 0Dh | empty | 0, OS-running:u8, output:0..64 |
| IRQ_TEST | 0Eh | empty | 0, result:u8, received:u16, target:u8 |
| DISK_MOUNT | 0Fh | empty or writable:u8 (0/1) | 0 |
| DISK_BOOT | 10h | empty | 0, then boot handoff |
| DISK_POLL | 11h | empty | 0, pending:u8, id:u16, LBA:u16, received:u16 |
| DISK_DATA | 12h | id:u16, offset:u16, error:u8, data:0..120 | 0 |
| DISK_WRITE_DATA | 13h | id:u16, offset:u16 | 0, id:u16, offset:u16, data:1..120 |
| DISK_WRITE_ACK | 14h | id:u16, BIOS-status:u8 | 0 |

No BIOS application registry or application transfer/execution commands exist.
Applications come from the OS filesystem. Remaining command IDs are unassigned.

## Status values

| Decimal | Meaning |
| --- | --- |
| 0 | Success |
| 1 | Unknown command |
| 5 | Invalid common/RAM/IRQ request |
| 10 | RAM diagnostic busy |
| 11 | Operating system/device ownership busy |
| 12 | Invalid keyboard/terminal/disk transport request |
| 13 | Keyboard queue full |
| 14 | Invalid memory range/request |
| 17 | EEPROM operation unavailable in ROM monitor |
| 18 | Invalid EEPROM/boot/disk state, request or range |
| 19 | EEPROM programming or readback failure |
| 20 | RAM updater active; other commands unavailable |
| 21 | PIC missing |
| 22 | PIT missing |
| 23 | RAM0 required |
| 24 | PIT counter not counting at POST |

The idle ROM monitor serves diagnostics, scratch memory, keyboard and disk
control. While an OS runs, foreground BIOS calls service PING, keyboard mode and
events, terminal polls, disk polls/data, and an identical boot-ACK retry. Other
commands return 11 (BIOS_WRITE returns 17). Reboot before ROM diagnostics or
flashing. The RAM updater accepts only PING, RESET and EEPROM operations;
other commands return 20. Normal monitor RESET acknowledges before restarting.

## RAM information and diagnostic

RAM_INFO is exactly 11 bytes:

| Offset | Type | Meaning |
| --- | --- | --- |
| 0 | u8 | Status |
| 1 | u8 | POST-approved bank mask, bits 0..4 |
| 2 | u8 | Working bank, 0..4 |
| 3 | u8 | Flags: bit 0 diagnostic running, bit 1 heartbeat supported |
| 4 | u32 | Total installed/tested RAM bytes |
| 8 | u16 | Contiguous conventional memory in KiB |
| 10 | u8 | Diagnostic state |

RAM_TEST action 0 reads status, action 1 starts with a second byte A5h, and
action 2 cancels while retaining results. A valid START with the same immediate
sequence is a retry and does not restart the diagnostic. Starting while busy
returns 10. Invalid action, length or confirmation returns 5.

The 31-byte status snapshot is:

| Offset | Type | Meaning |
| --- | --- | --- |
| 0 | u8 | Status |
| 1 | u8 | State: 0 idle, 1 running, 2 complete, 3 cancelled |
| 2 | u8 | Bank mask |
| 3 | u8 | Working bank |
| 4 | u8 | Phase 0..10 |
| 5 | u32 | Last 16-byte block physical address |
| 9 | u32 | Successful verifications |
| 13 | u32 | Failed verifications |
| 17 | u32 | Planned verifications: chip count × 655360 |
| 21 | u32 | First failing physical address |
| 25 | u8 | Expected byte |
| 26 | u8 | Actual byte |
| 27 | u32 | Processed 16-byte slices |

Ten phases write/verify 00, FF, 55, AA and an address-dependent byte. All tested
chips are covered. Reserved BIOS bytes and stack are saved/probed/restored in
short critical sections. Counters continue after errors; the first mismatch is
retained. Remote START/STATUS renew a heartbeat. With verified PIT it expires
after 37 ticks (about two seconds), otherwise after a bounded software budget.
Host loss cancels the test. Local LCD tests do not require heartbeats.

MEMORY_READ covers present physical SRAM banks. MEMORY_WRITE is restricted to
the selected bank's scratch offsets 8800..FBFF. IVT/BDA, BIOS state and stack
are protected. Full ranges are validated before writes; addresses do not wrap.
Both commands return 10 during RAM diagnostics and 11 while the OS owns RAM.

## Keyboard and terminal

Mode 0 is raw UART, 1 framed keyboard/LCD, 2 framed terminal. Changing mode
clears pending keys/output and retry state; attaching the same mode retains
pending data. Reset selects mode 0. Keys use ASCII in AL and PC-style scan code
in AH. Extended keys have ASCII zero and nonzero scan; the host decodes ANSI.
The BDA queue holds up to fifteen keys. Immediate duplicate events are not
injected twice. There are no modifier/release events or physical keyboard driver.

TERMINAL_POLL requires mode 2. It drains up to 64 binary output bytes and caches
the snapshot for an identical-sequence retry. ANSI/control bytes are unchanged;
protocol acknowledgements are never console output. BIOS output waits boundedly
for host polls and processes transport requests while waiting. IRQ4 only collects
RX bytes; foreground BIOS calls parse frames and generate replies. Detach does
not reset the operating system or stop a running application.

## IRQ and time

POST probes PIC IMR and PIT read-back/counting, then independently verifies
IRQ0 and UART IRQ4 (16550 internal loopback). Without a verified backend BIOS
falls back to polling/software delays. RAM0 is needed for interrupt vectors.
INFO reports installed chips and active runtime bits: bit 0 timer, bit 1 UART RX.

IRQ_TEST requires RAM0, PIC, PIT and a running PIT counter. It temporarily
replaces IRQ0 vector 08h and programs channel 0 with divisor 65536. Target is
eight interrupts; result 0 is PASS, 1 TIMEOUT. BIOS restores its runtime and
vector afterward. The host allows 15 seconds for this synchronous diagnostic.
Normal IRQ0 runs at approximately 18.2065 Hz and invokes INT 1Ch; UART IRQ4 uses
vector 0Ch. No protocol commands execute in an ISR.

## UART floppy

Geometry is 2880 × 512-byte sectors, 80 cylinders, 2 heads, 18 sectors/track.
DISK_MOUNT requires RAM0 and the idle monitor. Empty payload or a zero byte
mounts read only; byte 1 enables INT 13h writes. Other payloads are invalid. DISK_BOOT additionally requires
terminal mode 2 and a verified timer. It acknowledges before reading sector 0,
reserves the upper 32 KiB of contiguous RAM, loads 0000:7C00, checks AA55h and
enters with DL=0. Invalid boot sectors print BOOT FAILED and await Reset.

DISK_POLL is side-effect free. A pending request identifies one sector, LBA
0..2879. `pending=0` means idle, 1 means read, and 2 means write.
For a read, host sends ordered chunks at the reported `received` offset, no more
than 120 bytes each. Offset plus length cannot exceed 512. Zero-length success,
wrong IDs, gaps and mismatched duplicate data return 18 without changing data.
After 512 bytes BIOS completes the sector and may immediately request the next.
A nonzero host error ends the request with INT 13h error 80h; data is unnecessary.

The last accepted DISK_DATA payload is cached exactly, so its retry succeeds
even after the next sector has started. Earlier identical ranges are accepted
while the current sector remains pending. Unsolicited data is rejected. Boot
requests with the original explicit boot sequence are acknowledged while the OS
runs; other boot requests return 11. Mount/Reset clear transport retry state.

For a write, BIOS first copies the source sector into its reserved buffer and
reports `pending=2`, `received=0`. DISK_WRITE_DATA fetches the pending sector
without modifying it: the response contains exactly min(120,512-offset) bytes.
The ID must match; offsets are 0..511. Host validates every returned ID/offset,
collects 512 bytes, writes the sector at LBA*512, synchronizes storage and
verifies readback before sending DISK_WRITE_ACK with status 0. The in-memory
read snapshot is updated only after successful persistence and verification.
A read-only host ACKs with status 03h; a write/readback failure uses 20h. A
nonzero status completes the request with CF set and that INT 13h AH status.
An exact final ACK retry succeeds even after the guest starts the next sector;
changed/stale ACKs and fetches without a matching pending write return 18.
Fetching data and retransmitting ACKs never repeat the host file write.

Disk waits time out after about ten seconds, with a CPU watchdog if time stops.
Detach aborts a pending transfer. Reconnect with the same image and omit mount/boot
while the OS runs. Terminal, keys and disk must share one host connection. Writes
through INT 13h require a writable mount. boardctl `terminal --disk IMAGE`
requests writable mode by default (DISK_MOUNT payload 01h). `--disk-readonly`
requests read-only mode (empty payload) and never modifies the image. A writable
open or lock failure is an error, without automatic fallback. Mount mode is
fixed until Reset. Reconnect with the same image and mode, omitting --boot.
Writable sessions hold an exclusive advisory lock on a regular 1.44-MiB host
file and persist each sector before ACK; an interrupted multi-sector FAT update
is not a transactional operation and can leave an incomplete filesystem change.

## EEPROM upgrade service

PING/RESET/FLASHER_MODE/WRITE/READ retain IDs 01..06 independently of the normal
command set. boardctl's EEPROM commands probe frame majors 2 and 1 and then use
the detected service version for the entire update, including verification and
Reset. Valid matching replies can select their service-frame version. This
exception is restricted to EEPROM operations; normal boardctl has no fallback.

BIOS_FLASHER_MODE validates state, stops IRQ runtime and copies the independent
updater from ROM to working-bank RAM. Entry ACK comes from RAM; identical entry
retries are acknowledged there. The updater never calls ROM while programming.
BIOS_WRITE uses physical F8000..FFFFF addresses and validates complete ranges
before writes. ROM write SW1 must permit programming. The updater polls EEPROM
completion and verifies readback; host retries preserve the exact last block.
BIOS_READ reads 1..127 bytes per request. Host writes use 64-byte EEPROM pages
and verify the complete 32768-byte image with SHA-256 before optional Reset.
Reset is blocked after an incomplete update. There is no application-upload or
executable-RAM protocol involved in EEPROM programming.
