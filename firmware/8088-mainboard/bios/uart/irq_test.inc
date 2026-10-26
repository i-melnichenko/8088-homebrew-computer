; Synchronous, bounded IRQ0 diagnostic, only in the idle ROM monitor.
; Reply: status=0, result:u8, received:u16, target:u8.
; Result 0=pass, 1=timeout. Missing prerequisites return status-only errors.
; Immediate sequence retries replay the result.
monitor_irq_test:
    cmp word [es:DECODE_BUFFER + 3], 0
    jne .bad
    cmp byte [es:IRQ_TEST_VALID], 0
    je .new
    mov al, [es:DECODE_BUFFER + 2]
    cmp al, [es:IRQ_TEST_SEQUENCE]
    je .reply
.new:
    cmp byte [es:RAM_TEST_STATE], 1
    je .ram_busy
    call irq_test_run
.complete:
    mov al, [es:DECODE_BUFFER + 2]
    mov [es:IRQ_TEST_SEQUENCE], al
    mov byte [es:IRQ_TEST_VALID], 1
.reply:
    mov al, [es:IRQ_TEST_RESULT]
    cmp al, STATUS_PIC_MISSING
    jb .snapshot
    jmp monitor_reply
.snapshot:
    mov byte [es:RX_BUFFER + 5], 0
    mov al, [es:IRQ_TEST_RESULT]
    mov [es:RX_BUFFER + 6], al
    mov ax, [es:IRQ_TEST_COUNT]
    mov [es:RX_BUFFER + 7], ax
    mov byte [es:RX_BUFFER + 9], IRQ_TEST_TARGET
    mov cx, 5
    jmp monitor_reply_data
.bad:
    mov al, 5
    jmp monitor_reply
.ram_busy:
    mov al, 10
    jmp monitor_reply

; Preserve DX; clobber AX. Single edge-triggered PIC, normal EOI, all masked.
irq_test_pic_reset:
    mov al, 13h
    out PIC_COMMAND, al
    mov al, PIC_VECTOR_BASE
    out PIC_MASK, al
    mov al, 1
    out PIC_MASK, al
    mov al, 0FFh
    out PIC_MASK, al
    ret

; Hardware IRQ0: preserve all registers and caller flags via IRET.
irq_test_handler:
    push ax
    push es
    LOAD_STATE ax
    mov es, ax
    inc word [es:IRQ_TEST_COUNT]
    mov al, 20h                   ; non-specific EOI required for the next tick
    out PIC_COMMAND, al
    pop es
    pop ax
    iret
