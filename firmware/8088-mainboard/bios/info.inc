; Informational rows, not selectable actions. Reserve the last LCD column
; for a rail; scroll one row at a time, with no wrap at either end.
info_render:
    xor ax, ax
    mov al, [es:INFO_TOP]
    mov bp, ax
    mov dx, 3
.row:
    mov bx, bp
    shl bx, 1
    mov si, [info_items + bx]
    push di
    call info_line
    pop bx
.next:
    inc bp
    dec dx
    jnz .row
    xor ax, ax
    mov al, [es:INFO_TOP]
    mov bx, INFO_ITEM_COUNT
    jmp menu_scroll_rail

; Dynamic values use the same POST state and runtime flags as UART INFO.
info_line:
    cmp si, info_ram
    je .ram
    cmp si, info_banks
    je .banks
    cmp si, info_work_bank
    je .work_bank
    cmp si, info_int_available
    je .interrupts
    cmp si, info_pic_missing
    je .pic
    cmp si, info_pit_missing
    je .pit
    cmp si, info_pit_unavailable
    je .counter
    cmp si, info_timer_software
    je .timer
    cmp si, info_uart_polling
    je .uart
    jmp menu_line
.interrupts:
    mov ax, ss
    test ax, ax
    jz .line
    mov si, info_int_unavailable
    jmp .line
.pic:
    cmp byte [es:PIC_PRESENT], 1
    jne .line
    mov si, info_pic_masked
    cmp byte [ss:RT_FLAGS], 0
    je .line
    mov si, info_pic_active
    jmp .line
.pit:
    cmp byte [es:PIT_PRESENT], 1
    jne .line
    mov si, info_pit_present
    jmp .line
.counter:
    cmp byte [es:PIT_PRESENT], 1
    jne .line
    mov si, info_pit_stopped
    cmp byte [es:PIT_RUNNING], 1
    jne .line
    mov si, info_pit_running
    jmp .line
.timer:
    test byte [ss:RT_FLAGS], RT_TIMER
    jz .line
    mov si, info_timer_irq
    jmp .line
.uart:
    test byte [ss:RT_FLAGS], RT_UART
    jz .line
    mov si, info_uart_irq
.line:
    jmp menu_line
.work_bank:
    push di
    call menu_line
    pop bx
    mov ax, ss
    mov cl, 13
    shr ax, cl
    add al, '0'
    mov [es:bx + 11], al
    ret
.banks:
    push dx
    push di
    call menu_line
    pop bx
    push di
    mov di, bx
    add di, 7
    mov al, [es:RAM_BANK_MASK]
    call menu_hex8
    inc di
    mov bl, [es:RAM_BANK_MASK]
    mov cx, 5
    xor dh, dh
    mov dl, '0'
.bank:
    shr bl, 1
    jnc .next_bank
    test dh, dh
    jz .bank_digit
    mov al, ','
    stosb
.bank_digit:
    mov al, dl
    stosb
    mov dh, 1
.next_bank:
    inc dl
    loop .bank
    pop di
    pop dx
    ret
.ram:
    push dx
    push di
    call menu_line
    xor ax, ax
    mov bl, [es:RAM_BANK_MASK]
    mov cx, 5
.count:
    shr bl, 1
    jnc .next
    add ax, 128
.next:
    loop .count
    pop bx
    add bx, 7                       ; last of three digits after "RAM: "
    mov si, 10
    mov cx, 3
.decimal:
    xor dx, dx
    div si
    add dl, '0'
    mov [es:bx], dl
    dec bx
    loop .decimal
    pop dx
    ret

info_items:
    dw info_version, info_board, info_cpu, info_ram, info_rom, info_lcd
    dw info_uart, info_wire_version, info_api_version
    dw info_rom_address, info_uart_address, info_lcd_address
    dw info_banks, info_work_bank, info_int_available
    dw info_pic_missing, info_pit_missing, info_pit_unavailable
    dw info_timer_software, info_uart_polling
info_items_end:
INFO_ITEM_COUNT equ (info_items_end - info_items) / 2

info_title: db 'INFO', 0
info_version: db 'BIOS: ', BIOS_VERSION, 0
info_board: db 'BOARD: REV 2', 0
info_cpu: db 'CPU: 8088', 0
info_ram: db 'RAM: --- KiB', 0
info_rom: db 'ROM: 32 KiB', 0
info_lcd: db 'LCD: 20x4', 0
info_uart: db 'UART: 9600 8N1', 0
info_wire_version: db 'PROTOCOL: ', PROTO_VERSION_TEXT, 0
info_api_version: db 'API: ', '0' + BIOS_API_VERSION, 0
info_rom_address: db 'ROM AT: F800:0000', 0
info_uart_address: db 'UART I/O: 3F8-3FF', 0
info_lcd_address: db 'LCD I/O: 80-81', 0
info_banks: db 'BANKS: -- ', 0
info_work_bank: db 'WORK BANK: 0', 0
info_int_available: db 'SOFT INT: AVAILABLE', 0
info_int_unavailable: db 'SOFT INT: NO RAM0', 0
info_pic_missing: db 'PIC: NOT DETECTED', 0
info_pic_masked: db 'PIC: YES, MASKED', 0
info_pic_active: db 'PIC: YES, IRQ ON', 0
info_pit_missing: db 'PIT: NOT DETECTED', 0
info_pit_present: db 'PIT: DETECTED', 0
info_pit_unavailable: db 'PIT COUNT: N/A', 0
info_pit_stopped: db 'PIT COUNT: STOPPED', 0
info_pit_running: db 'PIT COUNT: RUNNING', 0
info_timer_software: db 'TIMER: SOFTWARE', 0
info_timer_irq: db 'TIMER: IRQ0 18.2Hz', 0
info_uart_polling: db 'UART RX: POLLING', 0
info_uart_irq: db 'UART RX: IRQ4 BUF', 0
info_protocol: db 'BIOS ', BIOS_VERSION, ';rev2;ROM F8000;SRAM '
info_protocol_ram: db '640K;UART 3F8 9600 8N1;protocol ', PROTO_VERSION_TEXT, ';API 1;RAM mask '
info_protocol_mask: db '1F;work '
info_protocol_bank: db '0;INT '
info_protocol_int: db '1;PIC '
info_protocol_pic: db '0;PIT '
info_protocol_pit: db '0;CLK '
info_protocol_clock: db '0;IRQ '
info_protocol_irq: db '0'
info_protocol_end:

%if info_protocol_end - info_protocol + 1 > PROTO_MAX_PAYLOAD
    %error INFO reply exceeds protocol payload limit
%endif
%if info_protocol_end - info_protocol + 8 > MONITOR_MAX_PACKET
    %error INFO reply exceeds the UART packet buffer
%endif
