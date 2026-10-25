; Stackless early POST. BP=usable bank mask; PIC/PIT untouched.
%macro POST_TEXT 1
    mov si, %1
%%next:
    lodsb
    test al, al
    jz %%done
    mov ah, al
    mov dx, UART_LSR
%%wait:
    in al, dx
    test al, UART_LSR_THR_EMPTY
    jz %%wait
    mov al, ah
    mov dx, UART_THR
    out dx, al
    jmp %%next
%%done:
%endmacro

; Distinct samples for A0..A15, both ends and both halves of each chip.
; Fill first, then verify: immediate write/read alone can hide address aliases.
%macro POST_PROBE 1
    mov ax, %1 * 2000h
    mov es, ax
    mov di, 2
%%half_write:
    mov si, post_offsets
    mov cx, (post_offsets_end - post_offsets) / 2
    mov al, 31h + %1 * 37
%%write:
    mov bx, [si]
    add si, 2
    mov [es:bx], al
    inc al
    loop %%write
    mov ax, es
    add ax, 1000h
    mov es, ax
    dec di
    jnz %%half_write
    mov di, 2
    mov ax, %1 * 2000h
    mov es, ax
%%half_verify:
    mov si, post_offsets
    mov cx, (post_offsets_end - post_offsets) / 2
    mov al, 31h + %1 * 37
%%verify:
    mov bx, [si]
    add si, 2
    cmp [es:bx], al
    jne %%failed
    not al
    mov [es:bx], al
    mov ah, [post_offsets]          ; ROM read breaks floating-bus write echo
    cmp [es:bx], al
    jne %%failed
    not al
    inc al
    loop %%verify
    mov ax, es
    add ax, 1000h
    mov es, ax
    dec di
    jnz %%half_verify
    ; Verify A16 independently; leave per-bank signatures for alias check.
    mov ax, %1 * 2000h
    mov es, ax
    mov byte [es:0], 55h
    add ax, 1000h
    mov es, ax
    mov byte [es:0], 0AAh
    sub ax, 1000h
    mov es, ax
    cmp byte [es:0], 55h
    jne %%failed
    mov byte [es:0], %1 + 20h
    add ax, 1000h
    mov es, ax
    mov byte [es:0], %1 + 70h
    or bp, 1 << %1
%%failed:
%endmacro

%macro POST_REPORT 1
    test bp, 1 << %1
    jz %%bad
    mov ax, %1 * 2000h
    mov es, ax
    cmp byte [es:0], %1 + 20h
    jne %%bad
    add ax, 1000h
    mov es, ax
    cmp byte [es:0], %1 + 70h
    jne %%bad
    POST_TEXT post_bank %+ %1
    POST_TEXT post_ok
    jmp %%next
%%bad:
    and bp, 0FFFFh ^ (1 << %1)
    POST_TEXT post_bank %+ %1
    POST_TEXT post_fail
%%next:
%endmacro

ram_detect:
    ; Inline UART initialization: no stack exists yet.
    mov dx, UART_LCR
    mov al, UART_LCR_8N1
    out dx, al
    mov dx, UART_IER
    xor al, al
    out dx, al
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
    mov al, 03h
    out dx, al
    POST_TEXT post_banner
    xor bp, bp
%assign bank 0
%rep 5
    POST_PROBE bank
%assign bank bank + 1
%endrep
%assign bank 0
%rep 5
    POST_REPORT bank
%assign bank bank + 1
%endrep
    test bp, bp
    jz .no_ram
    xor ax, ax
    mov dx, bp
.select:
    shr dx, 1
    jc .selected
    add ax, 2000h
    jmp .select
.selected:
    mov es, ax
    jmp ram_detected
.no_ram:
    POST_TEXT post_no_ram
.halt:
    hlt
    jmp .halt

post_offsets: dw 0, 1, 2, 4, 8, 10h, 20h, 40h, 80h, 100h, 200h
              dw 400h, 800h, 1000h, 2000h, 4000h, 8000h, 0FFFFh
post_offsets_end:
post_banner: db 13,10,'8088 REV2 RAM POST',13,10,0
post_bank0: db 'RAM0 00000-1FFFF ',0
post_bank1: db 'RAM1 20000-3FFFF ',0
post_bank2: db 'RAM2 40000-5FFFF ',0
post_bank3: db 'RAM3 60000-7FFFF ',0
post_bank4: db 'RAM4 80000-9FFFF ',0
post_ok: db 'OK',13,10,0
post_fail: db 'FAIL/ABSENT',13,10,0
post_no_ram: db 'NO USABLE RAM - HALTED',13,10,0
