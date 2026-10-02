; UART program upload and registry commands.
; BEGIN data: size:u32, payload CRC16:u16, 1..15 printable ASCII name bytes.
monitor_program_upload_begin:
    cmp byte [es:MENU_PAGE], PAGE_TEST
    jne .not_busy
    mov al, 10
    jmp monitor_reply
.not_busy:
    mov cx, [es:DECODE_BUFFER + 3]
    cmp cx, 7
    jb .error
    cmp cx, 21
    ja .error
    cmp word [es:DECODE_BUFFER + 7], 0
    jne .error
    mov ax, [es:DECODE_BUFFER + 5]
    test ax, ax
    jz .error
    cmp byte [es:PROGRAM_COUNT], PROGRAM_CAPACITY
    jae .full
    cmp ax, MAX_PROGRAM_SIZE
    ja .no_memory
    sub cx, 6
    mov bp, cx
    mov si, DECODE_BUFFER + 11
    xor bl, bl
.validate_name:
    mov al, [es:si]
    inc si
    cmp al, ' '
    jb .error
    cmp al, '~'
    ja .error
    cmp al, ' '
    je .space
    mov bl, 1
.space:
    loop .validate_name
    test bl, bl
    jz .error
    xor dx, dx
    mov bx, PROGRAM_TABLE
.duplicate:
    cmp dl, [es:PROGRAM_COUNT]
    jae .allocate
    mov si, DECODE_BUFFER + 11
    mov di, bx
    mov cx, bp
.compare:
    mov al, [es:si]
    cmp al, [es:di]
    jne .different
    inc si
    inc di
    loop .compare
    cmp byte [es:di], 0
    je .name_exists
.different:
    inc dl
    add bx, PROGRAM_RECORD_SIZE
    jmp .duplicate
.allocate:
    call program_allocate
    jc .no_memory
    mov [es:PROGRAM_ENTRY], ax
    ; BX points to the next free record; do not publish it until COMMIT.
    mov di, bx
    xor ax, ax
    mov cx, PROGRAM_RECORD_SIZE
    rep stosb
    mov di, bx
    mov si, DECODE_BUFFER + 11
    mov cx, bp
.name:
    mov al, [es:si]
    inc si
    stosb
    loop .name
    mov ax, [es:PROGRAM_ENTRY]
    mov [es:bx + 16], ax
    mov [es:PROGRAM_ENTRY], ax
    mov [es:MONITOR_NEXT], ax
    mov ax, [es:DECODE_BUFFER + 5]
    mov [es:bx + 18], ax
    mov [es:MONITOR_TOTAL], ax
    mov word [es:MONITOR_RECEIVED], 0
    mov ax, [es:DECODE_BUFFER + 9]
    mov [es:UPLOAD_CRC], ax
    mov byte [es:UPLOAD_ACTIVE], 1
    mov byte [es:LAST_WRITE_VALID], 0
    mov al, [es:PROGRAM_COUNT]
    inc al
    mov [es:ACTIVE_ID], al
    mov byte [es:MENU_PAGE], PAGE_PROGRAM
    mov byte [es:MENU_DIRTY], 1
    mov byte [es:RX_BUFFER + 5], 0
    mov [es:RX_BUFFER + 6], al
    mov ax, [es:PROGRAM_ENTRY]
    mov [es:RX_BUFFER + 7], ax
    mov word [es:RX_BUFFER + 9], 0
    mov cx, 6
    jmp monitor_reply_data
.error:
    mov al, 2
    jmp monitor_reply
.full:
    mov al, 6
    jmp monitor_reply
.no_memory:
    mov al, 7
    jmp monitor_reply
.name_exists:
    mov al, 8
    jmp monitor_reply

