BITS 16
ORG 0200h

; Example payload loaded by bios.asm into SRAM at 0800:0200.
; The BIOS has already initialized the LCD; this code demonstrates that it is
; executing from RAM by replacing the status screen and animating a spinner.

LCD_COMMAND_PORT equ 0C0h
LCD_DATA_PORT    equ 0C1h
LINE1_CMD        equ 080h
LINE2_CMD        equ 0C0h
LINE3_CMD        equ 094h
LINE4_CMD        equ 0D4h
SPINNER_POSITION equ LINE4_CMD + 19

start:
    cli
    cld
    mov ax, cs
    mov ds, ax
    xor ax, ax
    mov ss, ax
    mov sp, 0FFFEh

    mov al, LINE1_CMD
    mov si, line1
    call lcd_write_line
    mov al, LINE2_CMD
    mov si, line2
    call lcd_write_line
    mov al, LINE3_CMD
    mov si, line3
    call lcd_write_line
    mov al, LINE4_CMD
    mov si, line4
    call lcd_write_line

    mov si, spinner
.heartbeat:
    lodsb
    test al, al
    jnz .write_spinner
    mov si, spinner
    lodsb
.write_spinner:
    push ax
    mov al, SPINNER_POSITION
    call lcd_command
    pop ax
    call lcd_data
    call heartbeat_delay
    jmp short .heartbeat

lcd_write_line:
    call lcd_command
.next:
    lodsb
    test al, al
    jz .done
    call lcd_data
    jmp short .next
.done:
    ret

lcd_command:
    out LCD_COMMAND_PORT, al
    call lcd_delay
    ret

lcd_data:
    out LCD_DATA_PORT, al
    call lcd_delay
    ret

lcd_delay:
    mov cx, 01000h
.wait:
    loop .wait
    ret

heartbeat_delay:
    mov cx, 0FFFFh
.wait:
    loop .wait
    ret

line1:   db '8088 RAM PROGRAM', 0
line2:   db 'LOADED BY ROM BIOS', 0
line3:   db 'EXECUTING AT 0800', 0
line4:   db 'CONTROL TRANSFER OK ', 0
spinner: db '/', '-', '|', '-', 0
