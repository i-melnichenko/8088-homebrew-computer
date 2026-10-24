; Runtime state is bank-relative, in the protected 8000h..84FFh region.
RT_FLAGS equ 8000h                 ; bit0 verified timer IRQ, bit1 verified UART IRQ
RT_PAUSED equ 8001h
RT_TICKS equ 8002h                 ; u32, 18.2065 Hz active-runtime ticks
RT_UART_IRQS equ 8006h             ; u16, used by loopback self-test
RT_HEAD equ 8008h
RT_TAIL equ 8009h
RT_LOST equ 800Ah
RT_OVERRUNS equ 800Bh              ; u16 dropped bytes
RT_HEARTBEAT_LAST equ 800Dh        ; u16 last remote RAM-test heartbeat tick
RT_RING equ 8100h                  ; 256-byte SPSC RX ring, 255 usable
RT_END equ 8500h
RT_TIMER equ 1
RT_UART equ 2
RT_HZ equ 18                    ; integer hint; exact period is 65536 PIT clocks
RT_DIVISOR equ 0                  ; PIT encodes divisor 65536 as zero

; All routines below preserve registers unless explicitly documented.
runtime_initialize:
    RUNTIME_ENTER
    push ax
    push cx
    push di
    push es
    LOAD_BANK ax
    mov es, ax
    mov di, RT_FLAGS
    xor ax, ax
    mov cx, (RT_END - RT_FLAGS) / 2
    rep stosw
    pop es
    mov byte [es:RT_PAUSED], 1
    cmp word [es:BIOS_BANK_SIGNATURE], 0B105h
    jne .done
    mov ax, [es:BIOS_BANK_POINTER]
    test ax, ax
    jnz .done
    cmp byte [es:BIOS_STATE_BASE + PIC_PRESENT], 1
    jne .done
    push es
    xor ax, ax
    mov es, ax
    mov word [es:PIC_VECTOR_BASE * 4], runtime_timer_handler
    mov word [es:(PIC_VECTOR_BASE + 4) * 4], runtime_uart_handler
    mov ax, cs
    mov [es:PIC_VECTOR_BASE * 4 + 2], ax
    mov [es:(PIC_VECTOR_BASE + 4) * 4 + 2], ax
    pop es
    cmp byte [es:BIOS_STATE_BASE + PIT_PRESENT], 1
    jne .uart
    cmp byte [es:BIOS_STATE_BASE + PIT_RUNNING], 1
    jne .uart
    call runtime_timer_start
    call irq_test_pic_reset
    mov al, 0FEh
    out PIC_MASK, al
    sti
    nop
    mov di, 4
.timer_block:
    mov cx, 0FFFFh
.timer_wait:
    cmp word [es:RT_TICKS], 2
    jae .timer_ok
    loop .timer_wait
    dec di
    jnz .timer_block
    jmp .timer_done
.timer_ok:
    or byte [es:RT_FLAGS], RT_TIMER
.timer_done:
    cli
    mov al, 0FFh
    out PIC_MASK, al
    mov al, PIT_MODE0_WORD
    out PIT_CONTROL, al
.uart:
    call runtime_uart_probe
    mov word [es:RT_TICKS], 0
    mov word [es:RT_TICKS + 2], 0
.done:
    mov byte [es:RT_PAUSED], 0
    call runtime_resume
    pop di
    pop cx
    pop ax
    RUNTIME_LEAVE
    ret

runtime_timer_start:
    RUNTIME_ENTER
    push ax
    mov al, 34h
    out PIT_CONTROL, al
    mov ax, RT_DIVISOR
    out PIT_COUNTER0, al
    mov al, ah
    out PIT_COUNTER0, al
    pop ax
    RUNTIME_LEAVE
    ret

; Internal UART loopback generates a real IRQ4, without transmitting a host byte.
runtime_uart_probe:
    RUNTIME_ENTER
    push ax
    push bx
    push cx
    push dx
    mov dx, UART_FCR
    mov al, UART_FCR_ENABLE
    out dx, al
    mov dx, UART_MCR
    mov al, 1Bh                   ; loopback, OUT2, RTS, DTR
    out dx, al
    call irq_test_pic_reset
    mov al, 0EFh                  ; only IRQ4
    out PIC_MASK, al
    mov dx, UART_IER
    mov al, 1
    out dx, al
    mov dx, UART_THR
    mov al, 0A5h
    out dx, al
    mov bx, 4
    sti
    nop