; DATA contains only program bytes: 128 per block, except the final remainder.
; BIOS owns the write cursor. Retrying the last accepted sequence must contain
; identical bytes and only repeats its ACK, without advancing the cursor.
monitor_program_upload_data:
    cmp byte [es:UPLOAD_ACTIVE], 1
    jne .error
    cmp byte [es:MENU_PAGE], PAGE_TEST
    je .error
    mov cx, [es:DECODE_BUFFER + 3]
    test cx, cx
    jz .error
    cmp cx, PROTO_UPLOAD_BLOCK
    ja .error
    mov bx, cx
    add bx, DECODE_BUFFER + 5
    cmp byte [es:LAST_WRITE_VALID], 0
    je .new
    mov al, [es:DECODE_BUFFER + 2]
    cmp al, [es:LAST_WRITE_SEQUENCE]
    jne .new
    cmp cx, [es:LAST_WRITE_LENGTH]
    jne .error
    mov ax, [es:bx]
    cmp ax, [es:LAST_WRITE_CRC]
    jne .error
    ; Compare against the last written RAM block as well: CRC collisions must
    ; not allow changed bytes to masquerade as a successful retransmission.
    push ds
    xor ax, ax
    mov ds, ax
    mov si, DECODE_BUFFER + 5
    mov di, [es:LAST_WRITE_ADDR]
.compare_retry:
    mov al, [es:si]
    cmp al, [di]
    jne .retry_changed
    inc si
    inc di
    loop .compare_retry
    pop ds
    xor al, al
    jmp monitor_reply
.retry_changed:
    pop ds
    jmp .error
.new:
    mov ax, [es:MONITOR_TOTAL]
    sub ax, [es:MONITOR_RECEIVED]
    cmp ax, PROTO_UPLOAD_BLOCK
    jbe .expected_length
    mov ax, PROTO_UPLOAD_BLOCK
.expected_length:
    cmp cx, ax
    jne .error
    mov di, [es:MONITOR_NEXT]
    mov ax, [es:bx]
    mov [es:LAST_WRITE_CRC], ax
    mov [es:LAST_WRITE_ADDR], di
    mov [es:LAST_WRITE_LENGTH], cx
    mov al, [es:DECODE_BUFFER + 2]
    mov [es:LAST_WRITE_SEQUENCE], al
    mov byte [es:LAST_WRITE_VALID], 1
    mov ax, di
    add ax, cx
    mov [es:MONITOR_NEXT], ax
    push ds
    push es
    mov ax, es
    mov ds, ax
    xor ax, ax
    mov es, ax
    mov si, DECODE_BUFFER + 5
    push cx
    rep movsb
    pop cx
    pop es
    pop ds
    add [es:MONITOR_RECEIVED], cx
    mov byte [es:MENU_DIRTY], 1
    xor al, al
    jmp monitor_reply
.error:
    mov al, 3
    jmp monitor_reply

; COMMIT payload=ID:u8. Whole-image CRC precedes publication.
monitor_program_upload_commit:
    cmp word [es:DECODE_BUFFER + 3], 1
    jne .error
    xor ax, ax
    mov al, [es:DECODE_BUFFER + 5]
    test ax, ax
    jz .error
    cmp ax, PROGRAM_CAPACITY
    ja .error
    cmp byte [es:UPLOAD_ACTIVE], 0
    jne .pending
    cmp al, [es:PROGRAM_COUNT]
    ja .error
    xor al, al                      ; already committed: retry is a no-op
    jmp monitor_reply
.pending:
    mov bl, [es:PROGRAM_COUNT]
    inc bl
    cmp al, bl
    jne .error
    mov cx, [es:MONITOR_RECEIVED]
    cmp cx, [es:MONITOR_TOTAL]
    jne .error
    mov si, [es:PROGRAM_ENTRY]
    push es
    xor ax, ax
    mov es, ax
    call monitor_crc16
    pop es
    cmp ax, [es:UPLOAD_CRC]
    jne .crc_error
    inc byte [es:PROGRAM_COUNT]
    mov byte [es:UPLOAD_ACTIVE], 0
    mov byte [es:LAST_WRITE_VALID], 0
    mov al, [es:PROGRAM_COUNT]
    mov [es:ACTIVE_ID], al
    dec al
    mov [es:MENU_SELECTED], al
    mov byte [es:MENU_PAGE], PAGE_CUSTOM
    mov byte [es:MENU_DIRTY], 1
    xor al, al
    jmp monitor_reply
