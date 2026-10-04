; Cooperative UART keyboard sharing. No IRQ/PIC: the ROM loop and INT 16h
; pump frames. The BDA ring at 0040:001E..003D holds up to fifteen AX keys.
KEYBOARD_BEGIN equ 001Eh
KEYBOARD_END equ 003Eh

; AX=ASCII:scan (AL=ASCII, AH=scan). CF=1 if the ring is full.
keyboard_enqueue:
    push es
    mov dx, 0040h
    mov es, dx
    mov bx, [es:001Ch]
    mov dx, bx
    add dx, 2
    cmp dx, KEYBOARD_END
    jb .next
    mov dx, KEYBOARD_BEGIN
.next:
    cmp dx, [es:001Ah]
    je .full
    mov [es:bx], ax
    mov [es:001Ch], dx
    pop es
    clc
    ret
.full:
    pop es
    stc
    ret

; AX=oldest key, CF=1 if empty. Peek never advances the BDA head.
keyboard_peek:
    push es
    mov ax, 0040h
    mov es, ax
    mov bx, [es:001Ah]
    cmp bx, [es:001Ch]
    je .empty
    mov ax, [es:bx]
    pop es
    clc
    ret
.empty:
    pop es
    stc
    ret

keyboard_take:
    call keyboard_peek
    jc .done
    push es
    mov dx, 0040h
    mov es, dx
    add bx, 2
    cmp bx, KEYBOARD_END
    jb .head
    mov bx, KEYBOARD_BEGIN
.head:
    mov [es:001Ah], bx
    pop es
    clc
.done:
    ret

; Menu and payloads consume the same queue. Never read raw UART here;
; incoming host events have already been checked by the frame decoder.
keyboard_menu_step:
    cmp byte [es:API_RUNNING], 0
    jne .nothing
    call keyboard_take
    jc .nothing
    cmp al, 0Dh
    je .enter
    cmp al, 1Bh
    je .back
    test al, al
    jnz .nothing
    cmp ah, 48h
    je .up
    cmp ah, 50h
    jne .nothing
    mov al, KEY_DOWN
    jmp menu_key
.up:
    mov al, KEY_UP
    jmp menu_key
.enter:
    mov al, KEY_ENTER
    jmp menu_key
.back:
    mov al, KEY_BACK
    jmp menu_key
.nothing:
    xor al, al
    ret
