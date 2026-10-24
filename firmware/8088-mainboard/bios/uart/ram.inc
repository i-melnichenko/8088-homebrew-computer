; RAM_INFO returns a 11-byte atomic bank/memory snapshot (status included).
; RAM_TEST returns a 31-byte diagnostic snapshot. See uart-protocol-v1.md.
RAM_MAP_SIZE equ 11
RAM_STATUS_SIZE equ 31

; AX=number of POST-approved chips. Clobbers BX/CX; preserves DX/SI/DI/BP.
ram_bank_count:
    xor ax, ax
    mov bl, [es:RAM_BANK_MASK]
    mov cx, 5
.next:
    shr bl, 1
    adc ax, 0
    loop .next
    ret

monitor_ram_info:
    cmp word [es:DECODE_BUFFER + 3], 0
    jne ram_request_error
    mov byte [es:RX_BUFFER + 5], 0
    mov al, [es:RAM_BANK_MASK]
    mov [es:RX_BUFFER + 6], al
    mov ax, ss
    mov cl, 13
    shr ax, cl
    mov [es:RX_BUFFER + 7], al
    mov al, 2                      ; heartbeat support
    cmp byte [es:RAM_TEST_STATE], 1
    jne .flags
    or al, 1
.flags:
    mov [es:RX_BUFFER + 8], al
    call ram_bank_count
    shl ax, 1
    mov word [es:RX_BUFFER + 9], 0
    mov [es:RX_BUFFER + 11], ax
    LOAD_BDA ax
    push es
    mov es, ax
    mov ax, [es:0413h]
    pop es
    mov [es:RX_BUFFER + 13], ax
    mov al, [es:RAM_TEST_STATE]
    mov [es:RX_BUFFER + 15], al
    mov cx, 11
    jmp monitor_reply_data

; Action 0=status, 1=start with A5 confirmation, 2=cancel.
; Cache only the immediate valid START retry, including after completion.
monitor_ram_test:
    cmp word [es:DECODE_BUFFER + 3], 1
    jb ram_request_error
    mov al, [es:DECODE_BUFFER + 5]
    cmp al, 1
    je .start
    cmp word [es:DECODE_BUFFER + 3], 1
    jne ram_request_error
    cmp al, 2
    ja ram_request_error
    mov byte [es:RAM_TEST_START_VALID], 0
    test al, al
    jz monitor_ram_status
    call memory_cancel
    jmp monitor_ram_status
.start:
    cmp word [es:DECODE_BUFFER + 3], 2
    jne ram_request_error
    cmp byte [es:DECODE_BUFFER + 6], RAM_TEST_CONFIRM
    jne ram_request_error
    mov al, [es:DECODE_BUFFER + 2]
    cmp byte [es:RAM_TEST_START_VALID], 0
    je .new
    cmp al, [es:RAM_TEST_SEQUENCE]
    je monitor_ram_status
.new:
    cmp byte [es:RAM_TEST_STATE], 1
    je .busy
    call memory_start
    mov byte [es:RAM_TEST_REMOTE], 1
    mov al, [es:DECODE_BUFFER + 2]
    mov [es:RAM_TEST_SEQUENCE], al
    mov byte [es:RAM_TEST_START_VALID], 1
    mov byte [es:MENU_DIRTY], 1
    jmp monitor_ram_status
.busy:
    mov al, 10
    jmp monitor_reply

ram_request_error:
    mov al, 5
    jmp monitor_reply

monitor_ram_status:
    cmp byte [es:RAM_TEST_STATE], 1
    jne .snapshot
    cmp byte [es:RAM_TEST_REMOTE], 0
    je .snapshot
    mov word [es:RAM_TEST_HEARTBEAT], RAM_TEST_HEARTBEAT_TICKS
    mov ax, [ss:RT_TICKS]
    mov [ss:RT_HEARTBEAT_LAST], ax
.snapshot:
    mov di, RX_BUFFER + 5
    xor ax, ax
    mov cx, RAM_STATUS_SIZE
    rep stosb
    mov al, [es:RAM_TEST_STATE]
    mov [es:RX_BUFFER + 6], al
    mov al, [es:RAM_BANK_MASK]
    mov [es:RX_BUFFER + 7], al
    mov ax, ss
    mov cl, 13
    shr ax, cl
    mov [es:RX_BUFFER + 8], al
    mov al, [es:MEM_PHASE]
    mov [es:RX_BUFFER + 9], al
    mov ax, [es:MEM_BLOCK]
    mov [es:RX_BUFFER + 10], ax
    xor ax, ax
    mov al, [es:MEM_BLOCK_HI]
    mov [es:RX_BUFFER + 12], ax
    mov ax, [es:MEM_OK]
    mov [es:RX_BUFFER + 14], ax
    mov ax, [es:MEM_OK + 2]
    mov [es:RX_BUFFER + 16], ax
    mov ax, [es:MEM_BAD]
    mov [es:RX_BUFFER + 18], ax
    mov ax, [es:MEM_BAD + 2]
    mov [es:RX_BUFFER + 20], ax
    call ram_bank_count
    mov bx, 10
    mul bx
    mov [es:RX_BUFFER + 24], ax      ; count * 655360 verifications
    mov ax, [es:MEM_FAIL_ADDR]
    mov [es:RX_BUFFER + 26], ax
    xor ax, ax
    mov al, [es:MEM_FAIL_HI]
    mov [es:RX_BUFFER + 28], ax
    mov al, [es:MEM_EXPECTED]
    mov [es:RX_BUFFER + 30], al
    mov al, [es:MEM_ACTUAL]
    mov [es:RX_BUFFER + 31], al
    mov ax, [es:RAM_TEST_UNITS]
    mov [es:RX_BUFFER + 32], ax
    mov ax, [es:RAM_TEST_UNITS + 2]
    mov [es:RX_BUFFER + 34], ax
    mov cx, RAM_STATUS_SIZE
    jmp monitor_reply_data
