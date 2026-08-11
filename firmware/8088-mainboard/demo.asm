BITS 16
ORG 0

; LCD status demo for the 8088 Homebrew Computer.
;
; Hardware: an HD44780-compatible 20x4 LCD display module on Exp1.
; Exp1 occupies I/O ports C0h..DFh; A0 selects the LCD register:
;   C0h - command register (RS = 0)
;   C1h - data register    (RS = 1)
;
; The program draws a four-line status screen and then updates a spinner in
; the last character position. The spinner is a simple CPU/display heartbeat.
; See README.md for build and programming instructions.

LCD_COMMAND_PORT equ 0C0h
LCD_DATA_PORT    equ 0C1h
LCD_COLUMNS      equ 20

; Stack is in SRAM: 0000:SRAM_STACK_TOP = physical 0FFFEh
SRAM_STACK_TOP   equ 0FFFEh

; Standard 20x4 HD44780 DDRAM line addresses.
LINE1_CMD equ 080h
LINE2_CMD equ 0C0h
LINE3_CMD equ 094h
LINE4_CMD equ 0D4h
SPINNER_COLUMN equ LCD_COLUMNS - 1

start:
    ; Disable maskable interrupts during early initialization.
    cli
    ; Process strings from low to high addresses.
    cld

    ; Initialize the SRAM stack and the data segment.
    xor ax, ax
    mov ss, ax
    mov sp, SRAM_STACK_TOP
    mov ax, cs
    mov ds, ax

    call lcd_power_delay

    ; HD44780: 8-bit bus; N=1 ("2-line" mode, required for 20x4 displays).
    mov al, 030h
    call lcd_command
    call lcd_startup_delay
    mov al, 030h
    call lcd_command
    call lcd_delay
    mov al, 030h
    call lcd_command
    mov al, 038h
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
    mov al, LINE4_CMD + SPINNER_COLUMN
    call lcd_command
    pop ax
    call lcd_data
    call heartbeat_delay
    jmp short .heartbeat

; Write the NUL-terminated string at DS:SI after AL has set its DDRAM address.
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

; Conservative delays, safe at the default 4.77 MHz clock and lower speeds.
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

; Delay between spinner updates (~0.25 s at 4.77 MHz).
heartbeat_delay:
    mov cx, 0FFFFh
.wait:
    loop .wait
    ret

line1:   db '8088 HOME COMPUTER', 0
line2:   db 'RAM: 32 KiB', 0
line3:   db 'LCD: EXP1 C0/C1', 0
line4:   db 'CPU: RUNNING        ', 0
spinner: db '/', '-', '|', '-', 0

; Reset vector at physical FFFF0h maps to 28C256 offset 7FF0h.
; Jump to F800:0000 so CS matches the ROM segment.
times 7FF0h - ($ - $$) db 0FFh
    jmp 0F800h:0000h

; Pad the image to the 28C256's full 32 KiB capacity.
times 8000h - ($ - $$) db 0FFh
