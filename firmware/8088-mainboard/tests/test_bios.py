"""Execute the actual NASM image with emulated UART/LCD inside Docker.

Run via `make test`. This checks ROM code paths, not electrical timings.
"""
import binascii
import ctypes
import hashlib
from collections import deque
import mmap
import os
from pathlib import Path
import struct
import subprocess
import tempfile
import unittest

from unicorn import Uc, UC_ARCH_X86, UC_MODE_16, UC_HOOK_INSN, UC_HOOK_MEM_READ, UC_HOOK_MEM_WRITE, UC_HOOK_CODE, UC_HOOK_INTR
from unicorn.x86_const import (
    UC_X86_INS_IN, UC_X86_INS_OUT, UC_X86_REG_CS, UC_X86_REG_IP,
    UC_X86_REG_SS, UC_X86_REG_SP, UC_X86_REG_EFLAGS,
)

BIOS = Path(__file__).resolve().parents[1] / "build" / "bios.bin"
EEPROM_MONITOR = BIOS.with_name("flasher.bin")
BIOS_ADDRESS = 0xf8000
PAYLOAD_BASE = 0x8800
PAYLOAD_SIZE = 0xfc00 - PAYLOAD_BASE
BIOS_VERSION = os.environ.get("BIOS_VERSION", "dev")

# Protocol v1 command values, grouped exactly as the wire specification.
PING = 0x01
INFO = 0x02
RESET = 0x03
BIOS_FLASHER_MODE = 0x04
BIOS_WRITE = 0x05
BIOS_READ = 0x06
MEMORY_READ = 0x07
MEMORY_WRITE = 0x08
PROGRAM_LIST = 0x09
PROGRAM_UPLOAD_BEGIN = 0x0a
PROGRAM_UPLOAD_DATA = 0x0b
PROGRAM_UPLOAD_COMMIT = 0x0c
PROGRAM_UPLOAD_ABORT = 0x0d
PROGRAM_EXEC = 0x0e
PROGRAM_DELETE = 0x0f
PROGRAM_RENAME = 0x10
KEYBOARD_MODE = 0x11
KEYBOARD_EVENT = 0x12


def assemble(source):
    with tempfile.TemporaryDirectory() as directory:
        path = Path(directory)
        (path / "test.asm").write_text(
            'BITS 16\nCPU 8086\nORG 0\n%include "bios_api.inc"\n' + source)
        subprocess.run(["nasm", "-f", "bin", "-I", str(BIOS.parent.parent / "lib") + "/",
                        "-o", str(path / "test.bin"), str(path / "test.asm")], check=True)
        return (path / "test.bin").read_bytes()


def encode(command, sequence, data=b""):
    raw = struct.pack("<BBBH", 1, command, sequence, len(data)) + data
    raw += struct.pack("<H", binascii.crc_hqx(raw, 0xffff))
    return cobs_packet(raw)


def cobs_packet(raw):
    out = bytearray([0])
    code_position, code = 0, 1
    for value in raw:
        if value:
            out.append(value)
            code += 1
        else:
            out[code_position] = code
            code_position, code = len(out), 1
            out.append(0)
    out[code_position] = code
    return bytes([0]) + out + bytes([0])


def decode(packet):
    assert packet[0] == packet[-1] == 0
    raw, position = bytearray(), 1
    while position < len(packet) - 1:
        code = packet[position]
        assert code
        position += 1
        raw.extend(packet[position:position + code - 1])
        position += code - 1
        if code != 255 and position < len(packet) - 1:
            raw.append(0)
    assert binascii.crc_hqx(raw[:-2], 0xffff) == struct.unpack("<H", raw[-2:])[0]
    version, command, sequence, length = struct.unpack("<BBBH", raw[:5])
    assert version == 1 and len(raw) == length + 7
    return command, sequence, bytes(raw[5:-2])


class Board:
    def __init__(self, initial_ram=None):
        self.cpu = Uc(UC_ARCH_X86, UC_MODE_16)
        # One real SRAM backing store, selected in a 128-KiB bank. A15/A16
        # are not connected to the 32-KiB SRAM: all four aliases share bytes.
        self.sram = mmap.mmap(-1, 0x8000)
        pointer = ctypes.addressof(ctypes.c_char.from_buffer(self.sram))
        for address in range(0, 0x20000, 0x8000):
            self.cpu.mem_map_ptr(address, 0x8000, 7, pointer)
        if initial_ram is not None:
            assert len(initial_ram) == 0x8000
            self.cpu.mem_write(0, initial_ram)
        self.cpu.mem_map(0x20000, 0xe0000)
        self.cpu.mem_write(0xf8000, BIOS.read_bytes())
        self.cpu.reg_write(UC_X86_REG_CS, 0xffff)
        self.cpu.reg_write(UC_X86_REG_IP, 0)
        self.rx, self.tx = deque(), bytearray()
        self.lcr, self.sequence, self.stop_on_reply = 0, 0, False
        self.reply_started, self.reply_length = False, 0
        self.tx_ready = True
        self.lcd, self.cursor = bytearray(b" " * 128), 0
        self.lcd_operations = None  # opt-in trace for menu blink tests
        self.cgram, self.cgram_cursor = bytearray(64), None
        self.interrupts = []
        self.cpu.hook_add(UC_HOOK_INSN, self.input, None, 1, 0, UC_X86_INS_IN)
        self.cpu.hook_add(UC_HOOK_INSN, self.output, None, 1, 0, UC_X86_INS_OUT)
        self.cpu.hook_add(UC_HOOK_MEM_WRITE, self.memory_write)
        self.cpu.hook_add(UC_HOOK_INTR, self.interrupt)
        self.advance(700000)
        self.tx.clear()

    def memory_write(self, cpu, access, address, size, value, user):
        assert 0 <= address and address + size <= 0x20000, hex(address)

    def interrupt(self, cpu, number, user):
        # Unicorn reports software INT but does not perform real-mode IVT
        # dispatch. Reproduce only that CPU mechanism; actual ROM handlers
        # execute normally, including their real IRET instruction.
        flags = cpu.reg_read(UC_X86_REG_EFLAGS) & 0xffff
        cs, ip = cpu.reg_read(UC_X86_REG_CS), cpu.reg_read(UC_X86_REG_IP)
        ss, sp = cpu.reg_read(UC_X86_REG_SS), cpu.reg_read(UC_X86_REG_SP)
        for value in (flags, cs, ip):
            sp = (sp - 2) & 0xffff
            cpu.mem_write(ss * 16 + sp, struct.pack("<H", value))
        offset, segment = struct.unpack("<HH", cpu.mem_read(number * 4, 4))
        self.interrupts.append(number)
        cpu.reg_write(UC_X86_REG_SP, sp)
        cpu.reg_write(UC_X86_REG_EFLAGS, flags & ~0x300)
        cpu.reg_write(UC_X86_REG_CS, segment)
        cpu.reg_write(UC_X86_REG_IP, offset)

    def input(self, cpu, port, size, user):
        if port == 0xa5:
            return (0x60 if self.tx_ready else 0) | bool(self.rx)
        if port == 0xa0 and self.rx:
            return self.rx.popleft()
        return 0

    def output(self, cpu, port, size, value, user):
        if port in (0xc0, 0xc1) and self.lcd_operations is not None:
            self.lcd_operations.append((port, value))
        if port == 0xa3:
            self.lcr = value
        if port == 0xa2 and value & 2:
            self.rx.clear()
        if port == 0xa0 and not self.lcr & 0x80:
            self.tx.append(value)
            if self.stop_on_reply:
                if value == 0:
                    if self.reply_started and self.reply_length:
                        cpu.emu_stop()
                    self.reply_started = True
                elif self.reply_started:
                    self.reply_length += 1
        if port == 0xc0:
            if value == 1:
                self.lcd[:] = b" " * 128
                self.cgram_cursor = None
            elif value & 0x80:
                self.cursor = value & 0x7f
                self.cgram_cursor = None
            elif value & 0x40:
                self.cgram_cursor = value & 0x3f
        if port == 0xc1:
            if self.cgram_cursor is not None:
                self.cgram[self.cgram_cursor] = value & 31
                self.cgram_cursor = (self.cgram_cursor + 1) & 63
            else:
                self.lcd[self.cursor] = value
                self.cursor = (self.cursor + 1) & 0x7f

    def advance(self, instructions):
        start = self.cpu.reg_read(UC_X86_REG_CS) * 16 + self.cpu.reg_read(UC_X86_REG_IP)
        self.cpu.emu_start(start, 0x100000, count=instructions)

    def transact(self, command, data=b"", address=None, sequence=None):
        # Test convenience only: command arguments become payload bytes,
        # never a field in the wire header.
        if address is not None:
            if command in (PROGRAM_EXEC, PROGRAM_UPLOAD_COMMIT, PROGRAM_UPLOAD_ABORT):
                ident = bytes([address]) if 0 <= address <= 255 else struct.pack("<I", address)
                data = ident + data
            else:
                data += struct.pack("<I", address)  # intentionally malformed request
        if sequence is None:
            sequence = self.sequence
            self.sequence = (self.sequence + 1) & 255
        self.tx.clear()
        self.reply_started, self.reply_length = False, 0
        self.rx.extend(encode(command, sequence, data=data))
        self.stop_on_reply = True
        self.advance(2000000)
        self.stop_on_reply = False
        # Programs may emit ASCII before their next INT 16h pumps an ACK.
        # The real host frame reader skips that text; do the same here.
        cmd, seq, result = decode(self.tx[self.tx.find(0):])
        assert cmd == command | 0x80 and seq == sequence
        return result

    def screen(self):
        # Synchronize with the renderer, then inspect the actual emulated LCD.
        # No UART screen command or framebuffer contents are used here.
        for _ in range(2000):
            dirty = self.cpu.mem_read(0x8688, 1)[0]
            position = self.cpu.mem_read(0x8696, 1)[0]
            if not dirty and position >= 80:
                break
            self.advance(1000)
        else:
            raise AssertionError("menu LCD rendering did not finish")
        rows = [self.lcd[start:start + 20].decode("ascii")
                for start in (0, 0x40, 0x14, 0x54)]
        # Normalize only the blinking marker for content assertions. Separate
        # blink tests below inspect the physical cell without normalization.
        if (self.cpu.reg_read(UC_X86_REG_CS) == 0xf800
                and self.cpu.mem_read(0x867a, 1)[0] in (0, 8)):
            selected = self.cpu.mem_read(0x8679, 1)[0]
            row = min(selected, 2) + 1
            assert rows[row][0] in "> "
            rows[row] = ">" + rows[row][1:]
        return rows

    def begin(self, name, payload, crc=None):
        checksum = binascii.crc_hqx(payload, 0xffff) if crc is None else crc
        return self.transact(PROGRAM_UPLOAD_BEGIN, struct.pack("<IH", len(payload), checksum) + name.encode())

    def upload(self, name, payload):
        reply = self.begin(name, payload)
        assert len(reply) == 6 and reply[0] == 0, reply
        ident, address = reply[1], struct.unpack("<I", reply[2:])[0]
        for offset in range(0, len(payload), 128):
            assert self.transact(PROGRAM_UPLOAD_DATA, payload[offset:offset + 128]) == b"\0"
        assert self.transact(PROGRAM_UPLOAD_COMMIT, address=ident) == b"\0"
        # Shared backing aliases are not tracked by Unicorn's translation
        # cache. Real 8088 instruction fetch sees the newly written bytes.
        self.cpu.ctl_flush_tb()
        return ident, address

    def eeprom_mode(self, sequence=None):
        result = self.transact(BIOS_FLASHER_MODE, sequence=sequence)
        assert result == b"\0", result
        return result

    def programs(self):
        data = self.transact(PROGRAM_LIST)
        assert data[0] == 0 and len(data) == 2 + data[1] * 23
        return [struct.unpack("<BIH16s", data[i:i+23]) for i in range(2, len(data), 23)]

    def key(self, key, sequence=None):
        assert self.transact(KEYBOARD_MODE, b"\1")[0] == 0
        data = {1: b"\0\x48", 2: b"\0\x50", 3: b"\r\x1c", 4: b"\x1b\1"}[key]
        return self.transact(KEYBOARD_EVENT, data, sequence=sequence)

    def start_memory_test(self):
        self.key(2)
        self.key(2)
        self.key(3)
        assert [row.strip() for row in self.screen()] == [
            "ALL RAM PROGRAMS", "WILL BE DELETED!", "", "YES:ENTER  NO:ESC"]
        self.key(3)


