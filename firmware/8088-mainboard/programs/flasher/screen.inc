; Standalone 20x4 LCD screen. All code and strings execute from the RAM image.
; Reuse the BIOS framebuffer, with one private redraw flag after flasher state.
FLASHER_SCREEN_RESCAN equ EEPROM_STATE_END
FLASHER_LCD_COMMAND_PORT equ 080h
FLASHER_LCD_DATA_PORT equ 081h
%if FLASHER_SCREEN_RESCAN >= MENU_BLINK_TICKS
    %error Flasher screen state overlaps menu blink state
%endif

; BX=status string. DX=1 uses validated EEPROM-relative SI/CX range;
; DX=0 shows the complete EEPROM window. Preserve the command's registers.
eeprom_screen_update:
    pushf
    push ax
    push bx
    push cx
    push dx
    push si
    push di
    push bp
    test dx, dx
    jnz .range
    xor si, si
    mov cx, 8000h
.range:
    mov bp, si
    add cx, si
    dec cx
    mov dx, cx
    mov di, SCREEN_BUFFER
    mov si, eeprom_screen_title
    call eeprom_screen_line
    mov si, bx
    call eeprom_screen_line
    mov si, eeprom_screen_range
    call eeprom_screen_line
    mov si, eeprom_screen_footer
    call eeprom_screen_line
    mov di, SCREEN_BUFFER + 46
    mov ax, bp
    add ax, 8000h
    call eeprom_screen_hex16
    mov di, SCREEN_BUFFER + 52
    mov ax, dx
    add ax, 8000h
    call eeprom_screen_hex16
    mov byte [es:MENU_DIRTY], 1
    cmp byte [es:SCREEN_POSITION], 80
    jb .rescan
    mov byte [es:SCREEN_POSITION], 0
    mov byte [es:SCREEN_COMMAND], 0
    mov byte [es:FLASHER_SCREEN_RESCAN], 0
    jmp .done
.rescan:
    ; Finish this pass even during continuous host traffic. A second pass
    ; then updates cells already written before the latest status arrived.
    mov byte [es:FLASHER_SCREEN_RESCAN], 1
.done:
    pop bp
    pop di
    pop si
    pop dx
    pop cx
    pop bx
    pop ax
    popf
    ret

eeprom_screen_line:
    mov cx, 20
.copy:
    lodsb
    test al, al
    jz .pad
    stosb
    loop .copy
    ret
.pad:
    mov al, ' '
    rep stosb
    ret

eeprom_screen_hex16:
    mov cx, 4
.digit:
    rol ax, 1
    rol ax, 1
    rol ax, 1
    rol ax, 1
    push ax
    and al, 0Fh
    add al, '0'
    cmp al, '9'
    jbe .write
    add al, 'A' - '9' - 1
.write:
    stosb
    pop ax
    loop .digit
    ret

; One short LCD operation per polling iteration, matching the BIOS renderer.
; Entry preserves LCD configuration and UART FIFOs; no clear/startup delay.
eeprom_screen_step:
    cmp byte [es:MENU_DIRTY], 0
    je .done
    push ax
    push bx
    push cx
    cmp byte [es:SCREEN_POSITION], 80
    jb .cell
    cmp byte [es:FLASHER_SCREEN_RESCAN], 0
    jne .rescan
    mov byte [es:MENU_DIRTY], 0
    jmp .restore
.rescan:
    mov byte [es:FLASHER_SCREEN_RESCAN], 0
    mov byte [es:SCREEN_POSITION], 0
    mov byte [es:SCREEN_COMMAND], 0
.cell:
    xor ax, ax
    mov al, [es:SCREEN_POSITION]
    cmp byte [es:SCREEN_COMMAND], 0
    jne .data
    mov bl, 20
    div bl
    xor bx, bx
    mov bl, al
    mov al, [eeprom_screen_rows + bx]
    out FLASHER_LCD_COMMAND_PORT, al
    mov byte [es:SCREEN_COMMAND], 1
    jmp .delay
.data:
    mov bx, ax
    mov al, [es:SCREEN_BUFFER + bx]
    out FLASHER_LCD_DATA_PORT, al
    inc byte [es:SCREEN_POSITION]
    mov al, [es:SCREEN_POSITION]
    xor ah, ah
    mov bl, 20
    div bl
    test ah, ah
    jnz .delay
    mov byte [es:SCREEN_COMMAND], 0
.delay:
    mov cx, 128
.wait:
    loop .wait
.restore:
    pop cx
    pop bx
    pop ax
.done:
    ret

eeprom_screen_rows: db 080h, 0C0h, 094h, 0D4h
eeprom_screen_title: db 'BIOS FLASHER', 0
eeprom_screen_range: db 'ROM: F0000-F0000', 0
eeprom_screen_footer: db 'HOST RESET TO EXIT', 0
eeprom_screen_ready: db 'READY', 0
eeprom_screen_read_ok: db 'READ OK', 0
eeprom_screen_write_ok: db 'WRITE OK', 0
eeprom_screen_write_failed: db 'WRITE FAILED', 0
eeprom_screen_bad_read: db 'INVALID READ', 0
eeprom_screen_bad_write: db 'INVALID WRITE', 0
eeprom_screen_bad_request: db 'INVALID REQUEST', 0
eeprom_screen_blocked: db 'COMMAND UNAVAILABLE', 0
