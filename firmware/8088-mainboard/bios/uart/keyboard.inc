; UART keyboard commands and transport polling for RAM programs.
; MODE data: 0=raw console, 1=framed keyboard. Repeated attach preserves keys.
monitor_keyboard_mode:
    cmp word [es:DECODE_BUFFER + 3], 1
    jne keyboard_request_error
    mov al, [es:DECODE_BUFFER + 5]
    cmp al, 1
    ja keyboard_request_error
    cmp al, [es:API_HOST_MODE]
    je keyboard_reply
    mov [es:API_HOST_MODE], al
    mov byte [es:API_HOST_VALID], 0
    call api_key_reset
    jmp keyboard_reply

; EVENT data: ASCII:u8, scan:u8. Printable ASCII with scan=0 is translated
; by the BIOS. Extended keys have ASCII=0, nonzero PC-style scan code.
monitor_keyboard_event:
    cmp word [es:DECODE_BUFFER + 3], 2
    jne keyboard_request_error
    cmp byte [es:API_HOST_MODE], 1
    jne keyboard_request_error
    mov ax, [es:DECODE_BUFFER + 5]
    test ax, ax
    jz keyboard_request_error
    test al, 80h
    jnz keyboard_request_error
    mov dl, [es:DECODE_BUFFER + 2]
    cmp byte [es:API_HOST_VALID], 0
    je .new
    cmp dl, [es:API_HOST_SEQUENCE]
    jne .new
    cmp ax, [es:API_HOST_KEY]
    je keyboard_reply                ; lost ACK must not inject a second key
.new:
    push ax
    test ah, ah
    jnz .enqueue
    call api_key_scan
.enqueue:
    call keyboard_enqueue
    pop ax
    jc .full
    mov [es:API_HOST_KEY], ax
    mov dl, [es:DECODE_BUFFER + 2]
    mov [es:API_HOST_SEQUENCE], dl
    mov byte [es:API_HOST_VALID], 1
    call keyboard_menu_step
    test al, al
    jz keyboard_reply
    mov byte [es:MENU_FLAGS], 1
    call keyboard_reply
    call monitor_wait_tx_empty
    jmp boot_program
.full:
    mov al, 13                      ; ring full, no key accepted
    jmp monitor_reply

keyboard_request_error:
    mov al, 12
    jmp monitor_reply

keyboard_reply:
    mov byte [es:RX_BUFFER + 5], 0
    mov al, [es:API_RUNNING]
    or al, [es:MENU_FLAGS]
    mov [es:RX_BUFFER + 6], al
    mov cx, 2
    jmp monitor_reply_data

; Called only from INT 16h. Preserve the interrupt handler's BP frame and
; working registers: the protocol decoder/reply uses BP and both buffers.
keyboard_program_poll:
    push ax
    push bx
    push cx
    push dx
    push si
    push di
    push bp
    call monitor_read_frame
    jc .done
    mov byte [es:MENU_FLAGS], 0
    mov al, [es:DECODE_BUFFER + 1]
    cmp al, PROTO_KEYBOARD_MODE
    je .mode
    cmp al, PROTO_KEYBOARD_EVENT
    je .event
    cmp al, PROTO_PING
    je .ping
    cmp al, PROTO_BIOS_WRITE
    je .unsupported
    mov al, 11                      ; RAM program owns the board
    call monitor_reply
    jmp short .done
.unsupported:
    call monitor_bios_unsupported
    jmp short .done
.mode:
    call monitor_keyboard_mode
    jmp short .done
.event:
    call monitor_keyboard_event
    jmp short .done
.ping:
    call monitor_ping
.done:
    pop bp
    pop di
    pop si
    pop dx
    pop cx
    pop bx
    pop ax
    ret
