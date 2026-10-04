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
    call menu_line
    pop bx
    cmp bp, INFO_PROGRAM_INDEX
    jne .next
    mov al, [es:PROGRAM_COUNT]
    add al, '0'
    mov [es:bx + 10], al
.next:
    inc bp
    dec dx
    jnz .row
    xor ax, ax
    mov al, [es:INFO_TOP]
    mov bx, INFO_ITEM_COUNT
    jmp menu_scroll_rail

info_items:
    dw info_version, info_board, info_cpu, info_ram, info_rom, info_lcd
    dw info_uart, info_wire_version, info_api_version, info_programs
    dw info_rom_address, info_ram_address, info_uart_address, info_lcd_address
info_items_end:
INFO_ITEM_COUNT equ (info_items_end - info_items) / 2
INFO_PROGRAM_INDEX equ 9

info_title: db 'INFO', 0
info_version: db 'BIOS: ', BIOS_VERSION, 0
info_board: db 'BOARD: REV 2', 0
info_cpu: db 'CPU: 8088', 0
info_ram: db 'RAM: 640 KiB', 0
info_rom: db 'ROM: 32 KiB', 0
info_lcd: db 'LCD: 20x4', 0
info_uart: db 'UART: 9600 8N1', 0
info_wire_version: db 'PROTOCOL: ', '0' + PROTO_VERSION, 0
info_api_version: db 'API: ', '0' + BIOS_API_VERSION, 0
info_programs: db 'PROGRAMS: 0/', '0' + PROGRAM_CAPACITY, 0
info_rom_address: db 'ROM AT: F800:0000', 0
info_ram_address: db 'UPLOAD: 8800-FBFF', 0
info_uart_address: db 'UART I/O: 3F8-3FF', 0
info_lcd_address: db 'LCD I/O: 80-81', 0
info_protocol: db 'BIOS ', BIOS_VERSION, '; rev2; ROM F8000; SRAM 640K; payload 8800-FBFF; UART 3F8 9600 8N1; protocol 1; API 1'
info_protocol_end:
