"""PC BIOS boot path and native OS tests, executing the real ROM and FAT12 image."""
import importlib.util
import struct
import subprocess
import tempfile
import unittest
from pathlib import Path
import test_bios as fw
from unicorn.x86_const import UC_X86_REG_CS, UC_X86_REG_IP

spec = importlib.util.spec_from_file_location('os_image', fw.BIOS.parent.parent/'scripts/make-os-image.py')
image_builder = importlib.util.module_from_spec(spec)
spec.loader.exec_module(image_builder)


def com(source):
    with tempfile.TemporaryDirectory() as tmp:
        path = Path(tmp)
        (path/'p.asm').write_text('BITS 16\nCPU 8086\nORG 100h\n'+source)
        subprocess.run(['nasm','-f','bin','-o',str(path/'p.com'),str(path/'p.asm')],check=True)
        return (path/'p.com').read_bytes()


class Session:
    def __init__(self, image=None, mask=3):
        self.board = fw.Board(ram_mask=mask, pic=True, pit='running', irq_delivery='working')
        self.board.absent_accesses.clear()
        self.image = image or fw.BIOS.with_name('home8088.img').read_bytes()
        self.output = bytearray()
        self.sectors = []

    def boot(self, menu=False):
        assert self.board.transact(fw.KEYBOARD_MODE,b'\2') == b'\0\0'
        assert self.board.transact(fw.DISK_MOUNT) == b'\0'
        if menu:
            assert self.board.transact(fw.KEYBOARD_EVENT,b'\r\x1c') == b'\0\1'
        else:
            assert self.board.transact(fw.DISK_BOOT) == b'\0'

    def step(self, duplicate=False):
        b = self.board
        poll = b.transact(fw.DISK_POLL)
        assert len(poll) == 8 and poll[0] == 0
        if poll[1]:
            ident,lba,offset = struct.unpack('<3H',poll[2:])
            self.sectors.append(lba)
            assert lba < 2880 and offset < 512
            for pos in range(offset,512,120):
                data = struct.pack('<HHB',ident,pos,0)+self.image[lba*512+pos:lba*512+min(pos+120,512)]
                assert b.transact(fw.DISK_DATA,data) == b'\0'
                if duplicate:
                    assert b.transact(fw.DISK_DATA,data) == b'\0'
            b.cpu.ctl_flush_tb()
        self.output += b.transact(fw.TERMINAL_POLL)[2:]

    def until(self, marker=b'A> ', limit=220):
        start = len(self.output)
        for _ in range(limit):
            self.step()
            if marker in self.output[start:]:
                return bytes(self.output[start:])
        raise AssertionError(f'No {marker!r}: {bytes(self.output[start:])!r}')

    def type(self, text):
        for c in text:
            assert self.board.transact(fw.KEYBOARD_EVENT,bytes((c,0)))[0] == 0

    def command(self, text):
        self.type(text.encode()+b'\r')
        return self.until()


