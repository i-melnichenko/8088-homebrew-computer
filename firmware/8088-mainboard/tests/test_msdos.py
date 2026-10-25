"""Optional integration test: the pinned, unmodified Microsoft DOS binaries."""
from pathlib import Path
import unittest
import struct
import test_bios as fw
from test_pc import Session

IMAGE = fw.BIOS.with_name('msdos20.img')


class DOSSession(Session):
    def wait_output(self, marker, start=0, limit=1500):
        for _ in range(limit):
            if marker in self.output[start:]:
                return bytes(self.output[start:])
            self.step()
        raise AssertionError(f'No {marker!r}: {bytes(self.output[start:])!r}')

    def command(self, text, marker=b'A>'):
        start = len(self.output)
        # Give DOS its normal execution time between serial keys. The base
        # emulator stops at each ACK, unlike the real CPU between UART bytes.
        for c in text.encode()+b'\r':
            for _ in range(32):
                reply = self.board.transact(fw.KEYBOARD_EVENT, bytes((c,0)))
                if reply[0] == 0:
                    break
                if reply[0] != 13:
                    raise AssertionError(reply)
                self.step()
            else:
                raise AssertionError('DOS did not consume keyboard input')
            self.board.advance(5000)
            self.step()
        return self.wait_output(marker, start)


@unittest.skipUnless(IMAGE.exists(), 'build the pinned Microsoft image with make msdos')
class MSDOSBootTest(unittest.TestCase):
    def test_microsoft_kernel_command_files_and_dos_application(self):
        session = DOSSession(IMAGE.read_bytes())
        session.boot()
        boot = session.wait_output(b'MS-DOS Version  2.00\r\n\r\nA>')
        self.assertIn(b'MS-DOS version 2.00', boot)
        self.assertIn(b'Command v. 2.02', boot)
        self.assertIn(b'MS-DOS Version  2.00', session.command('VER'))
        directory = session.command('DIR')
        self.assertIn(b'COMMAND', directory)
        self.assertIn(b'HELLO', directory)
        self.assertIn(b'FAT12 through Microsoft MS-DOS', session.command('TYPE HELLO.TXT'))
        self.assertIn(b'Hello from Microsoft MS-DOS!', session.command('HELLO'))
        self.assertIn(b'MS-DOS Version  2.00', session.command('VER'))
        session.command('TIME', marker=b'Enter new time:')
        session.command('12:34:56.00')
        current = session.command('TIME', marker=b'Enter new time:')
        self.assertIn(b'12:34:', current)
        session.command('')
        self.assertFalse(session.board.absent_accesses)

    def test_debug_load_disassemble_step_execute_and_quit(self):
        session = DOSSession(IMAGE.read_bytes())
        session.boot()
        session.wait_output(b'MS-DOS Version  2.00\r\n\r\nA>')
        session.command('DEBUG HELLO.COM', marker=b'\r\n-')
        registers = session.command('R', marker=b'\r\n-')
        self.assertIn(b'AX=', registers)
        self.assertIn(b'IP=0100', registers)
        listing = session.command('U 100 10B', marker=b'\r\n-')
        self.assertIn(b'MOV', listing)
        self.assertIn(b'INT', listing)
        stepped = session.command('T', marker=b'\r\n-')
        self.assertIn(b'IP=0103', stepped)
        executed = session.command('G', marker=b'\r\n-')
        self.assertIn(b'Hello from Microsoft MS-DOS!', executed)
        session.command('Q')
        self.assertIn(b'MS-DOS Version  2.00', session.command('VER'))

    def test_boot_with_one_128k_ram_chip(self):
        session = DOSSession(IMAGE.read_bytes(), mask=1)
        session.boot()
        session.wait_output(b'MS-DOS Version  2.00\r\n\r\nA>')
        self.assertIn(b'MS-DOS Version  2.00', session.command('VER'))
        self.assertEqual(struct.unpack('<H',session.board.cpu.mem_read(0x413,2))[0],96)


