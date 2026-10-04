; No disk/default boot source exists yet. Uploaded RAM programs are explicit.
; Programs use ORG 0, their own segment, and RETF; preserve SS/SP.
boot_program:
    call api_console_reset
    mov al, [es:ACTIVE_ID]
    call program_record
    mov ax, [es:bx + 16]
    mov cl, 4
    shr ax, cl
    mov word [es:PROGRAM_FAR], 0
    mov [es:PROGRAM_FAR + 2], ax
    mov si, bx
    push ds
    mov ax, es
    mov ds, ax
    mov al, LCD_LINE1
    call lcd_write_line
    pop ds
    mov al, LCD_LINE2
    mov si, boot_running
    call lcd_write_line
    mov al, LCD_LINE3
    mov si, boot_blank
    call lcd_write_line
    mov al, LCD_LINE4
    mov si, boot_blank
    call lcd_write_line
    cli
    cld
    xor ax, ax
    mov ss, ax
    mov [es:SAVED_STACK], sp
    mov sp, PROGRAM_STACK_TOP
    mov ax, [es:PROGRAM_FAR + 2]
    mov ds, ax
    mov byte [es:API_RUNNING], 1
    call far [es:PROGRAM_FAR]
    cli
    cld
    mov ax, cs
    mov ds, ax
    mov ax, RAM_SEGMENT
    mov es, ax
    mov byte [es:API_RUNNING], 0
    xor ax, ax
    mov ss, ax
    mov sp, [es:SAVED_STACK]
    mov word [es:RX_COUNT], 0
    mov byte [es:RX_DROP], 0
    mov byte [es:MENU_FLAGS], 0
    mov byte [es:MENU_PAGE], PAGE_CUSTOM
    mov al, [es:ACTIVE_ID]
    dec al
    mov [es:MENU_SELECTED], al
    mov byte [es:MENU_DIRTY], 1
    call uart_initialize
    call api_key_reset
    call lcd_initialize
    mov si, greeting
    call uart_print
    ret

boot_running: db 'RUNNING PROGRAM', 0
boot_blank: db 0
