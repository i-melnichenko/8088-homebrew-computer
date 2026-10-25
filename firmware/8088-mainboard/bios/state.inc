; Monitor buffers/state are in RAM_SEGMENT. DS addresses ROM in the normal
; monitor and the executing RAM image in EEPROM mode.
RX_BUFFER             equ 0000h
DECODE_BUFFER         equ 0090h
SCREEN_BUFFER         equ 0120h       ; four fixed-width, 20-byte LCD rows
MONITOR_MAX_PACKET    equ 144
RX_COUNT              equ 0176h
RX_DROP               equ 0178h
MENU_SELECTED         equ 0179h
MENU_PAGE             equ 017Ah
MENU_FLAGS            equ 017Bh       ; bit 0: this reply starts OS boot
MEM_ADDR              equ 0180h
MEM_PHASE             equ 0182h
MEM_ERROR             equ 0183h
MEM_EXPECTED          equ 0184h
MEM_ACTUAL            equ 0185h
MEM_FAIL_ADDR         equ 0186h
MENU_DIRTY            equ 0188h
FLASHER_FAR           equ 0192h
SCREEN_POSITION       equ 0196h
SCREEN_COMMAND        equ 0197h
MEM_OK                equ 0198h       ; uint32 successful byte verifications
MEM_BAD               equ 019Ch       ; uint32 failed byte verifications
MEM_BLOCK             equ 01A0h       ; most recently processed 16-byte block
IRQ_TEST_COUNT        equ 01F6h       ; IRQ0 handler counter (u16)
INFO_TOP              equ 01FEh       ; first visible row of the Info list

IRQ_TEST_SEQUENCE     equ 01FFh

; Flasher entry sequence in unused space after the API keyboard state.
EEPROM_MODE_SEQUENCE  equ 0260h
EEPROM_STATE_END      equ 0261h
MEM_ADDR_HI           equ 0262h       ; physical address bits 16..19
MEM_BLOCK_HI          equ 0263h
MEM_FAIL_HI           equ 0264h
RAM_BANK_MASK         equ 0265h       ; uint16 bitmap, bits 0..4 = tested banks
RAM_TEST_STATE        equ 0267h       ; 0=idle, 1=running, 2=done, 3=cancelled
RAM_TEST_UNITS        equ 0268h       ; uint32 processed 16-byte slices, all phases
RAM_TEST_SEQUENCE     equ 026Ch
RAM_TEST_START_VALID  equ 026Dh       ; immediate START retry cache
RAM_TEST_REMOTE       equ 026Eh       ; UART-owned test requires heartbeat
RAM_TEST_HEARTBEAT    equ 026Fh       ; remaining monitor iterations (u16)
; Terminal output queue plus retry snapshot, within the protected state page.
TERMINAL_QUEUE        equ 0271h       ; 64 bytes
TERMINAL_REPLAY       equ 02B1h       ; 64-byte last poll snapshot
TERMINAL_COUNT        equ 02F1h
TERMINAL_LENGTH       equ 02F2h
TERMINAL_SEQUENCE     equ 02F3h
TERMINAL_VALID        equ 02F4h
TERMINAL_FLAGS        equ 02F5h
TERMINAL_CAPACITY     equ 64
PIC_PRESENT           equ 02F6h       ; POST IMR read/write check, 0=not detected, 1=detected
PIT_PRESENT           equ 02F7h       ; 8254 read-back status check
PIT_RUNNING           equ 02F8h       ; counter 0 decreased during POST
IRQ_TEST_RESULT       equ 02F9h
IRQ_TEST_VALID        equ 02FAh
MEM_STATE_END         equ 02FBh
RAM_TEST_HEARTBEAT_TICKS equ 32768     ; software budget, not wall-clock time

; Reserve four bytes at the end of the API state page for menu-only blink.
; Core monitor state still fits in 0000..01FF; API framebuffer is untouched.
MENU_BLINK_TICKS       equ STATE_END_OFFSET - 4
MENU_BLINK_PHASE       equ STATE_END_OFFSET - 2 ; 0=visible, 1=hidden
MENU_BLINK_PENDING     equ STATE_END_OFFSET - 1 ; 0=idle, 1=command, 2=data
MENU_BLINK_PERIOD      equ 2048        ; idle-loop ticks, not a hardware clock

%if MEM_STATE_END > MENU_BLINK_TICKS
    %error EEPROM state overlaps menu blink state
%endif

%if INFO_TOP >= BIOS_STATE_SIZE
    %error BIOS state overlaps payload RAM
%endif

PAGE_MENU equ 0
PAGE_INFO equ 1
PAGE_CONFIRM equ 2
PAGE_TEST equ 3
PAGE_RESULT equ 4
PAGE_NO_BOOT equ 5
PAGE_CANCELLED equ 6
MENU_ITEMS equ 3                       ; default boot, memory, info

%include "uart/constants.inc"

KEY_UP equ 1
KEY_DOWN equ 2
KEY_ENTER equ 3
KEY_BACK equ 4
