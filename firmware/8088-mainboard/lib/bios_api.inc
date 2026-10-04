%ifndef BIOS_API_INC
%define BIOS_API_INC
; Lightweight SDK: only constants and INT wrappers, no hardware drivers.
BIOS_INT_VIDEO equ 10h
BIOS_INT_SERIAL equ 14h
BIOS_INT_KEYBOARD equ 16h
BIOS_INT_BOARD equ 60h
BIOS_API_VERSION equ 1
BIOS_API_SIGNATURE equ 0B105h
BIOS_API_FEATURES equ 001Fh
BIOS_API_FEATURE_HOST_KEYBOARD equ 0010h
BIOS_LCD_COLUMNS equ 20
BIOS_LCD_ROWS equ 4

%macro bios_lcd_clear 0
    mov ah, 01h
    int BIOS_INT_BOARD
%endmacro
%macro bios_lcd_line 2
    mov dh, %1
    mov si, %2
    mov ah, 02h
    int BIOS_INT_BOARD
%endmacro
%macro bios_lcd_glyph 2
    mov al, %1
    mov si, %2
    mov ah, 03h
    int BIOS_INT_BOARD
%endmacro
%macro bios_lcd_cell 3
    mov dh, %1
    mov dl, %2
    mov al, %3
    mov ah, 04h
    int BIOS_INT_BOARD
%endmacro
%macro bios_set_cursor 0             ; DX = row:column, preserve BX
    push bx
    xor bh, bh
    mov ah, 02h
    int BIOS_INT_VIDEO
    pop bx
%endmacro
%macro bios_putc 0                   ; AL = character, preserve BX
    push bx
    xor bh, bh
    mov ah, 0Eh
    int BIOS_INT_VIDEO
    pop bx
%endmacro
%macro bios_print 1                  ; ASCIZ via board extension
    mov si, %1
    mov ah, 05h
    int BIOS_INT_BOARD
%endmacro
%macro bios_key_read 0
    xor ah, ah
    int BIOS_INT_KEYBOARD
%endmacro
%macro bios_key_peek 0               ; ZF=1 if empty, otherwise AX=key
    mov ah, 01h
    int BIOS_INT_KEYBOARD
%endmacro
%macro bios_uart_putc 0              ; AL = byte, return AH=status
    push dx
    xor dx, dx
    mov ah, 01h
    int BIOS_INT_SERIAL
    pop dx
%endmacro
%macro bios_uart_print 1
    pushf
    cld
    push ax
    push dx
    push si
    xor dx, dx
    mov si, %1
%%next:
    lodsb
    test al, al
    jz %%done
    mov ah, 01h
    int BIOS_INT_SERIAL
    jmp %%next
%%done:
    pop si
    pop dx
    pop ax
    popf
%endmacro
%endif
