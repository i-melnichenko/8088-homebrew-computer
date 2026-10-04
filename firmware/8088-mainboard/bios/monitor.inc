; ROM monitor. Drivers and memory constants are included by bios/main.asm.
; Packet buffers and upload state remain in RAM_SEGMENT:0000..01FF.
monitor_ready:
    mov ax, RAM_SEGMENT
    mov es, ax
    xor ax, ax
    xor di, di
    mov cx, 256
    rep stosw
    mov byte [es:MENU_DIRTY], 1
    mov si, greeting
    call uart_print

; Revision 1 uses polling; the board does not provide UART interrupts.
monitor_loop:
    call monitor_read_frame
    jc .idle
    call monitor_dispatch
.idle:
    call keyboard_menu_step
    test al, al
    jz .no_keyboard_boot
    call boot_program
.no_keyboard_boot:
    call memory_step
    call menu_render
    call menu_blink_step
    call menu_lcd_step
    jmp monitor_loop
