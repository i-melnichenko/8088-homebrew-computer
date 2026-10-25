; BIOS_FLASHER_MODE enters the embedded EEPROM flasher. Its ORG-0 image contains
; all framing, UART and readback code, so nothing calls ROM after the handoff.
monitor_bios_flasher_mode:
    cmp byte [es:MENU_PAGE], PAGE_TEST
    je .test_busy
    cmp word [es:DECODE_BUFFER + 3], 0
    jne .error
    mov al, [es:DECODE_BUFFER + 2]
    mov [es:EEPROM_MODE_SEQUENCE], al
    call runtime_pause
    mov byte [es:API_HOST_MODE], 0
    mov byte [es:API_HOST_VALID], 0
    call api_key_reset
    push es
    mov ax, ss
    mov es, ax
    mov si, eeprom_monitor_image
    mov di, FLASHER_BASE
    mov cx, eeprom_monitor_image_end - eeprom_monitor_image
    rep movsb
    pop es
    cli
    cld
    mov ax, ss
    add ax, FLASHER_BASE / 16
    mov [es:FLASHER_FAR + 2], ax
    mov word [es:FLASHER_FAR], 0
    mov sp, SRAM_STACK_TOP
    jmp far [es:FLASHER_FAR]
.error:
    mov al, 18
    jmp monitor_reply
.test_busy:
    mov al, 10
    jmp monitor_reply
eeprom_monitor_image:
    incbin "build/flasher.bin"
eeprom_monitor_image_end:
%if eeprom_monitor_image_end - eeprom_monitor_image > FLASHER_MAX_SIZE
    %error EEPROM monitor exceeds available payload SRAM
%endif
