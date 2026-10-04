; Test one complete SRAM alias. Payload RAM is destructive; reserved bytes
; (IVT, BDA, BIOS/API state and stacks) are restored after each probe.
; Ten phases: write/verify 00, FF, 55, AA, and an address-dependent byte in
; payload RAM; probe reserved bytes during the five verify phases.
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
    mov word [es:MEM_ADDR], SRAM_TEST_BASE
    mov word [es:MEM_BLOCK], SRAM_TEST_BASE
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
    push si
    mov bx, [es:MEM_ADDR]
    mov [es:MEM_BLOCK], bx
    mov dl, [es:MEM_PHASE]
    mov si, dx
    and si, 7
    shr si, 1
    xor ax, ax
    mov es, ax
    mov cx, 16
.byte:
    cmp dl, 8
    jae .address
    mov al, [memory_patterns + si]
    jmp short .value
.address:
    mov ax, bx
    xor al, ah
.value:
    cmp bx, PROGRAM_BASE
    jb .reserved
    cmp bx, SRAM_STACK_BOTTOM
    jae .reserved
    test dl, 1
    jnz .verify
    mov [es:bx], al
    jmp short .next
.reserved:
    ; No stack access or state update while a live SRAM byte is overwritten.
    ; Even phases only advance; odd phases perform the five probes.
    test dl, 1
    jz .next
    mov dh, [es:bx]
    mov [es:bx], al
    mov ah, [es:bx]
    mov [es:bx], dh
    jmp short .compare
.verify:
    mov ah, [es:bx]
.compare:
    cmp al, ah
    jne .failed
    add word [es:BIOS_STATE_BASE + MEM_OK], 1
    adc word [es:BIOS_STATE_BASE + MEM_OK + 2], 0
.next:
    inc bx
    loop .byte
    pop si
    pop es
    mov [es:MEM_ADDR], bx
    test bx, bx
    je .phase
    test bx, 07FFh
    jnz .done
    mov byte [es:MENU_DIRTY], 1
.done:
    ret
.phase:
    mov word [es:MEM_ADDR], SRAM_TEST_BASE
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
