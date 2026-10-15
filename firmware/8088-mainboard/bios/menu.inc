; BIOS owns the menu and its 80-byte LCD framebuffer.
MENU_BAR_CELLS equ LCD_COLUMNS - 2  ; '[' + 18 progress cells + ']'

; AL=key; return AL=1 only when a valid RAM program should start.
menu_key:
    cmp byte [es:MENU_PAGE], PAGE_MENU
    je .root
    cmp byte [es:MENU_PAGE], PAGE_CUSTOM
    je .custom
    cmp byte [es:MENU_PAGE], PAGE_INFO
    je .info_keys
    cmp al, KEY_BACK
    jne .enter
    cmp byte [es:MENU_PAGE], PAGE_TEST
    jne .back
    mov byte [es:MENU_PAGE], PAGE_CANCELLED
    jmp .changed
.back:
    cmp byte [es:MENU_PAGE], PAGE_PROGRAM
    jne .root_back
    mov byte [es:MENU_PAGE], PAGE_CUSTOM
    mov al, [es:ACTIVE_ID]
    dec al
    mov [es:MENU_SELECTED], al
    jmp .changed
.root_back:
    mov byte [es:MENU_PAGE], PAGE_MENU
    jmp .changed
.enter:
    cmp al, KEY_ENTER
    jne .nothing
    cmp byte [es:MENU_PAGE], PAGE_CONFIRM
    jne .program
    call memory_start
    jmp .changed
.program:
    cmp byte [es:MENU_PAGE], PAGE_PROGRAM
    jne .nothing
    cmp byte [es:UPLOAD_ACTIVE], 0
    jne .nothing
    mov al, [es:ACTIVE_ID]
    cmp al, [es:PROGRAM_COUNT]
    ja .nothing
    mov al, 1
    ret
.info_keys:
    cmp al, KEY_BACK
    je .root_back
    cmp al, KEY_UP
    jne .info_down
    cmp byte [es:INFO_TOP], 0
    je .nothing
    dec byte [es:INFO_TOP]
    jmp .changed
.info_down:
    cmp al, KEY_DOWN
    jne .nothing
    cmp byte [es:INFO_TOP], INFO_ITEM_COUNT - 3
    jae .nothing
    inc byte [es:INFO_TOP]
    jmp .changed
.custom:
    cmp al, KEY_BACK
    jne .custom_choose
    mov byte [es:MENU_SELECTED], 1
    jmp .root_back
.custom_choose:
    cmp al, KEY_ENTER
    jne .root
    mov al, [es:MENU_SELECTED]
    inc al
    cmp al, [es:PROGRAM_COUNT]
    ja .nothing
    mov [es:ACTIVE_ID], al
    mov byte [es:MENU_PAGE], PAGE_PROGRAM
    jmp .changed
.root:
    cmp al, KEY_UP
    jne .down
    cmp byte [es:MENU_SELECTED], 0
    jne .decrement
    mov byte [es:MENU_SELECTED], MENU_ITEMS
.decrement:
    dec byte [es:MENU_SELECTED]
    jmp .changed
.down:
    cmp al, KEY_DOWN
    jne .choose
    inc byte [es:MENU_SELECTED]
    cmp byte [es:MENU_SELECTED], MENU_ITEMS
    jb .changed
    mov byte [es:MENU_SELECTED], 0
    jmp .changed
.choose:
    cmp al, KEY_ENTER
    jne .nothing
    mov al, [es:MENU_SELECTED]
    test al, al
    jnz .ram
    mov byte [es:MENU_PAGE], PAGE_NO_BOOT
    jmp .changed
.ram:
    cmp al, 1
    jne .memory
    mov byte [es:MENU_PAGE], PAGE_CUSTOM
    mov byte [es:MENU_SELECTED], 0
    jmp .changed
.memory:
    cmp al, 2
    jne .info
    mov byte [es:MENU_PAGE], PAGE_CONFIRM
    jmp .changed
.info:
    mov byte [es:MENU_PAGE], PAGE_INFO
    mov byte [es:INFO_TOP], 0
.changed:
    mov byte [es:MENU_DIRTY], 1
.nothing:
    xor al, al
    ret

; Rebuild RAM framebuffer without touching receive/decoded packets.
menu_render:
    cmp byte [es:MENU_DIRTY], 0
    jne .dirty
    ret
.dirty:
    push ax
    push bx
    push cx
    push dx
    push si
    push di
    push bp
    mov byte [es:MENU_DIRTY], 0
    mov di, SCREEN_BUFFER
    ; Row 1 is the section header, except for the two-line warning dialog.
    xor bx, bx
    mov bl, [es:MENU_PAGE]
    shl bx, 1
    mov si, [menu_headers + bx]
    cmp byte [es:MENU_PAGE], PAGE_PROGRAM
    jne .header_rom
    mov al, [es:ACTIVE_ID]
    call program_record
    mov si, bx
    call menu_ram_line
    jmp .header_done
