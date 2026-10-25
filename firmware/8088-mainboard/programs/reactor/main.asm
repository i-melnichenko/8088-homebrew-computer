BITS 16
CPU 8086
ORG 100h
%include "bios_api.inc"

READY equ 0
PLAY equ 1
PAUSED equ 2
WON equ 3
LOST equ 4
CELLS equ 80
DURATION equ 180

start:
    cld
    push cs
    pop ds
    push cs
    pop es
    mov ah, 0
    int BIOS_INT_BOARD
    and dx, BIOS_API_FEATURE_TIME | BIOS_API_FEATURE_UART_BLOCK | BIOS_API_FEATURE_CONSOLE_INFO
    cmp dx, BIOS_API_FEATURE_TIME | BIOS_API_FEATURE_UART_BLOCK | BIOS_API_FEATURE_CONSOLE_INFO
    jne .return
    bios_console_info
    mov [host_mode], al
    mov [lcd_supported], dl
    mov [lcd_output], dl
    mov byte [uart_output], 0
    cmp al, 1                     ; keyboard sharing must never receive raw text
    je .outputs
    mov byte [uart_output], 1
.outputs:
    mov byte [quit], 0
    mov byte [uart_started], 0
    call restart
.loop:
    call keys
    cmp byte [quit], 0
    jne .exit
    call advance_time
    call compose
    call present
    mov cx, 200
    bios_delay
    jmp .loop
.exit:
    cmp byte [uart_started], 0
    je .return
    cmp byte [link_failed], 0
    jne .return
    mov si, restore_terminal
    mov cx, restore_terminal_end - restore_terminal
    call write
.return:
    cld
    int 20h

; Reset all game state, including any paused/elapsed time. Output preferences
; survive a restart. No direct hardware access: all services belong to BIOS.
restart:
    mov byte [state], READY
    mov byte [power], 35
    mov byte [temperature], 35
    mov byte [pressure], 25
    mov byte [coolant], 100
    mov byte [cool_left], 0
    mov byte [vent_left], 0
    mov byte [alarm], 0             ; 1 cooling fault, 2 pressure surge
    mov byte [deadline], 0
    mov byte [selection], 0
    mov byte [clock_valid], 0
    mov byte [soft_count], 0
    mov byte [link_failed], 0
    mov word [seconds], 0
    mov word [score], 0
    call invalidate
    ret

invalidate:
    push di
    push cx
    push ax
    mov di, previous
    mov cx, CELLS
    mov al, 0FFh
    rep stosb
    pop ax
    pop cx
    pop di
    ret

; Drain at most 8 keys per slice so key repeat cannot starve the simulation.
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
    je .quit
    cmp al, '1'
    je .uart
    cmp al, '2'
    je .lcd
    cmp al, '3'
    je .both
    cmp al, 'A'
    jb .normalized
    cmp al, 'Z'
    ja .normalized
    add al, 20h
.normalized:
    cmp al, 'r'
    je restart
    cmp al, 'p'
    je .pause
    cmp byte [state], READY
    je .ready
    cmp byte [state], PLAY
    jne .done
    cmp ah, 4Bh
    je .left
    cmp al, 'a'
    je .left
    cmp ah, 4Dh
    je .right
    cmp al, 'd'
    je .right
    cmp ah, 48h
    je .up
    cmp al, '+'
    je .up
    cmp al, '='
    je .up
    cmp ah, 50h
    je .down
    cmp al, '-'
    je .down
    cmp al, 'c'
    je cool
    cmp al, 'v'
    je vent
    cmp al, 0Dh
    je .activate
    cmp al, ' '
    je .activate
.done:
    ret
.ready:
    cmp al, 0Dh
    jne .done
    mov byte [state], PLAY
    mov byte [clock_valid], 0
    ret
.pause:
    cmp byte [state], PLAY
    je .stop
    cmp byte [state], PAUSED
    jne .done
    mov byte [state], PLAY
    mov byte [clock_valid], 0
    ret
.stop:
    mov byte [state], PAUSED
    mov byte [clock_valid], 0
    ret
.left:
    cmp byte [selection], 0
    jne .left_dec
    mov byte [selection], 3
.left_dec:
    dec byte [selection]
    ret
.right:
    inc byte [selection]
    cmp byte [selection], 3
    jb .done
    mov byte [selection], 0
    ret
.up:
    cmp byte [power], 100
    jae .done
    add byte [power], 5
    ret
.down:
    cmp byte [power], 0
    je .done
    sub byte [power], 5
    ret
.activate:
    cmp byte [selection], 1
    je cool
    cmp byte [selection], 2
    je vent
    jmp .up
.uart:
    cmp byte [host_mode], 1
    je .done
    mov byte [uart_output], 1
    mov byte [lcd_output], 0
    jmp invalidate
.lcd:
    cmp byte [lcd_supported], 0
    je .done
    mov byte [uart_output], 0
    mov byte [lcd_output], 1
    jmp invalidate