.block:
    mov cx, 0FFFFh
.wait:
    cmp word [es:RT_UART_IRQS], 0
    jne .check
    loop .wait
    dec bx
    jnz .block
    jmp .done
.check:
    cmp byte [es:RT_HEAD], 1
    jne .done
    cmp byte [es:RT_RING], 0A5h
    jne .done
    or byte [es:RT_FLAGS], RT_UART
.done:
    cli
    mov dx, UART_IER
    xor al, al
    out dx, al
    mov al, 0FFh
    out PIC_MASK, al
    mov dx, UART_MCR
    mov al, 0Bh                   ; external UART, OUT2, RTS, DTR
    out dx, al
    mov dx, UART_FCR
    mov al, UART_FCR_ENABLE
    out dx, al
    mov byte [es:RT_HEAD], 0
    mov byte [es:RT_TAIL], 0
    mov byte [es:RT_LOST], 0
    mov word [es:RT_OVERRUNS], 0
    pop dx
    pop cx
    pop bx
    pop ax
    RUNTIME_LEAVE
    ret

runtime_pause:
    RUNTIME_ENTER
    cli
    push ax
    push dx
    mov byte [es:RT_PAUSED], 1
    mov dx, UART_IER
    xor al, al
    out dx, al
    cmp byte [es:BIOS_STATE_BASE + PIC_PRESENT], 1
    jne .pit
    mov al, 0FFh
    out PIC_MASK, al
.pit:
    cmp byte [es:BIOS_STATE_BASE + PIT_PRESENT], 1
    jne .done
    mov al, PIT_MODE0_WORD
    out PIT_CONTROL, al
.done:
    pop dx
    pop ax
    RUNTIME_LEAVE
    ret

runtime_resume:
    RUNTIME_ENTER
    cli
    push ax
    push bx
    push dx
    mov byte [es:RT_PAUSED], 0
    cmp byte [es:RT_FLAGS], 0
    je .done
    test byte [es:RT_FLAGS], RT_TIMER
    jz .pic
    call runtime_timer_start
.pic:
    call irq_test_pic_reset
    mov bl, 0FFh
    test byte [es:RT_FLAGS], RT_TIMER
    jz .uart
    and bl, 0FEh
.uart:
    test byte [es:RT_FLAGS], RT_UART
    jz .mask
    and bl, 0EFh
    mov dx, UART_MCR
    mov al, 0Bh                   ; UART initialization clears OUT2; restore IRQ mode
    out dx, al
    mov dx, UART_IER
    mov al, 1
    out dx, al
.mask:
    mov al, bl
    out PIC_MASK, al
.done:
    pop dx
    pop bx
    pop ax
    RUNTIME_LEAVE
    jmp runtime_enable

runtime_enable:
    RUNTIME_ENTER
    cmp byte [es:RT_PAUSED], 0
    jne .done
    cmp byte [es:RT_FLAGS], 0
    je .done
    sti
.done:
    RUNTIME_LEAVE
    ret

runtime_timer_handler:
    RUNTIME_ENTER
    push ax
    add word [es:RT_TICKS], 1
    adc word [es:RT_TICKS + 2], 0
    push ds
    push ax
    xor ax, ax
    mov ds, ax
    add word [046Ch], 1
    adc word [046Eh], 0
    cmp word [046Eh], 0018h
    jb .clock_done
    cmp word [046Ch], 00B0h
    jb .clock_done
    mov word [046Ch], 0
    mov word [046Eh], 0
    inc byte [0470h]
.clock_done:
    pop ax
    pop ds
    push bx
    push cx
    push dx
    push si
    push di
    push bp
    push ds
    push es
    int 1Ch
    pop es
    pop ds
    pop bp
    pop di
    pop si
    pop dx
    pop cx
    pop bx
    mov al, 20h
    out PIC_COMMAND, al
    pop ax
    RUNTIME_LEAVE
    iret

runtime_uart_handler:
    RUNTIME_ENTER
    push ax
    push bx
    push cx
    push dx
    inc word [es:RT_UART_IRQS]
    mov cx, 32                    ; bounded FIFO drain; RX/timeout IRQs only
.read:
    mov dx, UART_LSR
    in al, dx
    test al, 1Eh                  ; overrun/parity/framing/break -> resync frames
    jz .ready
    mov byte [es:RT_LOST], 1
