; UART command dispatch and common monitor commands.
monitor_dispatch:
    mov al, [es:DECODE_BUFFER + 1]
    mov byte [es:MENU_FLAGS], 0
    cmp al, PROTO_PROGRAM_EXEC
    je .exec_cache
    mov byte [es:EXEC_VALID], 0
.exec_cache:
    cmp al, PROTO_PROGRAM_DELETE
    je .delete_cache
    mov byte [es:DELETE_VALID], 0
.delete_cache:
    cmp al, PROTO_BIOS_FLASHER_MODE
    jne .write
    jmp monitor_bios_flasher_mode
.write:
    cmp al, PROTO_BIOS_WRITE
    je monitor_bios_unsupported
    cmp al, PROTO_BIOS_READ
    je monitor_bios_unsupported
.monitor:
    cmp al, PROTO_PING
    je monitor_ping
    cmp al, PROTO_PROGRAM_UPLOAD_BEGIN
    je monitor_program_upload_begin
    cmp al, PROTO_PROGRAM_UPLOAD_DATA
    je monitor_program_upload_data
    cmp al, PROTO_PROGRAM_EXEC
    jne .check_reset
    jmp monitor_program_exec
.check_reset:
    cmp al, PROTO_RESET
    jne .check_info
    jmp monitor_reset
.check_info:
    cmp al, PROTO_INFO
    jne .check_commit
    jmp monitor_info
.check_commit:
    cmp al, PROTO_PROGRAM_UPLOAD_COMMIT
    jne .check_list
    jmp monitor_program_upload_commit
.check_list:
    cmp al, PROTO_PROGRAM_LIST
    jne .check_abort
    jmp monitor_program_list
.check_abort:
    cmp al, PROTO_PROGRAM_UPLOAD_ABORT
    jne .check_delete
    jmp monitor_program_upload_abort
.check_delete:
    cmp al, PROTO_PROGRAM_DELETE
    jne .check_rename
    jmp monitor_program_delete
.check_rename:
    cmp al, PROTO_PROGRAM_RENAME
    jne .check_keyboard_mode
    jmp monitor_program_rename
.check_keyboard_mode:
    cmp al, PROTO_KEYBOARD_MODE
    jne .check_keyboard_event
    jmp monitor_keyboard_mode
.check_keyboard_event:
    cmp al, PROTO_KEYBOARD_EVENT
    jne .check_memory_read
    jmp monitor_keyboard_event
.check_memory_read:
    cmp al, PROTO_MEMORY_READ
    jne .check_memory_write
    jmp monitor_memory_read
.check_memory_write:
    cmp al, PROTO_MEMORY_WRITE
    jne .unknown
    jmp monitor_memory_write
.unknown:
    mov al, 1                       ; unknown command
    jmp monitor_reply

monitor_bios_unsupported:
    mov al, 17                      ; unsupported on revision 1
    jmp monitor_reply

monitor_ping:
    cmp word [es:DECODE_BUFFER + 3], 0
    jne .error
    xor al, al
    jmp monitor_reply
.error:
    mov al, 5
    jmp monitor_reply


monitor_reset:
    cmp word [es:DECODE_BUFFER + 3], 0
    jne .error
    xor al, al
    call monitor_reply
    call monitor_wait_tx_empty
    jmp ROM_SEGMENT:0000h
.error:
    mov al, 5
    jmp monitor_reply

greeting: db '8088 ROM monitor ready', 13, 10, 0
