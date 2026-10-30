BITS 16
CPU 8086
ORG 100h
%include "bios_api.inc"

READY equ 0
RUNNING equ 1
PAUSED equ 2
FINISHED equ 3
CELLS equ 80

start:
    cld
    push cs
    pop ds
    push cs
    pop es
    bios_console_info
    mov [lcd_output], dl
    cmp al, 1
    je .ready
    mov byte [uart_output], 1
.ready:
    mov si, service_start
    mov cx, service_start_end - service_start
    call write
    call reset
.loop:
    call advance_time
    call keys
    cmp byte [quit], 0
    jne .exit
    call compose
    call report_status
    call present
    mov cx, 50
    bios_delay
    jmp .loop
.exit:
    mov si, service_exit
    mov cx, service_exit_end - service_exit
    call write
    int 20h

reset:
    mov ax, [duration]
    mov [remaining], ax
    mov byte [state], READY
    mov byte [clock_valid], 0
    mov word [fraction_low], 0
    mov word [fraction_high], 0
    ret

; Limit input per slice so held keys cannot starve the clock or display.
keys:
    mov cx, 8
.next:
    push cx
    bios_key_peek
    jz .empty
    bios_key_read
    call key
    pop cx
    cmp byte [quit], 0
    jne .done
    loop .next
.done:
    ret
.empty:
    pop cx
    ret

key:
    cmp al, 27
    je .exit
    cmp al, 'r'
    je reset
    cmp al, 'R'
    je reset
    cmp al, 13
    je .toggle
    cmp al, ' '
    je .toggle
    cmp byte [state], RUNNING
    je .done
    cmp al, '+'
    je .increase
    cmp al, '='
    je .increase
    cmp al, '-'
    jne .done
    cmp word [duration], 30
    jbe .done
    sub word [duration], 30
    jmp reset
.increase:
    cmp word [duration], 3600
    jae .done
    add word [duration], 30
    jmp reset
.toggle:
    cmp byte [state], RUNNING
    je .pause
    cmp byte [state], FINISHED
    jne .start
    call reset
.start:
    mov byte [state], RUNNING
    mov byte [clock_valid], 0
    ret
.pause:
    mov byte [state], PAUSED
    mov byte [clock_valid], 0
.done:
    ret
.exit:
    mov byte [quit], 1
    ret

; A BIOS tick represents 65536 PIT input clocks. Accumulate input clocks
; and subtract 1193182 per second, retaining fractions across pause/resume.
; Timing includes time spent drawing and waiting for serial transfers.
advance_time:
    bios_ticks
    jc .unavailable
    cmp byte [state], RUNNING
    jne .done
    mov bx, ax
    mov si, dx
    cmp byte [clock_valid], 0
    je .anchor
    sub ax, [last_tick]
    sbb dx, [last_high]
    mov [last_tick], bx
    mov [last_high], si
    ; More than an hour between samples means the clock was changed.
    test dx, dx
    jnz .done
    add [fraction_high], ax
.seconds:
    cmp word [fraction_high], 0012h
    jb .done
    ja .second
    cmp word [fraction_low], 034DEh
    jb .done
.second:
    sub word [fraction_low], 034DEh
    sbb word [fraction_high], 0012h
    dec word [remaining]
    jnz .seconds
    mov byte [state], FINISHED
    ret
.anchor:
    mov [last_tick], bx
    mov [last_high], si
    mov byte [clock_valid], 1
.done:
    ret
.unavailable:
    mov byte [no_clock], 1
    ret

compose:
    mov si, template
    mov di, frame
    mov cx, CELLS
    rep movsb
    mov ax, [remaining]
    xor dx, dx
    mov bx, 60
    div bx
    push dx
    mov di, frame + 20
    call two_digits
    pop ax
    mov di, frame + 23
    call two_digits
    xor bx, bx
    mov bl, [state]
    shl bx, 1
    mov si, [states + bx]
    mov di, frame + 28
    mov cx, 8
    rep movsb
    cmp byte [no_clock], 0
    je .done
    mov si, clock_error
    mov di, frame + 20
    mov cx, 20
    rep movsb
