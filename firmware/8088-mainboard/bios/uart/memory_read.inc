; MEMORY_READ payload: physical address:u32, size:u16 (1..127).
; Only the rev1 SRAM decode bank (including aliases) is readable.
; No execution or diagnostic can run concurrently with a monitor read.
monitor_memory_read:
    cmp byte [es:MENU_PAGE], PAGE_TEST
    je .busy
    cmp word [es:DECODE_BUFFER + 3], 6
    jne .error
    mov ax, [es:DECODE_BUFFER + 5]
    mov dx, [es:DECODE_BUFFER + 7]
    mov cx, [es:DECODE_BUFFER + 9]
    test cx, cx
    jz .error
    cmp cx, PROTO_MAX_PAYLOAD - 1
    ja .error
    ; Compute the inclusive end in BP:BX, without 16-bit wraparound.
    mov bx, ax
    mov bp, dx
    dec cx
    add bx, cx
    adc bp, 0
    jc .error
    inc cx
    cmp dx, 1
    ja .error
    cmp bp, 1
    ja .error
    ; RX is also readable RAM. Use memmove direction if copying its bytes
    ; upward into the reply would overwrite unread source bytes. The four
    ; SRAM aliases all share the same underlying 32 KiB.
    mov bx, ax
    and bx, 7FFFh
    cmp bx, (BIOS_STATE_BASE & 7FFFh) + RX_BUFFER + 6
    jae .forward
    add bx, cx
    cmp bx, (BIOS_STATE_BASE & 7FFFh) + RX_BUFFER + 6
    jbe .forward
    mov bl, 1                       ; overlapping source: copy backwards
    jmp .copy
.forward:
    xor bx, bx
.copy:
    mov bp, cx
    push ds
    ; Canonical segment:offset for the validated 20-bit physical address.
    mov si, ax
    and si, 0Fh
    shr ax, 1
    shr ax, 1
    shr ax, 1
    shr ax, 1
    push cx
    mov cl, 12
    shl dx, cl
    pop cx
    or ax, dx
    mov ds, ax
    mov di, RX_BUFFER + 6
    test bl, bl
    jz .bytes
    add si, cx
    dec si
    add di, cx
    dec di
    std
.bytes:
    rep movsb
    cld
    pop ds
    ; Write status after copying, since RX itself may be the source.
    mov byte [es:RX_BUFFER + 5], 0
    mov cx, bp
    inc cx
    jmp monitor_reply_data
.error:
    mov al, 14                      ; invalid memory request/range
    jmp monitor_reply
.busy:
    mov al, 10                      ; destructive memory check is active
    jmp monitor_reply