class PCBootTest(unittest.TestCase):
    def test_boot_shell_directory_and_applications_with_private_stacks(self):
        session = Session()
        session.boot()
        self.assertIn(b'HOME8088 OS 0.1',session.until())
        self.assertEqual(session.sectors[:17],list(range(17)))
        self.assertEqual(struct.unpack('<H',session.board.cpu.mem_read(0x413,2))[0],224)
        self.assertEqual(struct.unpack('<H',session.board.cpu.mem_read(0x4f0,2))[0],0x3000)
        self.assertIn(b'224 KiB conventional RAM',session.command('mem'))
        self.assertIn(b'RUN name',session.command('help'))
        directory = session.command('dir')
        for name in (b'CITY.COM',b'REACTOR.COM',b'TERMINAL.COM'):
            self.assertIn(name,directory)
        self.assertIn(b'Program not found',session.command('run missing'))
        for name, marker in (('terminal',b'UART-to-LCD terminal ready'),
                             ('city',b'CITY LIGHTS'),('reactor',b'ENTER:START')):
            session.type(('run '+name+'\r').encode())
            session.until(marker)
            session.type(b'\x1b')
            session.until()
            self.assertIn(b'224 KiB',session.command('mem'))
        self.assertTrue(session.board.hardware_irqs)
        self.assertFalse(session.board.absent_accesses)

    def test_one_bank_and_default_boot_menu(self):
        session = Session(mask=1)
        session.boot(menu=True)
        session.until()
        self.assertEqual(session.board.transact(fw.DISK_BOOT,sequence=0),b'\x0b')
        self.assertIn(b'96 KiB conventional RAM',session.command('mem'))
        self.assertIn(b'CITY.COM',session.command('dir'))

    def test_duplicate_chunks_and_invalid_boot_signature(self):
        image = bytearray(fw.BIOS.with_name('home8088.img').read_bytes())
        image[510:512] = b'XX'
        session = Session(bytes(image))
        session.boot()
        session.step(duplicate=True)
        session.until(b'BOOT FAILED')

    def test_disk_validation_and_retries_across_sector_handoffs(self):
        session = Session(mask=1)
        session.boot()
        board = session.board
        self.assertEqual(board.transact(fw.DISK_BOOT,sequence=2),b'\0')
        pending = board.transact(fw.DISK_POLL)
        ident,lba,offset = struct.unpack('<3H',pending[2:])
        self.assertEqual((lba,offset),(0,0))
        for payload in (struct.pack('<HHB',ident,1,0)+b'X',
                        struct.pack('<HHB',ident+1,0,0)+b'X',
                        struct.pack('<HHB',ident,0,0)):
            self.assertEqual(board.transact(fw.DISK_DATA,payload),b'\x12')
        first = struct.pack('<HHB',ident,0,0)+session.image[:120]
        self.assertEqual(board.transact(fw.DISK_DATA,first),b'\0')
        changed = first[:-1]+bytes([first[-1]^1])
        self.assertEqual(board.transact(fw.DISK_DATA,changed),b'\x12')
        for _ in range(220):
            session.step(duplicate=True)
            if b'A> ' in session.output:
                break
        self.assertIn(b'HOME8088 OS',session.output)
        self.assertIn(b'A> ',session.output)

    def test_fragmented_large_com_overwrites_old_bios_region_safely(self):
        program = com("""
            mov si, message
        next:
            lodsb
            test al, al
            jz done
            mov ah, 0eh
            xor bx, bx
            int 10h
            jmp next
        done:
            ret
        message: db 'LARGE COM PASS',13,10,0
        times 5000h-($-$$) db 0a5h
        """)
        image = bytearray(image_builder.build(fw.BIOS.with_name('os-boot.bin').read_bytes(),
            fw.BIOS.with_name('os-kernel.bin').read_bytes(), [('LARGE.COM',program)]))
        # Move the first cluster to 2800, with the rest of the chain unchanged.
        image[(49+2800-2)*512:(49+2800-1)*512] = image[49*512:50*512]
        struct.pack_into('<H',image,35*512+26,2800)
        for fat_start in (17*512,26*512):
            pos=fat_start+2800+2800//2
            word=struct.unpack_from('<H',image,pos)[0]
            struct.pack_into('<H',image,pos,(word&0xf000)|3)
        session=Session(bytes(image),mask=1)
        session.boot()
        session.until()
        self.assertIn(b'LARGE COM PASS',session.command('run large'))
        self.assertEqual(bytes(session.board.cpu.mem_read(0x8000,16)),b'\xa5'*16)
        self.assertIn(b'96 KiB',session.command('mem'))

    def test_boot_requires_ram0_timer_and_terminal(self):
        for mask,pic,pit in ((2,True,'running'),(1,None,'running'),(1,True,None)):
            with self.subTest(mask=mask,pic=pic,pit=pit):
                board = fw.Board(ram_mask=mask,pic=pic,pit=pit,irq_delivery='working')
                self.assertNotEqual(board.transact(fw.DISK_BOOT),b'\0')
                self.assertEqual(board.transact(fw.KEYBOARD_MODE,b'\2'),b'\0\0')
                mount = board.transact(fw.DISK_MOUNT)
                self.assertEqual(mount,b'\0' if mask & 1 else b'\x12')
                self.assertEqual(board.transact(fw.DISK_BOOT),b'\x12')

    def test_disk_bounds_readonly_status_and_native_com_return(self):
        program = com('''
            mov ax, 0301h
            xor dx, dx
            int 13h
            jnc failed
            cmp ah, 3
            jne failed
            mov ax, 0201h
            mov cx, 5001h
            xor dx, dx
            int 13h
            jnc failed
            mov ax, 0201h
            mov cx, 1
            xor dx, dx
            xor bx, bx
            xor ax, ax
            mov es, ax
            mov ax, 0201h
            int 13h
            jnc failed
            mov si, passed
            jmp print
        failed:
            mov si, error
        print:
            lodsb
            test al, al
            jz done
            mov ah, 0eh
            xor bx, bx
            int 10h
            jmp print
        done:
            ret
        passed: db 'DISK CHECK PASS',13,10,0
        error: db 'DISK CHECK FAIL',13,10,0
        ''')
        image = image_builder.build(fw.BIOS.with_name('os-boot.bin').read_bytes(),
            fw.BIOS.with_name('os-kernel.bin').read_bytes(), [('CHECK.COM',program)])
        session = Session(image)
        session.boot()
        session.until()
        self.assertIn(b'DISK CHECK PASS',session.command('run check'))


