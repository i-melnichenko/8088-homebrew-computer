; ROM monitor. Drivers and memory constants are included by bios/main.asm.
; Packet buffers and upload state remain in RAM_SEGMENT:0000..01FF.
monitor_ready:
    LOAD_STATE ax
    mov es, ax
    xor ax, ax
    xor di, di
    mov cx, 256
    rep stosw
    mov byte [es:IRQ_TEST_VALID], 0
    call pic_detect
    call pit_detect
    mov [es:RAM_BANK_MASK], bp
    mov byte [es:RAM_TEST_STATE], 0
    mov byte [es:RAM_TEST_START_VALID], 0
    mov byte [es:RAM_TEST_REMOTE], 0
    mov word [es:RAM_TEST_HEARTBEAT], 0
    mov word [es:RAM_TEST_UNITS], 0
    mov word [es:RAM_TEST_UNITS + 2], 0
    mov byte [es:MEM_ADDR_HI], 0
    mov byte [es:MEM_BLOCK_HI], 0
    mov byte [es:MEM_FAIL_HI], 0
    ; BDA conventional size covers contiguous banks starting at physical zero.
    mov dx, bp
    xor ax, ax
.contiguous:
    shr dx, 1
    jnc .size
    add ax, 128
    jmp .contiguous
.size:
    mov [ss:0413h], ax
    mov byte [es:MENU_DIRTY], 1
    call runtime_initialize
    mov si, greeting
    call uart_print

; Foreground protocol/menu work; optional IRQs collect time and UART bytes.
monitor_loop:
    call monitor_read_frame
    jc .idle
    call monitor_dispatch
.idle:
    call keyboard_menu_step
    test al, al
    jnz pc_boot
    call memory_step
    call menu_render
    call menu_blink_step
    call menu_lcd_step
    jmp monitor_loop