.both:
    mov al, [lcd_supported]
    mov [lcd_output], al
    cmp byte [host_mode], 1
    je .done
    mov byte [uart_output], 1
    jmp invalidate
.quit:
    mov byte [quit], 1
    ret

cool:
    cmp byte [cool_left], 0
    jne .done
    cmp byte [coolant], 12
    jb .done
    sub byte [coolant], 12
    mov byte [cool_left], 3
    cmp byte [alarm], 1
    jne .done
    call resolve
.done:
    ret
vent:
    cmp byte [vent_left], 0
    jne .done
    mov byte [vent_left], 5
    mov al, [pressure]
    sub al, 25
    jnc .pressure
    xor al, al
.pressure:
    mov [pressure], al
    cmp byte [power], 5
    jb .resolved
    sub byte [power], 5
.resolved:
    cmp byte [alarm], 2
    jne .done
    call resolve
.done:
    ret
resolve:
    mov byte [alarm], 0
    mov byte [deadline], 0
    add word [score], 100
    ret

; One game second per 1193182 PIT input clocks. Snapshot elapsed time, retaining
; fractions and handling 32-bit wrap. Catch up at most five seconds per slice.
; Without PIT, five software-paced slices form one training second.
advance_time:
    bios_ticks
    jc .software
    mov byte [hardware_clock], 1
    cmp byte [state], PLAY
    jne .anchor
    cmp byte [clock_valid], 0
    je .anchor
    mov bx, ax
    mov si, dx
    sub ax, [last_tick]
    sbb dx, [last_high]
    test dx, dx
    jnz .clamp
    cmp ax, 92
    jbe .elapsed
.clamp:
    mov ax, bx
    mov dx, si
    sub ax, 92
    sbb dx, 0
    mov [last_tick], ax
    mov [last_high], dx
    mov ax, 92
.elapsed:
    mov cx, 18
    cmp word [clock_fraction], 034DEh
    jae .tick_count
    inc cx
.tick_count:
    cmp ax, cx
    jb .done
    sub word [clock_fraction], 034DEh
    add word [last_tick], cx
    adc word [last_high], 0
    push ax
    push cx
    call second
    pop cx
    pop ax
    cmp byte [state], PLAY
    jne .done
    sub ax, cx
    jmp .elapsed
.anchor:
    mov word [clock_fraction], 0
    mov [last_tick], ax
    mov [last_high], dx
    mov byte [clock_valid], 1
    mov byte [soft_count], 0
.done:
    ret
.software:
    mov byte [hardware_clock], 0
    mov byte [clock_valid], 0
    cmp byte [state], PLAY
    jne .reset_soft
    inc byte [soft_count]
    cmp byte [soft_count], 5
    jb .done
    mov byte [soft_count], 0
    jmp second
.reset_soft:
    mov byte [soft_count], 0
    ret

; One second of simulation. Values saturate at zero/100, score <= 18900.
second:
    inc word [seconds]
    xor ax, ax
    mov al, [power]
    add [score], ax
    mov bl, 15
    div bl
    sub al, 2
    cmp byte [cool_left], 0
    je .heat
    dec byte [cool_left]
    sub al, 8
.heat:
    cmp byte [alarm], 0
    je .temperature
    add al, 2
.temperature:
    add al, [temperature]
    jns .temp_positive
    xor al, al
.temp_positive:
    cmp al, 100
    jae .failure
    mov [temperature], al
    xor ax, ax
    mov al, [power]
    mov bl, 20
    div bl
    sub al, 1
    cmp byte [alarm], 2
    jne .pressure
    add al, 3
.pressure:
    add al, [pressure]
    jns .pressure_positive
    xor al, al
.pressure_positive:
    cmp al, 100
    jae .failure
    mov [pressure], al
    cmp byte [coolant], 100
    jae .vent
    inc byte [coolant]
.vent:
    cmp byte [vent_left], 0
    je .alarm
    dec byte [vent_left]
.alarm:
    cmp byte [alarm], 0
    je .new_alarm
    dec byte [deadline]
    jz .failure
    jmp .finish
.new_alarm:
    mov ax, [seconds]
    xor dx, dx
    mov bx, 20
    div bx
    test dx, dx
    jnz .finish
    and al, 1
    inc al
    mov [alarm], al
    mov byte [deadline], 6
.finish:
    cmp word [seconds], DURATION
    jb .done
    mov byte [state], WON
.done:
    ret
.failure:
    mov byte [state], LOST
    ret

