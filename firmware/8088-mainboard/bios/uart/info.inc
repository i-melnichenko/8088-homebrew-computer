; UART INFO reply; the summary string is defined with the LCD Info data.
monitor_info:
    cmp word [es:DECODE_BUFFER + 3], 0
    jne .bad
    mov byte [es:RX_BUFFER + 5], 0
    mov si, info_protocol
    mov di, RX_BUFFER + 6
    mov cx, info_protocol_end - info_protocol
    rep movsb
    mov cx, info_protocol_end - info_protocol + 1
    jmp monitor_reply_data
.bad:
    mov al, 5
    jmp monitor_reply
