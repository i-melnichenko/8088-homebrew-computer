; BIOS_WRITE: physical address:u32 followed by 1..124 image bytes.
; Validate the whole request even on rev1, which cannot drive EEPROM WE#.
eeprom_write:
    mov cx, [es:DECODE_BUFFER + 3]
    cmp cx, 5
    jb .error
    cmp cx, PROTO_MAX_PAYLOAD
    ja .error
    sub cx, 4
    mov si, [es:DECODE_BUFFER + 5]
    mov dx, [es:DECODE_BUFFER + 7]
    call eeprom_validate_range
    jc .error
    mov bx, eeprom_screen_write_unsupported
    mov dx, 1
    call eeprom_screen_update
    mov al, 17                      ; valid request, unsupported rev1 write
    jmp monitor_reply
.error:
    mov bx, eeprom_screen_bad_write
    xor dx, dx
    call eeprom_screen_update
    mov al, 18                      ; invalid BIOS request/range
    jmp monitor_reply