.header_rom:
    call menu_line
.header_done:
    mov al, [es:MENU_PAGE]
    cmp al, PAGE_CUSTOM
    je .root_layout
    cmp al, PAGE_MENU
    jne .info
.root_layout:
    xor bp, bp
    cmp byte [es:MENU_SELECTED], 3
    jne .root_rows
    inc bp
.root_rows:
    mov dx, 3
.row:
    cmp byte [es:MENU_PAGE], PAGE_CUSTOM
    jne .rom_row
    push di
    mov si, menu_empty
    call menu_line
    pop di
    mov ax, bp
    inc ax
    cmp al, [es:PROGRAM_COUNT]
    ja .custom_empty
    push dx
    call program_record
    pop dx
    mov si, bx
    push di
    add di, 2
    call menu_ram_line_short
    pop di
.custom_empty:
    mov bx, di
    add di, LCD_COLUMNS
    jmp .selection
.rom_row:
    mov bx, bp
    shl bx, 1
    mov si, [menu_names + bx]
    push di
    call menu_line
    pop bx
.selection:
    mov ax, bp
    cmp al, [es:MENU_SELECTED]
    jne .unselected
    mov byte [es:bx], '>'
.unselected:
    inc bp
    dec dx
    jnz .row
    mov ax, bp
    sub ax, 3                       ; first visible item
    mov bx, MENU_ITEMS
    call menu_scroll_rail
    jmp .done
.info:
    cmp al, PAGE_INFO
    jne .program
    call info_render
    jmp .done
.program:
    cmp al, PAGE_PROGRAM
    jne .confirm
    mov si, program_details
    call menu_line
    mov al, [es:ACTIVE_ID]
    call program_record
    mov bp, bx
    mov di, SCREEN_BUFFER + 26
    mov ax, [es:bp + 18]
    call menu_hex16
    mov di, SCREEN_BUFFER + 35
    mov ax, [es:bp + 16]
    call menu_hex16
    mov di, SCREEN_BUFFER + 40
    mov ax, [es:bp + 18]
    mov bx, ax
    mov dl, [es:ACTIVE_ID]
    cmp dl, [es:PROGRAM_COUNT]
    jbe .program_bar
    mov ax, [es:MONITOR_RECEIVED]
.program_bar:
    call menu_bar
    mov si, program_loading
    cmp byte [es:UPLOAD_ACTIVE], 0
    jne .program_footer
    mov si, program_run
.program_footer:
    mov di, SCREEN_BUFFER + 60
    call menu_line
    jmp .done
.confirm:
    cmp al, PAGE_CONFIRM
    jne .test
    mov si, memory_confirm_warning
    call menu_line
    mov cx, LCD_COLUMNS
    mov al, ' '
    rep stosb
    mov si, memory_confirm_choices
    call menu_line
    jmp .done
.test:
    cmp al, PAGE_TEST
    jne .result
    call menu_memory_progress
    mov si, memory_region
    mov ax, [es:MEM_BLOCK]
    mov dl, [es:MEM_BLOCK_HI]
    call menu_memory_region
    jmp .memory_counts
.result:
    cmp al, PAGE_RESULT
    jne .no_boot
    call menu_memory_progress
    cmp byte [es:MEM_ERROR], 0
    jne .failure_log
    mov si, memory_ok
    mov ax, [es:MEM_BLOCK]
    mov dl, [es:MEM_BLOCK_HI]
    call menu_memory_region
    jmp .memory_counts
.failure_log:
    mov si, memory_failure
    mov ax, [es:MEM_FAIL_ADDR]
    mov dl, [es:MEM_FAIL_HI]
    call menu_memory_region
    jmp .memory_counts
.no_boot:
    cmp al, PAGE_NO_BOOT
    jne .cancelled
    mov si, boot_missing
    call menu_line
    mov si, boot_hint
    call menu_line
    jmp .footer
.cancelled:
    call menu_memory_progress
    mov si, memory_stopped
    mov ax, [es:MEM_BLOCK]
    mov dl, [es:MEM_BLOCK_HI]
    call menu_memory_region
    jmp .memory_counts
.footer:
    mov di, SCREEN_BUFFER + 60
    mov si, back_hint
    call menu_line
    jmp .done
.memory_counts:
    call menu_memory_counts
.done:
    mov byte [es:SCREEN_POSITION], 0
    mov byte [es:SCREEN_COMMAND], 0
    mov word [es:MENU_BLINK_TICKS], 0
    mov byte [es:MENU_BLINK_PHASE], 0
    mov byte [es:MENU_BLINK_PENDING], 0
    pop bp
    pop di
    pop si
    pop dx
    pop cx
    pop bx
    pop ax
    ret

