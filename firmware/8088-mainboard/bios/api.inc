; Software-interrupt API. Shared handlers live in ROM, vectors in low SRAM.
; All services preserve registers except their documented outputs. IRET
; restores caller flags (including DF/IF); only documented result bits change.
%include "bios_api.inc"
API_SCREEN       equ BIOS_STATE_SIZE       ; 80-byte program LCD framebuffer
API_CURSOR       equ API_SCREEN + 80       ; DL=column, DH=row
API_SCROLL_TOP   equ API_CURSOR + 2
API_SCROLL_LEFT  equ API_SCROLL_TOP + 1
API_SCROLL_BOTTOM equ API_SCROLL_LEFT + 1
API_SCROLL_RIGHT equ API_SCROLL_BOTTOM + 1
API_SCROLL_COUNT equ API_SCROLL_RIGHT + 1
API_HOST_MODE    equ API_SCROLL_COUNT + 1
API_RUNNING      equ API_HOST_MODE + 1
API_HOST_VALID   equ API_RUNNING + 1
API_HOST_SEQUENCE equ API_HOST_VALID + 1
API_HOST_KEY     equ API_HOST_SEQUENCE + 1
%if API_HOST_KEY + 2 > EEPROM_MODE_SEQUENCE
    %error API state overlaps EEPROM mode state
%endif

; Stack after entry: saved AX -2, BX -4, CX -6, DX -8, SI -10,
; DI -12, DS -14, ES -16; original FLAGS at BP+6.
%macro API_ENTER 0
    push bp
    mov bp, sp
    push ax
    push bx
    push cx
    push dx
    push si
    push di
    push ds
    push es
    cld
    mov ax, cs
    mov ds, ax
    mov ax, RAM_SEGMENT
    mov es, ax
    mov ax, [ss:bp - 2]
%endmacro

api_initialize:
    xor ax, ax
    mov es, ax
    xor di, di
    mov cx, 256
.vectors:
    mov ax, api_unsupported
    stosw
    mov ax, cs
    stosw
    loop .vectors
    xor ax, ax
    mov cx, 128                      ; clear 0400h..04FFh BDA only
    rep stosw
    mov word [es:BIOS_INT_VIDEO * 4], api_video
    mov word [es:BIOS_INT_SERIAL * 4], api_serial
    mov word [es:BIOS_INT_KEYBOARD * 4], api_keyboard
    mov word [es:BIOS_INT_BOARD * 4], api_board
    mov word [es:0400h], UART_BASE
    mov word [es:0413h], RAM_KIB
    mov byte [es:0449h], 7Fh         ; board-specific LCD mode, not PC mode 3
    mov word [es:044Ah], LCD_COLUMNS
    mov word [es:044Ch], 80
    mov word [es:0460h], 2000h
    mov byte [es:0484h], 3
    mov ax, RAM_SEGMENT
    mov es, ax
    mov byte [es:API_HOST_MODE], 0
    mov byte [es:API_RUNNING], 0
    mov byte [es:API_HOST_VALID], 0
    call api_console_reset
    ret

; Called at reset and before launching a program; no hardware writes.
api_console_reset:
    call api_display_reset
    call api_key_reset
    ret

api_display_reset:
    mov di, API_SCREEN
    mov cx, 80
    mov al, ' '
    rep stosb
    mov word [es:API_CURSOR], 0
    call api_cursor_bda
    ret

api_key_reset:
    push es
    xor ax, ax
    mov es, ax
    mov word [es:041Ah], 001Eh
    mov word [es:041Ch], 001Eh
    pop es
    ret

api_unsupported:
    API_ENTER
api_error:
    mov byte [ss:bp - 1], 86h        ; return AH=86h, CF=1 for unsupported input
    or word [ss:bp + 6], 1
    jmp api_return

api_board_ok:
    and word [ss:bp + 6], 0FFFEh
api_return:
    pop es
    pop ds
    pop di
    pop si
    pop dx
    pop cx
    pop bx
    pop ax
    pop bp
    iret

