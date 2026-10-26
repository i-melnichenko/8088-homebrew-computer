; Paint the running screen before starting the bounded hardware diagnostic.
menu_irq_step:
    cmp byte [es:MENU_PAGE], PAGE_IRQ_TEST
    jne .done
    cmp byte [es:SCREEN_POSITION], 80
    jb .done
    mov byte [es:IRQ_TEST_VALID], 0
    mov word [es:IRQ_TEST_COUNT], 0
    mov byte [es:IRQ_TEST_RESULT], 10
    cmp byte [es:RAM_TEST_STATE], 1
    je .result
    call irq_test_run
.result:
    mov byte [es:MENU_PAGE], PAGE_IRQ_RESULT
    mov byte [es:MENU_DIRTY], 1
.done:
    ret

; DI points at row 2; the section header remains IRQ TEST on every path.
menu_irq_render:
    cmp byte [es:MENU_PAGE], PAGE_IRQ_TEST
    jne .result
    mov si, menu_irq_running
    call menu_line
    mov si, menu_irq_path
    call menu_line
    mov si, menu_irq_wait
    jmp menu_line
.result:
    mov al, [es:IRQ_TEST_RESULT]
    mov si, menu_irq_pass
    test al, al
    jz .status
    mov si, menu_irq_timeout
    cmp al, 1
    je .status
    mov si, menu_irq_pic_missing
    cmp al, STATUS_PIC_MISSING
    je .status
    mov si, menu_irq_pit_missing
    cmp al, STATUS_PIT_MISSING
    je .status
    mov si, menu_irq_ram0_required
    cmp al, STATUS_RAM0_REQUIRED
    je .status
    mov si, menu_irq_pit_stopped
    cmp al, STATUS_PIT_NOT_COUNTING
    je .status
    mov si, menu_irq_ram_busy
.status:
    call menu_line
    push di
    mov si, menu_irq_count
    call menu_line
    pop bx
    add bx, 7
    mov ax, [es:IRQ_TEST_COUNT]
    mov si, 10
    mov cx, 3
.digits:
    xor dx, dx
    div si
    add dl, '0'
    mov [es:bx], dl
    dec bx
    loop .digits
    mov si, back_hint
    jmp menu_line

menu_irq_title: db 'IRQ TEST', 0
menu_irq_running: db 'CHECKING IRQ0...', 0
menu_irq_path: db 'PIT -> PIC -> CPU', 0
menu_irq_wait: db 'PLEASE WAIT', 0
menu_irq_pass: db 'RESULT: PASS', 0
menu_irq_timeout: db 'RESULT: TIMEOUT', 0
menu_irq_pic_missing: db 'PIC NOT DETECTED', 0
menu_irq_pit_missing: db 'PIT NOT DETECTED', 0
menu_irq_ram0_required: db 'RAM0 REQUIRED', 0
menu_irq_pit_stopped: db 'PIT NOT COUNTING', 0
menu_irq_ram_busy: db 'RAM TEST RUNNING', 0
menu_irq_count: db 'IRQS:000/008', 0
