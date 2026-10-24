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
    dw info_uart, info_wire_version, info_api_version
    dw info_rom_address, info_uart_address, info_lcd_address
info_items_end:
INFO_ITEM_COUNT equ (info_items_end - info_items) / 2

info_title: db 'INFO', 0
info_version: db 'BIOS: ', BIOS_VERSION, 0
info_board: db 'BOARD: REV 2', 0
info_cpu: db 'CPU: 8088', 0
info_ram: db 'RAM: 640 KiB', 0
info_rom: db 'ROM: 32 KiB', 0
info_lcd: db 'LCD: 20x4', 0
info_uart: db 'UART: 9600 8N1', 0
info_wire_version: db 'PROTOCOL: ', PROTO_VERSION_TEXT, 0
info_api_version: db 'API: ', '0' + BIOS_API_VERSION, 0
info_rom_address: db 'ROM AT: F800:0000', 0
info_uart_address: db 'UART I/O: 3F8-3FF', 0
info_lcd_address: db 'LCD I/O: 80-81', 0
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
