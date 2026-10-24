; BIOS_WRITE: canonical physical address:u32 followed by 1..124 bytes.
; Byte writes avoid AT28C256 page timing constraints. Poll the written byte
; until all eight bits match, then acknowledge only after readback.
eeprom_write:
    mov cx, [es:DECODE_BUFFER + 3]
    cmp cx, 5
    jb .invalid
    cmp cx, PROTO_MAX_PAYLOAD
    ja .invalid
    sub cx, 4
    mov si, [es:DECODE_BUFFER + 5]
    mov dx, [es:DECODE_BUFFER + 7]
    call eeprom_validate_range
    jc .invalid

    ; A completed transfer can be restarted only by a new-sequence block at 0.
    cmp word [eeprom_next], 8000h
    jne .retry
    test si, si
    jne .retry
    mov al, [es:DECODE_BUFFER + 2]
    cmp al, [eeprom_last_sequence]
    je .retry
    mov word [eeprom_next], 0
    mov byte [eeprom_started], 1
    mov byte [eeprom_last_valid], 0

.retry:
    cmp byte [eeprom_last_valid], 0
    je .new
    cmp si, [eeprom_last_address]
    jne .new
    cmp cx, [eeprom_last_length]
    jne .invalid
    push si
    push cx
    mov di, DECODE_BUFFER + 9
    mov si, eeprom_last_data
    repe cmpsb                     ; DS:SI cache against ES:DI request
    pop cx
    pop si
    jne .invalid
    cmp byte [eeprom_last_status], 0
    je .cached
    mov al, [es:DECODE_BUFFER + 2]
    cmp al, [eeprom_last_sequence]
    je .cached                     ; same failed attempt: no second write
    ; A fresh sequence may retry the same failed block.
    jmp .new
.cached:
    mov al, [eeprom_last_status]
    jmp monitor_reply

.new:
    cmp si, [eeprom_next]
    jne .invalid                   ; gaps, overlaps and out-of-order blocks
    mov byte [eeprom_started], 1
    mov [eeprom_last_address], si
    mov [eeprom_last_length], cx
    mov al, [es:DECODE_BUFFER + 2]
    mov [eeprom_last_sequence], al
    mov byte [eeprom_last_valid], 1
    ; Save the exact bytes before starting: retries must compare all bytes,
    ; including cases where the EEPROM was only partly programmed.
    push si
    push cx
    mov di, DECODE_BUFFER + 9
    mov si, eeprom_last_data
.save:
    mov al, [es:di]
    mov [si], al
    inc di
    inc si
    loop .save
    pop cx
    pop si

    push si
    push cx
    mov bx, si
    mov di, DECODE_BUFFER + 9
    push ds
    mov ax, ROM_SEGMENT
    mov ds, ax
.byte:
    mov al, [es:di]
    mov [bx], al                    ; WE# follows WR# when SW1 ROM write is on
    mov dx, 0FFFFh                 ; bounded completion polling
.poll:
    cmp [bx], al                    ; data polling plus full-byte verification
    je .next
    dec dx
    jnz .poll
    pop ds
    pop cx
    pop si
    mov byte [eeprom_last_status], 19
    mov bx, eeprom_screen_write_failed
    mov dx, 1
    call eeprom_screen_update
    mov al, 19
    jmp monitor_reply
.next:
    inc bx
    inc di
    loop .byte
    pop ds
    pop cx
    pop si
    add [eeprom_next], cx
    mov byte [eeprom_last_status], 0
    mov bx, eeprom_screen_write_ok
    mov dx, 1
    call eeprom_screen_update
    xor al, al
    jmp monitor_reply

.invalid:
    mov bx, eeprom_screen_bad_write
    xor dx, dx
    call eeprom_screen_update
    mov al, 18
    jmp monitor_reply

; Copied with the standalone RAM flasher. A cache of the last attempted block
; fits after its code and is never referenced through the ROM segment.
eeprom_next: dw 0
eeprom_started: db 0
eeprom_last_valid: db 0
eeprom_last_sequence: db 0
eeprom_last_status: db 0
eeprom_last_address: dw 0
eeprom_last_length: dw 0
eeprom_last_data: times 124 db 0
