# 8088 board protocol v1

Protocol v1 defines the binary UART interface: framing, command codes,
payloads, replies and retry rules. Supported operations and valid physical
memory ranges are properties of the implementation.

UART uses 8N1; host and board must use the same configured baud rate.
boardctl uses 9600 baud. The baud rate is configured outside the protocol.
This binary protocol is separate from raw console mode. Version remains 1
during unreleased development;
update BIOS and clients together when changing this contract.

## Framing

Every packet is COBS-encoded and delimited by zero bytes:

```text
00 | COBS(decoded-frame) | 00
```

The decoded frame uses little-endian integers:

```text
version:u8 | command:u8 | sequence:u8 | length:u16 | payload | crc16:u16
```

Version is `0x01`; `length` counts payload bytes, at most 128. The common header
is five bytes; a decoded frame occupies `7 + length` bytes including CRC.
The maximum decoded size is 135 bytes, the maximum COBS body is 136 bytes,
and the maximum packet including both delimiters is 138 bytes. Consecutive
zero delimiters are allowed and do not represent empty requests.
CRC-16/CCITT-FALSE uses polynomial `0x1021`, initial `0xFFFF`, no bit reflection
and no final XOR. It covers the decoded header and payload, excluding the CRC,
COBS encoding and delimiters. The CRC is transmitted little-endian;
the check value for ASCII `123456789` is `0x29B1`.

Invalid COBS, CRC, version, declared length or payloads above 128 bytes are
ignored without a reply. An oversized packet is discarded through the next
zero delimiter. A valid frame with an invalid command payload receives that
command's error status instead.

Hosts keep one request in flight. Each new request normally increments
sequence modulo 256; retries retain the command, sequence and all payload
bytes. The board echoes sequence but does not require consecutive values or
provide a global duplicate-request cache. Retry behavior is command-specific.
Clients match both reply command and sequence and ignore unrelated frames
and interleaved console text. boardctl uses one second per response and up to
ten attempts for ordinary requests; BIOS_WRITE defaults to ten seconds per
attempt, configurable with `--timeout`. A nonzero status ends a transaction;
it is not a transport timeout and is not automatically retried by boardctl.

### Complete packet examples

All byte sequences in this section and other byte dumps are hexadecimal;
each two-digit group is one byte. CRC values are shown as integers, then
appended little-endian before COBS encoding. The wire packet includes both
zero delimiters.

PING with sequence `0x2A`, followed by its successful reply:

```text
Request header + payload: 01 01 2A 00 00
CRC16:                    0x8CEE
Decoded frame:            01 01 2A 00 00 EE 8C
COBS body:                04 01 01 2A 01 03 EE 8C
Wire packet:              00 04 01 01 2A 01 03 EE 8C 00

Reply header + payload:   01 81 2A 01 00 00
CRC16:                    0xABE4
Decoded frame:            01 81 2A 01 00 00 E4 AB
COBS body:                05 01 81 2A 01 01 03 E4 AB
Wire packet:              00 05 01 81 2A 01 01 03 E4 AB 00
```

MEMORY_READ with sequence `0x2B`, address `0x8800` and size 2. This example
assumes the target permits that RAM range and it contains bytes `AA 00`:

```text
Request header + payload: 01 07 2B 06 00 00 88 00 00 02 00
CRC16:                    0x75AF
Decoded frame:            01 07 2B 06 00 00 88 00 00 02 00 AF 75
COBS body:                05 01 07 2B 06 01 02 88 01 02 02 03 AF 75
Wire packet:              00 05 01 07 2B 06 01 02 88 01 02 02 03 AF 75 00

Reply header + payload:   01 87 2B 03 00 00 AA 00
CRC16:                    0x2E9D
Decoded frame:            01 87 2B 03 00 00 AA 00 9D 2E
COBS body:                05 01 87 2B 03 01 02 AA 03 9D 2E
Wire packet:              00 05 01 87 2B 03 01 02 AA 03 9D 2E 00
```

## Commands and replies