; Copy a ROM string to one fixed-width LCD row, padded with spaces.
menu_line:
    mov cx, LCD_COLUMNS
.next:
    lodsb
    test al, al
    jz .pad
    stosb
    loop .next
    ret
.pad:
    mov al, ' '
    rep stosb
    ret

; AX=first visible item, BX=item count. Three rows share the same rail
; in root/custom menus and the informational list; no selection is drawn here.
menu_scroll_rail:
    mov byte [es:SCREEN_BUFFER + 39], '|'
    mov byte [es:SCREEN_BUFFER + 59], '|'
    mov byte [es:SCREEN_BUFFER + 79], '|'
    test ax, ax
    jz .below
    mov byte [es:SCREEN_BUFFER + 39], '^'
.below:
    add ax, 3
    cmp ax, bx
    jae .done
    mov byte [es:SCREEN_BUFFER + 79], 'v'
.done:
    ret

; Copy a program name from RAM, padding the remainder of the LCD row.
menu_ram_line:
    mov cx, LCD_COLUMNS
    jmp short menu_ram_copy
menu_ram_line_short:
    mov cx, LCD_COLUMNS - 2
menu_ram_copy:
    mov al, [es:si]
    inc si
    test al, al
    jz .pad
    stosb
    loop menu_ram_copy
    ret
.pad:
    mov al, ' '
    rep stosb
    ret

menu_hex16:
    push ax
    mov al, ah
    call menu_hex8
    pop ax
menu_hex8:
    push ax
    mov cl, 4
    shr al, cl
    call menu_nibble
    pop ax
menu_nibble:
    and al, 0Fh
    cmp al, 10
    jb .decimal
    add al, 'A' - 10
    jmp short .write
.decimal:
    add al, '0'
.write:
    stosb
    ret

; DX:AX unsigned value -> seven zero-padded decimal digits (8086 long divide).
menu_decimal7:
    add di, 6
    mov bp, 7
    mov bx, 10
.digit:
    push ax
    mov ax, dx
    xor dx, dx
    div bx
    mov si, ax
    pop ax
    div bx
    add dl, '0'
    mov [es:di], dl
    dec di
    mov dx, si
    dec bp
    jnz .digit
    add di, 8
    ret

menu_memory_counts:
    mov di, SCREEN_BUFFER + 60
    mov si, memory_counts
    call menu_line
    mov di, SCREEN_BUFFER + 63
    mov ax, [es:MEM_OK]
    mov dx, [es:MEM_OK + 2]
    call menu_decimal7
    mov di, SCREEN_BUFFER + 73
    mov ax, [es:MEM_BAD]
    mov dx, [es:MEM_BAD + 2]
    call menu_decimal7
    ret

; DL:AX=20-bit address, SI=region label. Show its inclusive 16-byte block.
menu_memory_region:
    push ax
    mov di, SCREEN_BUFFER + 40
    call menu_line
    mov di, SCREEN_BUFFER + 45
    pop ax
    and ax, 0FFF0h
    push ax
    call menu_hex20
    inc di                         ; leave the separating '-' intact
    pop ax
    add ax, 15
    call menu_hex20
    ret

menu_hex20:
    push ax
    mov al, dl
    call menu_nibble
    pop ax
    call menu_hex16
    ret

; 160 units per pass (ten 64-KiB banks, 16 units per bank).
menu_memory_progress:
    xor ax, ax
    mov al, [es:MEM_PHASE]
    mov bx, 160
    mul bx
    mov bx, ax
    xor ax, ax
    mov al, [es:MEM_ADDR_HI]
    mov cl, 4
    shl ax, cl
    add bx, ax
    mov ax, [es:MEM_ADDR]
    mov cl, 12
    shr ax, cl
    add ax, bx
    mov bx, 1600
    jmp menu_bar

; AX=completed, BX=total, ES:DI=bar. MUL uses the full 32-bit product.
menu_bar:
    test bx, bx
    jz .empty
    mov cx, MENU_BAR_CELLS
    mul cx
    div bx
    jmp short menu_bar_cells
.empty:
    xor ax, ax
menu_bar_cells:
    mov bx, ax
    cmp bx, MENU_BAR_CELLS
    jbe .bounded
    mov bx, MENU_BAR_CELLS
.bounded:
    mov al, '['
    stosb
    mov cx, MENU_BAR_CELLS
.cell:
    mov al, ' '
    test bx, bx
    jz .space
    mov al, '#'
    dec bx
.space:
    stosb
    loop .cell
    mov al, ']'
    stosb
    ret

