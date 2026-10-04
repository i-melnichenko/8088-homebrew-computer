BITS 16
CPU 8086
ORG 0

; EEPROM flasher command loop, assembled separately with ORG 0.
; This small image is embedded in bios.bin, copied to SRAM and entered at zero.
; UART configuration/FIFOs are preserved across the handoff. No ROM services
; or software interrupts are used; only EEPROM data reads touch ROM addresses.
jmp eeprom_start

%include "memory.inc"
%include "state.inc"
%include "uart16550.inc"
%include "uart/framing.inc"
%include "range.inc"
%include "read.inc"
%include "write.inc"
%include "screen.inc"

eeprom_start:
    cli
    cld
    xor ax, ax
    mov ss, ax
    mov sp, SRAM_STACK_TOP
    mov ax, cs
    mov ds, ax
    mov ax, MONITOR_BUFFER_SEGMENT
    mov es, ax
    mov word [es:RX_COUNT], 0
    mov byte [es:RX_DROP], 0
    mov dx, UART_IER
    xor al, al
    out dx, al
    mov byte [es:SCREEN_POSITION], 80
    mov bx, eeprom_screen_ready
    xor dx, dx
    call eeprom_screen_update
    ; The request remains in DECODE_BUFFER. ACK is generated entirely in RAM.
    call monitor_reply

eeprom_loop:
    call monitor_read_frame
    jc .screen
    call eeprom_dispatch
.screen:
    call eeprom_screen_step
    jmp eeprom_loop

eeprom_dispatch:
    mov al, [es:DECODE_BUFFER + 1]
    cmp al, PROTO_PING
    je eeprom_ping
    cmp al, PROTO_RESET
    je eeprom_reset
    cmp al, PROTO_BIOS_READ
    jne .write
    jmp eeprom_read
.write:
    cmp al, PROTO_BIOS_WRITE
    jne .mode
    jmp eeprom_write
.mode:
    cmp al, PROTO_BIOS_FLASHER_MODE
    je eeprom_mode_retry
    jmp eeprom_blocked

eeprom_ping:
    cmp word [es:DECODE_BUFFER + 3], 0
    jne eeprom_common_error
    xor al, al
    jmp monitor_reply

eeprom_reset:
    cmp word [es:DECODE_BUFFER + 3], 0
    jne eeprom_common_error
    ; Rev1 never writes EEPROM, so leaving the read-only monitor is safe.
    xor al, al
    call monitor_reply
    call monitor_wait_tx_empty
    jmp ROM_SEGMENT:0000h

eeprom_mode_retry:
    ; A lost entry ACK may be retried; no fresh entry/restart is accepted here.
    cmp word [es:DECODE_BUFFER + 3], 0
    jne .blocked
    mov al, [es:DECODE_BUFFER + 2]
    cmp al, [es:EEPROM_MODE_SEQUENCE]
    jne .blocked
    xor al, al
    jmp monitor_reply
.blocked:
eeprom_blocked:
    mov bx, eeprom_screen_blocked
    xor dx, dx
    call eeprom_screen_update
    mov al, 20
    jmp monitor_reply

eeprom_common_error:
    mov bx, eeprom_screen_bad_request
    xor dx, dx
    call eeprom_screen_update
    mov al, 5
    jmp monitor_reply