.error:
    mov al, 2
    jmp monitor_reply
.crc_error:
    mov al, 9
    jmp monitor_reply

monitor_program_upload_abort:
    cmp word [es:DECODE_BUFFER + 3], 1
    jne .error
    xor ax, ax
    mov al, [es:PROGRAM_COUNT]
    inc ax
    cmp al, [es:DECODE_BUFFER + 5]
    jne .error
    mov byte [es:UPLOAD_ACTIVE], 0
    mov byte [es:LAST_WRITE_VALID], 0
    mov word [es:MONITOR_TOTAL], 0
    mov word [es:MONITOR_RECEIVED], 0
    mov byte [es:MENU_SELECTED], 0
    mov byte [es:MENU_PAGE], PAGE_MENU
    mov byte [es:MENU_DIRTY], 1
    xor al, al
    jmp monitor_reply
.error:
    mov al, 2
    jmp monitor_reply

; Reply: status, count, repeated (ID:u8, address:u32, size:u16, name[16]).
monitor_program_list:
    cmp word [es:DECODE_BUFFER + 3], 0
    jne .error
    mov byte [es:RX_BUFFER + 5], 0
    mov al, [es:PROGRAM_COUNT]
    mov [es:RX_BUFFER + 6], al
    xor ah, ah
    mov bp, ax
    push ax
    push ds
    mov ax, es
    mov ds, ax
    mov bx, PROGRAM_TABLE
    mov di, RX_BUFFER + 7
    mov dl, 1
.record:
    test bp, bp
    jz .done
    mov al, dl
    stosb
    mov ax, [es:bx + 16]
    stosw
    xor ax, ax
    stosw
    mov ax, [es:bx + 18]
    stosw
    mov si, bx
    mov cx, PROGRAM_NAME_SIZE
    rep movsb
    add bx, PROGRAM_RECORD_SIZE
    inc dl
    dec bp
    jmp .record
.done:
    pop ds
    pop ax
    mov cx, 23
    mul cx
    mov cx, ax
    add cx, 2
    jmp monitor_reply_data
.error:
    mov al, 5
    jmp monitor_reply

; DELETE payload=ID:u8. Compact metadata/IDs only, never move program bytes.
; Cache the immediate successful request so a lost ACK cannot delete the
; following program, which now occupies the same ID.
monitor_program_delete:
    cmp byte [es:MENU_PAGE], PAGE_TEST
    je program_registry_test_busy
    cmp byte [es:UPLOAD_ACTIVE], 0
    jne program_registry_upload_busy
    cmp word [es:DECODE_BUFFER + 3], 1
    jne program_registry_error
    mov al, [es:DECODE_BUFFER + 5]
    mov ah, [es:DECODE_BUFFER + 2]
    cmp byte [es:DELETE_VALID], 0
    je .new
    cmp al, [es:DELETE_ID]
    jne .new
    cmp ah, [es:DELETE_SEQUENCE]
    jne .new
    xor al, al
    jmp monitor_reply
.new:
    test al, al
    jz program_registry_error
    cmp al, [es:PROGRAM_COUNT]
    ja program_registry_error
    mov [es:DELETE_ID], al
    mov [es:DELETE_SEQUENCE], ah
    call program_record
    mov di, bx
    mov si, bx
    add si, PROGRAM_RECORD_SIZE
    xor ax, ax
    mov al, [es:PROGRAM_COUNT]
    sub al, [es:DELETE_ID]
    mov cx, PROGRAM_RECORD_SIZE
    mul cx
    mov cx, ax
    push ds
    mov ax, es
    mov ds, ax
    rep movsb
    pop ds
    ; DI now points to the trailing record: clear stale name/address/size.
    xor ax, ax
    mov cx, PROGRAM_RECORD_SIZE
    rep stosb
    dec byte [es:PROGRAM_COUNT]
    mov al, [es:DELETE_ID]
    cmp al, [es:PROGRAM_COUNT]
    jbe .select
    mov al, [es:PROGRAM_COUNT]
