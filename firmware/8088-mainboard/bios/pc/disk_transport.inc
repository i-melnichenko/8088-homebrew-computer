monitor_disk_mount:
    xor bx, bx
    cmp word [es:DECODE_BUFFER + 3], 0
    je .validated
    cmp word [es:DECODE_BUFFER + 3], 1
    jne keyboard_request_error
    mov bl, [es:DECODE_BUFFER + 5]
    cmp bl, 1
    ja keyboard_request_error
.validated:
    cmp byte [es:OS_RUNNING], 0
    jne .busy
    cmp byte [es:RAM_TEST_STATE], 1
    je .busy
    push es
    LOAD_BANK ax
    mov es, ax
    cmp word [es:BIOS_BANK_SIGNATURE], 0B105h
    jne .bad
    mov [es:PC_WRITABLE], bl
    mov byte [es:PC_CACHE_VALID], 0
    mov byte [es:PC_MOUNTED], 1
    mov byte [es:PC_PENDING], 0
    mov byte [es:PC_ERROR], 0
    pop es
    xor al, al
    jmp monitor_reply
.bad:
    pop es
    mov al, 18
    jmp monitor_reply
.busy:
    mov al, 11
    jmp monitor_reply

monitor_disk_poll:
    cmp word [es:DECODE_BUFFER + 3], 0
    jne keyboard_request_error
    push es
    LOAD_BANK ax
    mov es, ax
    mov byte [es:BIOS_STATE_BASE + RX_BUFFER + 5], 0
    mov al, [es:PC_PENDING]
    mov [es:BIOS_STATE_BASE + RX_BUFFER + 6], al
    mov ax, [es:PC_REQUEST]
    mov [es:BIOS_STATE_BASE + RX_BUFFER + 7], ax
    mov ax, [es:PC_LBA]
    mov [es:BIOS_STATE_BASE + RX_BUFFER + 9], ax
    mov ax, [es:PC_RECEIVED]
    mov [es:BIOS_STATE_BASE + RX_BUFFER + 11], ax
    pop es
    mov cx, 8
    jmp monitor_reply_data

monitor_disk_data:
    mov cx, [es:DECODE_BUFFER + 3]
    cmp cx, 5
    jb keyboard_request_error
    cmp cx, 125
    ja keyboard_request_error
    sub cx, 5
    mov bx, [es:DECODE_BUFFER + 5] ; request id
    mov dx, [es:DECODE_BUFFER + 7] ; sector offset
    mov al, [es:DECODE_BUFFER + 9] ; host error
    push es
    push ds
    push ax
    mov ax, es
    mov ds, ax
    LOAD_BANK ax
    mov es, ax
    pop ax
    push ax
    push cx
    push si
    push di
    cmp byte [es:PC_CACHE_VALID], 1
    jne .cache_miss
    mov cx, [ds:DECODE_BUFFER + 3]
    cmp cx, [es:PC_CACHE_LENGTH]
    jne .cache_miss
    mov si, DECODE_BUFFER + 5
    mov di, PC_CACHE_DATA
    repe cmpsb
    jne .cache_miss
    pop di
    pop si
    pop cx
    pop ax
    jmp .reply_cached
.cache_miss:
    pop di
    pop si
    pop cx
    pop ax
    cmp byte [es:PC_PENDING], 1
    jne .bad
    cmp bx, [es:PC_REQUEST]
    jne .bad
    test al, al
    jnz .host_error
    jcxz .bad
    cmp dx, [es:PC_RECEIVED]
    ja .bad
    mov ax, dx
    add ax, cx
    jc .bad
    cmp ax, 512
    ja .bad
    mov si, DECODE_BUFFER + 10
    mov di, PC_SECTOR
    add di, dx
    cmp dx, [es:PC_RECEIVED]
    je .copy
    cmp ax, [es:PC_RECEIVED]
    ja .bad
    repe cmpsb                    ; exact duplicate chunk only
    jne .bad
    jmp .ok
.copy:
    rep movsb
    mov [es:PC_RECEIVED], ax
    cmp ax, 512
    jne .ok
    mov byte [es:PC_PENDING], 0
.ok:
    mov byte [es:PC_CACHE_VALID], 1
    mov cx, [ds:DECODE_BUFFER + 3]
    mov [es:PC_CACHE_LENGTH], cx
    mov si, DECODE_BUFFER + 5
    mov di, PC_CACHE_DATA
    rep movsb
.reply_cached:
    pop ds
    pop es
    xor al, al
    jmp monitor_reply
.host_error:
    mov byte [es:PC_ERROR], 80h
    mov byte [es:PC_PENDING], 0
    jmp .ok
.bad:
    pop ds
    pop es
    mov al, 18
    jmp monitor_reply