class PCTimeTest(unittest.TestCase):
    def test_api_and_irqs_do_not_depend_on_application_ss(self):
        helper = fw.FirmwareTest()
        helper.board = fw.Board(pic=True,pit='running',irq_delivery='working')
        untouched = bytes(helper.board.cpu.mem_read(0x28000,16))
        result = helper.run_api('''
            mov ax, ss
            mov [old_ss], ax
            mov [old_sp], sp
            cli
            mov ax, 2000h
            mov ss, ax
            mov sp, 0fff0h
            sti
            int 12h
            mov [results], ax
            mov cx, 120
            bios_delay
            bios_ticks
            mov [results+2], ax
            bios_runtime_info
            mov [results+4], dx
            cli
            mov ax, [old_ss]
            mov ss, ax
            mov sp, [old_sp]
            sti
        ''',data='old_ss: dw 0\nold_sp: dw 0')
        size,ticks,flags = struct.unpack('<3H',result[:6])
        self.assertEqual(size,640)
        self.assertGreater(ticks,0)
        self.assertEqual(flags,3)
        self.assertEqual(bytes(helper.board.cpu.mem_read(0x28000,16)),untouched)

    def test_pc_clock_set_get_and_midnight_hook(self):
        helper = fw.FirmwareTest()
        helper.board = fw.Board()
        result = helper.run_api('''
            mov cx, 12h
            mov dx, 3456h
            mov ah, 1
            int 1ah
            pushf
            pop word [results]
            xor ah, ah
            int 1ah
            mov [results+2], cx
            mov [results+4], dx
            mov [results+6], ax
        ''')
        flags,hi,lo,ax = struct.unpack('<4H',result[:8])
        self.assertFalse(flags & 1)
        self.assertEqual((hi,lo,ax),(0x12,0x3456,0))
        board = fw.Board(pic=True,pit='running',irq_delivery='working')
        board.cpu.mem_write(0x46c,struct.pack('<I',0x1800af))
        board.advance(20000)
        self.assertGreater(board.cpu.mem_read(0x470,1)[0],0)
        self.assertLess(struct.unpack('<I',board.cpu.mem_read(0x46c,4))[0],10)
        self.assertIn(0x1c,board.interrupts)