.select:
    mov [es:ACTIVE_ID], al
    test al, al
    jz .empty
    dec al
.empty:
    mov [es:MENU_SELECTED], al
    mov byte [es:MENU_PAGE], PAGE_CUSTOM
    mov byte [es:MENU_DIRTY], 1
    mov byte [es:DELETE_VALID], 1
    xor al, al
    jmp monitor_reply

; RENAME payload=ID:u8 + 1..15 printable ASCII bytes, no terminator.
monitor_program_rename:
    cmp byte [es:MENU_PAGE], PAGE_TEST
    je program_registry_test_busy
    cmp byte [es:UPLOAD_ACTIVE], 0
    jne program_registry_upload_busy
    mov cx, [es:DECODE_BUFFER + 3]
    cmp cx, 2
    jb program_registry_error
    cmp cx, PROGRAM_NAME_SIZE
    ja program_registry_error
    mov al, [es:DECODE_BUFFER + 5]
    test al, al
    jz program_registry_error
    cmp al, [es:PROGRAM_COUNT]
    ja program_registry_error
    dec cx
    mov bp, cx
    mov si, DECODE_BUFFER + 6
    xor bl, bl
.validate:
    mov al, [es:si]
    inc si
    cmp al, ' '
    jb program_registry_error
    cmp al, '~'
    ja program_registry_error
    cmp al, ' '
    je .space
    mov bl, 1
.space:
    loop .validate
    test bl, bl
    jz program_registry_error
    mov dl, 1
    mov bx, PROGRAM_TABLE
.duplicate:
    cmp dl, [es:PROGRAM_COUNT]
    ja .rename
    cmp dl, [es:DECODE_BUFFER + 5]
    je .next
    mov si, DECODE_BUFFER + 6
    mov di, bx
    mov cx, bp
.compare:
    mov al, [es:si]
    cmp al, [es:di]
    jne .next
    inc si
    inc di
    loop .compare
    cmp byte [es:di], 0
    je .name_exists
.next:
    inc dl
    add bx, PROGRAM_RECORD_SIZE
    jmp .duplicate
.rename:
    mov al, [es:DECODE_BUFFER + 5]
    call program_record
    mov di, bx
    xor ax, ax
    mov cx, PROGRAM_NAME_SIZE
    rep stosb
    mov di, bx
    mov si, DECODE_BUFFER + 6
    mov cx, bp
.copy:
    mov al, [es:si]
    inc si
    stosb
    loop .copy
    mov byte [es:MENU_DIRTY], 1
    xor al, al
    jmp monitor_reply
.name_exists:
    mov al, 8
    jmp monitor_reply

program_registry_error:
    mov al, 16
    jmp monitor_reply
program_registry_test_busy:
    mov al, 10
    jmp monitor_reply
program_registry_upload_busy:
    mov al, 15
    jmp monitor_reply

monitor_program_exec:
    cmp word [es:DECODE_BUFFER + 3], 1
    jne .error
    cmp byte [es:UPLOAD_ACTIVE], 0
    jne .error
    xor ax, ax
    mov al, [es:DECODE_BUFFER + 5]
    test ax, ax
    jz .error
    xor bx, bx
    mov bl, [es:PROGRAM_COUNT]
    cmp ax, bx
    ja .error
    mov ah, [es:DECODE_BUFFER + 2]
    cmp byte [es:EXEC_VALID], 0
    je .new
    cmp al, [es:EXEC_ID]
    jne .new
    cmp ah, [es:EXEC_SEQUENCE]
    jne .new
    xor al, al
    jmp monitor_reply
.new:
    mov [es:ACTIVE_ID], al
    mov [es:EXEC_ID], al
    mov [es:EXEC_SEQUENCE], ah
    mov byte [es:EXEC_VALID], 1
    xor al, al
    call monitor_reply
    call monitor_wait_tx_empty
    jmp boot_program
.error:
    mov al, 4
    jmp monitor_reply
