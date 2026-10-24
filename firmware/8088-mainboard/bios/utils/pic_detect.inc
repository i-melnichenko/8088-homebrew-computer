; Rev2 single 8259A at 20h/21h. Called once after UART/state initialization,
; with IF=0. Preserves all registers; ES addresses BIOS state. No IRQ enabled.
PIC_COMMAND equ 20h
PIC_MASK equ 21h
PIC_VECTOR_BASE equ 08h
UART_SCRATCH equ UART_BASE + 7

pic_detect:
    push ax
    push bx
    push cx
    push dx
    push si
    mov byte [es:PIC_PRESENT], 0
    mov al, 13h                   ; ICW1: single, edge-triggered, ICW4 follows
    out PIC_COMMAND, al
    mov al, PIC_VECTOR_BASE       ; ICW2: reserve hardware IRQ vectors 08h..0Fh
    out PIC_MASK, al
    mov al, 01h                   ; ICW4: 8086 mode, normal EOI, non-buffered
    out PIC_MASK, al
    mov dx, UART_SCRATCH
    in al, dx
    mov bh, al                    ; restore UART scratch even on failed probe
    mov si, .patterns
    mov cx, 4
.pattern:
    lodsb
    mov bl, al
    out PIC_MASK, al
    ; Drive/read the opposite value through a known UART register before each
    ; IMR read: an empty socket must not pass by retaining the last OUT byte.
    call .read_mask
    cmp al, bl
    jne .done
    call .read_mask
    cmp al, bl
    jne .done
    loop .pattern
    mov byte [es:PIC_PRESENT], 1
.done:
    mov al, 0FFh                  ; leave all eight IRQs masked on every path
    out PIC_MASK, al
    mov dx, UART_SCRATCH
    mov al, bh
    out dx, al
    pop si
    pop dx
    pop cx
    pop bx
    pop ax
    ret
.read_mask:
    mov al, bl
    not al
    out dx, al
    in al, dx
    in al, PIC_MASK
    ret
.patterns: db 55h, 0AAh, 00h, 0FFh