| Command | Value | Request payload | Result payload |
| --- | ---: | --- | --- |
| `PING` | `0x01` | empty | status |
| `INFO` | `0x02` | empty | status + ASCII information |
| `RESET` | `0x03` | empty | status; reset to ROM |
| `BIOS_FLASHER_MODE` | `0x04` | empty | status; enter BIOS flasher mode |
| `BIOS_WRITE` | `0x05` | address:u32, 1–124 image bytes | status; write and verify EEPROM |
| `BIOS_READ` | `0x06` | address:u32, size:u16 (1–127) | status + EEPROM bytes |
| `MEMORY_READ` | `0x07` | address:u32, size:u16 (1–127) | status + bytes |
| `MEMORY_WRITE` | `0x08` | address:u32, 1–124 bytes | status |
| `PROGRAM_LIST` | `0x09` | empty | status + count + records |
| `PROGRAM_UPLOAD_BEGIN` | `0x0A` | size:u32, image CRC16:u16, name | status + ID + address |
| `PROGRAM_UPLOAD_DATA` | `0x0B` | program bytes only, 1–128 | status |
| `PROGRAM_UPLOAD_COMMIT` | `0x0C` | program_id:u8 | status |
| `PROGRAM_UPLOAD_ABORT` | `0x0D` | program_id:u8 | status |
| `PROGRAM_EXEC` | `0x0E` | program_id:u8 | status; far call |
| `PROGRAM_DELETE` | `0x0F` | program_id:u8 | status |
| `PROGRAM_RENAME` | `0x10` | program_id:u8, name | status |
| `KEYBOARD_MODE` | `0x11` | enabled:u8 (0 or 1) | status + flags |
| `KEYBOARD_EVENT` | `0x12` | ASCII:u8, scan:u8 | status + flags |

Replies use `command | 0x80`, echo sequence, and start their payload
with status `0x00` for success or a nonzero error. Errors are status-only.

INFO returns status followed by human-readable ASCII without a NUL terminator.
Its text is informational, not a stable machine-readable field layout.

Unknown command codes return status 1 in the ordinary ROM monitor, 20 in
BIOS flasher mode, or 11 during cooperative program polling. Addresses and IDs are
command-specific payload fields. PROGRAM_UPLOAD_DATA contains no address; BIOS_WRITE and
BIOS_READ carry physical EEPROM addresses, just as memory commands carry RAM addresses.
For supported commands, request lengths must match the table exactly
(except variable-length names and data bytes).
Menu navigation uses KEYBOARD_EVENT; no command returns the LCD screen.

### Response statuses

The first reply payload byte is a status code. Success may include additional
command-specific data; an error reply contains only the status byte.
Numbers are decimal unless written with a `0x` prefix or included in an
explicitly hexadecimal byte dump. Numeric statuses in prose and availability
tables are decimal.

| Hex | Decimal | Status | Meaning |
| --- | ---: | --- | --- |
| `0x00` | 0 | Success | Request completed; any additional reply data follows the command contract. |
| `0x01` | 1 | Unknown command | Command is unknown in the ordinary ROM monitor. |
| `0x02` | 2 | Invalid upload request | Invalid PROGRAM_UPLOAD_BEGIN, PROGRAM_UPLOAD_COMMIT or PROGRAM_UPLOAD_ABORT request/state. |
| `0x03` | 3 | Invalid upload data | Invalid PROGRAM_UPLOAD_DATA length, bytes or transfer state. |
| `0x04` | 4 | Invalid program execution | Invalid PROGRAM_EXEC request/ID, or a pending upload prevents execution. |
| `0x05` | 5 | Invalid monitor request | Invalid PING, INFO, PROGRAM_LIST or RESET payload. |
| `0x06` | 6 | Slots full | All four program slots are occupied. |
| `0x07` | 7 | Insufficient RAM | Requested program exceeds the allocation limit or no suitable RAM range is available. |
| `0x08` | 8 | Duplicate name | Another resident program already has the requested name. |
| `0x09` | 9 | Image CRC mismatch | Complete program image does not match its expected CRC16. |
| `0x0A` | 10 | Memory check busy | Active memory check prevents this operation. |
| `0x0B` | 11 | Program busy | A running program prevents this operation; returned only when framed requests are serviced. |
| `0x0C` | 12 | Invalid keyboard request | Invalid KEYBOARD_MODE/KEYBOARD_EVENT payload, or sharing is disabled for an event. |
| `0x0D` | 13 | Keyboard queue full | Key was not accepted; the queue has no free entry. |
| `0x0E` | 14 | Invalid memory request | Invalid MEMORY_READ/MEMORY_WRITE payload or physical RAM range. |
| `0x0F` | 15 | Upload busy | Pending upload prevents BIOS_FLASHER_MODE, MEMORY_WRITE, PROGRAM_DELETE or PROGRAM_RENAME. |
| `0x10` | 16 | Invalid registry request | Invalid PROGRAM_DELETE/PROGRAM_RENAME payload or nonexistent program ID. |
| `0x11` | 17 | Unsupported operation | Operation is unavailable in this implementation or mode, as specified below. |
| `0x12` | 18 | Invalid BIOS request | Invalid BIOS_FLASHER_MODE/BIOS_READ/BIOS_WRITE payload, range or state, including a forbidden RESET. |
| `0x13` | 19 | EEPROM failure | EEPROM programming or readback verification failed; a block may be partially written. |
| `0x14` | 20 | BIOS flasher mode active | Request is outside the RAM flasher's allowed command set, including a fresh or changed mode-entry request. |

