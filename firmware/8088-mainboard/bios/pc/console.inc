; CF=1 only after the explicit disk boot handoff.
pc_is_booted:
    push es
    push ax
    LOAD_BANK ax
    mov es, ax
    cmp byte [es:PC_MODE], 1
    pop ax
    pop es
    je .yes
    clc
    ret
.yes:
    stc
    ret

; INT 10h PC boot console: teletype and essential mode/cursor queries.
; Full CGA/video RAM emulation is deliberately not advertised.
pc_video:
    cmp ah, 0Eh
    je .putc
    cmp ah, 0Fh
    je .mode
    cmp ah, 00h
    je .clear
    cmp ah, 03h
    je .cursor
    cmp ah, 02h
    je .set_cursor
    cmp ah, 01h
    je .ok
    jmp api_error
.mode:
    mov word [ss:bp - 2], 5003h
    mov byte [ss:bp - 3], 0
    jmp api_return
.clear:
    cmp al, 3
    jne api_error
    call pc_lcd_status
    mov si, pc_clear
    call pc_print_rom
    xor ax, ax
    mov ds, ax
    mov word [0450h], 0
    jmp api_return
.cursor:
    xor ax, ax
    mov ds, ax
    mov ax, [0450h]
    mov [ss:bp - 8], ax
    mov word [ss:bp - 6], 0607h
    jmp api_return
.set_cursor:
    test bh, bh
    jnz api_error
    cmp dh, 25
    jae api_error
    cmp dl, 80
    jae api_error
    xor ax, ax
    mov ds, ax
    mov [0450h], dx
    push dx
    mov al, 27
    call pc_putc
    mov al, '['
    call pc_putc
    pop dx
    push dx
    mov al, dh
    inc al
    call pc_decimal_pair
    mov al, ';'
    call pc_putc
    pop dx
    mov al, dl
    inc al
    call pc_decimal_pair
    mov al, 'H'
    call pc_putc
.ok:
    jmp api_return
.putc:
    call pc_putc
    xor dx, dx
    mov ds, dx
    mov dx, [0450h]
    cmp al, 13
    je .cr
    cmp al, 10
    je .lf
    cmp al, 8
    je .bs
    cmp al, 32
    jb .save
    inc dl
    cmp dl, 80
    jb .save
.cr:
    xor dl, dl
    cmp al, 13
    je .save
.lf:
    inc dh
    cmp dh, 25
    jb .save
    mov dh, 24
    jmp .save
.bs:
    test dl, dl
    jz .save
    dec dl
.save:
    mov [0450h], dx
    jmp api_return

pc_decimal_pair:
    aam
    add ax, 3030h
    push ax
    mov al, ah
    call pc_putc
    pop ax
    jmp pc_putc

pc_print_rom:
    lodsb
    test al, al
    jz .done
    call pc_putc
    jmp pc_print_rom
.done:
    ret

; AL byte, preserve all registers. Framed TX retains its bounded backpressure.
pc_putc:
    push ax
    push dx
    mov ah, 1
    xor dx, dx
    int 14h
    pop dx
    pop ax
    ret
pc_clear: db 27, '[2J', 27, '[H', 0

; LCD remains a board status display while the OS owns the UART console.
pc_lcd_status:
    push ax
    push si
    mov al, LCD_LINE1
    mov si, pc_lcd_title
    call lcd_write_line
    mov al, LCD_LINE2
    mov si, pc_lcd_console
    call lcd_write_line
    mov si, pc_lcd_disk_readonly
    RUNTIME_ENTER
    cmp byte [es:PC_WRITABLE], 1
    RUNTIME_LEAVE
    jne .disk
    mov si, pc_lcd_disk_writable
.disk:
    mov al, LCD_LINE3
    call lcd_write_line
    mov al, LCD_LINE4
    mov si, pc_lcd_blank
    call lcd_write_line
    pop si
    pop ax
    ret
pc_lcd_title: db 'OPERATING SYSTEM', 0
pc_lcd_console: db 'UART CONSOLE 80X25', 0
pc_lcd_disk_readonly: db 'DISK A: READ ONLY', 0
pc_lcd_disk_writable: db 'DISK A: READ/WRITE', 0
pc_lcd_blank: db 0
