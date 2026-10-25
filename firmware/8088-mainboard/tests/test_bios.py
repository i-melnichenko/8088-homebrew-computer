"""Execute the actual NASM image with emulated UART/LCD inside Docker.

Run via `make test`. This checks ROM code paths, not electrical timings.
"""
import binascii
import ctypes
import hashlib
from collections import deque
import mmap
import os
import re
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
FLASHER_BASE = 0x8800
FLASHER_SIZE = 0xfc00 - FLASHER_BASE
BIOS_VERSION = os.environ.get("BIOS_VERSION", "dev-os2")

# Protocol v1 command values, grouped exactly as the wire specification.
PING = 0x01
INFO = 0x02
RESET = 0x03
BIOS_FLASHER_MODE = 0x04
BIOS_WRITE = 0x05
BIOS_READ = 0x06
MEMORY_READ = 0x07
MEMORY_WRITE = 0x08
KEYBOARD_MODE = 0x09
KEYBOARD_EVENT = 0x0a
RAM_INFO = 0x0b
RAM_TEST = 0x0c
TERMINAL_POLL = 0x0d
IRQ_TEST = 0x0e
DISK_MOUNT = 0x0f
DISK_BOOT = 0x10
DISK_POLL = 0x11
DISK_DATA = 0x12


def assemble(source):
    with tempfile.TemporaryDirectory() as directory:
        path = Path(directory)
        (path / "test.asm").write_text(
            'BITS 16\nCPU 8086\nORG 0\n%include "bios_api.inc"\n' + source)
        subprocess.run(["nasm", "-f", "bin", "-I", str(BIOS.parent.parent / "lib") + "/",
                        "-o", str(path / "test.bin"), str(path / "test.asm")], check=True)
        return (path / "test.bin").read_bytes()


def encode(command, sequence, data=b""):
    raw = struct.pack("<BBBH", 2, command, sequence, len(data)) + data
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
    assert version == 2 and len(raw) == length + 7
    return command, sequence, bytes(raw[5:-2])


