; Floppy INT 13h subset: reset, status, CHS read/write, parameters, type.
pc_disk:
    API_ENTER
    test dl, dl
    jnz .not_ready
    cmp ah, 0
    je .reset
    cmp ah, 1
    je .status
    cmp ah, 2
    je .read
    cmp ah, 3
    je .write
    cmp ah, 8
    je .parameters
    cmp ah, 15h
    je .type
    mov ah, 1
    jmp .error
.reset:
    RUNTIME_ENTER
    cmp byte [es:PC_MOUNTED], 1
    RUNTIME_LEAVE
    jne .not_ready
    xor ax, ax
    jmp .success
.status:
    RUNTIME_ENTER
    mov ah, [es:PC_ERROR]
    RUNTIME_LEAVE
    test ah, ah
    jnz .error
    jmp .success
.parameters:
    mov word [ss:bp - 6], 4F12h ; cylinder 79, 18 sectors
    mov word [ss:bp - 8], 0101h ; last head 1, drive count 1
    mov byte [ss:bp - 4], 4     ; 1.44-MiB floppy
    jmp .reset
.type:
    mov ah, 1                  ; floppy without change-line support
    jmp .success
.protected:
    mov ah, 3
    jmp .error
.not_ready:
    mov ah, 80h
    jmp .error
.write:
    RUNTIME_ENTER
    cmp byte [es:PC_WRITABLE], 1
    RUNTIME_LEAVE
    jne .protected
.read:
    call pc_is_booted
    jnc .not_ready
    xor ah, ah
    test al, al
    jz .invalid
    cmp al, 18
    ja .invalid
    mov si, ax                ; sectors remaining
    cmp ch, 80
    jae .invalid
    test cl, 0C0h
    jnz .invalid
    cmp dh, 1
    ja .invalid
    mov al, ch
    xor ah, ah
    shl ax, 1
    xor ch, ch
    mov dl, dh
    xor dh, dh
    add ax, dx
    mov dx, 18
    mul dx
    cmp cl, 1
    jb .invalid
    cmp cl, 18
    ja .invalid
    dec cx
    add ax, cx
    mov di, ax                ; first LBA
    add ax, si
    cmp ax, 2880
    ja .invalid
    mov ax, si
    mov dx, 512
    mul dx
    add ax, bx
    jnc .dma_ok
    test ax, ax
    jnz .dma                       ; ending exactly on the boundary is valid
.dma_ok:
    ; Validate physical destination against conventional RAM and low IVT/BDA.
    push bx
    mov ax, [ss:bp - 16]
    mov dx, 16
    mul dx
    add ax, bx
    adc dx, 0
    test dx, dx
    jnz .above_low
    cmp ax, 0500h
    jb .bad_dest
.above_low:
    mov cx, si
    push cx
    mov cl, 9
    shl si, cl
    add ax, si
    adc dx, 0
    pop si
    push dx
    push ax
    push es
    xor ax, ax
    mov es, ax
    mov ax, [es:0413h]
    pop es
    mov bx, 1024
    mul bx
    mov cx, ax
    mov bx, dx
    pop ax
    pop dx
    cmp dx, bx
    ja .bad_dest
    jb .valid_dest
    cmp ax, cx
    ja .bad_dest
.valid_dest:
    pop bx
.next:
    mov ax, di
    cmp byte [ss:bp - 1], 3
    je .write_sector
    call pc_read_sector
    jc .transfer_error
    push ds
    push es
    push si
    push di
    push cx
    LOAD_BANK ax
    mov ds, ax
    mov es, [ss:bp - 16]
    mov si, PC_SECTOR
    mov di, bx
    mov cx, 256
    rep movsw
    pop cx
    pop di
    pop si
    pop es
    pop ds
    jmp .sector_done
.write_sector:
    push ds
    push es
    push si
    push di
    push cx
    mov ds, [ss:bp - 16]
    LOAD_BANK ax
    mov es, ax
    mov si, bx
    mov di, PC_SECTOR
    mov cx, 256
    rep movsw
    pop cx
    pop di
    pop si
    pop es
    pop ds
    mov ax, di
    call pc_write_sector
    jc .transfer_error
.sector_done:
    add bx, 512
    inc di
    dec si
    jnz .next
    mov al, [ss:bp - 2]
    xor ah, ah
    jmp .success
.transfer_error:
    RUNTIME_ENTER
    mov ah, [es:PC_ERROR]
    RUNTIME_LEAVE
    jmp .error
.bad_dest:
    pop bx
.dma:
    mov ah, 9
    jmp .error
.invalid:
    mov ah, 4
.error:
    RUNTIME_ENTER
    mov [es:PC_ERROR], ah
    RUNTIME_LEAVE
    mov byte [ss:bp - 2], 0
    mov [ss:bp - 1], ah
    or word [ss:bp + 6], 1
    jmp api_return
.success:
    RUNTIME_ENTER
    mov byte [es:PC_ERROR], 0
    RUNTIME_LEAVE
    mov [ss:bp - 2], ax
    jmp api_board_ok

; AX=LBA. Host supplies chunks while normal keyboard/terminal frames continue.
pc_read_sector:
    push bx
    mov bl, 1
    jmp pc_transfer_sector

; AX=LBA, PC_SECTOR holds source bytes. ACK only after the host commits them.
pc_write_sector:
    push bx
    mov bl, 2
pc_transfer_sector:
    push cx
    push dx
    push si
    push es
    push ax
    LOAD_BANK ax
    mov es, ax
    pop ax
    cmp byte [es:PC_MOUNTED], 1
    jne .error
    cmp bl, 2
    jne .start
    cmp byte [es:PC_WRITABLE], 1
    jne .protected
.start:
    mov [es:PC_LBA], ax
    inc word [es:PC_REQUEST]
    mov word [es:PC_RECEIVED], 0
    mov byte [es:PC_ERROR], 0
    mov [es:PC_PENDING], bl
    mov si, [es:RT_TICKS]
    mov dx, 32
    xor bx, bx
.wait:
    call runtime_enable
    call runtime_pump
    cmp byte [es:PC_PENDING], 0
    je .complete
    cmp byte [es:BIOS_STATE_BASE + API_HOST_MODE], 2
    jne .error
    mov ax, [es:RT_TICKS]
    sub ax, si
    cmp ax, 183                  ; about ten seconds
    jae .error
    dec bx
    jnz .wait
    dec dx
    jnz .wait
    jmp .error
.protected:
    mov byte [es:PC_ERROR], 3
    jmp .complete
.error:
    mov byte [es:PC_PENDING], 0
    mov byte [es:PC_ERROR], 80h
.complete:
    cmp byte [es:PC_ERROR], 0
    jne .failed
    pop es
    pop si
    pop dx
    pop cx
    pop bx
    clc
    ret
.failed:
    pop es
    pop si
    pop dx
    pop cx
    pop bx
    stc
    ret

