; BIOS_READ handler in the flasher: physical address:u32, size:u16 (1..127).
; Read the actual EEPROM window, independently of the executing BIOS's CS.
eeprom_read:
    cmp word [es:DECODE_BUFFER + 3], 6
    jne .error
    mov dx, [es:DECODE_BUFFER + 7]
    mov si, [es:DECODE_BUFFER + 5]
    mov cx, [es:DECODE_BUFFER + 9]
    test cx, cx
    jz .error
    cmp cx, PROTO_MAX_PAYLOAD - 1
    ja .error
    call eeprom_validate_range
    jc .error
    mov bx, eeprom_screen_read_ok
    mov dx, 1
    call eeprom_screen_update
    mov bp, cx
    push ds
    mov ax, ROM_SEGMENT
    mov ds, ax
    mov di, RX_BUFFER + 6
    rep movsb
    pop ds
    mov byte [es:RX_BUFFER + 5], 0
    mov cx, bp
    inc cx
    jmp monitor_reply_data
.error:
    mov bx, eeprom_screen_bad_read
    xor dx, dx
    call eeprom_screen_update
    mov al, 18                      ; invalid BIOS request/range
    jmp monitor_reply