class DOSClockArithmeticTest(unittest.TestCase):
    def test_pit_to_dos_time_and_back_across_16bit_boundaries(self):
        helper = fw.FirmwareTest()
        helper.board = fw.Board()
        io_source = (fw.BIOS.parent.parent/'os/msdos/io.asm').resolve()
        ticks = (0, 1, 18, 0x123456, 0x1800af)
        body = ['jmp client', f'%include "{io_source}"', 'client:']
        for index, tick in enumerate(ticks):
            body += [f'mov cx,{tick>>16}', f'mov dx,{tick&65535}', 'mov ah,1', 'int 1ah',
                     'call clock_update', 'push ds', 'pop es', 'mov si,clock_data',
                     f'mov di,results+{index*8}', 'mov cx,3', 'rep movsw',
                     'call clock_set', 'xor ah,ah', 'int 1ah',
                     f'mov [results+{index*8+6}],dx']
        result = helper.run_api('\n'.join(body))
        for index, tick in enumerate(ticks):
            days,minute,hour,hundredth,second = struct.unpack('<H4B',result[index*8:index*8+6])
            cs = tick*65536*100//1193182
            self.assertEqual((days,hour,minute,second,hundredth),
                             (0,cs//360000,(cs//6000)%60,(cs//100)%60,cs%100))
            round_trip = cs*1193182//(65536*100)
            self.assertEqual(struct.unpack_from('<H',result,index*8+6)[0],round_trip&65535)


class DOSDeviceContractTest(unittest.TestCase):
    def test_readonly_driver_switches_stack_and_preserves_registers_flags(self):
        helper = fw.FirmwareTest()
        helper.board = fw.Board()
        source = (fw.BIOS.parent.parent/'os/msdos/io.asm').resolve()
        result = helper.run_api(f"""
            jmp client
            %include "{source}"
        client:
            push ds
            pop es
            mov bx,packet
            push cs
            call strategy
            mov ax,1234h
            mov bx,5678h
            mov cx,9abch
            mov dx,0def0h
            mov si,1111h
            mov di,2222h
            mov bp,3333h
            stc
            std
            pushf
            pop word [before_flags]
            push cs
            call disk_interrupt
            pushf
            pop word [results]
            cld
            mov [results+2],ax
            mov [results+4],bx
            mov [results+6],cx
            mov [results+8],dx
            mov [results+10],si
            mov [results+12],di
            mov [results+14],bp
            mov ax,ss
            mov [results+16],ax
            mov [results+18],sp
            mov ax,[before_flags]
            mov [results+20],ax
            mov ax,[packet+3]
            mov [results+22],ax
            mov ax,[packet+18]
            mov [results+24],ax
            jmp done
        packet:
            db 22,0,8
            dw 0
            times 13 db 0
            dw 5,0
        before_flags: dw 0
        done:
        """)
        values = struct.unpack('<13H',result[:26])
        self.assertEqual(values[0],values[10])
        self.assertEqual(values[1:10],(0x1234,0x5678,0x9abc,0xdef0,0x1111,0x2222,0x3333,0,0xfdfe))
        self.assertEqual(values[11:],(0x8100,0))

@unittest.skipUnless(IMAGE.exists(), 'build DOS with make msdos')
class MSDOSWriteTest(unittest.TestCase):
    def test_files_directories_batch_and_persistence_after_reboot(self):
        session = DOSSession(IMAGE.read_bytes(), writable=True)
        session.boot()
        session.wait_output(b'MS-DOS Version  2.00\r\n\r\nA>')
        session.command('ECHO PERSIST>NOTE.TXT')
        self.assertIn(b'PERSIST', session.command('TYPE NOTE.TXT'))
        self.assertIn(b'1 File(s) copied',session.command('COPY NOTE.TXT COPY.TXT'))
        session.command('REN COPY.TXT RENAMED.TXT')
        self.assertIn(b'PERSIST',session.command('TYPE RENAMED.TXT'))
        session.command('MD TESTDIR')
        session.command('CD TESTDIR')
        self.assertIn(b'A:\\TESTDIR',session.command('CD'))
        self.assertNotIn(b'Invalid',session.command('CD .'))
        session.command('ECHO SUBDIRECTORY>INNER.TXT')
        session.command('CD \\')
        self.assertIn(b'SUBDIRECTORY',session.command('TYPE TESTDIR\\INNER.TXT'))
        session.command('ECHO ECHO BATCH_OK>CHECK.BAT')
        session.command('CHECK',marker=b'\r\nBATCH_OK\r\n')
        # COMMAND may emit two prompts while unwinding a batch invocation.
        for _ in range(10):
            session.step()
        session.command('DEL RENAMED.TXT')
        directory=session.command('DIR')
        self.assertNotIn(b'RENAMED',directory)
        self.assertIn(b'NOTE',directory)
        self.assertFalse(session.board.absent_accesses)
        reboot=DOSSession(bytes(session.image))
        reboot.boot()
        reboot.wait_output(b'MS-DOS Version  2.00\r\n\r\nA>')
        self.assertIn(b'PERSIST',reboot.command('TYPE NOTE.TXT'))
        self.assertIn(b'SUBDIRECTORY',reboot.command('TYPE TESTDIR\\INNER.TXT'))
        self.assertIn(b'MS-DOS Version  2.00',reboot.command('VER'))
