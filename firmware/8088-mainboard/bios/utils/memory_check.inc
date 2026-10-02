; Destructive test of ONLY payload RAM; buffers and 1 KiB stack are excluded.
; Ten passes: write/read 00, FF, 55, AA, and an address-dependent byte.
; Work is sliced into 16-byte units so menu/protocol remain responsive.
; Count every byte verification (five per address), continue after failures,
; and retain the first mismatch for the log. Counters are 32-bit.
memory_start:
    mov byte [es:PROGRAM_COUNT], 0
    mov byte [es:UPLOAD_ACTIVE], 0
    mov byte [es:ACTIVE_ID], 0
    mov byte [es:EXEC_VALID], 0
    mov byte [es:DELETE_VALID], 0
    mov word [es:MONITOR_TOTAL], 0
    mov word [es:MONITOR_RECEIVED], 0
    mov byte [es:LAST_WRITE_VALID], 0
    mov word [es:MEM_ADDR], PROGRAM_BASE
    mov word [es:MEM_BLOCK], PROGRAM_BASE
    mov byte [es:MEM_PHASE], 0
    mov byte [es:MEM_ERROR], 0
    mov word [es:MEM_OK], 0
    mov word [es:MEM_OK + 2], 0
    mov word [es:MEM_BAD], 0
    mov word [es:MEM_BAD + 2], 0
    mov byte [es:MENU_PAGE], PAGE_TEST
    ret

memory_step:
    cmp byte [es:MENU_PAGE], PAGE_TEST
    je .active
    ret
.active:
    push es
    mov bx, [es:MEM_ADDR]
    mov [es:MEM_BLOCK], bx
    mov dl, [es:MEM_PHASE]
    xor ax, ax
    mov es, ax
    mov cx, 16
.byte:
    cmp dl, 8
    jae .address
    xor ax, ax
    mov al, dl
    shr al, 1
    push bx
    mov bx, ax
    mov al, [memory_patterns + bx]
    pop bx
    jmp short .value
.address:
    mov ax, bx
    xor al, ah
.value:
    test dl, 1
    jnz .verify
    mov [es:bx], al
    jmp short .next
.verify:
    mov ah, [es:bx]
    cmp al, ah
    jne .failed
    add word [es:BIOS_STATE_BASE + MEM_OK], 1
    adc word [es:BIOS_STATE_BASE + MEM_OK + 2], 0
.next:
    inc bx
    loop .byte
    pop es
    mov [es:MEM_ADDR], bx
    cmp bx, SRAM_STACK_BOTTOM
    je .phase
    test bx, 07FFh
    jnz .done
    mov byte [es:MENU_DIRTY], 1
.done:
    ret
.phase:
    mov word [es:MEM_ADDR], PROGRAM_BASE
    inc byte [es:MEM_PHASE]
    cmp byte [es:MEM_PHASE], 10
    jb .redraw
    mov byte [es:MENU_PAGE], PAGE_RESULT
.redraw:
    mov byte [es:MENU_DIRTY], 1
    ret
.failed:
    add word [es:BIOS_STATE_BASE + MEM_BAD], 1
    adc word [es:BIOS_STATE_BASE + MEM_BAD + 2], 0
    cmp byte [es:BIOS_STATE_BASE + MEM_ERROR], 0
    jne .next
    mov [es:BIOS_STATE_BASE + MEM_FAIL_ADDR], bx
    mov [es:BIOS_STATE_BASE + MEM_EXPECTED], al
    mov [es:BIOS_STATE_BASE + MEM_ACTUAL], ah
    mov byte [es:BIOS_STATE_BASE + MEM_ERROR], 1
    mov byte [es:BIOS_STATE_BASE + MENU_DIRTY], 1
    jmp .next

memory_patterns: db 00h, 0FFh, 055h, 0AAh