.done:
    ret

two_digits:
    xor dx, dx
    mov bx, 10
    div bx
    add al, '0'
    add dl, '0'
    mov [di], al
    mov [di + 1], dl
    ret

; UART carries state changes only; the countdown display stays on the LCD.
report_status:
    mov al, [state]
    cmp al, [reported_state]
    jne .changed
    mov ax, [duration]
    cmp ax, [reported_duration]
    jne .changed
    mov al, [no_clock]
    cmp al, [reported_no_clock]
    je .done
.changed:
    mov al, [state]
    mov [reported_state], al
    mov ax, [duration]
    mov [reported_duration], ax
    mov al, [no_clock]
    mov [reported_no_clock], al
    test al, al
    jnz .clock_error
    mov si, frame + 28
    mov di, service_state
    mov cx, 8
    rep movsb
    mov si, frame + 20
    mov di, service_time
    mov cx, 5
    rep movsb
    mov si, service_status
    mov cx, service_status_end - service_status
    jmp write
.clock_error:
    mov si, service_clock_error
    mov cx, service_clock_error_end - service_clock_error
    jmp write
.done:
    ret

; Update only LCD cells that changed. Never send screen rows over UART.
present:
    cmp byte [lcd_output], 0
    je .done
    xor di, di
    xor dx, dx
.cell:
    mov al, [frame + di]
    cmp al, [previous + di]
    je .next
    mov [previous + di], al
    mov ah, 04h
    int BIOS_INT_BOARD
.next:
    inc di
    inc dl
    cmp dl, BIOS_LCD_COLUMNS
    jb .cell
    xor dl, dl
    inc dh
    cmp dh, BIOS_LCD_ROWS
    jb .cell
.done:
    ret

; DS:SI/CX bytes; preserve all registers. Timeout ends the program so loss of
; a draining terminal cannot leave it stuck repeatedly attempting output.
write:
    cmp byte [uart_output], 0
    je .skip
    cmp byte [link_failed], 0
    jne .skip
    push ax
    push dx
    bios_uart_write
    jnc .done
    mov byte [link_failed], 1
    mov byte [quit], 1
.done:
    pop dx
    pop ax
.skip:
    ret

%macro row 1
%%start: db %1
    times 20 - ($ - %%start) db ' '
%endmacro

template:
    row 'TIMER       ESC:EXIT'
    row '00:00   READY'
    row 'ENTER: START / PAUSE'
    row '+/-:30s  R:RESET'
ready_text: db 'READY   '
running_text: db 'RUNNING '
paused_text: db 'PAUSED  '
finished_text: db 'DONE!   '
states: dw ready_text, running_text, paused_text, finished_text
clock_error: row 'PIT CLOCK REQUIRED'
service_start: db 'TIMER: display on LCD.',13,10
    db 'Enter: start/pause; +/-: 30s; R: reset; Esc: exit.',13,10
service_start_end:
service_status: db 'TIMER: '
service_state: times 8 db ' '
    db ' '
service_time: db '00:00',13,10
service_status_end:
service_clock_error: db 'TIMER: PIT clock unavailable.',13,10
service_clock_error_end:
service_exit: db 'TIMER: exit.',13,10
service_exit_end:
reported_state: db 0FFh
reported_duration: dw 0FFFFh
reported_no_clock: db 0FFh
duration: dw 60
remaining: dw 60
state: db READY
clock_valid: db 0
no_clock: db 0
last_tick: dw 0
last_high: dw 0
fraction_low: dw 0
fraction_high: dw 0
lcd_output: db 0
uart_output: db 0
link_failed: db 0
quit: db 0
frame: times CELLS db ' '
previous: times CELLS db 0FFh
