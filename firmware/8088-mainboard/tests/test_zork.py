"""Optional Zork I integration through real Microsoft DOS and UART disk I/O."""
import unittest
import test_bios as fw
from test_msdos import DOSSession

IMAGE = fw.BIOS.with_name('msdos20-zork.img')


@unittest.skipUnless(IMAGE.exists(), 'build the Zork image with make msdos-zork')
class ZorkTest(unittest.TestCase):
    def test_exe_loader_fcb_paging_gameplay_and_return_to_dos(self):
        session = DOSSession(IMAGE.read_bytes())
        session.boot()
        session.wait_output(b'MS-DOS Version  2.00\r\n\r\nA>')
        opening = session.command('ZORK1', marker=b'\r\n>')
        self.assertIn(b'ZORK I: The Great Underground Empire', opening)
        self.assertIn(b'Release 119 / Serial number 880429', opening)
        self.assertIn(b'West of House', opening)
        self.assertIn(b'small mailbox', opening)
        self.assertIn(b'West of House', session.command('look', marker=b'\r\n>'))
        self.assertIn(b'reveals a leaflet', session.command('open mailbox', marker=b'\r\n>'))
        self.assertIn(b'WELCOME TO ZORK!', session.command('read leaflet', marker=b'\r\n>'))
        self.assertIn(b'North of House', session.command('north', marker=b'\r\n>'))
        session.command('quit', marker=b'(Y is affirmative): >')
        session.command('y')
        self.assertIn(b'MS-DOS Version  2.00', session.command('VER'))
        self.assertFalse(session.board.absent_accesses)

    def test_save_restore_and_restore_after_reboot(self):
        session = DOSSession(IMAGE.read_bytes(), writable=True)
        session.boot()
        session.wait_output(b'MS-DOS Version  2.00\r\n\r\nA>')
        session.command('ZORK1', marker=b'\r\n>')
        self.assertIn(b'North of House',session.command('north',marker=b'\r\n>'))
        session.command('save',marker=b'SAV): ')
        self.assertIn(b'Ok.',session.command('',marker=b'\r\n>'))
        self.assertIn(b'West of House',session.command('west',marker=b'\r\n>'))
        session.command('restore',marker=b'SAV): ')
        self.assertIn(b'Ok.',session.command('',marker=b'\r\n>'))
        self.assertIn(b'North of House',session.command('look',marker=b'\r\n>'))
        session.command('quit',marker=b'(Y is affirmative): >')
        session.command('y')
        self.assertIn(b'ZORK1',session.command('DIR *.SAV'))
        reboot=DOSSession(bytes(session.image))
        reboot.boot()
        reboot.wait_output(b'MS-DOS Version  2.00\r\n\r\nA>')
        reboot.command('ZORK1',marker=b'\r\n>')
        reboot.command('restore',marker=b'SAV): ')
        reboot.command('',marker=b'\r\n>')
        self.assertIn(b'North of House',reboot.command('look',marker=b'\r\n>'))
        self.assertFalse(session.board.absent_accesses)
        self.assertFalse(reboot.board.absent_accesses)