; INT 10h: page 0, 20x4 monochrome text. AH=02/03/06/0E only.
api_video:
    API_ENTER
    cmp ah, 06h
    je .scroll
    test bh, bh
    jnz api_error
    cmp ah, 02h
    je .set_cursor
    cmp ah, 03h
    je .get_cursor
    cmp ah, 0Eh
    je .putc
    jmp api_error
.set_cursor:
    call api_valid_position
    jc api_error
    mov [es:API_CURSOR], dx
    call api_cursor_bda
    call api_cursor_hardware
    jmp api_return
.get_cursor:
    mov ax, [es:API_CURSOR]
    mov [ss:bp - 8], ax
    mov word [ss:bp - 6], 2000h      ; hidden hardware cursor
    jmp api_return
.putc:
    call api_console_putc
    jmp api_return
.scroll:
    cmp ch, dh
    ja api_error
    cmp cl, dl
    ja api_error
    call api_valid_position
    jc api_error
    mov [es:API_SCROLL_TOP], ch
    mov [es:API_SCROLL_LEFT], cl
    mov [es:API_SCROLL_BOTTOM], dh
    mov [es:API_SCROLL_RIGHT], dl
    sub dh, ch
    inc dh                          ; window height
    test al, al
    jz .clear_window
    cmp al, dh
    jb .scroll_count
.clear_window:
    mov al, dh
.scroll_count:
    mov [es:API_SCROLL_COUNT], al
    call api_scroll_rectangle
    call api_redraw
    jmp api_return

; INT 14h: COM index DX=0. Fixed 9600 8N1 initialization (AL=E3h).
; Send/receive have a bounded polling timeout and return AH bit7 on timeout.
; Status is AH=line status, AL=modem status. No hardware IRQ is required.
api_serial:
    API_ENTER
    test dx, dx
    jnz api_error
    cmp ah, 00h
    je .initialize
    cmp ah, 01h
    je .send
    cmp ah, 02h
    je .receive
    cmp ah, 03h
    je .status
    jmp api_error
.initialize:
    cmp al, 0E3h
    jne api_error
    call uart_initialize
    call api_key_reset
.status:
    mov dx, UART_LSR
    in al, dx
    and al, 7Fh                     ; BIOS bit7 is timeout, not FIFO-error flag
    mov ah, al
    mov dx, UART_BASE + 6
    in al, dx
    mov [ss:bp - 2], ax
    jmp api_return
.send:
    mov bl, al
    mov bh, UART_LSR_THR_EMPTY
    jmp short .wait
.receive:
    cmp byte [es:API_HOST_MODE], 0
    jne api_error                   ; framed keyboard transport owns RX
    mov bl, [ss:bp - 2]
    mov bh, UART_LSR_DATA_READY
.wait:
    mov dx, UART_LSR
    mov cx, 0FFFFh
.poll:
    in al, dx
    test al, bh
    jnz .ready
    loop .poll
    or al, 80h
    jmp short .result
.ready:
    and al, 7Fh
    mov ch, al
    cmp bh, UART_LSR_DATA_READY
    je .read
    mov dx, UART_THR
    mov al, bl
    out dx, al
    mov al, ch
    jmp short .result
.read:
    mov dx, UART_RBR
    in al, dx
    mov bl, al
    mov al, ch
.result:
    mov ah, al
    mov al, bl
    mov [ss:bp - 2], ax
    jmp api_return

; INT 16h: UART-backed ASCII keyboard. AH=00 blocks, AH=01 peeks (ZF),
; AH=02 returns zero modifier state. Raw ASCII or framed host scan codes;
; terminal sequences are decoded on the PC, not by this handler. No PS/2 yet.
api_keyboard:
    API_ENTER
    cmp ah, 02h
    je .modifiers
    cmp ah, 00h
    je .get
    cmp ah, 01h
    jne api_error
    call api_key_poll
    jc .empty
    and word [ss:bp + 6], 0FFBFh     ; ZF=0; do not consume the pending key
    jmp short .result
.empty:
    or word [ss:bp + 6], 40h         ; ZF=1; leave AX unchanged
    jmp api_return
.get:
    call api_key_poll
    jc .get
    call keyboard_take
    mov [ss:bp - 2], ax
    jmp api_return
