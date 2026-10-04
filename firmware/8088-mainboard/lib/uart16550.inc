; GM16C550 / 16550 register layout. Rev2 UART is on the mainboard at COM1.

UART_BASE equ 03F8h
UART_DIVISOR equ 12                   ; 9600 baud
UART_RBR  equ UART_BASE + 0       ; receive buffer (read)
UART_THR  equ UART_BASE + 0       ; transmit holding register (write)
UART_IER  equ UART_BASE + 1
UART_FCR  equ UART_BASE + 2
UART_LCR  equ UART_BASE + 3
UART_MCR  equ UART_BASE + 4
UART_LSR  equ UART_BASE + 5
UART_DLL  equ UART_BASE + 0       ; DLAB set
UART_DLM  equ UART_BASE + 1       ; DLAB set

UART_LCR_8N1       equ 03h
UART_LCR_DLAB      equ 80h
UART_FCR_ENABLE    equ 07h        ; enable and clear both FIFOs
UART_LSR_DATA_READY equ 01h
UART_LSR_THR_EMPTY equ 20h

; 1.8432 MHz / 16 / divisor.

uart_initialize:
    mov dx, UART_IER
    xor al, al
    out dx, al                    ; polling only; no UART interrupts yet
    mov dx, UART_LCR
    mov al, UART_LCR_DLAB + UART_LCR_8N1
    out dx, al
    mov dx, UART_DLL
    mov al, UART_DIVISOR
    out dx, al
    mov dx, UART_DLM
    xor al, al
    out dx, al
    mov dx, UART_LCR
    mov al, UART_LCR_8N1
    out dx, al
    mov dx, UART_FCR
    mov al, UART_FCR_ENABLE
    out dx, al
    mov dx, UART_MCR
    mov al, 03h                   ; DTR and RTS asserted
    out dx, al
    ret

; Write AL, waiting until the transmitter can accept a byte.
uart_putc:
    push ax
.wait:
    mov dx, UART_LSR
    in al, dx
    test al, UART_LSR_THR_EMPTY
    jz .wait
    pop ax
    mov dx, UART_THR
    out dx, al
    ret

; Nonblocking receive: CF=1 when no byte is available, otherwise AL=byte.
uart_try_getc:
    mov dx, UART_LSR
    in al, dx
    test al, UART_LSR_DATA_READY
    jz .empty
    mov dx, UART_RBR
    in al, dx
    clc
    ret
.empty:
    stc
    ret

; Write a NUL-terminated string from DS:SI.
uart_print:
    lodsb
    test al, al
    jz .done
    call uart_putc
    jmp short uart_print
.done:
    ret
