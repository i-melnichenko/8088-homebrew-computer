; HD44780-compatible LCD connected to Exp1.

LCD_COMMAND_PORT equ 0C0h
LCD_DATA_PORT    equ 0C1h
LCD_COLUMNS      equ 20
LCD_LINE1        equ 080h
LCD_LINE2        equ 0C0h
LCD_LINE3        equ 094h
LCD_LINE4        equ 0D4h

lcd_initialize:
    call lcd_power_delay
    mov al, 030h
    call lcd_command
    call lcd_startup_delay
    mov al, 030h
    call lcd_command
    call lcd_delay
    mov al, 030h
    call lcd_command
    mov al, 038h                  ; 8-bit interface, 20x4 module
    call lcd_command
    mov al, 008h
    call lcd_command
    mov al, 001h
    call lcd_command
    call lcd_clear_delay
    mov al, 006h
    call lcd_command
    mov al, 00Ch
    call lcd_command
    ret

; AL is a line-start DDRAM command; DS:SI is a NUL-terminated ASCII string.
; Pad or truncate to the display width so a shorter status clears old text.
lcd_write_line:
    push bx
    mov bx, LCD_COLUMNS
    call lcd_command
.next:
    lodsb
    test al, al
    jz .pad
    call lcd_data
    dec bx
    jnz .next
    jmp short .done
.pad:
    mov al, ' '
    call lcd_data
    dec bx
    jnz .pad
.done:
    pop bx
    ret

lcd_command:
    out LCD_COMMAND_PORT, al
    call lcd_delay
    ret

lcd_data:
    out LCD_DATA_PORT, al
    call lcd_delay
    ret

lcd_power_delay:
    mov cx, 0FFFFh
.wait:
    loop .wait
    ret

lcd_startup_delay:
    mov cx, 02000h
.wait:
    loop .wait
    ret

lcd_clear_delay:
    mov cx, 03000h
.wait:
    loop .wait
    ret

lcd_delay:
    mov cx, 01000h
.wait:
    loop .wait
    ret