Mode and busy-state checks can take precedence over payload validation;
command-specific rules below determine which status is returned.

## Command availability

Entries assume a valid request and show either its action or a decimal error
status. A pending upload is a state within the ROM menu. The running-program
column requires sharing enabled before launch and cooperative INT `0x16`
polling; without polling, framed requests receive no reply. See
[Keyboard sharing](#keyboard-sharing) for the polling and detach rules.
In the RAM flasher, commands returning 20 perform no action.

| Command | ROM menu, no upload | Memory check | Pending program upload | Running program with sharing and polling | RAM flasher |
| --- | --- | --- | --- | --- | --- |
| `PING` | 0 | 0 | 0 | 0 | 0 |
| `INFO` | information | information | information | 11 | 20 |
| `RESET` | reset | reset | reset | 11 | reset if permitted; otherwise 18 |
| `BIOS_FLASHER_MODE` | enter | 10 | 15 | 11 | identical retry: 0; otherwise 20 |
| `BIOS_WRITE` | 17 or 18 | 17 or 18 | 17 or 18 | 17 or 11 | write/verify or 17 |
| `BIOS_READ` | 17 | 17 | 17 | 11 | EEPROM data |
| `MEMORY_READ` | RAM data | 10 | RAM data | 11 | 20 |
| `MEMORY_WRITE` | payload RAM write | 10 | 15 | 11 | 20 |
| `PROGRAM_LIST` | committed records | empty list | committed records | 11 | 20 |
| `PROGRAM_UPLOAD_BEGIN` | reserve | 10 | replace reservation | 11 | 20 |
| `PROGRAM_UPLOAD_DATA` | 3 | 3 | accept block or repeat ACK | 11 | 20 |
| `PROGRAM_UPLOAD_COMMIT` | 0 for committed ID; otherwise 2 | 2 | publish if complete and CRC matches | 11 | 20 |
| `PROGRAM_UPLOAD_ABORT` | 0 for ID=count+1; otherwise 2 | 0 for ID=1; otherwise 2 | discard for ID=count+1; otherwise 2 | 11 | 20 |
| `PROGRAM_EXEC` | launch committed ID | 4 | 4 | 11 | 20 |
| `PROGRAM_DELETE` | delete committed ID | 10 | 15 | 11 | 20 |
| `PROGRAM_RENAME` | rename committed ID | 10 | 15 | 11 | 20 |
| `KEYBOARD_MODE` | set sharing | set sharing | set sharing | set sharing | 20 |
| `KEYBOARD_EVENT` | accept key if sharing enabled | accept key if sharing enabled | accept key if sharing enabled | accept key | 20 |
| Unknown command | 1 | 1 | 1 | 11 | 20 |

Memory check clears all registry entries and pending uploads before testing,
which explains the empty list and upload/execution results in that column.
Key acceptance remains subject to queue capacity; menu actions can change
state. Upload completion, valid IDs and names follow the command contracts below.

For BIOS_WRITE, status 17 means writing is unsupported. When writing is
supported, requests outside the flasher return 18, or 11 while a program runs.
In the flasher, invalid BIOS_READ/BIOS_WRITE payloads or ranges return 18;
a valid unsupported write returns 17. Supported writes return 0 after
verification, or 19 on programming/readback failure.
RESET permission in the flasher is defined in [Exit and recovery](#exit-and-recovery).

Mode and busy-state restrictions can take precedence over payload validation.
Outside the flasher, BIOS_READ returns 17 without payload validation, except
while a program runs, when it returns 11. Unsupported BIOS_WRITE outside the
flasher returns 17 without payload validation. BIOS_FLASHER_MODE during an
upload returns 15 even for an invalid payload. Other malformed requests follow
their command-specific status rules; invalid PING/INFO/PROGRAM_LIST/RESET
payloads in the ordinary monitor return 5.

## Retry reference

Transport retries reuse the command, sequence and every payload byte. This
table summarizes behavior; detailed recovery rules remain in each command's
section. There is no global duplicate-request cache, and an eight-bit sequence
number does not identify a session. BIOS_WRITE caching and recovery in this
table apply when EEPROM writing is supported.

| Command | Effect of an identical retry | Cache lifetime or recovery condition |
| --- | --- | --- |
| `PING` | Replies again. | No cached result. |
| `INFO` | Generates current information again. | No snapshot cache. |
| `RESET` | Resets again if the current mode permits it. | Reset clears registry and sharing state; a lost ACK does not imply reset failed. |
| `BIOS_FLASHER_MODE` | In the flasher, repeats the entry ACK without copying or restarting. | Entry sequence must match and payload must be empty; fresh or changed entry returns 20. |
| `BIOS_WRITE` | Repeats the last successful result without programming again; the same sequence after a failed attempt repeats its failure. | Unrelated requests do not clear the cache. A new sequence permits retry of a failed programming block. After a complete transfer, a WRITE at `0xF8000` with a sequence different from the last WRITE starts a new transfer. See [Writing EEPROM blocks](#writing-eeprom-blocks). |
| `BIOS_READ` | Reads EEPROM again. | No snapshot cache; transfer progress and WRITE cache are preserved. |
| `MEMORY_READ` | Reads RAM again. | No snapshot cache. |
| `MEMORY_WRITE` | Writes the same bytes to the same address again. | No transaction cache or rollback. |
| `PROGRAM_LIST` | Reads the current registry again. | No snapshot cache. |
| `PROGRAM_UPLOAD_BEGIN` | Replaces the pending reservation and starts it again. | Retry only before DATA; a successful BEGIN clears the DATA cache. |
| `PROGRAM_UPLOAD_DATA` | Repeats the last accepted block's ACK without advancing; changed bytes with that sequence return 3. | Successful BEGIN, COMMIT and ABORT clear the cache; rejected requests preserve it. |
| `PROGRAM_UPLOAD_COMMIT` | Already committed ID is a no-op if no upload is pending. | CRC failure leaves the upload pending; ABORT or a new BEGIN discards/replaces it. |
| `PROGRAM_UPLOAD_ABORT` | No-op without a pending upload, provided ID=count+1. | Successful ABORT clears the DATA cache. |
| `PROGRAM_EXEC` | Same sequence/ID repeats ACK without launching again once serviced by the ROM menu. | Any non-EXEC command handled by the ROM menu clears the cache; send PING before an intentional new launch. |
| `PROGRAM_DELETE` | Same sequence/ID repeats ACK without deleting the next program. | Any non-DELETE command handled by the ROM menu clears the cache; send PING before an intentional new deletion. |
| `PROGRAM_RENAME` | Applies the same name again. | Harmless while the ID still refers to that program. |
| `KEYBOARD_MODE` | Repeated mode preserves the queue. | Mode transitions clear the queue and EVENT retry cache. |
| `KEYBOARD_EVENT` | Last accepted sequence/data repeats ACK without enqueueing or navigating twice. | A newly accepted event replaces the cache; mode transitions and Reset clear it. A full queue returns 13 without accepting the event. |

## BIOS flasher mode

BIOS_FLASHER_MODE copies the built-in EEPROM flasher to RAM. Command
availability and unsupported-operation statuses are listed in
[Command availability](#command-availability). BIOS_WRITE programs EEPROM
when writing is supported.
No complete BIOS image is staged in RAM: only the BIOS flasher, its state,
stack and packet/block buffers need memory. boardctl exposes `bios-flasher-mode`,
`bios-read` and `bios-write`; see its [EEPROM guide](../tools/boardctl/README.md#bios-flasher-mode).

The programming, WRITE retry-cache and RESET rules below apply when EEPROM
writing is supported. After EEPROM writing begins, RESET requires a complete
transfer verified by readback.

### Entering BIOS flasher mode

BIOS_FLASHER_MODE accepts an empty payload. The EEPROM image size is fixed at
32,768 bytes and is not transmitted. Entering the mode permits full EEPROM
replacement, including boot code and reset vector, if writing is available;
entry itself writes nothing. Read-only clients simply never send BIOS_WRITE.

The ROM BIOS copies its self-contained BIOS flasher into payload RAM and
transfers control to it. All code, constants, state, buffers, stack and used
handlers are in RAM. The monitor owns UART, uses polling with hardware
interrupts disabled and makes no calls into ROM BIOS. It preserves UART
configuration and queued RX bytes. The FLASHER_MODE success reply is generated only
once the RAM monitor is ready to receive requests.

Entry restrictions follow [Command availability](#command-availability).
Invalid payload length returns 18 when entry is otherwise permitted.
Failed entry preserves programs and uploads. Successful entry clears the
resident program registry and keyboard sharing/queue because the RAM monitor
may overwrite program memory. It does not run the ROM menu or Memory check.
The flasher is embedded in the BIOS; no host upload of its program is needed.

An identical FLASHER_MODE retry with the same sequence and empty payload
repeats its ACK from RAM without copying again or changing progress. This
lost-ACK retry is the only exception to the RAM-flasher command set. Fresh
FLASHER_MODE requests or non-empty payloads return 20; they cannot restart or
switch the active mode.

Sequence is only eight bits, so a new client can reuse an earlier entry's
sequence. An entry ACK alone therefore cannot prove a fresh session.
To inspect the current mode, send BIOS_READ at `0xF8000` with
size 1: status 17 means the ordinary monitor; status 0 plus one byte means the RAM flasher.
Other statuses remain errors. boardctl probes before automatic entry: it can
read from an existing flasher, but rejects a new write into that active session.

### Writing EEPROM blocks

BIOS_WRITE accepts 5–128 payload bytes: physical `address:u32` followed by
1–124 image bytes. The entire block must lie in the canonical EEPROM window
`0xF8000..0xFFFFF`; RAM, EEPROM mirrors and unmapped addresses are rejected.
Addresses never wrap or truncate to 20 bits. Invalid length/range returns 18
before any write; mode and support checks follow the availability table.
Hosts send consecutive blocks starting at `0xF8000`, keep one request in flight
and wait for its reply before sending another.
On writing firmware, empty data, gaps, overlaps, overflow and out-of-range
blocks return 18 before any byte is written; the last accepted block retry and
restart after completion are the exceptions below.

The programmer validates the complete frame and block before writing. It
splits writes at EEPROM page boundaries, waits for each programming cycle
using the device's completion polling, and checks every written byte by
readback. Block success is sent only after verification; failure returns 19
and may leave part of that block programmed. The host may choose 64-byte
aligned blocks for AT28C256, but the wire limit remains 124 image bytes.

The programmer retains the last attempted block's address, bytes and result.
After success, an identical retry returns the saved ACK without programming
again or advancing progress twice, regardless of sequence wrap. Changed bytes
at that address return 18. After a programming failure, an identical retry with
the same sequence repeats the failure; a different sequence permits another
attempt at that failed address.
No unrelated request clears the WRITE retry cache.

The last WRITE returns success after that block has been programmed and
verified by readback, completing the full 32-KiB transfer. After completion,
an identical last-block retry still returns its saved result. A valid WRITE
at `0xF8000` with a sequence different from the last attempted WRITE explicitly
starts a new full transfer, even if its bytes match the previous image. Other
new blocks return 18. The programmer validates this first block before
changing state, then clears completion and the old WRITE cache, sets progress
to zero and programs the block normally. RESET is blocked from that point
until the new full transfer completes, including if this first write fails.
A lost restart ACK is handled by the normal last-block retry rules; retries
never restart the transfer twice. The RAM flasher remains active throughout;
no BIOS_FLASHER_MODE or RESET is sent to restart a transfer.

No COMMIT or separate final flashing command is needed. Clients must allow
time for programming and readback; the ordinary
one-second monitor timeout is not a completion deadline.
A timeout does not cancel a write. If its outcome is unknown, retry the exact
same request; do not assume the EEPROM or transfer cursor remained unchanged.

### Reading EEPROM and comparing hashes

BIOS_READ accepts exactly six payload bytes: physical `address:u32, size:u16`.
Success returns exactly `status=0x00` followed by the requested 1–127 bytes.
The complete range must lie in the canonical EEPROM window `0xF8000..0xFFFFF`.
Invalid lengths/sizes, RAM, EEPROM mirrors, unmapped addresses, overflow or
ranges crossing the window boundary return 18 before any read. Addresses never
truncate to 20 bits or wrap.

For example, these are decoded request payloads before frame CRC and COBS:

| Request | Payload bytes in hex | RAM-flasher result |
| --- | --- | --- |
| READ `0xF8000`, 127 bytes | `00 80 0F 00 7F 00` | status 0 + 127 EEPROM bytes |
| READ `0xFFFFF`, 1 byte | `FF FF 0F 00 01 00` | status 0 + last EEPROM byte |
| READ `0xFFFFF`, 2 bytes | `FF FF 0F 00 02 00` | status 18, no read |
| WRITE `0xF8000`, byte `0xAA` | `00 80 0F 00 AA` | valid range; status 17 if writing is unsupported |
| WRITE RAM `0x8800`, byte `0xAA` | `00 88 00 00 AA` | status 18, no write |

READ is available from the RAM programmer after BIOS_FLASHER_MODE while
waiting for requests, including before flashing, after the final block and
after a failed write. The programmer must finish any EEPROM
programming cycle before serving a read. READ reads the actual EEPROM,
not the copied monitor or an image buffer. It does not write EEPROM, change
transfer progress, clear the WRITE retry cache or start the new BIOS.

The host reads the full 32-KiB image in chunks and compares its SHA-256 with
that of the source file, including padding and reset vector, before sending
RESET. SHA-256 is computed on the host; the wire reply contains bytes, not a
hash. This check complements the programmer's per-block readback verification.
Chunked reads are not an atomic snapshot: do not send WRITE between chunks
when comparing the image. Repeating READ reads EEPROM again.

If SHA-256 does not match, the host must not send RESET. It may repeat the
complete read to confirm the mismatch, then start a new full transfer using
the WRITE restart rule above, write all 32 KiB and compare SHA-256 again.
The flasher cannot detect a host-side hash mismatch: its RESET permission
reflects only completed writes and per-block readback, so the host must enforce
this check. The current boardctl reports a mismatch and leaves the flasher
active; starting another transfer in an active flasher is not yet exposed by
its CLI.

### Exit and recovery

RESET is available before any EEPROM write or after complete successful
verification. If writing is unsupported, a valid RESET is always permitted.
When writing is supported, RESET after a partial/failed write
returns 18 until all 32,768 image bytes have been written and verified by
readback. Invalid PING/RESET payloads return 5. A successful RESET sends an ACK, waits for UART TX to drain
and starts the ROM BIOS at `0xF800:0x0000`; registry and sharing state are reset.
Firmware gates RESET on completion of the full transfer and per-block readback
verification. It does not receive or compute the host's SHA-256 comparison.
Perform the host comparison described in
[Reading EEPROM and comparing hashes](#reading-eeprom-and-comparing-hashes)
before resetting after flashing. There is no automatic reset after the final WRITE.

Once EEPROM has been changed, the RAM monitor must remain active after errors
or host disconnection. Retrying writes permits recovery without returning to
incomplete ROM; it provides no rollback. Power loss or hardware Reset during
writing can require an external programmer or an independently preserved
recovery loader.

## RAM reads and writes

MEMORY_READ returns exactly `status=0x00` followed by the requested 1–127 bytes.
The entire range must lie in the board's configured RAM address space.
Clients must use the target implementation's memory map, including any RAM
aliases. The protocol does not prescribe RAM capacity or its address layout.
ROM, unmapped addresses, ranges crossing the RAM boundary,
sizes outside 1–127 and payload lengths other than 6 return status 14.
Addresses never wrap or truncate to 20 bits. EEPROM reads use BIOS_READ after
entering BIOS flasher mode.
Reads include IVT, BDA and BIOS state but do not write the requested memory;
normal UART packet buffers, BIOS stack and live state still change during
handling. Chunked dumps are not atomic snapshots. Retries read again, not a
cached snapshot. Mode restrictions follow the availability table.

MEMORY_WRITE accepts payload lengths 5–128: address followed by 1–124 bytes.
The complete range is checked before any byte is written. Only the firmware's
payload/program RAM is writable. The implementation defines its writable
ranges and protects reserved memory through every physical alias. IVT/BDA, BIOS/API
state, stacks, ROM and unmapped regions are rejected with status 14, including
ranges crossing a protected boundary. Mode restrictions follow the availability
table. Retrying a write overwrites the same address with the same bytes.
Raw writes do not allocate or register programs and may modify resident program
code. Multi-packet writes are not transactional and provide no rollback.

## Upload and program registry

BEGIN/DATA/COMMIT/ABORT below abbreviate the PROGRAM_UPLOAD_* commands.

The registry has four slots. Programs occupy the implementation's payload RAM;
BIOS state and stacks remain reserved. BEGIN returns the chosen physical address.
Allocation is first-fit, aligned upward to 16 bytes. Deleted ranges are reused;
resident program bytes are never moved. Fragmentation can prevent an allocation
even when the total free byte count would suffice.
Use BIOS's reply rather than assuming a fixed allocation address.

BEGIN names are 1–15 printable ASCII bytes (`0x20`..`0x7E`), not all spaces,
case-sensitive and unique among committed programs. No name terminator;
total request data length is 7–21 bytes. Image CRC uses the frame CRC algorithm
over the entire binary. Success returns
`status:u8, ID:u8, physical_address:u32` (6 bytes).
Although BEGIN carries size:u32, each program must be 1–65,535 bytes because
PROGRAM_LIST carries size:u16 and the program ABI uses a 16-bit segment.
The implementation's allocation limit can be smaller. Zero or a nonzero high
16-bit word returns 2; insufficient payload RAM or a firmware allocation limit
returns 7 (or 6 if all slots are occupied).

Successful BEGIN replaces any pending reservation without touching committed programs.
It is not globally deduplicated: retrying BEGIN starts the reservation again.
Retry it only before sending DATA; a rejected BEGIN preserves the previous upload.
Only committed programs occupy allocation ranges. DATA contains only program bytes;
BIOS maintains the write address and received byte count inside the reservation.
Each block must contain exactly 128 bytes, except the last block, which must
contain all remaining bytes (1–128). Hosts keep only one request in flight and
wait for its ACK before sending the next block. Retrying the last accepted DATA
with the same sequence and identical bytes repeats its ACK without writing or
counting twice; changed bytes with that sequence return status 3.
Sequence wraps from 255 to 0 and is not a block index. Successful BEGIN, COMMIT
and ABORT clear the DATA retry cache. Rejected requests, including a failed
COMMIT CRC check, do not clear it. No additional new block is accepted after completion.

COMMIT publishes only after all declared bytes arrive and image CRC matches.
COMMIT of an already committed ID is a no-op when no upload is pending.
CRC mismatch returns 9 and leaves the upload pending; use ABORT or a new BEGIN
to discard or replace it. Already accepted DATA cannot be patched by resending
different bytes with its old sequence.
ABORT requires ID=count+1; repeating it without a pending upload is a no-op.
Neither command clears resident programs.

PROGRAM_EXEC rejects pending uploads and accepts only committed IDs (1..count).
Immediate retries of the same sequence/ID do not execute twice; any non-PROGRAM_EXEC
command handled by the ordinary ROM menu clears this cache. Send PING from
the menu before a new intentional launch.
RESET clears all registry metadata.

PROGRAM_LIST returns `status:u8, count:u8`, then count 23-byte records:
`ID:u8, physical_address:u32, size:u16, name[16]`. Names are NUL-terminated
and zero-padded; IDs are consecutive 1..count. Pending entries are excluded.
Records are ID-ordered, not necessarily address-ordered after hole reuse.

PROGRAM_DELETE removes a committed ID and frees its slot and RAM range without
moving or zeroing any program bytes. Following IDs shift down by one; refresh
PROGRAM_LIST before using saved IDs. The LCD switches to Custom programs.
Immediate retries with the same sequence/ID are acknowledged without deleting
the next program. Any non-PROGRAM_DELETE command clears this retry cache; send
PING before a new intentional deletion (boardctl does this automatically).

PROGRAM_RENAME changes only the name of a committed ID. Names obey the same
rules as BEGIN; the name may remain unchanged, but must not match another
resident's name. Request payload length is 2–16 bytes. Address, size and program
bytes are preserved, and the LCD is refreshed. Repeating the rename is harmless.
Both commands return status 16 for malformed payloads/nonexistent IDs, 15 during
pending uploads, 10 during memory checks, or 11 from a polling running program.
Duplicate names return status 8.

## Keyboard sharing

Attach with KEYBOARD_MODE data `0x01` from the ROM menu before launching a
program; `0x00` detaches. Mode transitions clear the key queue and retry cache;
repeated attachment preserves keys. Reset disables sharing; RETF preserves
sharing but clears pending keys and UART RX.

Successful mode/event replies are exactly `status=0x00, flags:u8`.
Bit 0 indicates a running program or a menu key about to launch one after the
reply drains; all other bits are zero. No LCD data is included.

Events require `(ASCII != 0 || scan != 0)`, with ASCII in `0x00`..`0x7F`.
ASCII with scan=0 is translated by BIOS where supported. Extended keys use
ASCII=0, scan!=0 (Up=`0x48`, Down=`0x50`, Left=`0x4B`, Right=`0x4D`).
The menu consumes Enter, Esc, Up and Down from the shared 15-key BDA ring;
other keys have no menu action. Retrying the last accepted sequence/data
does not enqueue or navigate twice. Status 13 means the key was not accepted.

The protocol guarantees framed replies in the ROM menu and RAM flasher.
Enable sharing before launching a program. Framed traffic while it runs
requires the BIOS to service UART requests. Cooperative implementations do
this through INT `0x16`/AH=`0x00` or AH=`0x01` calls with sharing enabled.
The accepted subset and busy/unsupported replies are listed in
[Command availability](#command-availability). Without regular polling,
requests receive no reply and UART FIFO overflow is possible.
Protocol v1 does not require background command handling while a program owns
the board. Clients
must wait for return unless that firmware provides the cooperative subset.
Sharing owns UART RX: raw INT `0x14` receive is rejected; TX/status still work.
Detaching with KEYBOARD_MODE=0 while a program runs stops framed polling;
subsequent INT `0x16` reads use raw UART input until program return or Reset.
Only one host process may own the UART. Reconnect after Reset; Reset also
recovers raw mode if detach cannot be acknowledged.

## Runtime availability

PROGRAM_EXEC and menu launches transfer control after their reply drains.
Stop ordinary monitor requests until `8088 ROM monitor ready\r\n` announces
return/reset; clients must tolerate ASCII text between frames. The cooperative
keyboard polling subset above is the only exception while a program runs.
RETF preserves committed programs.

Confirmed Memory check invalidates all programs and pending uploads;
cancellation does not restore them. During testing UART remains polled:
keyboard, PING and INFO requests work, but uploads and launches cannot run.

## Related documentation

- [BIOS API and program ABI](../firmware/8088-mainboard/docs/bios-api.md):
  binary format, entry registers, stacks, memory aliases and INT services.
- [BIOS interface](../firmware/8088-mainboard/README.md#boot-menu) and
  [Memory check](../firmware/8088-mainboard/README.md#memory-check): menu,
  LCD layout, progress and diagnostic details.
- [boardctl keyboard](../tools/boardctl/README.md#keyboard-sharing): host
  shortcuts, upload prompt and connection instructions.
