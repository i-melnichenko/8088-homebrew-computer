BITS 16
ORG 0

; Minimal ROM BIOS for the 8088 mainboard.
;
; It initializes the HD44780 LCD on Exp1, copies ram-program.bin from the
; EEPROM into SRAM, then transfers control to the copied program.

LCD_COMMAND_PORT equ 0C0h
LCD_DATA_PORT    equ 0C1h

; The 32 KiB SRAM is mapped at physical 08000h..0FFFFh.
; Keep the program clear of the stack at physical 0FFFEh.
RAM_SEGMENT      equ 0800h
PROGRAM_OFFSET   equ 0200h
SRAM_STACK_TOP   equ 0FFFEh

LINE1_CMD equ 080h
LINE2_CMD equ 0C0h
LINE3_CMD equ 094h
LINE4_CMD equ 0D4h
COPY_BAR_COLUMN equ 6
COPY_BAR_WIDTH  equ 10

start:
    cli
    cld

    ; The reset vector enters at F800:0000.  The BIOS itself stays in ROM;
    ; its temporary stack is in SRAM.
    xor ax, ax
    mov ss, ax
    mov sp, SRAM_STACK_TOP
    mov ax, cs
    mov ds, ax

    call lcd_initialize

    mov al, LINE1_CMD
    mov si, bios_line1
    call lcd_write_line
    mov al, LINE2_CMD
    mov si, bios_line2
    call lcd_write_line
    mov al, LINE3_CMD
    mov si, bios_line3
    call lcd_write_line
    mov al, LINE4_CMD
    mov si, bios_line4_copy
    call lcd_write_line

    ; Copy in visible chunks so the LCD confirms that RAM loading progresses.
    call copy_program

    mov al, LINE4_CMD
    mov si, bios_line4
    call lcd_write_line
    call visible_delay

    ; The program was assembled for this exact segment:offset address.
    jmp RAM_SEGMENT:PROGRAM_OFFSET

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
    mov al, 038h                    ; 8-bit interface, display enabled
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

; AL is a DDRAM command; DS:SI is a NUL-terminated ASCII string.
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

; Makes the RAM-copy bar observable instead of completing in a few ms.
progress_delay:
    mov cx, 04000h
.wait:
    loop .wait
    ret

visible_delay:
    mov cx, 0FFFFh
.wait:
    loop .wait
    ret

bios_line1: db '8088 ROM BIOS', 0
bios_line2: db 'LCD INITIALIZED', 0
bios_line3: db 'RAM: [          ]', 0
bios_line4_copy: db 'COPYING PROGRAM...', 0
bios_line4: db 'STARTING PROGRAM...', 0

; Payload generated from ram-program.asm.  It is copied verbatim to
; RAM_SEGMENT:PROGRAM_OFFSET before the far jump above.
program_image:
    incbin "ram-program.bin"
program_image_end:

; Refuse to build an image if a replacement payload would reach the stack.
%if program_image_end - program_image > 07DFEh
    %error "RAM payload overlaps the SRAM stack"
%endif

PROGRAM_SIZE equ program_image_end - program_image
; Round up: the full payload is copied in at most COPY_BAR_WIDTH chunks.
COPY_CHUNK equ (PROGRAM_SIZE + COPY_BAR_WIDTH - 1) / COPY_BAR_WIDTH

; Copy the embedded payload to RAM and replace one space in the progress bar
; after every chunk.  DS remains the ROM segment throughout this routine.
copy_program:
    mov ax, RAM_SEGMENT
    mov es, ax
    mov si, program_image
    mov di, PROGRAM_OFFSET
    mov bp, PROGRAM_SIZE
    mov dl, LINE3_CMD + COPY_BAR_COLUMN
    mov bx, COPY_BAR_WIDTH
.next_chunk:
    mov cx, COPY_CHUNK
    cmp bp, cx
    jae .copy
    mov cx, bp
.copy:
    sub bp, cx
    rep movsb

    ; LCD routines use CX for their delay, so keep the copy state in BP/BX.
    push dx
    mov al, dl
    call lcd_command
    mov al, '#'
    call lcd_data
    call progress_delay
    pop dx
    inc dl
    dec bx
    jnz .next_chunk
    ret

; Reset vector at physical FFFF0h = EEPROM offset 7FF0h.
times 7FF0h - ($ - $$) db 0FFh
    jmp 0F800h:0000h

; Full 32 KiB image for the 28C256 EEPROM.
times 8000h - ($ - $$) db 0FFh
