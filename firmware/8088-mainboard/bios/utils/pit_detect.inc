; Optional Rev2 8254, channel 0 only. ES=state, DS=ROM, IF=0, PIC masked.
; Preserve all registers. Read-back status identifies an 8254 independently
; of CLK/GATE; a separate bounded check looks for a decreasing latched count.
PIT_COUNTER0 equ 40h
PIT_CONTROL equ 43h
PIT_MODE0_WORD equ 30h             ; binary, mode 0, low/high byte
PIT_STATUS0 equ 0E2h               ; latch status only, counter 0 selected
PIT_NULL_COUNT equ 40h
PIT_LOAD_ATTEMPTS equ 256

pit_detect:
    push ax
    push bx
    push cx
    push dx
    push si
    mov byte [es:PIT_PRESENT], 0
    mov byte [es:PIT_RUNNING], 0
    mov dx, UART_SCRATCH
    in al, dx
    mov bh, al
    mov si, .formats
    mov cx, 3
.format:
    lodsb
    mov bl, al
    out PIT_CONTROL, al            ; mode 0 forces OUT0 low, no count loaded
    or bl, PIT_NULL_COUNT          ; status must report NULL until a count loads
    call .status
    and al, 7Fh                   ; OUT pin is not part of register detection
    cmp al, bl
    jne .done
    call .status
    and al, 7Fh
    cmp al, bl
    jne .done
    loop .format
    mov byte [es:PIT_PRESENT], 1
    mov al, 0FFh                   ; mode 0, 65535 ticks, no periodic interrupts
    out PIT_COUNTER0, al
    out PIT_COUNTER0, al
    mov cx, PIT_LOAD_ATTEMPTS
.loaded:
    call .status
    test al, PIT_NULL_COUNT
    jz .sample
    loop .loaded
    jmp .done                     ; no CLK: CR has not loaded into CE
.sample:
    call .count
    mov si, ax
    mov cx, 128                    ; bounded software interval; not a frequency test
.delay:
    loop .delay
    call .count
    cmp ax, si
    jae .done
    mov byte [es:PIT_RUNNING], 1
.done:
    mov al, PIT_MODE0_WORD         ; stop channel 0; OUT0 low, NULL count set
    out PIT_CONTROL, al
    mov dx, UART_SCRATCH
    mov al, bh
    out dx, al
    pop si
    pop dx
    pop cx
    pop bx
    pop ax
    ret
.status:
    mov al, PIT_STATUS0
    out PIT_CONTROL, al
    ; Disturb open-bus data with the complement of expected register status.
    mov al, bl
    not al
    out dx, al
    in al, dx
    in al, PIT_COUNTER0
    ret
.count:
    xor al, al                    ; counter 0 latch command, no mode change
    out PIT_CONTROL, al
    in al, PIT_COUNTER0
    mov ah, al
    in al, PIT_COUNTER0
    xchg al, ah
    ret
.formats: db 30h, 10h, 30h
