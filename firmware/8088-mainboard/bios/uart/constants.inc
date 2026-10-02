; Wire protocol v1 constants shared by BIOS and the RAM flasher.
PROTO_VERSION equ 1
PROTO_REPLY   equ 80h
PROTO_MAX_PAYLOAD equ 128
PROTO_UPLOAD_BLOCK equ PROTO_MAX_PAYLOAD

PROTO_PING                    equ 01h
PROTO_INFO                    equ 02h
PROTO_RESET                   equ 03h

PROTO_BIOS_FLASHER_MODE        equ 04h
PROTO_BIOS_WRITE              equ 05h
PROTO_BIOS_READ               equ 06h

PROTO_MEMORY_READ             equ 07h
PROTO_MEMORY_WRITE            equ 08h

PROTO_PROGRAM_LIST            equ 09h
PROTO_PROGRAM_UPLOAD_BEGIN    equ 0Ah
PROTO_PROGRAM_UPLOAD_DATA     equ 0Bh
PROTO_PROGRAM_UPLOAD_COMMIT   equ 0Ch
PROTO_PROGRAM_UPLOAD_ABORT    equ 0Dh
PROTO_PROGRAM_EXEC            equ 0Eh
PROTO_PROGRAM_DELETE          equ 0Fh
PROTO_PROGRAM_RENAME          equ 10h

PROTO_KEYBOARD_MODE           equ 11h
PROTO_KEYBOARD_EVENT          equ 12h
