; Monitor buffers/state are in RAM_SEGMENT. DS addresses ROM in the normal
; monitor and the executing RAM image in EEPROM mode.
MONITOR_BUFFER_SEGMENT equ RAM_SEGMENT
RX_BUFFER             equ 0000h
DECODE_BUFFER         equ 0090h
SCREEN_BUFFER         equ 0120h       ; four fixed-width, 20-byte LCD rows
MONITOR_MAX_PACKET    equ 144
MONITOR_TOTAL         equ 0170h
MONITOR_RECEIVED      equ 0172h
MONITOR_NEXT          equ 0174h
RX_COUNT              equ 0176h
RX_DROP               equ 0178h
MENU_SELECTED         equ 0179h
MENU_PAGE             equ 017Ah
MENU_FLAGS            equ 017Bh       ; bit 0: this reply launches a program
LAST_WRITE_CRC        equ 017Ch
PROGRAM_ENTRY         equ 017Eh
MEM_ADDR              equ 0180h
MEM_PHASE             equ 0182h
MEM_ERROR             equ 0183h
MEM_EXPECTED          equ 0184h
MEM_ACTUAL            equ 0185h
MEM_FAIL_ADDR         equ 0186h
MENU_DIRTY            equ 0188h
DELETE_VALID          equ 0189h
DELETE_SEQUENCE       equ 018Ah
DELETE_ID             equ 018Bh
LAST_WRITE_ADDR       equ 018Ch       ; BIOS-owned address for retry comparison
LAST_WRITE_LENGTH     equ 018Eh
LAST_WRITE_SEQUENCE   equ 0190h
LAST_WRITE_VALID      equ 0191h
PROGRAM_FAR           equ 0192h
SCREEN_POSITION       equ 0196h
SCREEN_COMMAND        equ 0197h
MEM_OK                equ 0198h       ; uint32 successful byte verifications
MEM_BAD               equ 019Ch       ; uint32 failed byte verifications
MEM_BLOCK             equ 01A0h       ; most recently processed 16-byte block
PROGRAM_TABLE         equ 01A2h       ; 4 records: name[16], address:u16, size:u16
PROGRAM_RECORD_SIZE   equ 20
PROGRAM_CAPACITY      equ 4
PROGRAM_NAME_SIZE     equ 16
PROGRAM_COUNT         equ 01F2h
ACTIVE_ID             equ 01F3h       ; 1-based program ID / viewed upload
UPLOAD_ACTIVE         equ 01F4h
EXEC_SEQUENCE         equ 01F5h
UPLOAD_CRC            equ 01F8h
SAVED_STACK           equ 01FAh
EXEC_ID               equ 01FCh
EXEC_VALID            equ 01FDh
INFO_TOP              equ 01FEh       ; first visible row of the Info list

; Flasher entry sequence in unused space after the API keyboard state.
EEPROM_MODE_SEQUENCE  equ 0260h
EEPROM_STATE_END      equ 0261h

; Reserve four bytes at the end of the API state page for menu-only blink.
; Core monitor state still fits in 0000..01FF; API framebuffer is untouched.
MENU_BLINK_TICKS       equ PROGRAM_OFFSET - 4
MENU_BLINK_PHASE       equ PROGRAM_OFFSET - 2 ; 0=visible, 1=hidden
MENU_BLINK_PENDING     equ PROGRAM_OFFSET - 1 ; 0=idle, 1=command, 2=data
MENU_BLINK_PERIOD      equ 2048        ; idle-loop ticks, not a hardware clock

%if EEPROM_STATE_END > MENU_BLINK_TICKS
    %error EEPROM state overlaps menu blink state
%endif

%if PROGRAM_TABLE + PROGRAM_RECORD_SIZE * PROGRAM_CAPACITY != PROGRAM_COUNT
    %error Program table overlaps BIOS state
%endif
%if INFO_TOP >= BIOS_STATE_SIZE
    %error BIOS state overlaps payload RAM
%endif

PAGE_MENU equ 0
PAGE_INFO equ 1
PAGE_PROGRAM equ 2
PAGE_CONFIRM equ 3
PAGE_TEST equ 4
PAGE_RESULT equ 5
PAGE_NO_BOOT equ 6
PAGE_CANCELLED equ 7
PAGE_CUSTOM equ 8
MENU_ITEMS equ 4                       ; default boot, custom programs, memory, info

%include "uart/constants.inc"

KEY_UP equ 1
KEY_DOWN equ 2
KEY_ENTER equ 3
KEY_BACK equ 4
