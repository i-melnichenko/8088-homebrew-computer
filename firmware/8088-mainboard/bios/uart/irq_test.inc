; Synchronous, bounded IRQ0 diagnostic, only in the idle ROM monitor.
; Reply: status=0, result:u8, received:u16, target:u8.
; Result 0=pass, 1=timeout. Missing prerequisites return status-only errors.
; Immediate sequence retries replay the result.
IRQ_TEST_TARGET equ 8
IRQ_TEST_WAIT_BLOCKS equ 8          ; 8 * 65535 iterations; no PIT-dependent timeout

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
    mov word [es:IRQ_TEST_COUNT], 0
    mov byte [es:IRQ_TEST_RESULT], STATUS_RAM0_REQUIRED
    mov ax, ss
    test ax, ax
    jnz .complete
    mov byte [es:IRQ_TEST_RESULT], STATUS_PIC_MISSING
    cmp byte [es:PIC_PRESENT], 1
    jne .complete
    mov byte [es:IRQ_TEST_RESULT], STATUS_PIT_MISSING
    cmp byte [es:PIT_PRESENT], 1
    jne .complete
    mov byte [es:IRQ_TEST_RESULT], STATUS_PIT_NOT_COUNTING
    cmp byte [es:PIT_RUNNING], 1
    jne .complete

    call runtime_pause
    push es
    xor ax, ax
    mov es, ax
    push word [es:PIC_VECTOR_BASE * 4]
    push word [es:PIC_VECTOR_BASE * 4 + 2]
    mov word [es:PIC_VECTOR_BASE * 4], irq_test_handler
    mov ax, cs
    mov [es:PIC_VECTOR_BASE * 4 + 2], ax
    LOAD_STATE ax
    mov es, ax
    ; Program periodic channel 0; reset PIC afterwards to discard startup edges.
    mov al, 34h                   ; binary mode 2, low/high byte
    out PIT_CONTROL, al
    xor al, al                    ; divisor 65536 -> nominal 18.2065 Hz
    out PIT_COUNTER0, al
    out PIT_COUNTER0, al
    call irq_test_pic_reset
    mov al, 0FEh                  ; only IRQ0 may reach INTR
    out PIC_MASK, al
    mov byte [es:IRQ_TEST_RESULT], 1
    mov bx, IRQ_TEST_WAIT_BLOCKS
    sti
    nop                           ; explicit STI interrupt shadow
.block:
    mov cx, 0FFFFh
.wait:
    cmp word [es:IRQ_TEST_COUNT], IRQ_TEST_TARGET
    jae .passed
    loop .wait
    dec bx
    jnz .block
    jmp .cleanup
.passed:
    mov byte [es:IRQ_TEST_RESULT], 0
.cleanup:
    cli
    mov al, 0FFh
    out PIC_MASK, al
    mov al, PIT_MODE0_WORD         ; stop channel 0, OUT0 low
    out PIT_CONTROL, al
    call irq_test_pic_reset        ; discard pending requests, mask every IRQ
    xor ax, ax
    mov es, ax
    pop word [es:PIC_VECTOR_BASE * 4 + 2]
    pop word [es:PIC_VECTOR_BASE * 4]
    pop es
    call runtime_resume
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
