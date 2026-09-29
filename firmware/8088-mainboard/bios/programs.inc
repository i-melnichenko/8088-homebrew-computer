; Four resident ORG-0 programs. Only committed records appear in the menu.
; First-fit paragraph-aligned allocation reuses deleted ranges without moving
; residents. A new BEGIN replaces a pending upload, never residents.

; AL=1-based ID (validated by caller), return ES:BX=record. Clobbers AX/DX.
program_record:
    xor ah, ah
    dec ax
    mov bx, PROGRAM_RECORD_SIZE
    mul bx
    add ax, PROGRAM_TABLE
    mov bx, ax
    ret

; Return AX=lowest free aligned address for the BEGIN size, CF=1 if full.
; Records are ID-ordered, not necessarily address-ordered after deletion.
program_allocate:
    push bx
    push cx
    push dx
    push di
    mov ax, PROGRAM_BASE
.restart:
    mov di, ax
    add di, [es:DECODE_BUFFER + 5]
    jc .full
    cmp di, SRAM_STACK_BOTTOM
    ja .full
    mov bx, PROGRAM_TABLE
    xor cx, cx
    mov cl, [es:PROGRAM_COUNT]
.scan:
    jcxz .found
    mov dx, [es:bx + 16]
    cmp di, dx
    jbe .next
    add dx, [es:bx + 18]
    cmp ax, dx
    jae .next
    mov ax, dx
    add ax, 15
    and ax, 0FFF0h
    jmp .restart
.next:
    add bx, PROGRAM_RECORD_SIZE
    loop .scan
.found:
    pop di
    pop dx
    pop cx
    pop bx
    clc
    ret
.full:
    pop di
    pop dx
    pop cx
    pop bx
    stc
    ret