monitor_disk_boot:
    cmp word [es:DECODE_BUFFER + 3], 0
    jne keyboard_request_error
    cmp byte [es:OS_RUNNING], 0
    jne .bad
    cmp byte [es:API_HOST_MODE], 2
    jne .bad
    cmp byte [es:RAM_TEST_STATE], 1
    je .bad
    RUNTIME_ENTER
    cmp byte [es:PC_MOUNTED], 1
    jne .runtime_bad
    test byte [es:RT_FLAGS], RT_TIMER
    jz .runtime_bad
    mov al, [es:BIOS_STATE_BASE + DECODE_BUFFER + 2]
    mov [es:PC_BOOT_SEQUENCE], al
    mov byte [es:PC_BOOT_VALID], 1
    RUNTIME_LEAVE
    xor al, al
    call monitor_reply
    call monitor_wait_tx_empty
    jmp pc_boot
.runtime_bad:
    RUNTIME_LEAVE
.bad:
    mov al, 18
    jmp monitor_reply

; A lost boot ACK must not turn a successful boot into a host-side busy error.
monitor_disk_boot_retry:
    cmp word [es:DECODE_BUFFER + 3], 0
    jne keyboard_request_error
    mov al, [es:DECODE_BUFFER + 2]
    RUNTIME_ENTER
    cmp byte [es:PC_BOOT_VALID], 1
    jne .busy
    cmp byte [es:PC_MODE], 1
    jne .busy
    cmp al, [es:PC_BOOT_SEQUENCE]
    jne .busy
    RUNTIME_LEAVE
    xor al, al
    jmp monitor_reply
.busy:
    RUNTIME_LEAVE
    mov al, 11
    jmp monitor_reply


; Side-effect-free fetch of a pending write's immutable sector buffer.
; Request: id:u16, offset:u16. Reply: 0, id:u16, offset:u16, <=120 bytes.
monitor_disk_write_data:
    cmp word [es:DECODE_BUFFER + 3], 4
    jne keyboard_request_error
    mov bx, [es:DECODE_BUFFER + 5]
    mov dx, [es:DECODE_BUFFER + 7]
    cmp dx, 512
    jae .invalid
    push es
    push ds
    LOAD_BANK ax
    mov es, ax
    cmp byte [es:PC_PENDING], 2
    jne .bad
    cmp bx, [es:PC_REQUEST]
    jne .bad
    mov byte [es:BIOS_STATE_BASE + RX_BUFFER + 5], 0
    mov [es:BIOS_STATE_BASE + RX_BUFFER + 6], bx
    mov [es:BIOS_STATE_BASE + RX_BUFFER + 8], dx
    mov cx, 512
    sub cx, dx
    cmp cx, 120
    jbe .length
    mov cx, 120
.length:
    push cx
    mov si, PC_SECTOR
    add si, dx
    mov di, BIOS_STATE_BASE + RX_BUFFER + 10
    push es
    pop ds
    rep movsb
    pop cx
    add cx, 5
    pop ds
    pop es
    jmp monitor_reply_data
.bad:
    pop ds
    pop es
.invalid:
    mov al, 18
    jmp monitor_reply

; Host reports BIOS status (0=committed). Exact ACK retries are accepted even
; after the guest starts its next request. Cache length 3 distinguishes ACKs
; from DISK_DATA payloads, whose minimum length is 5.
monitor_disk_write_ack:
    cmp word [es:DECODE_BUFFER + 3], 3
    jne keyboard_request_error
    push ds
    push es
    push es
    pop ds
    LOAD_BANK ax
    mov es, ax
    cmp byte [es:PC_CACHE_VALID], 1
    jne .active
    cmp word [es:PC_CACHE_LENGTH], 3
    jne .active
    mov si, DECODE_BUFFER + 5
    mov di, PC_CACHE_DATA
    mov cx, 3
    repe cmpsb
    je .ok
.active:
    cmp byte [es:PC_PENDING], 2
    jne .bad
    mov ax, [ds:DECODE_BUFFER + 5]
    cmp ax, [es:PC_REQUEST]
    jne .bad
    mov al, [ds:DECODE_BUFFER + 7]
    mov [es:PC_ERROR], al
    mov byte [es:PC_PENDING], 0
    mov byte [es:PC_CACHE_VALID], 1
    mov word [es:PC_CACHE_LENGTH], 3
    mov si, DECODE_BUFFER + 5
    mov di, PC_CACHE_DATA
    mov cx, 3
    rep movsb
.ok:
    pop es
    pop ds
    xor al, al
    jmp monitor_reply
.bad:
    pop es
    pop ds
    mov al, 18
    jmp monitor_reply
