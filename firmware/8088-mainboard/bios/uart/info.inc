; UART INFO reply; the summary string is defined with the LCD Info data.
monitor_info:
    cmp word [es:DECODE_BUFFER + 3], 0
    jne .bad
    mov byte [es:RX_BUFFER + 5], 0
    mov si, info_protocol
    mov di, RX_BUFFER + 6
    mov cx, info_protocol_end - info_protocol
    rep movsb
    ; Patch actual tested capacity and selected bank into the copied text.
    xor ax, ax
    mov bl, [es:RAM_BANK_MASK]
    mov cx, 5
.count:
    shr bl, 1
    jnc .next
    add ax, 128
.next:
    loop .count
    mov bx, 10
    mov di, RX_BUFFER + 6 + info_protocol_ram - info_protocol + 2
    mov cx, 3
.decimal:
    xor dx, dx
    div bx
    add dl, '0'
    mov [es:di], dl
    dec di
    loop .decimal
    mov al, [es:RAM_BANK_MASK]
    mov ah, al
    shr al, 1
    shr al, 1
    shr al, 1
    shr al, 1
    call .hex
    mov [es:RX_BUFFER + 6 + info_protocol_mask - info_protocol], al
    mov al, ah
    and al, 0Fh
    call .hex
    mov [es:RX_BUFFER + 7 + info_protocol_mask - info_protocol], al
    mov ax, ss
    mov cl, 13
    shr ax, cl
    add al, '0'
    mov [es:RX_BUFFER + 6 + info_protocol_bank - info_protocol], al
    mov ax, ss
    test ax, ax
    jz .has_ivt
    mov byte [es:RX_BUFFER + 6 + info_protocol_int - info_protocol], '0'
.has_ivt:
    mov al, [es:PIC_PRESENT]
    add al, '0'
    mov [es:RX_BUFFER + 6 + info_protocol_pic - info_protocol], al
    mov al, [es:PIT_PRESENT]
    add al, '0'
    mov [es:RX_BUFFER + 6 + info_protocol_pit - info_protocol], al
    mov al, [es:PIT_RUNNING]
    add al, '0'
    mov [es:RX_BUFFER + 6 + info_protocol_clock - info_protocol], al
    mov al, [ss:RT_FLAGS]
    add al, '0'
    mov [es:RX_BUFFER + 6 + info_protocol_irq - info_protocol], al
    mov cx, info_protocol_end - info_protocol + 1
    jmp monitor_reply_data
.bad:
    mov al, 5
    jmp monitor_reply

.hex:
    add al, '0'
    cmp al, '9'
    jbe .digit
    add al, 'A' - '9' - 1
.digit:
    ret
