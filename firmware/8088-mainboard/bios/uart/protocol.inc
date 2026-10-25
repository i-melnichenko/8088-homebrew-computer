; UART command dispatch and common monitor commands.
monitor_dispatch:
    mov al, [es:DECODE_BUFFER + 1]
    ; Terminal polls consume sequence numbers too. Only immediate EVENT
    ; retries are duplicates, so a wrapped sequence can accept a new key.
    cmp byte [es:API_HOST_MODE], 2
    jne .event_cache
    cmp al, PROTO_KEYBOARD_EVENT
    je .event_cache
    mov byte [es:API_HOST_VALID], 0
.event_cache:
    cmp al, PROTO_TERMINAL_POLL
    je .terminal_cache
    mov byte [es:TERMINAL_VALID], 0
.terminal_cache:
    cmp al, PROTO_IRQ_TEST
    je .irq_retry
    mov byte [es:IRQ_TEST_VALID], 0
.irq_retry:
    cmp al, PROTO_RAM_TEST
    je .ram_retry
    mov byte [es:RAM_TEST_START_VALID], 0
.ram_retry:
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
    cmp al, PROTO_RESET
    je monitor_reset
    cmp al, PROTO_INFO
    je monitor_info
    cmp al, PROTO_KEYBOARD_MODE
    je monitor_keyboard_mode
    cmp al, PROTO_KEYBOARD_EVENT
    je monitor_keyboard_event
    cmp al, PROTO_MEMORY_READ
    je monitor_memory_read
    cmp al, PROTO_MEMORY_WRITE
    je monitor_memory_write
    cmp al, PROTO_RAM_INFO
    je monitor_ram_info
    cmp al, PROTO_RAM_TEST
    je monitor_ram_test
    cmp al, PROTO_TERMINAL_POLL
    je monitor_terminal_poll
    cmp al, PROTO_IRQ_TEST
    je monitor_irq_test
    cmp al, PROTO_DISK_MOUNT
    je monitor_disk_mount
    cmp al, PROTO_DISK_BOOT
    je monitor_disk_boot
    cmp al, PROTO_DISK_POLL
    je monitor_disk_poll
    cmp al, PROTO_DISK_DATA
    je monitor_disk_data
    cmp al, PROTO_DISK_WRITE_DATA
    je monitor_disk_write_data
    cmp al, PROTO_DISK_WRITE_ACK
    je monitor_disk_write_ack
    mov al, 1
    jmp monitor_reply

monitor_bios_unsupported:
    mov al, 17                      ; EEPROM commands require RAM flasher mode
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
