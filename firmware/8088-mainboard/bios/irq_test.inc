; Shared bounded IRQ0 diagnostic for UART and the LCD menu.
; ES=BIOS state, DS=ROM. Restores the runtime and original IRQ0 vector.
IRQ_TEST_TARGET equ 8
IRQ_TEST_WAIT_BLOCKS equ 8          ; software timeout, independent of PIT

irq_test_run:
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
    ret
