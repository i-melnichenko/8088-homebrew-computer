; Wire protocol 2.0 constants shared by BIOS and the RAM flasher.
PROTO_VERSION equ 2                 ; binary framing major
%define PROTO_VERSION_TEXT "2.0"      ; compatible command-set revision
PROTO_REPLY   equ 80h
PROTO_MAX_PAYLOAD equ 128

PROTO_PING                    equ 01h
PROTO_INFO                    equ 02h
PROTO_RESET                   equ 03h

PROTO_BIOS_FLASHER_MODE        equ 04h
PROTO_BIOS_WRITE              equ 05h
PROTO_BIOS_READ               equ 06h

PROTO_MEMORY_READ             equ 07h
PROTO_MEMORY_WRITE            equ 08h


PROTO_KEYBOARD_MODE           equ 09h
PROTO_KEYBOARD_EVENT          equ 0Ah

PROTO_RAM_INFO                equ 0Bh
PROTO_RAM_TEST                equ 0Ch
RAM_TEST_CONFIRM              equ 0A5h

PROTO_TERMINAL_POLL           equ 0Dh

PROTO_IRQ_TEST                equ 0Eh

STATUS_PIC_MISSING equ 21
STATUS_PIT_MISSING equ 22
STATUS_RAM0_REQUIRED equ 23
STATUS_PIT_NOT_COUNTING equ 24

PROTO_DISK_MOUNT equ 0Fh
PROTO_DISK_BOOT equ 10h
PROTO_DISK_POLL equ 11h
PROTO_DISK_DATA equ 12h

PROTO_DISK_WRITE_DATA equ 13h
PROTO_DISK_WRITE_ACK equ 14h