.ready:
    test al, UART_LSR_DATA_READY
    jz .done
    mov dx, UART_RBR
    in al, dx
    xor bx, bx
    mov bl, [es:RT_HEAD]
    mov ah, bl
    inc ah
    cmp ah, [es:RT_TAIL]
    je .full
    mov [es:RT_RING + bx], al
    mov [es:RT_HEAD], ah
    jmp .next
.full:
    inc word [es:RT_OVERRUNS]
    mov byte [es:RT_LOST], 1
.next:
    loop .read
.done:
    mov al, 20h
    out PIC_COMMAND, al
    pop dx
    pop cx
    pop bx
    pop ax
    RUNTIME_LEAVE
    iret

; Same contract as uart_try_getc: AL byte, CF=1 if empty, clobber DX only.
runtime_uart_getc:
    RUNTIME_ENTER
    cmp byte [es:RT_PAUSED], 0
    jne .poll
    test byte [es:RT_FLAGS], RT_UART
    jz .poll
    pushf
    ; Permit a queued IRQ even when the caller entered BIOS with IF=0.
    call runtime_enable
    nop
    cli
    push bx
    cmp byte [es:RT_LOST], 0
    je .read
    mov byte [es:RT_LOST], 0
    mov al, [es:RT_HEAD]
    mov [es:RT_TAIL], al
    mov word [es:BIOS_STATE_BASE + RX_COUNT], 0
    mov byte [es:BIOS_STATE_BASE + RX_DROP], 1
.read:
    xor bx, bx
    mov bl, [es:RT_TAIL]
    cmp bl, [es:RT_HEAD]
    je .empty
    mov al, [es:RT_RING + bx]
    inc byte [es:RT_TAIL]
    pop bx
    popf
    clc
    RUNTIME_LEAVE
    ret
.empty:
    pop bx
    popf
    stc
    RUNTIME_LEAVE
    ret

; CX milliseconds; preserve all but AX. Return AL=0 timer, AL=1 approximate
; software fallback, AH=0. Always pump cooperative host frames without consuming keys.

.poll:
    RUNTIME_LEAVE
    jmp uart_try_getc_poll

runtime_delay:
    RUNTIME_ENTER
    push bx
    push cx
    push dx
    push si
    push di
    push bp
    xor ax, ax
    jcxz .done
    test byte [es:RT_FLAGS], RT_TIMER
    jz .software
    mov ax, cx
    mov bx, 1194
    mul bx
    mov ax, dx
    inc ax                        ; ceil to ticks (slightly conservative)
    inc ax                        ; account for initial tick phase
    mov di, ax
    mov si, [es:RT_TICKS]
    mov dx, 4                     ; inactivity watchdog, independent of PIT
    xor bx, bx
    xor bp, bp
.hardware:
    call runtime_enable
    call runtime_pump
    mov ax, [es:RT_TICKS]
    sub ax, si                     ; wrap-safe, waits <=6555 ticks
    cmp ax, di
    jae .timer_done
    cmp ax, bp
    je .watchdog
    mov bp, ax
    xor bx, bx
    mov dx, 4
.watchdog:
    dec bx
    jnz .hardware
    dec dx
    jnz .hardware
    ; Lost timer: disable its backend and finish with approximate waiting.
    call runtime_pause
    and byte [es:RT_FLAGS], 0FEh
    call runtime_resume
.software:
    mov di, cx
.millisecond:
    mov bx, 256                    ; approximate at 4.77 MHz, not a time guarantee
.slice:
    dec bx
    jnz .slice
    call runtime_enable
    call runtime_pump
    dec di
    jnz .millisecond
    mov ax, 1
    jmp .done
.timer_done:
    xor ax, ax
.done:
    pop bp
    pop di
    pop si
    pop dx
    pop cx
    pop bx
    RUNTIME_LEAVE
    ret

; Only framed host sessions own UART input. Raw input must remain available
; for INT 14h/INT 16h instead of being consumed as incomplete COBS packets.
runtime_pump:
    RUNTIME_ENTER
    cmp byte [es:BIOS_STATE_BASE + API_HOST_MODE], 0
    je .done
    push es
    push ax
    LOAD_STATE ax
    mov es, ax
    pop ax
    call host_poll
    pop es
.done:
    RUNTIME_LEAVE
    ret