; No timer/IRQ support in the current BIOS. Count idle iterations after a draw;
; toggles schedule two short LCD operations, never a full redraw or a sleep.
; The logical SCREEN_BUFFER retains '>'; blinking changes only the LCD cell.
menu_blink_step:
    cmp byte [es:MENU_PAGE], PAGE_MENU
    je .menu
    cmp byte [es:MENU_PAGE], PAGE_CUSTOM
    jne .done
.menu:
    cmp byte [es:SCREEN_POSITION], 80
    jb .done
    cmp byte [es:MENU_BLINK_PENDING], 0
    jne .done
    inc word [es:MENU_BLINK_TICKS]
    cmp word [es:MENU_BLINK_TICKS], MENU_BLINK_PERIOD
    jb .done
    mov word [es:MENU_BLINK_TICKS], 0
    xor byte [es:MENU_BLINK_PHASE], 1
    mov byte [es:MENU_BLINK_PENDING], 1
.done:
    ret

; Refresh one LCD operation per event-loop iteration, not a blocking screen.
; Startup/clear retain the conservative driver delays. Normal DDRAM writes
; use a shorter delay so UART FIFO is drained frequently during a RAM test.
menu_lcd_step:
    push ax
    push bx
    push cx
    cmp byte [es:SCREEN_POSITION], 80
    jae .blink
    mov al, [es:SCREEN_POSITION]
    xor ah, ah
    cmp byte [es:SCREEN_COMMAND], 0
    jne .data
    mov bl, LCD_COLUMNS
    div bl
    xor bx, bx
    mov bl, al
    mov al, [menu_lcd_rows + bx]
    out LCD_COMMAND_PORT, al
    mov byte [es:SCREEN_COMMAND], 1
    jmp short .delay
.data:
    mov bx, ax
    mov al, [es:SCREEN_BUFFER + bx]
    out LCD_DATA_PORT, al
    inc byte [es:SCREEN_POSITION]
    mov al, [es:SCREEN_POSITION]
    xor ah, ah
    mov bl, LCD_COLUMNS
    div bl
    test ah, ah
    jnz .delay
    mov byte [es:SCREEN_COMMAND], 0
    jmp short .delay
.blink:
    cmp byte [es:MENU_BLINK_PENDING], 0
    je .done
    cmp byte [es:MENU_BLINK_PENDING], 2
    je .blink_data
    xor bx, bx
    mov bl, [es:MENU_SELECTED]
    cmp bl, 3
    jb .blink_row
    mov bl, 2                      ; fourth item is on the last visible row
.blink_row:
    inc bx                         ; row zero is the header
    mov al, [menu_lcd_rows + bx]
    out LCD_COMMAND_PORT, al
    mov byte [es:MENU_BLINK_PENDING], 2
    jmp short .delay
.blink_data:
    mov al, '>'
    cmp byte [es:MENU_BLINK_PHASE], 0
    je .blink_write
    mov al, ' '
.blink_write:
    out LCD_DATA_PORT, al
    mov byte [es:MENU_BLINK_PENDING], 0
.delay:
    mov cx, 128
.wait:
    loop .wait
.done:
    pop cx
    pop bx
    pop ax
    ret

menu_lcd_rows: db LCD_LINE1, LCD_LINE2, LCD_LINE3, LCD_LINE4
; Indexed by PAGE_* in state.inc. Confirmation is a warning dialog;
; test progress/result/cancel retain the MEMORY CHECK section header.
menu_headers: dw menu_title, info_title, program_title, memory_confirm_title
              dw memory_title, memory_title, boot_title, memory_title, custom_title
menu_title: db '8088 BIOS ', BIOS_VERSION, 0
menu_names: dw menu_boot, menu_custom, menu_memory, menu_info
menu_boot: db '  Default boot', 0
menu_custom: db '  Custom programs', 0
menu_empty: db '  Empty', 0
custom_title: db 'CUSTOM PROGRAMS', 0
menu_memory: db '  Memory check', 0
menu_info: db '  Info', 0
back_hint: db 'ESC: BACK', 0
boot_title: db 'DEFAULT BOOT', 0
boot_missing: db 'NO BOOT TARGET', 0
boot_hint: db 'Use Custom programs', 0
program_title: db 'RAM PROGRAM', 0
program_details: db 'SIZE: 0000 AT: 0000', 0
program_loading: db 'RECEIVING...', 0
program_run: db 'ENTER:RUN ESC:BACK', 0
memory_title: db 'MEMORY CHECK', 0
memory_confirm_title: db 'ALL RAM PROGRAMS', 0
memory_confirm_warning: db 'WILL BE DELETED!', 0
memory_confirm_choices: db 'YES:ENTER  NO:ESC', 0
memory_region: db 'RAM: 00000-00000', 0
memory_ok: db 'DONE 00000-00000', 0
memory_failure: db 'FAIL 00000-00000', 0
memory_stopped: db 'STOP 00000-00000', 0
memory_counts: db 'OK:0000000 B:0000000', 0