.result:
    mov [ss:bp - 2], ax
    jmp api_return
.modifiers:
    mov byte [ss:bp - 2], 0
    jmp api_return

api_key_poll:
    cmp byte [es:API_HOST_MODE], 0
    je .raw
    call keyboard_program_poll
    jmp keyboard_peek
.raw:
    call keyboard_peek
    jnc .done
    call uart_try_getc
    jc .done
    call api_key_scan
    call keyboard_enqueue
    jmp keyboard_peek
.done:
    ret

; Convert an ASCII UART byte to a PC-style AX key, scan code zero if unknown.
api_key_scan:
    xor ah, ah
    mov bl, al
    cmp bl, 'A'
    jb .lookup
    cmp bl, 'Z'
    ja .lookup
    add bl, 'a' - 'A'
.lookup:
    mov si, api_scan_pairs
    mov cx, (api_scan_pairs_end - api_scan_pairs) / 2
.scan:
    cmp bl, [cs:si]
    je .found
    add si, 2
    loop .scan
    ret
.found:
    mov ah, [cs:si + 1]
    ret

api_scan_pairs:
    db 1Bh,01h, 08h,0Eh, 09h,0Fh, 0Dh,1Ch, ' ',39h
    db '1',02h,'2',03h,'3',04h,'4',05h,'5',06h,'6',07h
    db '7',08h,'8',09h,'9',0Ah,'0',0Bh,'-',0Ch,'=',0Dh
    db 'q',10h,'w',11h,'e',12h,'r',13h,'t',14h,'y',15h
    db 'u',16h,'i',17h,'o',18h,'p',19h,'[',1Ah,']',1Bh
    db 'a',1Eh,'s',1Fh,'d',20h,'f',21h,'g',22h,'h',23h
    db 'j',24h,'k',25h,'l',26h,';',27h,27h,28h,60h,29h
    db 5Ch,2Bh,'z',2Ch,'x',2Dh,'c',2Eh,'v',2Fh,'b',30h
    db 'n',31h,'m',32h,',',33h,'.',34h,'/',35h
api_scan_pairs_end:

; INT 60h board extension: AH=00 capabilities, 01 clear, 02 padded line,
; 03 define CGRAM glyph, 04 write one raw cell, 05 print ASCIZ on console.
api_board:
    API_ENTER
    cmp ah, 00h
    je .capabilities
    cmp ah, 01h
    je .clear
    cmp ah, 02h
    je .line
    cmp ah, 03h
    je .glyph
    cmp ah, 04h
    je .cell
    cmp ah, 05h
    je .string
    jmp api_error
.capabilities:
    mov word [ss:bp - 2], BIOS_API_VERSION
    mov word [ss:bp - 4], BIOS_API_SIGNATURE
    mov word [ss:bp - 6], (BIOS_LCD_ROWS << 8) | BIOS_LCD_COLUMNS
    mov word [ss:bp - 8], BIOS_API_FEATURES
    jmp api_board_ok
.clear:
    call api_display_reset
    call api_redraw
    jmp api_board_ok
.line:
    xor dl, dl
    call api_valid_position
    jc api_error
    call api_cell_index
    mov di, bx
    mov ds, [ss:bp - 14]             ; caller's DS:SI, never BIOS ROM DS
    mov cx, LCD_COLUMNS
.line_byte:
    lodsb
    test al, al
    jz .line_pad
    stosb
    loop .line_byte
    jmp short .line_draw
.line_pad:
    mov al, ' '
    rep stosb
.line_draw:
    mov dx, [ss:bp - 8]
    xor dl, dl
    call api_draw_line
    call api_cursor_hardware
    jmp api_board_ok
.glyph:
    cmp al, 7
    ja api_error
    mov cl, 3
    shl al, cl
    add al, 40h
    call lcd_command
    mov ds, [ss:bp - 14]
    mov cx, 8
.glyph_byte:
    lodsb
    and al, 1Fh
    push cx
    call lcd_data
    pop cx
    loop .glyph_byte
    call api_cursor_hardware
    jmp api_board_ok
