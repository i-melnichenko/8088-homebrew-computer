; MEMORY_WRITE payload: physical address:u32, 1..124 data bytes.
; Restrict the complete range to selected-bank payload SRAM.
; IVT/BDA, monitor/API state and both stacks are never writable by this command.
monitor_memory_write:
    cmp byte [es:MENU_PAGE], PAGE_TEST
    je .test_busy
    mov cx, [es:DECODE_BUFFER + 3]
    cmp cx, 5
    jb .error
    cmp cx, PROTO_MAX_PAYLOAD
    ja .error
    mov ax, ss
    mov cl, 12
    shr ax, cl
    mov cx, [es:DECODE_BUFFER + 3]
    sub cx, 4
    cmp [es:DECODE_BUFFER + 7], ax
    jne .error
    mov ax, [es:DECODE_BUFFER + 5]
    cmp ax, FLASHER_BASE
    jb .error
    mov di, ax
    add ax, cx
    jc .error
    cmp ax, SRAM_STACK_BOTTOM
    ja .error                       ; whole block must precede stack RAM
    ; Validation is complete before the first write. Use the selected bank.
    push ds
    push es
    mov ax, es
    mov ds, ax
    mov ax, ss
    mov es, ax
    mov si, DECODE_BUFFER + 9
    rep movsb
    pop es
    pop ds
    xor al, al
    jmp monitor_reply
.error:
    mov al, 14                      ; invalid memory request/range
    jmp monitor_reply
.test_busy:
    mov al, 10
    jmp monitor_reply