; Build exactly four padded 20-byte rows. No text scrolling or glyph uploads.
compose:
    mov si, template
    mov di, frame
    mov cx, CELLS
    rep movsb
    mov ax, DURATION
    sub ax, [seconds]
    mov di, frame + 8
    mov cx, 3
    call number
    mov ax, [score]
    mov di, frame + 15
    mov cx, 5
    call number
    xor ax, ax
    mov al, [power]
    mov di, frame + 22
    mov cx, 3
    call number
    mov al, [temperature]
    mov di, frame + 28
    mov cx, 3
    call number
    mov al, [pressure]
    mov di, frame + 36
    mov cx, 3
    call number
    mov al, [coolant]
    mov di, frame + 45
    mov cx, 3
    call number
    mov al, [cool_left]
    add al, '0'
    mov [frame + 52], al
    mov al, [vent_left]
    add al, '0'
    mov [frame + 56], al
    cmp byte [hardware_clock], 0
    jne .message
    mov byte [frame + 58], '~'
.message:
    mov si, ready_text
    cmp byte [state], READY
    je .copy
    mov si, pause_text
    cmp byte [state], PAUSED
    je .copy
    mov si, won_text
    cmp byte [state], WON
    je .copy
    mov si, lost_text
    cmp byte [state], LOST
    je .copy
    mov si, cool_alarm
    cmp byte [alarm], 1
    je .alarm
    mov si, vent_alarm
    cmp byte [alarm], 2
    je .alarm
    xor bx, bx
    mov bl, [selection]
    shl bx, 1
    mov si, [actions + bx]
    jmp .copy
.alarm:
    mov di, frame + 60
    mov cx, 20
    rep movsb
    mov al, [deadline]
    add al, '0'
    mov [frame + 75], al
    ret
.copy:
    mov di, frame + 60
    mov cx, 20
    rep movsb
    ret

; AX value, CX decimal width, ES:DI destination. Leading zeros, <=5 digits.
number:
    push di
    add di, cx
    dec di
    mov bx, 10
.digit:
    xor dx, dx
    div bx
    add dl, '0'
    mov [di], dl
    dec di
    loop .digit
    pop di
    ret

; Compare cells once; update changed LCD cells and only changed UART rows.
present:
    cmp byte [uart_output], 0
    je .begin
    cmp byte [uart_started], 0
    jne .begin
    mov byte [uart_started], 1
    mov si, open_terminal
    mov cx, open_terminal_end - open_terminal
    call write
.begin:
    xor di, di
    xor dx, dx                     ; DH=row, DL=column
.row:
    mov byte [row_dirty], 0
.cell:
    mov al, [frame + di]
    cmp al, [previous + di]
    je .next
    mov byte [row_dirty], 1
    mov [previous + di], al
    cmp byte [lcd_output], 0
    je .next
    mov ah, 04h
    int BIOS_INT_BOARD
.next:
    inc di
    inc dl
    cmp dl, 20
    jb .cell
    cmp byte [row_dirty], 0
    je .next_row
    cmp byte [uart_output], 0
    je .next_row
    ; Fixed rows 1..4: ESC [ row ; 1 H followed by exactly 20 cells.
    mov al, dh
    add al, '1'
    mov [cursor + 2], al
    mov si, cursor
    mov cx, 6
    call write
    mov si, frame
    add si, di
    sub si, 20
    mov cx, 20
    call write
.next_row:
    xor dl, dl
    inc dh
    cmp dh, 4
    jb .row
    ret

; DS:SI/CX bytes; preserve all registers. Timeout ends the program so loss of
; a draining terminal cannot leave it stuck repeatedly attempting output.
write:
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
    row 'REACTOR 000s S:00000'
    row 'P:000 T:000 BAR:000'
    row 'COOL:000% C:0 V:0 H'
    row ''
ready_text: row 'ENTER:START ESC:EXIT'
pause_text: row 'PAUSED P:RESUME'
won_text: row 'SAFE R:RETRY ESC:END'
lost_text: row 'FAIL R:RETRY ESC:END'
power_text: row '>POWER +/- ENTER:UP'
cool_text: row '>COOL ENTER / C'
vent_text: row '>VENT ENTER / V'
cool_alarm: row 'COOL NOW!      0 SEC'
vent_alarm: row 'VENT NOW!      0 SEC'
actions: dw power_text, cool_text, vent_text
open_terminal: db 27,'[2J',27,'[H',27,'[?25l'
open_terminal_end:
restore_terminal: db 27,'[?25h',13,10
restore_terminal_end:
cursor: db 27,'[1;1H'
state: db READY
power: db 35
temperature: db 35
pressure: db 25
coolant: db 100
cool_left: db 0
vent_left: db 0
alarm: db 0
deadline: db 0
selection: db 0
seconds: dw 0
score: dw 0
last_tick: dw 0
last_high: dw 0
clock_valid: db 0
hardware_clock: db 0
soft_count: db 0
host_mode: db 0
lcd_supported: db 0
lcd_output: db 0
uart_output: db 0
uart_started: db 0
link_failed: db 0
quit: db 0
row_dirty: db 0
frame: times CELLS db ' '
previous: times CELLS db 0FFh

clock_fraction: dw 0