.cell:
    call api_valid_position
    jc api_error
    call api_write_cell
    call api_cursor_hardware
    jmp api_board_ok
.string:
    mov ds, [ss:bp - 14]
.string_byte:
    lodsb
    test al, al
    jz api_board_ok
    push si
    call api_console_putc
    pop si
    jmp .string_byte

; DX = row:column. CF=1 outside the physical LCD.
api_valid_position:
    cmp dh, 4
    jae .bad
    cmp dl, LCD_COLUMNS
    jae .bad
    clc
    ret
.bad:
    stc
    ret

; DX = row:column -> BX = framebuffer offset, AX/DX preserved.
api_cell_index:
    push ax
    push dx
    xor ax, ax
    mov al, dh
    mov bx, LCD_COLUMNS
    mul bx
    mov bx, ax
    pop dx
    xor ax, ax
    mov al, dl
    add bx, ax
    add bx, API_SCREEN
    pop ax
    ret

api_write_cell:
    push ax
    call api_cell_index
    mov [es:bx], al
    xor bx, bx
    mov bl, dh
    mov al, [cs:menu_lcd_rows + bx]
    add al, dl
    call lcd_command
    pop ax
    call lcd_data
    ret

api_cursor_bda:
    push ax
    push es
    mov ax, [es:API_CURSOR]
    xor dx, dx
    mov es, dx
    mov [es:0450h], ax
    pop es
    pop ax
    ret

api_cursor_hardware:
    mov dx, [es:API_CURSOR]
    xor bx, bx
    mov bl, dh
    mov al, [cs:menu_lcd_rows + bx]
    add al, dl
    call lcd_command
    ret

api_redraw:
    xor dx, dx
.row:
    push dx
    call api_draw_line
    pop dx
    inc dh
    cmp dh, 4
    jb .row
    call api_cursor_hardware
    ret

api_draw_line:
    call api_cell_index
    mov si, bx
    xor bx, bx
    mov bl, dh
    mov al, [cs:menu_lcd_rows + bx]
    call lcd_command
    mov di, LCD_COLUMNS
.byte:
    mov al, [es:si]
    inc si
    call lcd_data
    dec di
    jnz .byte
    ret

api_console_putc:
    mov dx, [es:API_CURSOR]
    cmp al, 07h                     ; no speaker in the current BIOS
    je .done
    cmp al, 08h
    je .backspace
    cmp al, 0Dh
    je .carriage
    cmp al, 0Ah
    je .newline
    call api_write_cell
    inc dl
    cmp dl, LCD_COLUMNS
    jb .cursor
    xor dl, dl
.newline:
    inc dh
    cmp dh, 4
    jb .cursor
    mov byte [es:API_SCROLL_TOP], 0
    mov byte [es:API_SCROLL_LEFT], 0
    mov byte [es:API_SCROLL_BOTTOM], 3
    mov byte [es:API_SCROLL_RIGHT], LCD_COLUMNS - 1
    mov byte [es:API_SCROLL_COUNT], 1
    push dx
    call api_scroll_rectangle
    call api_redraw
    pop dx
    mov dh, 3
    jmp short .cursor
.carriage:
    xor dl, dl
    jmp short .cursor
.backspace:
    test dl, dl
    jz .done
    dec dl                          ; move, do not erase (PC teletype semantics)
.cursor:
    mov [es:API_CURSOR], dx
    call api_cursor_bda
    call api_cursor_hardware
.done:
    ret

api_scroll_rectangle:
    mov dh, [es:API_SCROLL_TOP]
.row:
    mov dl, [es:API_SCROLL_LEFT]
.cell:
    call api_cell_index
    mov si, bx
    push dx
    add dh, [es:API_SCROLL_COUNT]
    cmp dh, [es:API_SCROLL_BOTTOM]
    ja .blank
    call api_cell_index
    mov al, [es:bx]
    jmp short .write
.blank:
    mov al, ' '
.write:
    pop dx
    mov [es:si], al
    inc dl
    cmp dl, [es:API_SCROLL_RIGHT]
    jbe .cell
    inc dh
    cmp dh, [es:API_SCROLL_BOTTOM]
    jbe .row
    ret
