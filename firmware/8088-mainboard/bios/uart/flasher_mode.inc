; BIOS_FLASHER_MODE enters the embedded EEPROM flasher. Its ORG-0 image contains
; all framing, UART and readback code, so nothing calls ROM after the handoff.
monitor_bios_flasher_mode:
    cmp byte [es:MENU_PAGE], PAGE_TEST
    je .test_busy
    cmp byte [es:UPLOAD_ACTIVE], 0
    jne .upload_busy
    cmp word [es:DECODE_BUFFER + 3], 0
    jne .error
    mov al, [es:DECODE_BUFFER + 2]
    mov [es:EEPROM_MODE_SEQUENCE], al
    ; The copied monitor may overwrite payloads. Publish no stale programs.
    mov byte [es:PROGRAM_COUNT], 0
    mov byte [es:ACTIVE_ID], 0
    mov byte [es:UPLOAD_ACTIVE], 0
    mov byte [es:LAST_WRITE_VALID], 0
    mov byte [es:EXEC_VALID], 0
    mov byte [es:DELETE_VALID], 0
    mov byte [es:API_HOST_MODE], 0
    mov byte [es:API_HOST_VALID], 0
    call api_key_reset
    push es
    xor ax, ax
    mov es, ax
    mov si, eeprom_monitor_image
    mov di, PROGRAM_BASE
    mov cx, eeprom_monitor_image_end - eeprom_monitor_image
    rep movsb
    pop es
    cli
    cld
    xor ax, ax
    mov ss, ax
    mov sp, SRAM_STACK_TOP
    jmp (PROGRAM_BASE / 16):0000h
.error:
    mov al, 18
    jmp monitor_reply
.test_busy:
    mov al, 10
    jmp monitor_reply
.upload_busy:
    mov al, 15
    jmp monitor_reply

eeprom_monitor_image:
    incbin "build/flasher.bin"
eeprom_monitor_image_end:
%if eeprom_monitor_image_end - eeprom_monitor_image > MAX_PROGRAM_SIZE
    %error EEPROM monitor exceeds available payload SRAM
%endif
