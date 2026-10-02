BITS 16
CPU 8086
ORG 0

; UART-to-LCD terminal for the combined LCD and UART BIOS.

; BIOS supplies hardware drivers through real software interrupts.
%include "bios_api.inc"

start:
    cli
    cld
    mov ax, cs
    mov ds, ax

    bios_lcd_clear
    bios_lcd_line 0, terminal_title
    bios_lcd_line 1, terminal_hint
    bios_uart_print greeting

    mov byte [cursor_line], INPUT_FIRST_LINE
    mov byte [cursor_column], 0

.read:
    bios_key_read
    cmp al, 1Bh                   ; Escape returns to BIOS.
    je .exit
    cmp al, 0Dh                   ; Enter from a serial terminal
    je .newline
    cmp al, 0Ah                   ; Ignore the LF in CR/LF
    je .read
    cmp al, 08h                   ; Backspace
    je .backspace
    cmp al, 7Fh                   ; Delete often sent as Backspace
    je .backspace
    cmp al, ' '
    jb .read                       ; Ignore other control characters
    cmp al, 7Eh
    ja .read

    call lcd_putc_at_cursor
    bios_uart_putc                 ; Echo to the PC terminal.
    call cursor_advance
    jmp short .read

.newline:
    call cursor_newline
    mov al, 13
    bios_uart_putc
    mov al, 10
    bios_uart_putc
    jmp short .read

.backspace:
    call cursor_backspace
    jmp short .read
.exit:
    retf

; Write AL at the current LCD cursor position. AL is preserved for UART echo.
lcd_putc_at_cursor:
    push dx
    bios_lcd_cell [cursor_line], [cursor_column], al
    pop dx
    ret

lcd_set_cursor:
    push ax
    push dx
    mov dh, [cursor_line]
    mov dl, [cursor_column]
    bios_set_cursor
    pop dx
    pop ax
    ret

cursor_advance:
    inc byte [cursor_column]
    cmp byte [cursor_column], BIOS_LCD_COLUMNS
    jb .done
    call cursor_newline
.done:
    ret

cursor_newline:
    mov byte [cursor_column], 0
    inc byte [cursor_line]
    cmp byte [cursor_line], INPUT_LAST_LINE + 1
    jb .done
    mov byte [cursor_line], INPUT_FIRST_LINE
.done:
    ret

cursor_backspace:
    cmp byte [cursor_column], 0
    jne .previous_column
    cmp byte [cursor_line], INPUT_FIRST_LINE
    je .done
    dec byte [cursor_line]
    mov byte [cursor_column], BIOS_LCD_COLUMNS - 1
    jmp short .erase
.previous_column:
    dec byte [cursor_column]
.erase:
    call lcd_set_cursor
    mov al, ' '
    call lcd_putc_at_cursor
    call lcd_set_cursor
    mov al, 08h
    bios_uart_putc
    mov al, ' '
    bios_uart_putc
    mov al, 08h
    bios_uart_putc
.done:
    ret

INPUT_FIRST_LINE  equ 2
INPUT_LAST_LINE   equ 3

terminal_title: db 'UART TERMINAL', 0
terminal_hint:  db 'TYPE ON PC  ESC:BACK', 0
greeting: db 'UART-to-LCD terminal ready', 13, 10, 0

cursor_line:    db 0
cursor_column:  db 0