class Board:
    def __init__(self, initial_ram=None, ram_mask=0x1f, image=None, aliases=None, stuck_a16=False, pic=None, pit=None, irq_delivery=None, lcd_present=True):
        self.cpu = Uc(UC_ARCH_X86, UC_MODE_16)
        self.uart_base = 0x3f8
        self.lcd_port = 0x80
        self.lcd_present = lcd_present
        ram_size = 0xa0000
        self.sram = mmap.mmap(-1, ram_size)
        pointer = ctypes.addressof(ctypes.c_char.from_buffer(self.sram))
        self.ram_mask = ram_mask
        self.pic = pic  # None/constant/last-bus: absent, True: working IMR
        self.pit = pit  # running, stopped (GATE low), no-clock, or absent bus value
        self.pit_config = 0x30
        self.pit_null = True
        self.pit_pending = None
        self.pit_value = 0xffff
        self.pit_status_latch = None
        self.pit_count_latch = None
        self.pit_write_low = None
        self.pit_read_high = False
        self.irq_delivery = irq_delivery
        self.pic_pending = 0
        self.pic_in_service = False
        self.irq0_epoch = 0
        self.ier = 0
        self.mcr = 3
        self.pit_reload = 65536
        self.hardware_irqs = 0
        self.irq_instructions = self.irq_shadow = 0
        self.pic_init = 0
        self.pic_mask = 0
        self.uart_scratch = 0x39
        self.last_bus = 0
        self.io_writes = []
        self.lcd_reads = []
        self.absent_accesses = []
        self.bios_bank_probes = 0
        aliases = aliases or {}
        for bank in range(5):
            physical = aliases.get(bank, bank) * 0x20000
            for half in range(2):
                offset = physical + (0 if stuck_a16 else half * 0x10000)
                self.cpu.mem_map_ptr(bank * 0x20000 + half * 0x10000,
                                     0x10000, 7, pointer + offset)
        self.cpu.mem_map(0xa0000, 0x60000)
        if initial_ram is not None:
            assert len(initial_ram) == ram_size
            self.cpu.mem_write(0, initial_ram)
        image = (image or BIOS).read_bytes()
        for address in range(0xe0000, 0x100000, 0x8000):
            self.cpu.mem_write(address, image)
        self.cpu.reg_write(UC_X86_REG_CS, 0xffff)
        self.cpu.reg_write(UC_X86_REG_IP, 0)
        self.rx, self.tx = deque(), bytearray()
        self.lcr, self.sequence, self.stop_on_reply = 0, 0, False
        self.uart_divisor = [None, None]
        self.reply_started, self.reply_length = False, 0
        self.tx_ready = True
        self.lcd, self.cursor = bytearray(b" " * 128), 0
        self.lcd_operations = None  # opt-in trace for menu blink tests
        self.cgram, self.cgram_cursor = bytearray(64), None
        self.interrupts = []
        self.eeprom_writes = []
        self.cpu.hook_add(UC_HOOK_INSN, self.input, None, 1, 0, UC_X86_INS_IN)
        self.cpu.hook_add(UC_HOOK_INSN, self.output, None, 1, 0, UC_X86_INS_OUT)
        self.cpu.hook_add(UC_HOOK_MEM_WRITE, self.memory_write)
        for bank in range(5):
            if not ram_mask & (1 << bank):
                self.cpu.hook_add(UC_HOOK_MEM_READ, self.memory_read,
                                  begin=bank * 0x20000, end=(bank + 1) * 0x20000 - 1)
        self.cpu.hook_add(UC_HOOK_INTR, self.interrupt)
        if irq_delivery is not None:
            self.cpu.hook_add(UC_HOOK_CODE, self.irq_step)
        self.advance(700000)
        if pic is True:
            self.advance(2000000)
        self.boot_output = bytes(self.tx)
        self.tx.clear()

    def memory_read(self, cpu, access, address, size, value, user):
        if address < 0xa0000 and not self.ram_mask & (1 << (address // 0x20000)):
            # Empty socket/open bus: stable FF, independent of preceding writes.
            cpu.mem_write(address, b"\xff" * size)
            if address == 0x4f2 and size == 2:
                # Safe BDA-extension probe selects the monitor-only fallback
                # when RAM0 is absent. No writes are made to the empty bank.
                self.bios_bank_probes += 1
            else:
                self.absent_accesses.append(("read", address, size))

    def memory_write(self, cpu, access, address, size, value, user):
        if address < 0xa0000 and not self.ram_mask & (1 << (address // 0x20000)):
            self.absent_accesses.append(("write", address, size))
        if (BIOS_ADDRESS <= address and address + size <= 0x100000
                and cpu.reg_read(UC_X86_REG_CS) == cpu.reg_read(UC_X86_REG_SS) + (FLASHER_BASE >> 4)):
            self.eeprom_writes.append((address, size, value))
            return
        assert 0 <= address and address + size <= 0xa0000, hex(address)

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

    def irq_step(self, cpu, address, size, user):
        self.irq_instructions += 1
        # Coarse functional IRQ clock; CPU state is read only for pending IRQs.
        if self.irq_instructions % 128 == 0:
            self.pit_tick(1024)
        if self.rx and self.ier & 1 and self.mcr & 8:
            self.pic_pending |= 0x10
        pending = self.pic_pending & ~self.pic_mask
        if self.irq_delivery in ("broken", "uart-only"):
            pending &= ~1
        if self.irq_delivery in ("broken", "timer-only"):
            pending &= ~0x10
        if (self.pic is True and pending and not self.pic_in_service and
                cpu.reg_read(UC_X86_REG_EFLAGS) & 0x200):
            if cpu.mem_read(address - 1, 1) == b"\xfb":
                return
            irq = 0 if pending & 1 else 4
            self.pic_pending &= ~(1 << irq)
            self.pic_in_service = True
            self.hardware_irqs += 1
            if irq == 0:
                self.irq0_epoch += 1
            self.interrupt(cpu, 0x08 + irq, None)

    def pit_tick(self, amount=17):
        # Functional timer model: advance on I/O, not an electrical timing model.
        if self.pit not in ("running", "stopped"):
            return
        if self.pit_pending is not None:
            self.pit_value = self.pit_pending or 65536
            self.pit_reload = self.pit_value
            self.pit_pending = None
            self.pit_null = False
        elif not self.pit_null and self.pit == "running":
            self.pit_value -= amount
            if self.pit_config == 0x34 and self.pit_value <= 0:
                self.pit_value += self.pit_reload
                if self.irq_delivery != "once" or self.irq0_epoch == 0:
                    self.pic_pending |= 1
            else:
                self.pit_value = max(0, self.pit_value)

    def pit_write(self, port, value):
        if self.pit not in ("running", "stopped", "no-clock"):
            return
        if port == 0x43:
            if value == 0xe2:
                if self.pit_status_latch is None:
                    self.pit_status_latch = self.pit_config | (0x40 if self.pit_null else 0)
            elif value == 0:
                if self.pit_count_latch is None:
                    self.pit_count_latch = self.pit_value & 0xffff
                    self.pit_read_high = False
            else:
                assert value in (0x10, 0x30, 0x34), hex(value)
                self.pit_config = value
                self.pit_null = True
                self.pit_pending = None
                self.pit_write_low = None
                self.pit_status_latch = self.pit_count_latch = None
        elif port == 0x40:
            if self.pit_write_low is None:
                self.pit_write_low = value
            else:
                self.pit_pending = self.pit_write_low | (value << 8)
                self.pit_write_low = None

    def pit_read(self):
        if self.pit == "last-bus":
            return self.last_bus
        if isinstance(self.pit, int):
            return self.pit
        if self.pit not in ("running", "stopped", "no-clock"):
            return 0xff
        if self.pit_status_latch is not None:
            value, self.pit_status_latch = self.pit_status_latch, None
            return value
        value = self.pit_count_latch if self.pit_count_latch is not None else self.pit_value
        if self.pit_read_high:
            self.pit_read_high = False
            self.pit_count_latch = None
            return (value >> 8) & 0xff
        self.pit_read_high = True
        return value & 0xff

    def input(self, cpu, port, size, user):
        if port in (0x80,0x81):
            self.lcd_reads.append(port)
        self.pit_tick()
        if port == 0x40:
            return self.pit_read()
        if port == 0x21:
            if self.pic is True:
                return self.pic_mask
            if self.pic == "last-bus":
                return self.last_bus
            return self.pic if isinstance(self.pic, int) else 0xff
        if port == self.uart_base + 7:
            self.last_bus = self.uart_scratch
            return self.uart_scratch
        if port == self.uart_base + 5:
            return (0x60 if self.tx_ready else 0) | bool(self.rx)
        if port == self.uart_base and self.rx:
            return self.rx.popleft()
        return 0

    def output(self, cpu, port, size, value, user):
        self.pit_tick()
        self.io_writes.append((port, value))
        if port in (0x40, 0x43):
            self.pit_write(port, value)
        self.last_bus = value
        if port == self.uart_base + 7:
            self.uart_scratch = value
        if self.pic is True:
            if port == 0x20 and value & 0x10:
                self.pic_pending = 0
                self.pic_in_service = False
                self.irq0_epoch = 0
                self.pic_init = 2  # single PIC: ICW2 and ICW4
            elif port == 0x20 and value == 0x20:
                self.pic_in_service = False
            elif port == 0x21:
                if self.pic_init:
                    self.pic_init -= 1
                else:
                    self.pic_mask = value

        if port in (self.lcd_port, self.lcd_port + 1) and self.lcd_operations is not None:
            self.lcd_operations.append((port, value))
        if port == self.uart_base + 3:
            self.lcr = value
        if port == self.uart_base + 4:
            self.mcr = value
        if port == self.uart_base + 1 and not self.lcr & 0x80:
            self.ier = value
        if self.lcr & 0x80 and port in (self.uart_base, self.uart_base + 1):
            self.uart_divisor[port - self.uart_base] = value
        if port == self.uart_base + 2 and value & 2:
            self.rx.clear()
        if port == self.uart_base and not self.lcr & 0x80 and self.mcr & 0x10:
            self.rx.append(value)
            return
        if port == self.uart_base and not self.lcr & 0x80:
            self.tx.append(value)
            if self.stop_on_reply:
                if value == 0:
                    if self.reply_started and self.reply_length:
                        cpu.emu_stop()
                    self.reply_started = True
                elif self.reply_started:
                    self.reply_length += 1
        if port in (self.lcd_port, self.lcd_port + 1) and not self.lcd_present:
            return  # unconnected write-only expansion module
        if port == self.lcd_port:
            if value == 1:
                self.lcd[:] = b" " * 128
                self.cgram_cursor = None
            elif value & 0x80:
                self.cursor = value & 0x7f
                self.cgram_cursor = None
            elif value & 0x40:
                self.cgram_cursor = value & 0x3f
        if port == self.lcd_port + 1:
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
            if False:
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
        # Finish the reply and let a pending render leave its initial state.
        self.advance(1000)
        for _ in range(2000):
            dirty = self.cpu.mem_read(0x8688, 1)[0]
            position = self.cpu.mem_read(0x8696, 1)[0]
            if not dirty and position >= 80:
                # MENU_DIRTY is cleared before the new frame is built. Do not
                # mistake that short window for a completed physical redraw.
                frame = bytes(self.cpu.mem_read(0x8620, 80))
                physical = b"".join(self.lcd[start:start + 20]
                                    for start in (0, 0x40, 0x14, 0x54))
                if all(a == b or (i in (20, 40, 60) and a == ord('>') and b == ord(' '))
                       for i, (a, b) in enumerate(zip(frame, physical))):
                    break
            self.advance(1000)
        else:
            raise AssertionError("menu LCD rendering did not finish")
        rows = [self.lcd[start:start + 20].decode("ascii")
                for start in (0, 0x40, 0x14, 0x54)]
        # Normalize only the blinking marker for content assertions. Separate
        # blink tests below inspect the physical cell without normalization.
        if (self.cpu.reg_read(UC_X86_REG_CS) == 0xf800
                and self.cpu.mem_read(0x867a, 1)[0] == 0):
            selected = self.cpu.mem_read(0x8679, 1)[0]
            row = min(selected, 2) + 1
            assert rows[row][0] in "> "
            rows[row] = ">" + rows[row][1:]
        return rows

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
        self.key(3)
        assert [row.strip() for row in self.screen()] == [
            "FULL RAM TEST", "RAM WILL BE ERASED!", "", "YES:ENTER  NO:ESC"]
        self.key(3)


class FirmwareTest(unittest.TestCase):
    # Test-only CPU harness. Applications are loaded by the OS, never by a ROM
    # upload/EXEC command. Enter assembled API clients directly in Unicorn.
    def run_api(self,source,data="",incoming=b"",**kwargs):
        from unicorn.x86_const import UC_X86_REG_DS,UC_X86_REG_ES
        payload=assemble(source+"\njmp finish\n"+data+"\nfinish: retf\nresults: times 128 db 0\n")
        address=0x10000
        result_address=address+len(payload)-128
        self.board.cpu.mem_write(address,payload)
        self.board.cpu.ctl_flush_tb()
        reached=[]
        def finish(cpu,addr,size,user):reached.append(addr);cpu.emu_stop()
        hook=self.board.cpu.hook_add(UC_HOOK_CODE,finish,begin=result_address-1,end=result_address-1)
        for reg,value in ((UC_X86_REG_CS,0x1000),(UC_X86_REG_IP,0),(UC_X86_REG_DS,0x1000),(UC_X86_REG_ES,0x1000),(UC_X86_REG_SS,0),(UC_X86_REG_SP,0xfdfe),(UC_X86_REG_EFLAGS,0x202)):
            self.board.cpu.reg_write(reg,value)
        self.board.rx.extend(incoming)
        self.board.advance(16000000)
        self.assertTrue(reached,"API client did not finish")
        self.board.cpu.hook_del(hook)
        return bytes(self.board.cpu.mem_read(result_address,128))


class MonitorV2Test(unittest.TestCase):
    def test_info_ram_menu_and_strict_wire_version(self):
        board=Board(ram_mask=3,pic=True,pit="running",irq_delivery="working")
        self.assertEqual(board.transact(PING),b"\0")
        self.assertIn(b"protocol 2.0",board.transact(INFO))
        ram=board.transact(RAM_INFO)
        self.assertEqual(len(ram),11)
        self.assertEqual(struct.unpack('<I',ram[4:8])[0],256*1024)
        self.assertEqual(struct.unpack('<H',ram[8:10])[0],256)
        self.assertEqual([line.strip() for line in board.screen()],["8088 MAINBOARD","> Default boot","Memory check","Info"])
        self.assertEqual(board.transact(0x7f),b"\1")
        raw=struct.pack('<BBBH',1,PING,99,0)
        raw+=struct.pack('<H',binascii.crc_hqx(raw,0xffff))
        board.tx.clear();board.rx.extend(cobs_packet(raw));board.advance(300000)
        self.assertEqual(board.tx,b'')
        self.assertEqual(board.transact(PING),b'\0')

    def test_optional_ram_banks_and_chips(self):
        for mask in (1,2,4,8,16,3,5,31):
            with self.subTest(mask=mask):
                board=Board(ram_mask=mask,lcd_present=False)
                self.assertEqual(board.transact(PING),b'\0')
                ram=board.transact(RAM_INFO)
                self.assertEqual(ram[1],mask)
                self.assertEqual(ram[2],(mask&-mask).bit_length()-1)
                self.assertEqual(board.transact(IRQ_TEST),b'\x15' if mask&1 else b'\x17')

    def test_ram_diagnostic_start_retry_cancel_and_preservation(self):
        board=Board(ram_mask=1)
        vectors=bytes(board.cpu.mem_read(0,0x400))
        self.assertEqual(board.transact(RAM_TEST,b'\1'),b'\5')
        start=board.transact(RAM_TEST,b'\1\xa5',sequence=20)
        retry=board.transact(RAM_TEST,b'\1\xa5',sequence=20)
        self.assertEqual(start[1],1);self.assertEqual(retry[1],1)
        for _ in range(20):board.transact(RAM_TEST,b'\0')
        result=board.transact(RAM_TEST,b'\2')
        self.assertEqual(result[1],3)
        self.assertEqual(bytes(board.cpu.mem_read(0,0x400)),vectors)
        self.assertEqual(board.transact(PING),b'\0')

    def test_irq_diagnostic_restores_active_runtime(self):
        board=Board(ram_mask=1,pic=True,pit="running",irq_delivery="working")
        before=bytes(board.cpu.mem_read(8*4,4))
        reply=board.transact(IRQ_TEST)
        self.assertEqual(reply[0:2],b'\0\0')
        self.assertGreaterEqual(struct.unpack('<H',reply[2:4])[0],8)
        self.assertEqual(reply[4],8)
        self.assertEqual(bytes(board.cpu.mem_read(8*4,4)),before)
        self.assertEqual(board.cpu.mem_read(0x8000,1),b'\3')

    def test_ram_final_bank_boundary_publishes_completed_snapshot(self):
        board=Board(ram_mask=1)
        board.transact(RAM_TEST,b'\1\xa5')
        board.cpu.mem_write(0x8680,struct.pack('<H',0xfff0))
        board.cpu.mem_write(0x8762,b'\1')
        board.cpu.mem_write(0x8682,b'\11')
        board.cpu.mem_write(0x8698,struct.pack('<I',655360-16))
        board.cpu.mem_write(0x8768,struct.pack('<I',81919))
        board.cpu.mem_write(0x1fff0,bytes((v&255)^((v>>8)&255)^1 for v in range(0xfff0,0x10000)))
        board.advance(200000)
        reply=board.transact(RAM_TEST,b'\0')
        self.assertEqual(reply[1],2)
        self.assertEqual(struct.unpack('<I',reply[9:13])[0],655360)
        self.assertEqual(struct.unpack('<I',reply[13:17])[0],0)

    def test_memory_ranges_and_flasher_readback(self):
        board=Board(ram_mask=1)
        self.assertEqual(board.transact(MEMORY_WRITE,struct.pack('<I',0x8500)+b'X'),b'\x0e')
        self.assertEqual(board.transact(MEMORY_WRITE,struct.pack('<I',0x9000)+b'test'),b'\0')
        self.assertEqual(board.transact(MEMORY_READ,struct.pack('<IH',0x9000,4)),b'\0test')
        board.eeprom_mode()
        self.assertEqual(board.transact(PING),b'\0')
        self.assertEqual(board.transact(BIOS_READ,struct.pack('<IH',0xf8000,127)),b'\0'+BIOS.read_bytes()[:127])
        self.assertEqual(board.transact(DISK_POLL),b'\x14')

    def test_flasher_full_transfer_verification_and_restart(self):
        board=Board(ram_mask=1)
        image=BIOS.read_bytes()
        board.eeprom_mode()
        for offset in range(0,len(image),124):
            chunk=image[offset:offset+124]
            self.assertEqual(board.transact(BIOS_WRITE,struct.pack('<I',0xf8000+offset)+chunk),b'\0')
        for offset in range(0,len(image),127):
            size=min(127,len(image)-offset)
            self.assertEqual(board.transact(BIOS_READ,struct.pack('<IH',0xf8000+offset,size)),b'\0'+image[offset:offset+size])
        self.assertEqual(board.transact(RESET),b'\0')
        board.advance(2800000)
        self.assertEqual(board.transact(PING),b'\0')

if __name__=='__main__':unittest.main()
