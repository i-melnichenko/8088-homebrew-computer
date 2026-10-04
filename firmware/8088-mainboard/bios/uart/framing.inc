; Read a zero-delimited COBS packet.  On success ES addresses the buffers
; and BP is the decoded frame size.
monitor_read_frame:
    mov ax, MONITOR_BUFFER_SEGMENT
    mov es, ax
    mov di, [es:RX_COUNT]
.read:
    call uart_try_getc
    jc .pending
    test al, al
    jnz .byte
    cmp byte [es:RX_DROP], 0
    jne .resync
    cmp di, RX_BUFFER
    je .read
    mov bp, di
    mov word [es:RX_COUNT], 0
    call monitor_cobs_decode
    jc .bad
    cmp bp, 7
    jb .bad
    mov si, DECODE_BUFFER
    mov cx, bp
    sub cx, 2
    call monitor_crc16
    mov dx, [es:si]
    cmp ax, dx
    jne .bad
    mov ax, [es:DECODE_BUFFER + 3]
    add ax, 7
    cmp ax, bp
    jne .bad
    cmp byte [es:DECODE_BUFFER], PROTO_VERSION
    jne .bad
    cmp word [es:DECODE_BUFFER + 3], PROTO_MAX_PAYLOAD
    ja .bad
    clc
    ret
.byte:
    cmp byte [es:RX_DROP], 0
    jne .read
    cmp di, MONITOR_MAX_PACKET
    jae .overflow
    mov [es:di], al
    inc di
    jmp short .read
.overflow:
    mov byte [es:RX_DROP], 1
    mov di, RX_BUFFER
    jmp short .read
.resync:
    mov byte [es:RX_DROP], 0
    mov di, RX_BUFFER
    jmp .read
.pending:
    mov [es:RX_COUNT], di
.bad:
    stc
    ret

; Decode ES:RX_BUFFER[0..BP) to ES:DECODE_BUFFER.
monitor_cobs_decode:
    mov si, RX_BUFFER
    mov di, DECODE_BUFFER
.block:
    cmp si, bp
    jae .done
    mov dl, [es:si]
    inc si
    test dl, dl
    jz .bad
    xor cx, cx
    mov cl, dl
    dec cx
    mov ax, si
    add ax, cx
    cmp ax, bp
    ja .bad
.copy:
    jcxz .separator
    mov al, [es:si]
    inc si
    mov [es:di], al
    inc di
    loop .copy
.separator:
    cmp dl, 0FFh
    je .block
    cmp si, bp
    jae .block
    mov byte [es:di], 0
    inc di
    jmp short .block
.done:
    sub di, DECODE_BUFFER
    mov bp, di
    clc
    ret
.bad:
    stc
    ret

; CRC-16/CCITT-FALSE over ES:SI, CX bytes.  AX is the result.
monitor_crc16:
    mov ax, 0FFFFh
.byte:
    jcxz .done
    mov dl, [es:si]
    inc si
    xor ah, dl
    mov dl, 8
.bit:
    shl ax, 1
    jnc .next_bit
    xor ax, 1021h
.next_bit:
    dec dl
    jnz .bit
    loop .byte
.done:
    ret

; The last reply byte may still be in the UART shift register when THRE is
; set. Finish transmitting before handing control to another program.
monitor_wait_tx_empty:
    mov dx, UART_LSR
.wait:
    in al, dx
    test al, 040h
    jz .wait
    ret

; Status-only response: version, command|80h, sequence, payload length=1,
; status byte, CRC; then COBS-encode it between delimiters.
monitor_reply:
    mov [es:RX_BUFFER + 5], al
    mov cx, 1
monitor_reply_data:
    mov byte [es:RX_BUFFER], PROTO_VERSION
    mov al, [es:DECODE_BUFFER + 1]
    or al, PROTO_REPLY
    mov [es:RX_BUFFER + 1], al
    mov al, [es:DECODE_BUFFER + 2]
    mov [es:RX_BUFFER + 2], al
    mov [es:RX_BUFFER + 3], cx
    add cx, 5
    push cx
    mov si, RX_BUFFER
    call monitor_crc16
    mov [es:si], ax
    pop cx
    add cx, 2
    mov si, RX_BUFFER
    call monitor_cobs_send
    ret

monitor_cobs_send:
    mov di, DECODE_BUFFER
    mov bp, di
    inc di
    mov bl, 1
.encode:
    jcxz .finish
    mov al, [es:si]
    inc si
    dec cx
    test al, al
    jz .zero
    mov [es:di], al
    inc di
    inc bl
    cmp bl, 0FFh
    jne .encode
.zero:
    mov [es:bp], bl
    mov bp, di
    inc di
    mov bl, 1
    jmp short .encode
.finish:
    mov [es:bp], bl
    sub di, DECODE_BUFFER
    mov cx, di
    xor al, al
    call uart_putc
    mov si, DECODE_BUFFER
.send:
    jcxz .end
    mov al, [es:si]
    inc si
    call uart_putc
    loop .send
.end:
    xor al, al
    call uart_putc
    ret