class FirmwareTest(unittest.TestCase):
    def setUp(self):
        self.board = Board()

    def run_api(self, source, data="", incoming=b"", delayed=False, tx_ready=True):
        """Execute an SDK client and stop immediately before its real RETF."""
        payload = assemble(source + "\njmp finish\n" + data +
                           "\nfinish: retf\nresults: times 128 db 0\n")
        ident, address = self.board.upload("API test", payload)
        result_address = address + len(payload) - 128
        reached = []
        def finish(cpu, address, size, user):
            reached.append(address)
            cpu.emu_stop()
        hook = self.board.cpu.hook_add(UC_HOOK_CODE, finish,
                                      begin=result_address - 1, end=result_address - 1)
        # A blocked transmitter must be simulated after PROGRAM_EXEC's ACK has fully
        # drained; otherwise the monitor rightly waits before entering RAM.
        ready_hook = None
        if not tx_ready:
            def at_entry(cpu, address, size, user):
                self.board.tx_ready = False
            ready_hook = self.board.cpu.hook_add(UC_HOOK_CODE, at_entry,
                                                begin=address, end=address)
        self.board.transact(PROGRAM_EXEC, address=ident)
        if delayed:
            self.board.advance(700000)
            self.assertEqual(reached, [])
        self.board.rx.extend(incoming)
        self.board.advance(4000000)
        self.assertTrue(reached, "API client did not reach RETF")
        self.board.cpu.hook_del(hook)
        if ready_hook is not None:
            self.board.cpu.hook_del(ready_hook)
        return bytes(self.board.cpu.mem_read(result_address, 128))

    def lcd_rows(self):
        return [bytes(self.board.lcd[a:a+20]) for a in (0, 0x40, 0x14, 0x54)]

    def assert_lcd_matches_menu(self, screen):
        lcd = [row.decode() for row in self.lcd_rows()]
        for row in range(1, 4):
            if screen[row].startswith('>'):
                self.assertIn(lcd[row][0], '> ')
                lcd[row] = '>' + lcd[row][1:]
        self.assertEqual(lcd, screen)

    def test_bios_write_is_unsupported_and_preserves_program_upload(self):
        board = self.board
        image = b"\xcb" + b"A" * 128
        reply = board.begin("pending", image)
        ident = reply[1]
        address = struct.unpack("<I", reply[2:])[0]
        self.assertEqual(board.transact(PROGRAM_UPLOAD_DATA, image[:128]), b"\0")
        payload_before = bytes(board.cpu.mem_read(PAYLOAD_BASE, PAYLOAD_SIZE))
        registry_before = bytes(board.cpu.mem_read(0x86a2, 80))
        rom_before = bytes(board.cpu.mem_read(0xf8000, 32768))
        commands = [
            (BIOS_WRITE, struct.pack("<I", 0) + b"X" * 124),
        ]
        for command, request in commands:
            for data in (request, b"", b"X", b"X" * 128):
                with self.subTest(command=command, length=len(data)):
                    self.assertEqual(board.transact(command, data, sequence=42), b"\x11")
                    self.assertEqual(board.transact(command, data, sequence=42), b"\x11")
        self.assertEqual(bytes(board.cpu.mem_read(PAYLOAD_BASE, PAYLOAD_SIZE)), payload_before)
        self.assertEqual(bytes(board.cpu.mem_read(0x86a2, 80)), registry_before)
        self.assertEqual(bytes(board.cpu.mem_read(0xf8000, 32768)), rom_before)
        # Unsupported BIOS requests neither cancel nor advance a program upload.
        self.assertEqual(board.transact(PROGRAM_UPLOAD_DATA, image[128:]), b"\0")
        self.assertEqual(board.transact(PROGRAM_UPLOAD_COMMIT, bytes([ident])), b"\0")
        self.assertEqual(bytes(board.cpu.mem_read(address, len(image))), image)
        self.assertEqual(board.programs()[0][0], ident)
        self.assertEqual(board.transact(PING), b"\0")

    def test_bios_mode_and_write_during_memory_check(self):
        board = self.board
        board.start_memory_test()
        self.assertEqual(board.transact(BIOS_FLASHER_MODE), b"\x0a")
        self.assertEqual(board.transact(BIOS_WRITE), b"\x11")
        self.assertEqual(board.cpu.mem_read(0x867a, 1), b"\4")
        self.assertEqual(board.transact(PING), b"\0")

    def test_eeprom_mode_entry_validation_and_upload_busy(self):
        board = self.board
        for request in (b"X", b"X" * 2, b"X" * 3, struct.pack("<IH", 32768, 0), b"X" * 128):
            self.assertEqual(board.transact(BIOS_FLASHER_MODE, request), b"\x12")
            self.assertEqual(board.transact(PING), b"\0")
        payload = b"\xcb" + b"X" * 128
        reply = board.begin("pending", payload)
        self.assertEqual(board.transact(PROGRAM_UPLOAD_DATA, payload[:128]), b"\0")
        before = bytes(board.cpu.mem_read(PAYLOAD_BASE, PAYLOAD_SIZE))
        self.assertEqual(board.transact(BIOS_FLASHER_MODE), b"\x0f")
        self.assertEqual(bytes(board.cpu.mem_read(PAYLOAD_BASE, PAYLOAD_SIZE)), before)
        self.assertEqual(board.transact(PROGRAM_UPLOAD_DATA, payload[128:]), b"\0")
        self.assertEqual(board.transact(PROGRAM_UPLOAD_COMMIT, bytes([reply[1]])), b"\0")

    def test_eeprom_mode_copies_monitor_and_disables_programs_and_sharing(self):
        board = self.board
        board.upload("resident", b"\xcb")
        self.assertEqual(board.transact(KEYBOARD_MODE, b"\1"), b"\0\0")
        board.eeprom_mode()
        self.assertEqual(board.cpu.reg_read(UC_X86_REG_CS), PAYLOAD_BASE >> 4)
        monitor = EEPROM_MONITOR.read_bytes()
        self.assertLess(len(monitor), PAYLOAD_SIZE)
        self.assertEqual(bytes(board.cpu.mem_read(PAYLOAD_BASE, len(monitor))), monitor)
        self.assertEqual(board.cpu.mem_read(0x86f2, 1), b"\0")
        self.assertEqual(board.cpu.mem_read(0x8757, 1), b"\0")
        head, tail = struct.unpack("<HH", board.cpu.mem_read(0x41a, 4))
        self.assertEqual(head, tail)
        self.assertEqual(board.cpu.reg_read(UC_X86_REG_EFLAGS) & 0x600, 0)
        for command in (INFO, MEMORY_READ, MEMORY_WRITE, PROGRAM_LIST,
                        PROGRAM_UPLOAD_BEGIN, PROGRAM_UPLOAD_DATA, PROGRAM_UPLOAD_COMMIT,
                        PROGRAM_UPLOAD_ABORT, PROGRAM_EXEC, PROGRAM_DELETE, PROGRAM_RENAME,
                        KEYBOARD_MODE, KEYBOARD_EVENT, 0, 0x7f):
            self.assertEqual(board.transact(command), b"\x14")
        for request in (b"", b"X"):
            self.assertEqual(board.transact(BIOS_WRITE, request), b"\x12")
        self.assertEqual(board.transact(BIOS_WRITE, struct.pack("<I", BIOS_ADDRESS) + b"X" * 124), b"\x11")
        self.assertEqual(board.transact(PING), b"\0")
        self.assertEqual(bytes(board.cpu.mem_read(PAYLOAD_BASE, len(monitor))), monitor)

    def test_eeprom_mode_lost_entry_ack_retry_and_readback_hash(self):
        board = self.board
        board.eeprom_mode(sequence=42)
        before = bytes(board.cpu.mem_read(PAYLOAD_BASE, len(EEPROM_MONITOR.read_bytes())))
        self.assertEqual(board.transact(BIOS_FLASHER_MODE, sequence=42), b"\0")
        self.assertEqual(board.transact(PING), b"\0")
        readback = bytearray()
        source = BIOS.read_bytes()
        for offset in range(0, 32768, 127):
            size = min(127, 32768 - offset)
            data = board.transact(BIOS_READ, struct.pack("<IH", BIOS_ADDRESS + offset, size))
            self.assertEqual(data[0], 0)
            readback.extend(data[1:])
        self.assertEqual(hashlib.sha256(readback).digest(), hashlib.sha256(source).digest())
        self.assertEqual(board.transact(BIOS_FLASHER_MODE, sequence=42), b"\0")
        self.assertEqual(board.transact(BIOS_FLASHER_MODE, struct.pack("<H", 0x5678), sequence=42), b"\x14")
        self.assertEqual(board.transact(BIOS_FLASHER_MODE, sequence=43), b"\x14")
        self.assertEqual(bytes(board.cpu.mem_read(PAYLOAD_BASE, len(before))), before)

    def test_eeprom_mode_has_no_rom_code_dependency_and_resets_after_tx_drain(self):
        board = self.board
        board.eeprom_mode()
        fetched_rom = []
        def rom_fetch(cpu, address, size, user):
            fetched_rom.append(address)
        hook = board.cpu.hook_add(UC_HOOK_CODE, rom_fetch, begin=0xf8000, end=0xfffff)
        # A RAM monitor must still work even if the whole original ROM changes.
        board.cpu.mem_write(0xf8000, b"Z" * 32768)
        board.cpu.ctl_flush_tb()
        self.assertEqual(board.transact(PING), b"\0")
        self.assertEqual(board.transact(BIOS_READ, struct.pack("<IH", BIOS_ADDRESS, 127)), b"\0" + b"Z" * 127)
        self.assertEqual(board.transact(BIOS_WRITE, struct.pack("<I", BIOS_ADDRESS) + b"X"), b"\x11")
        self.assertEqual(board.transact(INFO), b"\x14")
        self.assertEqual(board.transact(BIOS_READ, struct.pack("<IH", 0xfffff, 1)), b"\0Z")
        self.assertEqual(fetched_rom, [])
        board.cpu.mem_write(0xf8000, BIOS.read_bytes())
        board.cpu.ctl_flush_tb()
        self.assertEqual(board.transact(RESET, b"X"), b"\x05")
        self.assertEqual(board.transact(PING, b"X"), b"\x05")
        self.assertEqual(board.transact(RESET), b"\0")
        board.tx_ready = False
        board.advance(10000)
        self.assertEqual(fetched_rom, [])
        board.tx_ready = True
        board.advance(700000)
        self.assertTrue(fetched_rom)
        board.cpu.hook_del(hook)
        self.assertEqual(board.transact(INFO)[0], 0)
        self.assertEqual(board.programs(), [])
        self.assertEqual(board.cpu.reg_read(UC_X86_REG_CS), 0xf800)

    def test_eeprom_mode_read_bounds_and_malformed_frame_recovery(self):
        board = self.board
        board.eeprom_mode()
        for request in (b"", struct.pack("<IH", BIOS_ADDRESS, 0), struct.pack("<IH", BIOS_ADDRESS, 128),
                        struct.pack("<IH", 0xfffff, 2), struct.pack("<IH", 0x10000, 1),
                        struct.pack("<IH", 0xffffffff, 1)):
            self.assertEqual(board.transact(BIOS_READ, request), b"\x12")
        board.tx.clear()
        board.rx.extend(b"\0\x02\xff\0")
        board.rx.extend(encode(BIOS_READ, 42, b"X" * 129))
        board.advance(100000)
        self.assertEqual(board.tx, b"")
        self.assertEqual(board.transact(PING), b"\0")
        self.assertEqual(board.transact(BIOS_READ, struct.pack("<IH", 0xfffff, 1)), b"\0\xff")

    def test_bios_read_unsupported_preserves_program_upload(self):
        board = self.board
        image = b"\xcb" + b"A" * 128
        reply = board.begin("pending", image)
        self.assertEqual(board.transact(PROGRAM_UPLOAD_DATA, image[:128]), b"\0")
        payload_before = bytes(board.cpu.mem_read(PAYLOAD_BASE, PAYLOAD_SIZE))
        for request in (b"", struct.pack("<IH", 0, 127), struct.pack("<IH", 32767, 1)):
            self.assertEqual(board.transact(BIOS_READ, request), b"\x11")
        self.assertEqual(bytes(board.cpu.mem_read(PAYLOAD_BASE, PAYLOAD_SIZE)), payload_before)
        self.assertEqual(board.transact(PROGRAM_UPLOAD_DATA, image[128:]), b"\0")
        self.assertEqual(board.transact(PROGRAM_UPLOAD_COMMIT, bytes([reply[1]])), b"\0")

    def test_flasher_read_bounds_retries_and_eeprom_source(self):
        board = self.board
        board.eeprom_mode()
        for request in [b"", b"X" * 5, b"X" * 7,
                        struct.pack("<IH", BIOS_ADDRESS, 0), struct.pack("<IH", BIOS_ADDRESS, 128),
                        struct.pack("<IH", 0, 1), struct.pack("<IH", 0x8800, 1),
                        struct.pack("<IH", 0xe0000, 1), struct.pack("<IH", 0xf0000, 1),
                        struct.pack("<IH", 0xf7fff, 2), struct.pack("<IH", 0xfffff, 2),
                        struct.pack("<IH", 0x1f8000, 1),
                        struct.pack("<IH", 0x100000, 1),
                        struct.pack("<IH", 0xffffffff, 127)]:
            self.assertEqual(board.transact(BIOS_READ, request), b"\x12")
        for address, size in [(BIOS_ADDRESS, 1), (0xfffff, 1), (0x100000 - 127, 127)]:
            request = struct.pack("<IH", address, size)
            expected = b"\0" + bytes(board.cpu.mem_read(address, size))
            self.assertEqual(board.transact(BIOS_READ, request, sequence=42), expected)
            self.assertEqual(board.transact(BIOS_READ, request, sequence=42), expected)
        # Read the hardware EEPROM window on every retry, without a cached image.
        request = struct.pack("<IH", 0xfff00, 1)
        self.assertEqual(board.transact(BIOS_READ, request, sequence=42), b"\0\xff")
        board.cpu.mem_write(0xfff00, b"Z")
        self.assertEqual(board.transact(BIOS_READ, request, sequence=42), b"\0Z")
        self.assertEqual(board.transact(PING), b"\0")

    def test_flasher_write_validates_physical_range_before_unsupported(self):
        board = self.board
        board.eeprom_mode()
        rom = bytes(board.cpu.mem_read(BIOS_ADDRESS, 32768))
        monitor = bytes(board.cpu.mem_read(PAYLOAD_BASE, len(EEPROM_MONITOR.read_bytes())))
        for request in (b"", b"X" * 4, struct.pack("<I", BIOS_ADDRESS)):
            self.assertEqual(board.transact(BIOS_WRITE, request), b"\x12")
        for address, size in ((0, 1), (0x8800, 1), (0xe0000, 1), (0xf0000, 1),
                              (0xf7fff, 2), (0xfffff, 2), (0x100000, 1),
                              (0x1f8000, 1), (0xffffffff, 1)):
            self.assertEqual(board.transact(BIOS_WRITE, struct.pack("<I", address) + b"X" * size), b"\x12")
        for address, size in ((BIOS_ADDRESS, 124), (0x100000 - 124, 124), (0xfffff, 1)):
            self.assertEqual(board.transact(BIOS_WRITE, struct.pack("<I", address) + b"X" * size), b"\x11")
        self.assertEqual(bytes(board.cpu.mem_read(BIOS_ADDRESS, 32768)), rom)
        self.assertEqual(bytes(board.cpu.mem_read(PAYLOAD_BASE, len(monitor))), monitor)
        self.assertEqual(board.transact(PING), b"\0")

    def test_flasher_lcd_entry_operations_errors_and_reset(self):
        board = self.board
        board.eeprom_mode()
        self.assertEqual([row.strip() for row in board.screen()], [
            "BIOS FLASHER", "READY - READ ONLY", "ROM: F8000-FFFFF",
            "HOST RESET TO EXIT",
        ])
        board.lcd_operations = []
        board.advance(10000)
        self.assertEqual(board.lcd_operations, [])
        request = struct.pack("<IH", BIOS_ADDRESS, 127)
        self.assertEqual(board.transact(BIOS_READ, request), b"\0" + BIOS.read_bytes()[:127])
        self.assertEqual([row.strip() for row in board.screen()], [
            "BIOS FLASHER", "READ OK", "ROM: F8000-F807E", "HOST RESET TO EXIT",
        ])
        self.assertEqual(board.transact(PING), b"\0")
        self.assertEqual(board.screen()[1].strip(), "READ OK")
        self.assertEqual(board.transact(BIOS_WRITE, struct.pack("<I", 0xfffff) + b"X"), b"\x11")
        self.assertEqual([row.strip() for row in board.screen()], [
            "BIOS FLASHER", "WRITE UNSUPPORTED", "ROM: FFFFF-FFFFF",
            "HOST RESET TO EXIT",
        ])
        for command, request, status in (
            (BIOS_READ, struct.pack("<IH", 0xfffff, 2), "INVALID READ"),
            (BIOS_WRITE, b"X", "INVALID WRITE"),
            (PING, b"X", "INVALID REQUEST"),
            (KEYBOARD_EVENT, b"\r\x1c", "COMMAND UNAVAILABLE"),
        ):
            self.assertNotEqual(board.transact(command, request)[0], 0)
            self.assertEqual(board.screen()[1].strip(), status)
            self.assertEqual(board.screen()[2].strip(), "ROM: F8000-FFFFF")
        self.assertEqual(board.transact(RESET), b"\0")
        board.advance(700000)
        self.assertEqual(board.screen()[0].strip(), "8088 BIOS " + BIOS_VERSION)

    def test_flasher_lcd_redraw_keeps_up_with_read_traffic(self):
        board = self.board
        board.eeprom_mode(sequence=42)
        board.lcd_operations = []
        # Keep sending requests during the first draw, without waiting for LCD.
        for offset in range(0, 4096, 127):
            request = struct.pack("<IH", BIOS_ADDRESS + offset, 127)
            self.assertEqual(board.transact(BIOS_READ, request),
                             b"\0" + BIOS.read_bytes()[offset:offset + 127])
        self.assertTrue(any(port == 0xc1 for port, _ in board.lcd_operations))
        self.assertEqual(board.transact(BIOS_READ, struct.pack("<IH", 0xfffff, 1)), b"\0\xff")
        self.assertEqual([row.strip() for row in board.screen()], [
            "BIOS FLASHER", "READ OK", "ROM: FFFFF-FFFFF", "HOST RESET TO EXIT",
        ])
        self.assertEqual(board.transact(BIOS_FLASHER_MODE, sequence=42), b"\0")
        self.assertEqual(board.screen()[1].strip(), "READ OK")
        self.assertFalse(any(port == 0xc0 and value == 1
                             for port, value in board.lcd_operations))

    def test_bios_read_unsupported_during_memory_check(self):
        board = self.board
        board.start_memory_test()
        self.assertEqual(board.transact(BIOS_READ, struct.pack("<IH", 0, 127)), b"\x11")
        self.assertEqual(board.transact(PING), b"\0")
        self.assertEqual(board.cpu.mem_read(0x867a, 1), b"\4")

    def test_ivt_mirrors_and_reserved_memory_protection(self):
        board = self.board
        ivt_bda = bytes(board.cpu.mem_read(0, 0x500))
        for base in (0x8000, 0x10000, 0x18000):
            self.assertEqual(bytes(board.cpu.mem_read(base, 0x500)), ivt_bda)
        for number in (0x10, 0x14, 0x16, 0x60):
            offset, segment = struct.unpack("<HH", ivt_bda[number*4:number*4+4])
            self.assertEqual(segment, 0xf800)
            self.assertLess(offset, 0x7ff0)
        self.assertEqual(struct.unpack("<H", ivt_bda[0x413:0x415])[0], 32)
        self.assertEqual(struct.unpack("<H", ivt_bda[0x400:0x402])[0], 0xa0)
        self.assertEqual(struct.unpack("<H", ivt_bda[0x44a:0x44c])[0], 20)
        self.assertEqual(ivt_bda[0x484], 3)
        board.begin("Pending", b"test")
        for alias in (0x40, 0x8040, 0x10040, 0x18040, 0x8400, 0x8500, 0x8700):
            # The removed address-prefixed DATA layout is not a valid block.
            self.assertEqual(board.transact(PROGRAM_UPLOAD_DATA, struct.pack("<I", alias) + b"x"), b"\x03")
        self.assertEqual(bytes(board.cpu.mem_read(0, 0x500)), ivt_bda)
        board.transact(PROGRAM_UPLOAD_ABORT, address=1)
        board.start_memory_test()
        # Navigation legitimately updates the BDA keyboard ring. Everything
        # outside that ring must still match boot, and the diagnostic itself
        # must preserve the entire IVT/BDA including those consumed keys.
        protected = bytes(board.cpu.mem_read(0, 0x500))
        self.assertEqual(protected[:0x41a], ivt_bda[:0x41a])
        self.assertEqual(protected[0x43e:], ivt_bda[0x43e:])
        board.advance(20000000)
        self.assertEqual(bytes(board.cpu.mem_read(0, 0x500)), protected)
        self.assertEqual(board.screen()[3], "OK:148480 BAD:000000")

    def test_api_capabilities_and_register_flags_preservation(self):
        result = self.run_api("""
            mov ah, 0
            int 60h
            mov [results], ax
            mov [results+2], bx
            mov [results+4], cx
            mov [results+6], dx
            mov ax, 0123h
            mov es, ax
            mov bx, 4567h
            mov cx, 89abh
            mov dx, 0000h
            mov si, text
            mov di, 0cdefh
            mov bp, 2345h
            mov ax, 0500h
            mov [results+28], si
            std
            stc
            int 60h
            pushf
            pop word [results+8]
            mov [results+10], bx
            mov [results+12], cx
            mov [results+14], dx
            mov [results+16], si
            mov [results+18], di
            mov [results+20], bp
            mov [results+22], ds
            mov [results+24], es
            mov [results+26], sp
            cld
        """, 'text: db "Hello INT!",0')
        self.assertEqual(struct.unpack("<4H", result[:8]), (1, 0xb105, 0x414, 31))
        flags, bx, cx, dx, si, di, bp, ds, es, sp = struct.unpack("<10H", result[8:28])
        self.assertTrue(flags & 0x400, "IRET must restore DF")
        self.assertFalse(flags & 1, "extension returns CF=0")
        self.assertEqual((bx,cx,dx,di,bp,ds,es,sp),
                         (0x4567,0x89ab,0,0xcdef,0x2345,PAYLOAD_BASE>>4,0x123,0xfdfa))
        self.assertGreater(si, 0)
        self.assertEqual(si, struct.unpack("<H", result[28:30])[0])
        self.assertEqual(self.lcd_rows()[0][:10], b"Hello INT!")
        self.board.advance(700000)
        self.assertEqual(self.board.screen()[0].strip(), "CUSTOM PROGRAMS")
        self.assertEqual(len(self.board.programs()), 1)

    def test_api_video_cursor_and_teletype(self):
        result = self.run_api("""
            bios_lcd_clear
            mov dx, 0112h
            bios_set_cursor
            mov al, 'A'
            bios_putc
            mov al, 'B'
            bios_putc
            mov al, 13
            bios_putc
            mov al, 10
            bios_putc
            mov al, 'C'
            bios_putc
            mov al, 8
            bios_putc
            xor bx, bx
            mov ah, 3
            stc
            std
            int 10h
            mov [results], cx
            mov [results+2], dx
            pushf
            pop word [results+4]
            cld
        """)
        self.assertEqual(struct.unpack("<HH", result[:4]), (0x2000,0x300))
        self.assertEqual(struct.unpack("<H", result[4:6])[0] & 0x401, 0x401)
        rows = self.lcd_rows()
        self.assertEqual(rows[1][18:], b"AB")
        self.assertEqual(rows[3][0:1], b"C")
        self.assertEqual(bytes(self.board.cpu.mem_read(0x450, 2)), b"\0\x03")

    def test_api_scroll_window_and_line_truncation(self):
        self.run_api("""
            bios_lcd_clear
            bios_lcd_line 0, first
            bios_lcd_line 1, second
            bios_lcd_line 2, third
            bios_lcd_line 3, fourth
            mov ax, 0601h
            mov bh, 7
            mov cx, 0102h
            mov dx, 0305h
            int 10h
            mov ax, 0600h
            mov cx, 0000h
            mov dx, 0001h
            int 10h
        """, """
            first: db 'abcdefghijklmnopqrstEXTRA',0
            second: db '11111111111111111111',0
            third: db '22222222222222222222',0
            fourth: db '33333333333333333333',0
        """)
        self.assertEqual(self.lcd_rows(), [b"  cdefghijklmnopqrst",
                         b"11222211111111111111", b"22333322222222222222",
                         b"33    33333333333333"])
        # Short line replaces all old text, rather than leaving suffix bytes.
        self.board.advance(700000)
        self.board.transact(RESET)
        self.board.advance(700000)
        self.run_api("bios_lcd_line 0, text", 'text: db "Short",0')
        self.assertEqual(self.lcd_rows()[0], b"Short" + b" "*15)

    def test_api_automatic_scroll_and_glyphs(self):
        self.run_api("""
            bios_lcd_clear
            bios_lcd_glyph 0, glyph
            bios_lcd_cell 0, 2, 0
            bios_lcd_cell 0, 3, 7
            bios_lcd_line 1, second
            bios_lcd_line 2, third
            bios_lcd_line 3, fourth
            mov dx, 0313h
            bios_set_cursor
            mov al, 'X'
            bios_putc
        """, """
            glyph: db 04h,0eh,1fh,15h,1fh,04h,0ah,31h
            second: db 'Second',0
            third: db 'Third',0
            fourth: db 'Fourth',0
        """)
        self.assertEqual(bytes(self.board.cgram[:8]), bytes.fromhex("040e1f151f040a11"))
        self.assertEqual(self.lcd_rows()[0], b"Second" + b" "*14)
        self.assertEqual(self.lcd_rows()[1], b"Third" + b" "*15)
        self.assertEqual(self.lcd_rows()[2], b"Fourth" + b" "*13 + b"X")
        self.assertEqual(self.lcd_rows()[3], b" "*20)

    def test_api_keyboard_peek_get_and_zf(self):
        result = self.run_api("""
            mov ax, 0100h
            stc
            int 16h
            pushf
            pop word [results]
        wait_key:
            mov ah, 1
            int 16h
            jz wait_key
            mov [results+2], ax
            pushf
            pop word [results+4]
            mov ah, 1
            int 16h
            mov [results+6], ax
            xor ah, ah
            int 16h
            mov [results+8], ax
            mov ah, 1
            int 16h
            pushf
            pop word [results+10]
            mov ah, 2
            int 16h
            mov [results+12], al
        """, incoming=b"A", delayed=True)
        empty, peek, available, peek2, key, empty2 = struct.unpack("<6H", result[:12])
        self.assertTrue(empty & 0x40)
        self.assertEqual((peek,peek2,key), (0x1e41,)*3)
        self.assertFalse(available & 0x40)
        self.assertTrue(empty2 & 0x40)
        self.assertEqual(result[12], 0)
        self.assertEqual(bytes(self.board.cpu.mem_read(0x41a, 4)), b"\x20\0\x20\0")

    def test_api_uart_status_receive_timeout_and_transmit(self):
        result = self.run_api("""
            xor dx, dx
            mov ah, 3
            int 14h
            mov [results], ax
            mov ah, 2
            int 14h
            mov [results+2], ax
            mov ah, 2
            int 14h
            mov [results+4], ax
            mov ax, 015ah
            int 14h
            mov [results+6], ax
        """, incoming=b"Q")
        status, receive, timeout, send = struct.unpack("<4H", result[:8])
        self.assertEqual(status, 0x6100)
        self.assertEqual(receive, 0x6151)
        self.assertTrue(timeout & 0x8000)
        self.assertEqual(send, 0x605a)
        self.assertTrue(self.board.tx.endswith(b"Z"))

    def test_api_uart_initialize_and_transmit_timeout(self):
        result = self.run_api("""
            xor dx, dx
            mov ax, 00e3h
            int 14h
            mov [results], ax
            mov ax, 0003h
            int 14h
            mov [results+2], ax
            pushf
            pop word [results+4]
        """)
        self.assertEqual(struct.unpack("<HH", result[:4]), (0x6000,0x8603))
        self.assertTrue(struct.unpack("<H", result[4:6])[0] & 1)
        self.board.advance(700000)
        self.board = Board()
        result = self.run_api("""
            xor dx, dx
            mov ax, 015ah
            int 14h
            mov [results], ax
        """, tx_ready=False)
        self.assertEqual(struct.unpack("<H", result[:2])[0], 0x805a)
        self.assertFalse(self.board.tx.endswith(b"Z"))
        self.board.tx_ready = True

    def test_api_raw_cells_do_not_scroll_or_move_cursor(self):
        result = self.run_api("""
            bios_lcd_clear
            mov dx, 0102h
            bios_set_cursor
            bios_lcd_glyph 7, glyph
            bios_lcd_cell 0, 3, 0
            bios_lcd_cell 3, 19, 7
            mov ah, 3
            xor bh, bh
            int 10h
            mov [results], dx
        """, "glyph: times 8 db 31")
        self.assertEqual(struct.unpack("<H", result[:2])[0], 0x102)
        self.assertEqual(bytes(self.board.cgram[56:64]), bytes([31]*8))
        self.assertEqual(self.lcd_rows()[0][3], 0)
        self.assertEqual(self.lcd_rows()[3][19], 7)
        self.assertEqual(self.lcd_rows()[1], b" "*20)

    def test_api_unsupported_and_invalid_parameters(self):
        result = self.run_api("""
            mov ax, 7701h
            int 60h
            mov [results], ax
            pushf
            pop word [results+2]
            mov ax, 0200h
            xor bx, bx
            mov dx, 0400h
            int 10h
            mov [results+4], ax
            pushf
            pop word [results+6]
            mov ax, 03ffh
            int 60h
            mov [results+8], ax
            mov ax, 03e3h
            mov dx, 1
            int 14h
            mov [results+10], ax
            mov ax, 1234h
            int 61h
            mov [results+12], ax
            mov [results+14], sp
        """)
        self.assertEqual(struct.unpack("<8H", result[:16])[::2], (0x8601,0x8600,0x86ff,0x8634))
        self.assertTrue(struct.unpack("<H", result[2:4])[0] & 1)
        self.assertTrue(struct.unpack("<H", result[6:8])[0] & 1)
        self.assertEqual(struct.unpack("<H", result[10:12])[0], 0x86e3)
        self.assertEqual(struct.unpack("<H", result[14:16])[0], 0xfdfa)

    def test_menu_navigation_lcd_and_info(self):
        board = self.board
        screen = board.screen()
        self.assertEqual(screen[0].strip(), f"8088 BIOS {BIOS_VERSION}")
        self.assertEqual(screen[1][:-1].strip(), "> Default boot")
        self.assertEqual(screen[3][-1], "v")
        board.advance(20000)
        self.assert_lcd_matches_menu(screen)
        board.key(1)
        self.assertEqual(board.screen()[3][:-1].strip(), "> Info")
        self.assertEqual(board.screen()[1][-1], "^")
        board.key(3)
        screen = board.screen()
        self.assertEqual(screen[0].strip(), "INFO")
        self.assertEqual(screen[1][:-1].strip(), f"BIOS: {BIOS_VERSION}")
        self.assertEqual(screen[2][:-1].strip(), "BOARD: REV 1")
        self.assertEqual(screen[3][:-1].strip(), "CPU: 8088")
        self.assertIn(b"UART A0 9600", board.transact(INFO))
        self.assertTrue(board.transact(INFO).startswith(b"\0BIOS " + BIOS_VERSION.encode() + b";"))
        board.key(4)
        board.key(2)
        board.key(3)
        screen = board.screen()
        self.assertEqual(screen[0].strip(), "DEFAULT BOOT")
        self.assertIn("NO BOOT TARGET", screen[1])

    def test_menu_cursor_blinks_without_redrawing_or_blocking_uart(self):
        board = self.board
        logical = board.screen()
        board.lcd_operations = []
        seen = set()
        for _ in range(16):
            board.advance(100000)
            seen.add(self.lcd_rows()[1][0])
            self.assertEqual(board.transact(PING), b"\0")
            self.assertEqual(board.screen(), logical)
            self.assert_lcd_matches_menu(logical)
        self.assertEqual(seen, {ord('>'), ord(' ')})
        self.assertGreater(len(board.lcd_operations), 2)
        for port, value in board.lcd_operations:
            if port == 0xc0:
                self.assertEqual(value, 0xc0)  # selected row, column zero only
            else:
                self.assertIn(value, (ord('>'), ord(' ')))
        board.key(2)
        board.advance(30000)
        self.assertEqual(self.lcd_rows()[1][0], ord(' '))
        self.assertEqual(self.lcd_rows()[2][0], ord('>'))  # visible after move
        board.key(3)  # Custom programs, four empty slots
        board.advance(30000)
        seen = set()
        for _ in range(16):
            board.advance(100000)
            seen.add(self.lcd_rows()[1][0])
            self.assert_lcd_matches_menu(board.screen())
        self.assertEqual(seen, {ord('>'), ord(' ')})
        board.key(2)
        board.key(2)
        board.key(2)  # fourth slot scrolls onto the last row
        board.advance(30000)
        seen = set()
        for _ in range(16):
            board.advance(100000)
            seen.add(self.lcd_rows()[3][0])
            self.assert_lcd_matches_menu(board.screen())
        self.assertEqual(seen, {ord('>'), ord(' ')})
        board.key(4)  # root, selected Custom
        board.key(2)
        board.key(2)
        board.key(3)  # Info is not selectable and must not blink
        board.advance(30000)
        info = self.lcd_rows()
        board.lcd_operations = []
        board.advance(1000000)
        self.assertEqual(self.lcd_rows(), info)
        self.assertEqual(board.lcd_operations, [])

    def test_info_scroll_bounds_headers_and_lcd(self):
        board = self.board
        board.key(1)
        board.key(3)
        rows = [f"BIOS: {BIOS_VERSION}", "BOARD: REV 1", "CPU: 8088",
                "RAM: 32 KiB", "ROM: 32 KiB", "LCD: 20x4", "UART: 9600 8N1",
                "PROTOCOL: 1", "API: 1", "PROGRAMS: 0/4", "ROM AT: F800:0000",
                "UPLOAD: 8800-FBFF", "UART I/O: A0-A7", "LCD I/O: C0-C1"]
        initial = board.screen()
        board.key(1)
        self.assertEqual(board.screen(), initial)
        board.key(3)  # Info has no selectable actions.
        self.assertEqual(board.screen(), initial)
        for top in range(len(rows) - 2):
            screen = board.screen()
            self.assertEqual(screen[0].strip(), "INFO")
            self.assertEqual([row[:-1].strip() for row in screen[1:]], rows[top:top+3])
            self.assertEqual(screen[1][-1], '^' if top else '|')
            self.assertEqual(screen[2][-1], '|')
            self.assertEqual(screen[3][-1], 'v' if top < len(rows)-3 else '|')
            self.assertFalse(any('>' in row for row in screen))
            board.advance(20000)
            self.assertEqual([row.decode() for row in self.lcd_rows()], screen)
            board.key(2)
        last = board.screen()
        board.key(2)
        self.assertEqual(board.screen(), last)
        for _ in range(len(rows)-3):
            board.key(1)
        self.assertEqual(board.screen(), initial)
        board.key(4)
        self.assertIn("> Info", board.screen()[3])
        board.key(3)
        self.assertEqual(board.screen(), initial)

    def test_info_program_count_and_shared_keyboard_scroll(self):
        board = self.board
        board.upload("First", b"\xcb")
        board.upload("Second", b"\xcb")
        board.key(4)  # Custom -> root, selected Custom.
        board.transact(KEYBOARD_MODE, b"\1")
        for _ in range(2):
            board.transact(KEYBOARD_EVENT, b"\0\x50")
        board.transact(KEYBOARD_EVENT, b"\r\x1c")
        self.assertEqual(board.screen()[0].strip(), "INFO")
        for _ in range(7):
            board.transact(KEYBOARD_EVENT, b"\0\x50")
        self.assertEqual(board.screen()[3][:-1].strip(), "PROGRAMS: 2/4")
        board.transact(KEYBOARD_EVENT, b"\0\x48")
        self.assertEqual(board.screen()[1][:-1].strip(), "UART: 9600 8N1")
        board.transact(KEYBOARD_EVENT, b"\x1b\1")
        self.assertIn("> Info", board.screen()[3])

    def test_build_version_changes_without_clean(self):
        with tempfile.TemporaryDirectory() as directory:
            image = Path(directory) / "bios.bin"
            for version in ("dev", "ci-test", "dev"):
                subprocess.run(["make", "-C", str(BIOS.parent.parent),
                                f"BUILD_DIR={directory}", f"BIOS_VERSION={version}",
                                "bios"], check=True, capture_output=True)
                data = image.read_bytes()
                self.assertEqual(len(data), 32768)
                self.assertIn(f"8088 BIOS {version}\0".encode(), data)
                self.assertIn(f"BIOS {version}; rev1;".encode(), data)
            result = subprocess.run(["make", "-C", str(BIOS.parent.parent),
                                     f"BUILD_DIR={directory}", "BIOS_VERSION=too-long-version",
                                     "bios"], capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            # NASM may remove its failed output; the main tested ROM is untouched.
            self.assertIn(b"BIOS_VERSION must be 1..9", result.stderr)

    def test_keyboard_menu_and_duplicate_event(self):
        board = self.board
        self.assertEqual(board.transact(KEYBOARD_EVENT, b"A\0"), b"\x0c")
        self.assertEqual(board.transact(KEYBOARD_MODE, b"\x02"), b"\x0c")
        self.assertEqual(board.transact(KEYBOARD_MODE, b"\1"), b"\0\0")
        self.assertEqual(board.transact(KEYBOARD_EVENT, b"\0\x50", sequence=42), b"\0\0")
        self.assertEqual(board.transact(KEYBOARD_EVENT, b"\0\x50", sequence=42), b"\0\0")
        self.assertIn("> Custom programs", board.screen()[2])
        self.assertEqual(board.transact(KEYBOARD_EVENT, b"\r\x1c"), b"\0\0")
        self.assertEqual(board.screen()[0].strip(), "CUSTOM PROGRAMS")
        self.assertEqual(board.transact(KEYBOARD_EVENT, b"Q\0"), b"\0\0")
        self.assertEqual(board.transact(KEYBOARD_EVENT, b"U\0"), b"\0\0")
        self.assertEqual(board.screen()[0].strip(), "CUSTOM PROGRAMS")
        self.assertEqual(board.transact(KEYBOARD_EVENT, b"\x1b\1"), b"\0\0")
        self.assertEqual(board.screen()[0].strip(), f"8088 BIOS {BIOS_VERSION}")
        for data in (b"", b"\0\0", b"\x80\0", b"A"):
            self.assertEqual(board.transact(KEYBOARD_EVENT, data), b"\x0c")
        self.assertEqual(board.transact(KEYBOARD_EVENT, b"A\0", address=1), b"\x0c")
        self.assertEqual(board.transact(KEYBOARD_MODE, b"\0"), b"\0\0")
        self.assertEqual(board.transact(KEYBOARD_EVENT, b"A\0"), b"\x0c")

    def test_keyboard_attach_after_boot_with_dirty_sram(self):
        # Physical SRAM is not zero-filled at power-on. In particular, an
        # uninitialized API_RUNNING would produce the observed 00 FF ACK.
        for fill in (0xff, 0x55, 0xaa):
            with self.subTest(fill=fill):
                board = Board(bytes([fill]) * 0x8000)
                self.assertEqual(board.transact(KEYBOARD_MODE, b"\1"), b"\0\0")
                self.assertEqual(board.transact(KEYBOARD_EVENT, b"\0\x50"), b"\0\0")
                self.assertIn("> Custom programs", board.screen()[2])
                self.assertEqual(board.transact(KEYBOARD_MODE, b"\0"), b"\0\0")
                self.assertEqual(board.transact(KEYBOARD_MODE, b"\1"), b"\0\0")

    def test_host_keyboard_int16_and_raw_uart_exclusion(self):
        self.assertEqual(self.board.transact(KEYBOARD_MODE, b"\1"), b"\0\0")
        result = self.run_api("""
            mov bp, 1234h
            std
            xor dx, dx
            mov ah, 2
            int 14h
            mov [results], ax
            pushf
            pop word [results+2]
            mov ax, 01aah
            int 16h
            mov [results+4], ax
            pushf
            pop word [results+6]
        host_wait:
            mov ah, 1
            int 16h
            jz host_wait
            mov [results+8], ax
            xor ah, ah
            int 16h
            mov [results+10], ax
            xor ah, ah
            int 16h
            mov [results+12], ax
            mov [results+14], bp
            pushf
            pop word [results+16]
            cld
        """, incoming=encode(KEYBOARD_EVENT, 40, data=b"\0\x48") +
                       encode(KEYBOARD_EVENT, 41, data=b"A\0"), delayed=True)
        serial, serial_flags, empty_ax, empty_flags, peek, key, letter, bp, flags = struct.unpack("<9H", result[:18])
        self.assertEqual(serial >> 8, 0x86)
        self.assertTrue(serial_flags & 1)
        self.assertEqual(empty_ax, 0x01aa)
        self.assertTrue(empty_flags & 0x40)
        self.assertEqual((peek, key, letter, bp), (0x4800, 0x4800, 0x1e41, 0x1234))
        self.assertTrue(flags & 0x400)
        self.board.advance(700000)
        self.assertEqual(self.board.transact(KEYBOARD_MODE, b"\1"), b"\0\0")

    def test_keyboard_queue_wrap_and_program_event_retry(self):
        self.board.transact(KEYBOARD_MODE, b"\1")
        incoming = encode(KEYBOARD_EVENT, 40, data=b"a\0") * 2
        incoming += b"".join(encode(KEYBOARD_EVENT, 40 + i, data=bytes([ord('a') + i, 0]))
                             for i in range(1, 20))
        result = self.run_api("""
            mov di, results
            mov cx, 20
        next_key:
            xor ah, ah
            int 16h
            mov [di], ax
            add di, 2
            loop next_key
        """, incoming=incoming, delayed=True)
        keys = struct.unpack("<20H", result[:40])
        self.assertEqual([key & 255 for key in keys], list(range(ord('a'), ord('a') + 20)))
        self.assertTrue(all(key >> 8 for key in keys))
        self.assertEqual(bytes(self.board.cpu.mem_read(0x41a, 4)), b"\x26\0\x26\0")

    def test_keyboard_queue_full_busy_and_mode_detach(self):
        board = self.board
        board.transact(KEYBOARD_MODE, b"\1")
        payload = assemble("host_wait: mov ah,1\nint 16h\njmp host_wait\n")
        ident, _ = board.upload("Polling", payload)
        board.transact(PROGRAM_EXEC, address=ident)
        board.advance(700000)
        self.assertEqual(board.transact(KEYBOARD_MODE, b"\1"), b"\0\1")
        for _ in range(15):
            self.assertEqual(board.transact(KEYBOARD_EVENT, b"A\0"), b"\0\1")
        self.assertEqual(board.transact(KEYBOARD_EVENT, b"B\0"), b"\x0d")
        self.assertEqual(board.transact(INFO), b"\x0b")
        self.assertEqual(board.transact(BIOS_READ, struct.pack("<IH", 0, 1)), b"\x0b")
        self.assertEqual(board.transact(BIOS_FLASHER_MODE), b"\x0b")
        self.assertEqual(board.transact(BIOS_WRITE), b"\x11")
        self.assertEqual(board.transact(MEMORY_READ, struct.pack("<IH", PAYLOAD_BASE, 1)), b"\x0b")
        self.assertEqual(board.transact(MEMORY_WRITE, struct.pack("<I", PAYLOAD_BASE) + b"x"), b"\x0b")
        self.assertEqual(board.transact(PROGRAM_DELETE, b"\1"), b"\x0b")
        self.assertEqual(board.transact(PROGRAM_RENAME, b"\1Other"), b"\x0b")
        self.assertEqual(board.begin("Other", b"\xcb"), b"\x0b")
        self.assertEqual(board.transact(KEYBOARD_MODE, b"\1"), b"\0\1")
        self.assertEqual(board.transact(KEYBOARD_EVENT, b"B\0"), b"\x0d")
        self.assertEqual(board.transact(KEYBOARD_MODE, b"\0"), b"\0\1")
        self.assertEqual(bytes(board.cpu.mem_read(0x41a, 4)), b"\x1e\0\x1e\0")

    def test_keyboard_launch_and_return_preserves_mode(self):
        board = self.board
        board.upload("Terminal", (BIOS.parent / "terminal.bin").read_bytes())
        board.transact(KEYBOARD_MODE, b"\1")
        board.transact(KEYBOARD_EVENT, b"\r\x1c")  # open selected program details
        self.assertEqual(board.transact(KEYBOARD_EVENT, b"\r\x1c", sequence=99), b"\0\1")
        board.advance(4000000)
        self.assertEqual(self.lcd_rows()[0].rstrip(), b"UART TERMINAL")
        self.assertEqual(self.lcd_rows()[1], b"TYPE ON PC  ESC:BACK")
        self.assertEqual(board.transact(KEYBOARD_EVENT, b"\r\x1c", sequence=99), b"\0\1")
        for char in b"QUa":
            self.assertEqual(board.transact(KEYBOARD_EVENT, bytes([char, 0])), b"\0\1")
            board.advance(700000)
        board.advance(700000)
        self.assertEqual(self.lcd_rows()[2][:3], b"QUa")
        self.assertEqual(board.transact(KEYBOARD_EVENT, b"\x1b\1"), b"\0\1")
        board.advance(700000)
        self.assertEqual(board.screen()[0].strip(), "CUSTOM PROGRAMS")
        self.assertEqual(board.transact(KEYBOARD_MODE, b"\1"), b"\0\0")
        self.assertEqual(len(board.programs()), 1)

    def test_city_animation_twinkles_and_wipes_day_and_night_until_escape(self):
        board = self.board
        board.upload("City Lights", (BIOS.parent / "citylights.bin").read_bytes())
        board.transact(KEYBOARD_MODE, b"\1")
        board.transact(PROGRAM_EXEC, address=1)
        # Allow the LCD clear, CGRAM loading and skyline to finish.
        board.advance(2000000)
        star_patterns = set()
        cloud_shapes = set()
        cloud_positions = set()
        walker_positions = set()
        saw_houses = False
        saw_night = saw_day = saw_dawn = saw_dusk = False
        for _ in range(160):
            board.advance(125000)
            rows = self.lcd_rows()
            walker_positions.update(i for i in (4, 5) if rows[3][i] == 7)
            saw_houses |= rows[3][11:13] == b"\5\5" and rows[3][18:20] == b"\5\5"
            moon = rows[0][17] == 0
            sun = rows[0][17] == 1
            stars = 2 in rows[0] or 2 in rows[1]
            clouds = 3 in rows[0] or 3 in rows[1]
            if moon and stars and not clouds:
                saw_night = True
                pattern = (rows[0][3], rows[0][11], rows[1][6], rows[1][15])
                # An LCD read may catch a frame while its cells are updating.
                if pattern.count(2) == 2 and pattern.count(ord('.')) == 2:
                    star_patterns.add(pattern)
            if sun and clouds and not stars:
                saw_day = True
                cloud_shapes.add(bytes(board.cgram[24:32]))
                cloud_positions.add(tuple(i for i, cell in enumerate(rows[1]) if cell == 3))
            saw_dawn |= sun and stars
            saw_dusk |= moon and clouds
            if (saw_night and saw_day and saw_dawn and saw_dusk
                    and len(star_patterns) >= 2 and len(cloud_shapes) >= 2
                    and len(cloud_positions) >= 2 and walker_positions == {4, 5}
                    and saw_houses):
                break
        else:
            self.fail("city scenes did not twinkle, drift, walk and wipe in both directions")
        self.assertTrue(set(rows[0]) <= set(range(8)) | {ord(" "), ord(".")})
        for column in (1, 2, 7, 8, 9, 14, 15, 16):
            self.assertEqual(rows[2][column], 4)  # Fixed roofs
        self.assertTrue(set(rows[3]) <= {ord(" "), 5, 6, 7, 0xff} | set(b"ESC:BACK"))
        board.advance(8000000)
        self.assertEqual(self.lcd_rows()[2][1:3], b"\4\4")
        self.assertEqual(board.transact(KEYBOARD_EVENT, b"\x1b\1"), b"\0\1")
        board.advance(700000)
        self.assertEqual(board.screen()[0].strip(), "CUSTOM PROGRAMS")
        self.assertEqual(len(board.programs()), 1)

    def test_city_intro_and_periodic_escape_hint(self):
        board = self.board
        board.upload("City Lights", (BIOS.parent / "citylights.bin").read_bytes())
        board.transact(KEYBOARD_MODE, b"\1")
        board.transact(PROGRAM_EXEC, address=1)

        for _ in range(40):
            rows = self.lcd_rows()
            if b"CITY LIGHTS" in rows[1] and rows[2][1:3] == b"\4\4":
                break
            board.advance(50000)
        else:
            self.fail("city title did not appear")
        self.assertEqual(rows[2][1:3], b"\4\4")

        for _ in range(80):
            board.advance(50000)
            if self.lcd_rows()[3][12:] == b"ESC:BACK":
                break
        else:
            self.fail("initial Esc hint did not appear")

        for _ in range(200):
            board.advance(50000)
            hint_area = self.lcd_rows()[3][12:]
            if all(ch in (5, 6, 7, 0xff, ord(" ")) for ch in hint_area):
                break
        else:
            self.fail("Esc hint did not hide")

        for _ in range(300):
            board.advance(50000)
            if self.lcd_rows()[3][12:] == b"ESC:BACK":
                break
        else:
            self.fail("Esc hint did not return")

        self.assertEqual(board.transact(KEYBOARD_EVENT, b"\x1b\1"), b"\0\1")
        board.advance(700000)
        self.assertEqual(board.screen()[0].strip(), "CUSTOM PROGRAMS")

    def test_keyboard_polling_required_no_irq(self):
        board = self.board
        board.transact(KEYBOARD_MODE, b"\1")
        ident, _ = board.upload("No polling", b"\xeb\xfe")
        board.transact(PROGRAM_EXEC, address=ident)
        board.advance(700000)
        board.tx.clear()
        packet = encode(KEYBOARD_EVENT, 40, data=b"A\0")
        board.rx.extend(packet)
        board.advance(700000)
        self.assertEqual(board.tx, b"")
        self.assertEqual(bytes(board.rx), packet)

    def test_duplicate_key(self):
        board = self.board
        first = board.key(2, sequence=42)
        second = board.key(2, sequence=42)
        self.assertEqual(first, second)
        self.assertEqual(board.screen()[2][:-1].strip(), "> Custom programs")

    def test_unknown_command_codes(self):
        board = self.board
        initial = board.screen()
        for command in (0x00, 0x13, 0x7f):
            self.assertEqual(board.transact(command), b"\x01")
        self.assertEqual(board.screen(), initial)

    def test_program_delete_retry_reuses_hole_without_moving_residents(self):
        board = self.board
        images = [b"\xcb" + bytes([value]) * 16 for value in (65, 66, 67)]
        addresses = [board.upload(name, image)[1] for name, image in zip(("One", "Two", "Three"), images)]
        self.assertEqual(addresses, [0x8800, 0x8820, 0x8840])
        self.assertEqual(board.transact(PROGRAM_DELETE, b"\2", sequence=42), b"\0")
        self.assertEqual(board.transact(PROGRAM_DELETE, b"\2", sequence=42), b"\0")
        records = board.programs()
        self.assertEqual([(r[0], r[1], r[3].rstrip(b"\0")) for r in records], [(1, addresses[0], b"One"), (2, addresses[2], b"Three")])
        self.assertEqual(board.screen()[0].strip(), "CUSTOM PROGRAMS")
        for address, image in zip((addresses[0], addresses[2]), (images[0], images[2])):
            self.assertEqual(bytes(board.cpu.mem_read(address, len(image))), image)
        self.assertEqual(board.upload("Reused", images[1]), (3, addresses[1]))
        self.assertEqual([r[1] for r in board.programs()], [addresses[0], addresses[2], addresses[1]])
        # New intentional deletion after PING must not hit the old retry cache.
        board.transact(PING)
        self.assertEqual(board.transact(PROGRAM_DELETE, b"\2", sequence=42), b"\0")
        self.assertEqual([r[3].rstrip(b"\0") for r in board.programs()], [b"One", b"Reused"])

    def test_program_delete_empty_registry_and_reuse_first_slot(self):
        board = self.board
        board.upload("Only", b"\xcb")
        self.assertEqual(board.transact(PROGRAM_DELETE, b"\1", sequence=254), b"\0")
        self.assertEqual(board.transact(PROGRAM_DELETE, b"\1", sequence=254), b"\0")
        self.assertEqual(board.programs(), [])
        self.assertEqual(board.screen()[0].strip(), "CUSTOM PROGRAMS")
        self.assertTrue(all("Empty" in row for row in board.screen()[1:]))
        board.key(3)  # Empty cannot launch or index a nonexistent record.
        self.assertEqual(board.screen()[0].strip(), "CUSTOM PROGRAMS")
        self.assertEqual(board.upload("Again", b"\xcb"), (1, PAYLOAD_BASE))

    def test_program_allocator_coalesces_deleted_ranges_and_checks_fit(self):
        board = self.board
        board.upload("First", b"\xcb" + b"A" * 15)
        board.upload("Second", b"\xcb" + b"B" * 15)
        tail = b"\xcb" + b"C" * (PAYLOAD_SIZE - 33)
        _, address = board.upload("Tail", tail)
        self.assertEqual(address, 0x8820)
        self.assertEqual(board.transact(PROGRAM_DELETE, b"\1", sequence=40), b"\0")
        self.assertEqual(board.transact(PROGRAM_DELETE, b"\1", sequence=41), b"\0")
        self.assertEqual(board.begin("Too large", b"x" * 33), b"\x07")
        self.assertEqual(board.upload("Fits", b"\xcb" + b"D" * 31), (2, PAYLOAD_BASE))
        self.assertEqual(bytes(board.cpu.mem_read(address, len(tail))), tail)
        self.assertEqual([r[1] for r in board.programs()], [address, PAYLOAD_BASE])

    def test_program_rename_preserves_bytes_and_updates_lcd(self):
        board = self.board
        image = b"\xcb" + b"contents" * 3
        board.upload("Old", image)
        board.upload("Other", b"\xcb")
        board.key(1)  # Select Old in Custom programs.
        board.key(3)
        self.assertEqual(board.screen()[0].strip(), "Old")
        before = board.programs()
        name = b"123456789abcdef"
        self.assertEqual(board.transact(PROGRAM_RENAME, b"\1" + name), b"\0")
        self.assertEqual(board.transact(PROGRAM_RENAME, b"\1" + name), b"\0")
        self.assertEqual(board.screen()[0].strip(), name.decode())
        self.assertEqual(board.transact(PROGRAM_RENAME, b"\1X"), b"\0")
        after = board.programs()
        self.assertEqual(after[0][:3], before[0][:3])
        self.assertEqual(after[0][3], b"X" + bytes(15))
        self.assertEqual(after[1], before[1])
        self.assertEqual(bytes(board.cpu.mem_read(PAYLOAD_BASE, len(image))), image)
        self.assertEqual(board.screen()[0].strip(), "X")

    def test_program_registry_invalid_requests_and_busy_modes(self):
        board = self.board
        board.upload("One", b"\xcb")
        board.upload("Two", b"\xcb")
        before = board.programs()
        for request in (b"", b"\0", b"\3", b"\xff", b"\1\0"):
            self.assertEqual(board.transact(PROGRAM_DELETE, request), b"\x10")
        for request in (b"", b"\1", b"\0Name", b"\3Name", b"\1   ", b"\1\0", b"\1\xff", b"\1" + b"x" * 16):
            self.assertEqual(board.transact(PROGRAM_RENAME, request), b"\x10")
        self.assertEqual(board.transact(PROGRAM_RENAME, b"\1Two"), b"\x08")
        self.assertEqual(board.programs(), before)
        board.begin("Pending", b"\xcb")
        for command, data in ((PROGRAM_DELETE, b"\1"), (PROGRAM_RENAME, b"\1Name")):
            self.assertEqual(board.transact(command, data), b"\x0f")
        self.assertEqual(board.programs(), before)
        board.transact(PROGRAM_UPLOAD_ABORT, b"\3")
        board.start_memory_test()
        for command, data in ((PROGRAM_DELETE, b"\1"), (PROGRAM_RENAME, b"\1Name")):
            self.assertEqual(board.transact(command, data), b"\x0a")

    def test_memory_write_payload_aliases_retry_and_readback(self):
        board = self.board
        ivt_bda = bytes(board.cpu.mem_read(0, 0x500))
        rom = bytes(board.cpu.mem_read(0xf8000, 0x8000))
        for alias in (0x1000, 0x9000, 0x11000, 0x19000):
            image = bytes(range(124))
            request = struct.pack("<I", alias) + image
            self.assertEqual(board.transact(MEMORY_WRITE, request, sequence=42), b"\0")
            self.assertEqual(board.transact(MEMORY_WRITE, request, sequence=42), b"\0")
            self.assertEqual(board.transact(MEMORY_READ, struct.pack("<IH", alias, len(image))), b"\0" + image)
            self.assertEqual(bytes(board.cpu.mem_read(0x9000, len(image))), image)
        for alias in (0x800, 0x8800, 0x10800, 0x18800, 0x7bff, 0xfbff, 0x17bff, 0x1fbff):
            self.assertEqual(board.transact(MEMORY_WRITE, struct.pack("<I", alias) + b"Z"), b"\0")
            self.assertEqual(board.transact(MEMORY_READ, struct.pack("<IH", alias, 1)), b"\0Z")
        self.assertEqual(bytes(board.cpu.mem_read(0, 0x500)), ivt_bda)
        self.assertEqual(bytes(board.cpu.mem_read(0xf8000, 0x8000)), rom)
        self.assertEqual(board.programs(), [])  # raw writes never publish a slot

    def test_memory_write_rejects_rom_unmapped_and_reserved_aliases(self):
        board = self.board
        image = b"\xcb" + b"guard" * 25
        board.upload("Keep", image)
        residents = board.programs()
        ivt_bda = bytes(board.cpu.mem_read(0, 0x500))
        rom = bytes(board.cpu.mem_read(0xf8000, 0x8000))
        for alias in (0, 0x8000, 0x10000, 0x18000):
            for offset, data in ((0, b"x"), (0x400, b"x"), (0x500, b"x"),
                                 (0x700, b"x"), (0x7ff, b"xy"),
                                 (0x7bff, b"xy"), (0x7c00, b"x"), (0x7fff, b"xy")):
                self.assertEqual(board.transact(MEMORY_WRITE, struct.pack("<I", alias + offset) + data), b"\x0e")
        for address in (0x20000, 0xf8000, 0xfffff, 0x100000, 0xffffffff):
            self.assertEqual(board.transact(MEMORY_WRITE, struct.pack("<I", address) + b"xy"), b"\x0e")
        for request in (b"", bytes(3), struct.pack("<I", 0x9000)):
            self.assertEqual(board.transact(MEMORY_WRITE, request), b"\x0e")
        # A boundary-crossing block must not even write its valid first byte.
        self.assertEqual(bytes(board.cpu.mem_read(0x7bff, 1)), b"\0")
        self.assertEqual(bytes(board.cpu.mem_read(PAYLOAD_BASE, len(image))), image)
        self.assertEqual(bytes(board.cpu.mem_read(0, 0x500)), ivt_bda)
        self.assertEqual(bytes(board.cpu.mem_read(0xf8000, 0x8000)), rom)
        self.assertEqual(board.programs(), residents)

    def test_memory_write_is_busy_during_upload_and_diagnostic(self):
        board = self.board
        request = struct.pack("<I", 0x9000) + b"x"
        board.begin("Pending", b"\xcb")
        self.assertEqual(board.transact(MEMORY_WRITE, request), b"\x0f")
        self.assertEqual(board.transact(PROGRAM_UPLOAD_ABORT, b"\1"), b"\0")
        board.start_memory_test()
        self.assertEqual(board.transact(MEMORY_WRITE, request), b"\x0a")
        board.key(4)
        self.assertEqual(board.transact(MEMORY_WRITE, request), b"\0")

    def test_memory_read_ram_aliases_and_max_reply(self):
        board = self.board
        image = bytes(range(256)) + b"tail"
        ident, address = board.upload("Readable", image)
        residents = board.programs()
        for alias in (0x1000, 0x9000, 0x11000, 0x19000):
            board.cpu.mem_write(0x9000, bytes(range(127)))
            request = struct.pack("<IH", alias, 127)
            reply = board.transact(MEMORY_READ, request, sequence=99)
            self.assertEqual(reply, b"\0" + bytes(range(127)))
            self.assertEqual(board.transact(MEMORY_READ, request, sequence=99), reply)
        self.assertEqual(board.transact(MEMORY_READ, struct.pack("<IH", address, 127)), b"\0" + image[:127])
        ivt = bytes(board.cpu.mem_read(0, 127))
        self.assertEqual(board.transact(MEMORY_READ, struct.pack("<IH", 0, 127)), b"\0" + ivt)
        # Last SRAM byte is readable; it is live BIOS stack, not static data.
        self.assertEqual(len(board.transact(MEMORY_READ, struct.pack("<IH", 0x1ffff, 1))), 2)
        self.assertEqual(board.programs(), residents)
        self.assertEqual(bytes(board.cpu.mem_read(PAYLOAD_BASE, len(image))), image)

    def test_memory_read_packet_buffer_overlap_and_df_restore(self):
        board = self.board
        for address in (0x500, 0x8500, 0x10500, 0x18500):
            request = struct.pack("<IH", address, 127)
            # The UART has just put this COBS packet into RX. Reading RX into
            # its own reply needs backwards copying to avoid repeating bytes.
            encoded = encode(MEMORY_READ, 42, request)[1:-1]
            reply = board.transact(MEMORY_READ, request, sequence=42)
            self.assertEqual(reply[0], 0)
            self.assertEqual(reply[1:1 + len(encoded)], encoded)
            self.assertFalse(board.cpu.reg_read(UC_X86_REG_EFLAGS) & 0x400)
        self.assertEqual(board.transact(PING), b"\0")

    def test_memory_read_rejects_invalid_ranges_and_lengths(self):
        board = self.board
        residents = board.programs()
        for payload in (b"", bytes(5), bytes(7),
                        struct.pack("<IH", PAYLOAD_BASE, 0),
                        struct.pack("<IH", PAYLOAD_BASE, 128),
                        struct.pack("<IH", PAYLOAD_BASE, 65535)):
            self.assertEqual(board.transact(MEMORY_READ, payload), b"\x0e")
        for address, size in ((0x20000, 1), (0x1ffff, 2), (0xf7fff, 2),
                              (0xf8000, 127), (0xffff0, 16), (0xfffff, 1),
                              (0xfffff, 2), (0x100000, 1), (0xffffffff, 127),
                              (0xfffffff0, 127)):
            self.assertEqual(board.transact(MEMORY_READ, struct.pack("<IH", address, size)), b"\x0e")
        self.assertEqual(board.programs(), residents)
        self.assertEqual(board.transact(PING), b"\0")

    def test_memory_read_is_busy_during_diagnostic(self):
        board = self.board
        board.start_memory_test()
        self.assertEqual(board.transact(MEMORY_READ, struct.pack("<IH", PAYLOAD_BASE, 1)), b"\x0a")
        board.key(4)
        self.assertEqual(board.transact(MEMORY_READ, struct.pack("<IH", PAYLOAD_BASE, 1))[0], 0)

    def test_command_specific_payloads_and_lengths(self):
        board = self.board
        image = bytes(range(128))
        board.begin("Payload", image)
        # Empty and short non-final blocks cannot advance the BIOS cursor.
        for payload in (b"", b"x", image[:127],
                        struct.pack("<I", PAYLOAD_BASE) + b"x"):
            self.assertEqual(board.transact(PROGRAM_UPLOAD_DATA, payload), b"\x03")
        self.assertEqual(board.transact(PROGRAM_UPLOAD_DATA, image), b"\0")
        self.assertEqual(bytes(board.cpu.mem_read(PAYLOAD_BASE, 128)), image)
        for command, status in ((PROGRAM_UPLOAD_COMMIT, 2),
                                (PROGRAM_UPLOAD_ABORT, 2), (PROGRAM_EXEC, 4)):
            for payload in (b"", b"\0", b"\xff", b"\1\0", struct.pack("<I", 1)):
                self.assertEqual(board.transact(command, payload), bytes([status]))
        self.assertEqual(board.transact(PROGRAM_UPLOAD_COMMIT, b"\1"), b"\0")
        self.assertEqual(len(board.programs()), 1)

    def test_old_header_and_oversized_payload_are_ignored(self):
        board = self.board
        # Former address=0 header becomes four unexpected payload bytes.
        # Its CRC is valid, but its declared length still says zero.
        # Rebuild the raw legacy header independently and COBS-encode it.
        raw = struct.pack("<BBBIH", 1, PING, 100, 0, 0)
        raw += struct.pack("<H", binascii.crc_hqx(raw, 0xffff))
        for packet in (cobs_packet(raw), encode(PING, 101, data=b"x" * 129)):
            board.tx.clear()
            board.rx.extend(packet)
            board.advance(100000)
            self.assertEqual(board.tx, b"")
            self.assertEqual(board.transact(PING), b"\0")

    def test_upload_duplicate_and_stack_protection(self):
        board = self.board
        board.cpu.mem_write(0xfc00, b"STACK GUARD")
        image = b"A" * 128 + b"CD"
        self.assertEqual(board.begin("Letters", image), b"\0\1\0\x88\0\0")
        self.assertEqual(board.screen()[0].strip(), "Letters")
        self.assertEqual(board.screen()[2], "[" + " " * 18 + "]")
        self.assertEqual(board.transact(PROGRAM_UPLOAD_DATA, image[:128], sequence=50), b"\0")
        self.assertEqual(board.screen()[2], "[" + "#" * 17 + " " + "]")
        self.assertEqual(board.transact(PROGRAM_UPLOAD_DATA, image[:128], sequence=50), b"\0")
        self.assertEqual(board.transact(PROGRAM_UPLOAD_DATA, b"B" * 128, sequence=50), b"\x03")
        self.assertEqual(board.transact(PROGRAM_UPLOAD_DATA, b"C", sequence=51), b"\x03")
        self.assertEqual(board.transact(PROGRAM_UPLOAD_DATA, b"CD", sequence=51), b"\0")
        self.assertEqual(board.screen()[2], "[" + "#" * 18 + "]")
        self.assertEqual(board.transact(PROGRAM_UPLOAD_DATA, b"CD", sequence=51), b"\0")
        self.assertEqual(board.transact(PROGRAM_UPLOAD_DATA, b"CD", sequence=52), b"\x03")
        self.assertEqual(bytes(board.cpu.mem_read(PAYLOAD_BASE, len(image))), image)
        self.assertIn("RECEIVING", board.screen()[3])
        self.assertEqual(board.programs(), [])
        self.assertEqual(board.transact(PROGRAM_UPLOAD_COMMIT, address=1), b"\0")
        self.assertEqual(board.transact(PROGRAM_UPLOAD_COMMIT, address=1), b"\0")
        board.key(3)
        self.assertIn("ENTER:RUN", board.screen()[3])
        self.assertNotEqual(board.transact(PROGRAM_UPLOAD_DATA, b"XX"), b"\0")
        self.assertEqual(bytes(board.cpu.mem_read(0xfc00, 11)), b"STACK GUARD")
        self.assertNotEqual(board.begin("Big", b"a" * (PAYLOAD_SIZE + 1)), b"\0")

    def test_upload_sequence_wrap_and_new_begin_retry_reset(self):
        board = self.board
        block = b"x" * 128
        image = block * 3
        board.begin("Wrap", image)
        for sequence in (254, 255, 0):
            self.assertEqual(board.transact(PROGRAM_UPLOAD_DATA, block, sequence=sequence), b"\0")
            self.assertEqual(board.transact(PROGRAM_UPLOAD_DATA, block, sequence=sequence), b"\0")
        self.assertEqual(bytes(board.cpu.mem_read(PAYLOAD_BASE, len(image))), image)
        self.assertEqual(board.transact(PROGRAM_UPLOAD_COMMIT, b"\1"), b"\0")
        board.begin("Fresh", block)
        # The previous upload's final sequence may be reused after BEGIN.
        self.assertEqual(board.transact(PROGRAM_UPLOAD_DATA, block, sequence=0), b"\0")
        self.assertEqual(board.transact(PROGRAM_UPLOAD_COMMIT, b"\2"), b"\0")
        self.assertEqual(bytes(board.cpu.mem_read(PAYLOAD_BASE + len(image), 128)), block)
        self.assertEqual(len(board.programs()), 2)

    def test_upload_retry_rejects_crc_collision(self):
        board = self.board
        block = bytes(128)
        checksum = binascii.crc_hqx(block, 0xffff)
        # Forge different payload bytes with the same CRC. With identical
        # headers this also collides in the full frame CRC retry cache.
        prefix = b"\1" + bytes(125)
        for tail in range(65536):
            changed = prefix + struct.pack("<H", tail)
            if binascii.crc_hqx(changed, 0xffff) == checksum:
                break
        else:
            self.fail("could not construct CRC collision")
        board.begin("Collision", block)
        self.assertEqual(board.transact(PROGRAM_UPLOAD_DATA, block, sequence=42), b"\0")
        self.assertEqual(board.transact(PROGRAM_UPLOAD_DATA, changed, sequence=42), b"\x03")
        self.assertEqual(board.transact(PROGRAM_UPLOAD_DATA, block, sequence=42), b"\0")
        self.assertEqual(bytes(board.cpu.mem_read(PAYLOAD_BASE, 128)), block)
        self.assertEqual(board.transact(PROGRAM_UPLOAD_COMMIT, b"\1"), b"\0")

    def test_memory_confirmation_preserves_programs_until_enter(self):
        board = self.board
        payload = b"\xcb" + b"PROGRAM DATA" * 8
        board.upload("Keep me", payload)
        resident = board.programs()
        board.key(4)  # Custom -> root, selected Custom.
        board.key(2)  # Memory check.
        board.key(3)
        dialog = ["ALL RAM PROGRAMS", "WILL BE DELETED!", "", "YES:ENTER  NO:ESC"]
        self.assertEqual([row.strip() for row in board.screen()], dialog)
        board.advance(300000)
        self.assertEqual([board.lcd[start:start + 20].decode().strip()
                          for start in (0, 0x40, 0x14, 0x54)], dialog)
        for key in (1, 2):  # Arrows do not confirm the destructive action.
            board.key(key)
            self.assertEqual([row.strip() for row in board.screen()], dialog)
        self.assertEqual(board.programs(), resident)
        self.assertEqual(bytes(board.cpu.mem_read(PAYLOAD_BASE, len(payload))), payload)
        board.key(4)  # No: Esc.
        self.assertIn("> Memory check", board.screen()[3])
        self.assertEqual(board.programs(), resident)
        self.assertEqual(bytes(board.cpu.mem_read(PAYLOAD_BASE, len(payload))), payload)

        # The boardctl keyboard path uses the same confirmation dialog.
        board.transact(KEYBOARD_MODE, b"\1")
        board.transact(KEYBOARD_EVENT, b"\r\x1c")
        self.assertEqual([row.strip() for row in board.screen()], dialog)
        board.transact(KEYBOARD_EVENT, b"\x1b\1")
        self.assertIn("> Memory check", board.screen()[3])
        self.assertEqual(board.programs(), resident)
        self.assertEqual(bytes(board.cpu.mem_read(PAYLOAD_BASE, len(payload))), payload)
        board.transact(KEYBOARD_EVENT, b"\r\x1c")
        board.transact(KEYBOARD_EVENT, b"\r\x1c")  # Yes: Enter.
        self.assertEqual(board.screen()[0].strip(), "MEMORY CHECK")
        self.assertEqual(board.programs(), [])

    def test_memory_success_and_cancel(self):
        board = self.board
        board.cpu.mem_write(0xfc00, b"STACK GUARD")
        board.start_memory_test()
        screen = board.screen()
        self.assertEqual(screen[0].strip(), "MEMORY CHECK")
        self.assertEqual(screen[1], "[" + " " * 18 + "]")
        self.assertEqual(screen[2].strip(), "RAM: 8800-880F")
        self.assertEqual(screen[3], "OK:000000 BAD:000000")
        self.assertNotEqual(board.transact(PROGRAM_UPLOAD_BEGIN, struct.pack("<I", 4)), b"\0")
        self.assertEqual(board.transact(PING), b"\0")
        board.advance(20000000)
        screen = board.screen()
        self.assertEqual(screen[0].strip(), "MEMORY CHECK")
        self.assertEqual(screen[1], "[" + "#" * 18 + "]")
        self.assertEqual(screen[2].strip(), "DONE FBF0-FBFF")
        self.assertEqual(screen[3], "OK:148480 BAD:000000")
        self.assertEqual(bytes(board.cpu.mem_read(0xfc00, 11)), b"STACK GUARD")
        board.key(4)
        board.key(3)
        board.key(3)
        board.key(4)
        screen = board.screen()
        self.assertEqual(screen[0].strip(), "MEMORY CHECK")
        self.assertTrue(screen[2].startswith("STOP 88"))
        self.assertEqual(screen[3], "OK:000000 BAD:000000")

    def test_memory_failure(self):
        board = self.board
        board.start_memory_test()

        def bad_read(cpu, access, address, size, value, user):
            cpu.mem_write(0x9000, b"\x01")

        board.cpu.hook_add(UC_HOOK_MEM_READ, bad_read, begin=0x9000, end=0x9000)
        board.advance(20000000)
        screen = board.screen()
        self.assertEqual(screen[0].strip(), "MEMORY CHECK")
        self.assertEqual(screen[1], "[" + "#" * 18 + "]")
        self.assertEqual(screen[2].strip(), "FAIL 9000-900F")
        self.assertEqual(bytes(board.cpu.mem_read(0x8684, 4)), bytes.fromhex("00010090"))
        self.assertEqual(screen[3], "OK:148475 BAD:000005")

    def test_memory_progress_never_resets(self):
        board = self.board
        board.start_memory_test()
        previous = 0
        for _ in range(30):
            board.advance(700000)
            screen = board.screen()
            completed = screen[1].count("#")
            self.assertGreaterEqual(completed, previous)
            self.assertEqual(screen[1], "[" + "#" * completed + " " * (18 - completed) + "]")
            self.assertTrue(screen[3].startswith("OK:"))
            if screen[2].startswith("RAM:"):
                start, end = (int(address, 16) for address in screen[2][5:14].split("-"))
                self.assertEqual(start & 15, 0)
                self.assertEqual(end - start, 15)
                self.assertGreaterEqual(start, PAYLOAD_BASE)
                self.assertLess(end, 0xfc00)
            previous = completed
            if screen[2].startswith("DONE"):
                break
        self.assertEqual(previous, 18)
        self.assertEqual(screen[3], "OK:148480 BAD:000000")

    def test_memory_counts_multiple_errors_and_keeps_first_log(self):
        board = self.board
        board.start_memory_test()

        def bad_reads(cpu, access, address, size, value, user):
            cpu.mem_write(address, b"\x01")

        board.cpu.hook_add(UC_HOOK_MEM_READ, bad_reads, begin=0x9000, end=0x9001)
        board.advance(20000000)
        screen = board.screen()
        self.assertEqual(screen[2].strip(), "FAIL 9000-900F")
        self.assertEqual(screen[3], "OK:148470 BAD:000010")

    def test_run_returns_to_rom_menu(self):
        board = self.board
        # Clobber DS/ES and DF, then RETF; BIOS restores its own environment.
        payload = bytes.fromhex("31c08ed88ec0fdcb")
        board.upload("Return", payload)
        board.upload("Other", b"\xcb")
        board.key(1)
        board.key(3)  # details of the first resident
        self.assertEqual(board.screen()[0].strip(), "Return")
        reply = board.key(3)
        self.assertEqual(reply[1], 1)

        def at_program(cpu, address, size, user):
            cpu.emu_stop()

        hook = board.cpu.hook_add(UC_HOOK_CODE, at_program, begin=PAYLOAD_BASE, end=PAYLOAD_BASE)
        board.advance(700000)
        lcd = [bytes(board.lcd[a:a + 20]).decode().strip() for a in (0, 0x40, 0x14, 0x54)]
        self.assertEqual(lcd, ["Return", "RUNNING PROGRAM", "", ""])
        board.cpu.hook_del(hook)
        board.advance(700000)
        board.tx.clear()
        self.assertEqual(board.screen()[0].strip(), "CUSTOM PROGRAMS")
        self.assertEqual(len(board.programs()), 2)
        self.assertEqual(board.transact(PROGRAM_EXEC, address=1), b"\0")
        board.advance(700000)
        self.assertEqual(len(board.programs()), 2)
        self.assertNotEqual(board.transact(PROGRAM_EXEC, address=PAYLOAD_BASE), b"\0")

    def test_custom_slots_scroll_and_capacity(self):
        board = self.board
        board.key(2)
        board.key(3)
        screen = board.screen()
        self.assertEqual(screen[0].strip(), "CUSTOM PROGRAMS")
        self.assertEqual([row[1:19].strip() for row in screen[1:]], ["Empty"] * 3)
        self.assertEqual(screen[3][-1], "v")
        board.key(3)  # empty slot is not runnable
        self.assertEqual(board.screen()[0].strip(), "CUSTOM PROGRAMS")
        for n in range(4):
            ident, address = board.upload(f"Demo {n+1}", b"\xcb" * (n+1))
            self.assertEqual(ident, n+1)
            self.assertEqual(address, PAYLOAD_BASE + n*16)
        screen = board.screen()
        self.assertEqual(screen[1][-1], "^")
        self.assertEqual([row[1:19].strip() for row in screen[1:]], ["Demo 2", "Demo 3", "Demo 4"])
        self.assertEqual(screen[3][0], ">")
        self.assertEqual(board.begin("Fifth", b"\xcb"), b"\x06")
        board.key(3)
        self.assertEqual(board.screen()[0].strip(), "Demo 4")
        board.key(4)
        self.assertEqual(board.screen()[0].strip(), "CUSTOM PROGRAMS")
        board.key(4)
        self.assertEqual(board.screen()[2][:-1].strip(), "> Custom programs")

    def test_pending_crc_abort_and_resident_protection(self):
        board = self.board
        board.upload("First", b"\xcb")
        self.assertEqual(board.begin("First", b"\xcb"), b"\x08")
        reply = board.begin("Second", b"ABCD", crc=0)
        self.assertEqual(reply[1], 2)
        self.assertEqual(struct.unpack("<I", reply[2:])[0], PAYLOAD_BASE + 16)
        self.assertEqual(board.transact(PROGRAM_UPLOAD_DATA, b"xx"), b"\x03")
        self.assertEqual(board.transact(PROGRAM_EXEC, address=1), b"\x04")
        self.assertEqual(board.transact(PROGRAM_UPLOAD_COMMIT, address=2), b"\x02")
        self.assertEqual(board.transact(PROGRAM_UPLOAD_DATA, b"ABCD"), b"\0")
        self.assertEqual(board.transact(PROGRAM_UPLOAD_COMMIT, address=2), b"\x09")
        self.assertEqual(len(board.programs()), 1)
        self.assertEqual(board.transact(PROGRAM_UPLOAD_ABORT, address=2), b"\0")
        self.assertEqual(board.transact(PROGRAM_UPLOAD_ABORT, address=2), b"\0")
        self.assertEqual(board.upload("Replacement", b"\xcb"), (2, PAYLOAD_BASE + 16))
        self.assertEqual(bytes(board.cpu.mem_read(PAYLOAD_BASE, 1)), b"\xcb")
        board.key(4)  # custom -> root
        board.key(2)  # memory
        board.key(3)
        board.key(3)
        self.assertEqual(board.programs(), [])

    def test_allocation_exhaustion_and_duplicate_exec(self):
        board = self.board
        board.upload("Full", b"\xcb" + b"x" * (PAYLOAD_SIZE - 1))
        self.assertEqual(board.begin("Overflow", b"\xcb"), b"\x07")
        # count runs through a hook; retransmitted PROGRAM_EXEC must not run twice.
        calls = []
        def at_program(cpu, address, size, user):
            calls.append(address)
        board.cpu.hook_add(UC_HOOK_CODE, at_program, begin=PAYLOAD_BASE, end=PAYLOAD_BASE)
        self.assertEqual(board.transact(PROGRAM_EXEC, address=1, sequence=99), b"\0")
        board.advance(700000)
        self.assertEqual(board.transact(PROGRAM_EXEC, address=1, sequence=99), b"\0")
        board.advance(20000)
        self.assertEqual(len(calls), 1)
        board.transact(PING)
        self.assertEqual(board.transact(PROGRAM_EXEC, address=1, sequence=99), b"\0")
        board.advance(700000)
        self.assertEqual(len(calls), 2)

    def test_bundled_programs_return_without_reset(self):
        board = self.board
        directory = BIOS.parent
        for name in ("citylights", "terminal"):
            board.upload(name, (directory / f"{name}.bin").read_bytes())
        board.transact(KEYBOARD_MODE, b"\1")
        board.transact(PROGRAM_EXEC, address=1)
        board.advance(8000000)
        self.assertEqual(self.lcd_rows()[2][1:3], b"\4\4")
        self.assertEqual(board.transact(KEYBOARD_EVENT, b"\x1b\1"), b"\0\1")
        board.advance(700000)
        self.assertEqual(board.screen()[0].strip(), "CUSTOM PROGRAMS")
        self.assertEqual(len(board.programs()), 2)
        board.transact(PROGRAM_EXEC, address=2)
        board.advance(700000)
        for char in b"abc":
            self.assertEqual(board.transact(KEYBOARD_EVENT, bytes([char, 0])), b"\0\1")
            board.advance(700000)
        self.assertEqual(board.transact(KEYBOARD_EVENT, b"\x1b\1"), b"\0\1")
        board.advance(700000)
        self.assertEqual(board.screen()[0].strip(), "CUSTOM PROGRAMS")
        self.assertEqual(len(board.programs()), 2)

    def test_fragmented_and_corrupt_packets(self):
        board = self.board
        packet = encode(PING, 80)
        board.rx.extend(packet[:5])
        board.advance(5000)
        board.rx.extend(packet[5:])
        board.stop_on_reply = True
        board.advance(10000)
        board.stop_on_reply = False
        self.assertEqual(decode(board.tx), (0x81, 80, b"\0"))
        board.rx.extend(b"\0" + b"\x01" * 200 + b"\0")
        board.advance(5000)
        self.assertEqual(board.transact(PING), b"\0")
        corrupt = bytearray(encode(PING, 81))
        corrupt[-2] ^= 1
        board.rx.extend(corrupt)
        board.advance(5000)
        self.assertEqual(board.transact(PING), b"\0")


if __name__ == "__main__":
    unittest.main()
