BITS 16
CPU 8086
ORG 0
; DOS 2.0 OEM I/O layer. Unmodified Microsoft kernel and COMMAND.COM use
; these device headers; all hardware access goes through PC BIOS interrupts.
IO_SEG equ 0070h
DOS_SEG equ 0300h
ROOT_PSP equ 0060h
DOS_LBA equ 17 + 2 * 9 + 14       ; first FAT12 data sector
DOS_SECTORS equ (16690 + 511) / 512 ; pinned Microsoft MSDOS.SYS

REQ_COMMAND equ 2
REQ_STATUS equ 3
REQ_INIT_UNITS equ 13
REQ_INIT_END equ 14
REQ_INIT_BPBS equ 18
REQ_BUFFER equ 14
REQ_COUNT equ 18
REQ_SECTOR equ 20
REQ_PEEK equ 13
REQ_MEDIA_CHECK equ 14
REQ_BPB_RESULT equ 18

CMD_INIT equ 0
CMD_MEDIA_CHECK equ 1
CMD_BUILD_BPB equ 2
CMD_READ equ 4
CMD_PEEK equ 5
CMD_INPUT_STATUS equ 6
CMD_INPUT_FLUSH equ 7
CMD_WRITE equ 8
CMD_WRITE_VERIFY equ 9
CMD_OUTPUT_STATUS equ 10
CMD_OUTPUT_FLUSH equ 11

DOS_DONE equ 0100h
DOS_BUSY equ 0300h
DOS_BAD_COMMAND equ 8103h
DOS_WRITE_PROTECTED equ 8100h
DOS_READ_ERROR equ 810ch
DOS_WRITE_ERROR equ 810ah
jmp initialize

%macro device 5
%1:
    dw %2, IO_SEG
    dw %3
    dw strategy, %4
    db %5
%endmacro
device console, auxiliary, 8003h, con_interrupt, 'CON     '
device auxiliary, printer, 8000h, aux_interrupt, 'AUX     '
device printer, clock, 8000h, aux_interrupt, 'PRN     '
device clock, disk, 8008h, clock_interrupt, 'CLOCK$  '
disk:
    dw -1, -1, 2000h, strategy, disk_interrupt
    db 1, 0, 0, 0, 0, 0, 0, 0

strategy:
    mov [cs:request], bx
    mov [cs:request+2], es
    retf
con_interrupt:
    mov byte [cs:device_kind], 0
    jmp device_entry
aux_interrupt:
    mov byte [cs:device_kind], 1
    jmp device_entry
clock_interrupt:
    mov byte [cs:device_kind], 2
    jmp device_entry
disk_interrupt:
    mov byte [cs:device_kind], 3

; DOS has a shallow internal stack. Switch before saving registers or calling
; ROM BIOS, then restore the original stack/registers/FLAGS before RETF.
; DOS 2.0 device requests are synchronous; this driver is not reentrant.
device_entry:
    mov [cs:saved_ax], ax
    mov [cs:saved_ss], ss
    mov [cs:saved_sp], sp
    pushf
    pop word [cs:saved_flags]
    cli
    mov ax, cs
    mov ss, ax
    mov sp, driver_stack_top
    mov ax, [cs:saved_ax]
    push ax
    push bx
    push cx
    push dx
    push si
    push di
    push bp
    push ds
    push es
    cld
    mov ax, cs
    mov ds, ax
    sti
    les bx, [request]
    mov word [es:bx+REQ_STATUS], DOS_DONE
    mov al, [es:bx+REQ_COMMAND]
    test al, al
    jz device_init
    cmp byte [device_kind], 3
    je disk_request
    cmp byte [device_kind], 2
    je clock_request
    cmp byte [device_kind], 1
    je auxiliary_request
    cmp al, CMD_READ
    je console_read
    cmp al, CMD_PEEK
    je console_peek
    cmp al, CMD_INPUT_STATUS
    je console_status
    cmp al, CMD_INPUT_FLUSH
    je console_flush
    cmp al, CMD_WRITE
    je console_write
    cmp al, CMD_WRITE_VERIFY
    je console_write
    cmp al, CMD_OUTPUT_STATUS
    je device_done
    cmp al, CMD_OUTPUT_FLUSH
    je device_done
bad_command:
    mov word [es:bx+REQ_STATUS], DOS_BAD_COMMAND
    jmp device_done

device_init:
    mov word [es:bx+REQ_INIT_END], resident_end
    mov word [es:bx+REQ_INIT_END+2], IO_SEG
    cmp byte [device_kind], 3
    jne device_done
    mov byte [es:bx+REQ_INIT_UNITS], 1
    mov word [es:bx+REQ_INIT_BPBS], bpb_array
    mov word [es:bx+REQ_INIT_BPBS+2], IO_SEG
    jmp device_done

console_read:
    mov cx, [es:bx+REQ_COUNT]
    les di, [es:bx+REQ_BUFFER]
.next:
    test cx, cx
    jz device_done
    cmp byte [extended_key], 0
    je .key
    mov al, [extended_key]
    mov byte [extended_key], 0
    jmp .store
.key:
    xor ah, ah
    int 16h
    test al, al
    jnz .store
    mov [extended_key], ah
.store:
    stosb
    loop .next
    jmp device_done
console_peek:
    cmp byte [extended_key], 0
    je .peek
    mov al, [extended_key]
    jmp .ready
.peek:
    mov ah, 1
    int 16h
    jz input_busy
.ready:
    mov [es:bx+REQ_PEEK], al
    jmp device_done
console_status:
    cmp byte [extended_key], 0
    jne device_done
    mov ah, 1
    int 16h
    jnz device_done
input_busy:
    mov word [es:bx+REQ_STATUS], DOS_BUSY
    jmp device_done
console_flush:
    mov byte [extended_key], 0
.loop:
    mov ah, 1
    int 16h
    jz device_done
    xor ah, ah
    int 16h
    jmp .loop
console_write:
    mov cx, [es:bx+REQ_COUNT]
    lds si, [es:bx+REQ_BUFFER]
.next:
    test cx, cx
    jz device_done
    lodsb
    mov ah, 0eh
    xor bx, bx
    int 10h
    loop .next
    jmp device_done
auxiliary_request:
    cmp al, CMD_READ
    jne .status
    mov word [es:bx+REQ_COUNT], 0
.status:
    jmp device_done

clock_request:
    cmp al, CMD_READ
    je .read
    cmp al, CMD_WRITE
    je .write
    cmp al, CMD_WRITE_VERIFY
    je .write
    jmp bad_command
.read:
    call clock_update
    les bx, [request]
    les di, [es:bx+REQ_BUFFER]
    mov si, clock_data
    mov cx, 6
    rep movsb
    jmp device_done
.write:
    mov di, clock_data
    lds si, [es:bx+REQ_BUFFER]
    push cs
    pop es
    mov cx, 6
    rep movsb
    push cs
    pop ds
    call clock_set
    jmp device_done

disk_request:
    cmp al, CMD_MEDIA_CHECK
    je .media
    cmp al, CMD_BUILD_BPB
    je .bpb
    cmp al, CMD_READ
    je .read
    cmp al, CMD_WRITE
    je .write
    cmp al, CMD_WRITE_VERIFY
    je .write
    jmp bad_command
.media:
    mov byte [es:bx+REQ_MEDIA_CHECK], 1
    jmp device_done
.bpb:
    mov word [es:bx+REQ_BPB_RESULT], bpb
    mov word [es:bx+REQ_BPB_RESULT+2], IO_SEG
    jmp device_done
.write:
    mov byte [disk_operation], 3
    jmp .transfer
.read:
    mov byte [disk_operation], 2
.transfer:
    mov ax, [es:bx+REQ_SECTOR]
    mov [next_lba], ax
    mov ax, [es:bx+REQ_COUNT]
    mov [remaining], ax
    mov word [transferred], 0
    mov ax, [es:bx+REQ_BUFFER]
    mov [transfer], ax
    mov ax, [es:bx+REQ_BUFFER+2]
    mov [transfer+2], ax
.sector:
    cmp word [remaining], 0
    je device_done
    mov ax, [next_lba]
    les bx, [transfer]
    call transfer_sector
    jc .error
    inc word [next_lba]
    inc word [transferred]
    dec word [remaining]
    add word [transfer], 512
    jnc .sector
    add word [transfer+2], 1000h
    jmp .sector
.error:
    mov dx, DOS_READ_ERROR
    cmp byte [disk_operation], 3
    jne .report_error
    mov dx, DOS_WRITE_ERROR
    cmp ah, 3
    jne .report_error
    mov dx, DOS_WRITE_PROTECTED
.report_error:
    les bx, [request]
    mov ax, [transferred]
    mov [es:bx+REQ_COUNT], ax
    mov [es:bx+REQ_STATUS], dx

device_done:
    cli
    pop es
    pop ds
    pop bp
    pop di
    pop si
    pop dx
    pop cx
    pop bx
    pop ax
    mov ss, [cs:saved_ss]
    mov sp, [cs:saved_sp]
    push word [cs:saved_flags]
    popf
    retf

; AX=LBA, ES:BX buffer. Single-sector transfers avoid track/DMA splits.
read_sector:
    mov byte [cs:disk_operation], 2
transfer_sector:
    push bx
    push cx
    push dx
    push bp
    mov bp, sp
    xor dx, dx
    mov cx, 18
    div cx
    inc dl
    mov cl, dl
    xor dx, dx
    mov bx, 2
    div bx
    mov ch, al
    mov dh, dl
    xor dl, dl
    mov bx, [ss:bp+6]
    mov al, 1
    mov ah, [cs:disk_operation]
    int 13h
    pop bp
    pop dx
    pop cx
    pop bx
    ret

; Exact PIT conversion: centiseconds = floor(ticks*65536*100/1193182).
; The 48/32-bit division uses only 8086 registers/instructions.
clock_update:
    xor ah, ah
    int 1ah
    xor ah, ah
    add [clock_data], ax
    mov ax, dx
    mov bx, 100
    mul bx
    mov si, ax
    mov di, dx
    mov ax, cx
    mul bx
    add ax, di
    mov bp, ax
    xor di, di
    xor ax, ax
    xor dx, dx
    mov cx, 48
.divide:
    shl di, 1
    rcl si, 1
    rcl bp, 1
    rcl ax, 1
    rcl dx, 1
    cmp dx, 0012h
    jb .next
    ja .subtract
    cmp ax, 034deh
    jb .next
.subtract:
    sub ax, 034deh
    sbb dx, 0012h
    or di, 1
.next:
    loop .divide
    mov bx, 100
    mov ax, si
    xor dx, dx
    div bx
    mov bp, ax
    mov ax, di
    div bx
    mov [clock_data+4], dl
    mov dx, bp
    mov bx, 3600
    div bx
    mov [clock_data+3], al
    mov ax, dx
    xor dx, dx
    mov bx, 60
    div bx
    mov [clock_data+2], al
    mov [clock_data+5], dl
    ret

; Convert DOS days/hours/minutes/seconds/hundredths back to the PC tick count.
clock_set:
    xor ax, ax
    mov al, [clock_data+3]
    mov bx, 60
    mul bx
    xor dx, dx
    mov dl, [clock_data+2]
    add ax, dx
    mul bx
    xor bx, bx
    mov bl, [clock_data+5]
    add ax, bx
    adc dx, 0
    mov bp, dx
    mov bx, 100
    mul bx
    mov di, ax
    mov si, dx
    mov ax, bp
    mul bx
    add ax, si
    mov bp, ax
    xor ax, ax
    mov al, [clock_data+4]
    add di, ax
    adc bp, 0
    ; Compute (centiseconds*1193182)>>16, then divide by 100.
    mov ax, di
    mov bx, 034deh
    mul bx
    mov si, dx
    xor cx, cx
    mov ax, bp
    mul bx
    add si, ax
    adc cx, dx
    mov ax, di
    mov bx, 0012h
    mul bx
    add si, ax
    adc cx, dx
    mov ax, bp
    mul bx
    add cx, ax
    mov bx, 100
    mov ax, cx
    xor dx, dx
    div bx
    mov bp, ax
    mov ax, si
    div bx
    mov cx, bp
    mov dx, ax
    cmp cx, 0018h
    jb .valid
    cmp dx, 00b0h
    jb .valid
    mov cx, 0018h
    mov dx, 00afh
.valid:
    mov ah, 1
    int 1ah
    ret

request: dd 0
device_kind: db 0
saved_ax: dw 0
saved_ss: dw 0
saved_sp: dw 0
saved_flags: dw 0
extended_key: db 0
disk_operation: db 2
remaining: dw 0
transferred: dw 0
next_lba: dw 0
transfer: dd 0
bpb_array: dw bpb, IO_SEG
bpb:
    dw 512
    db 1
    dw 17
    db 2
    dw 224, 2880
    db 0f0h
    dw 9
clock_data: dw 0
    db 0, 0, 0, 0
align 2
    times 1024 db 0
driver_stack_top:
resident_end:

    times 1024 db 0
bootstrap_stack_top:
initialize:
    cli
    mov ax, cs
    mov ss, ax
    mov sp, bootstrap_stack_top
    mov ax, cs
    mov ds, ax
    sti
    cld
    xor ax, ax
    mov es, ax
    mov word [es:1bh*4], interrupt_return
    mov word [es:1bh*4+2], cs
    mov word [es:24h*4], critical_error
    mov word [es:24h*4+2], cs
    mov word [es:29h*4], fast_console
    mov word [es:29h*4+2], cs
    mov si, loading
    call print
    mov ax, DOS_SEG
    mov es, ax
    xor bx, bx
    mov ax, DOS_LBA
    mov cx, DOS_SECTORS
.load:
    push ax
    push cx
    call read_sector
    pop cx
    pop ax
    jc boot_error
    inc ax
    add bx, 512
    loop .load
    int 12h
    mov cl, 6
    shl ax, cl
    mov dx, ax
    mov si, console
    call DOS_SEG:0
    ; DOS initialization returns the initial PSP in DS and DOS info in ES:DI.
    push ds
    push es
    mov ax, ROOT_PSP
    mov es, ax
    xor si, si
    xor di, di
    mov cx, 128
    rep movsw
    pop es
    pop ds
    mov bx, ROOT_PSP
    mov ah, 50h
    int 21h
    mov ax, cs
    mov ds, ax
    mov es, ax
    mov si, initialized
    call print
    mov ah, 30h
    int 21h
    mov [dos_version], ax
    mov dx, critical_error
    mov ax, 2524h
    int 21h
    mov ax, ROOT_PSP
    mov es, ax
    mov [es:16h], ax
    mov ax, cs
    mov es, ax
    ; Normalize inherited standard handles exactly as Microsoft SYSINIT does.
    xor bx, bx
    mov ah, 3eh
    int 21h
    mov bx, 2
    mov cx, 3
.close_standard:
    mov ah, 3eh
    int 21h
    inc bx
    loop .close_standard
    mov dx, con_name
    mov ax, 3d02h
    int 21h
    jc boot_error
    push ax
    mov bx, 1
    mov ah, 3eh
    int 21h
    pop bx
    mov ah, 45h
    int 21h
    mov ah, 45h
    int 21h
    ; Initialize AUX/PRN standard handles as SYSINIT does.
    mov dx, aux_name
    mov ax, 3d02h
    int 21h
    mov dx, prn_name
    mov ax, 3d01h
    int 21h
    xor dl, dl
    mov ah, 0eh
    int 21h
    mov dx, command_name
    mov bx, exec_block
    mov ax, 4b00h
    int 21h
    mov [exec_error], ax
boot_error:
    mov ax, cs
    mov ds, ax
    mov si, error_message
    call print
.halt:
    mov ah, 1
    int 16h
    jmp .halt

print:
    lodsb
    test al, al
    jz .done
    mov ah, 0eh
    xor bx, bx
    int 10h
    jmp print
.done:
    ret
interrupt_return:
    iret
critical_error:
    mov al, 3
    iret
fast_console:
    push ax
    push bx
    mov ah, 0eh
    xor bx, bx
    int 10h
    pop bx
    pop ax
    iret
loading: db 'Loading Microsoft MS-DOS 2.0...', 13, 10, 0
initialized: db 'Starting COMMAND.COM...', 13, 10, 0
error_message: db 'DOS boot failed.', 13, 10, 0
con_name: db 'CON', 0
aux_name: db 'AUX', 0
prn_name: db 'PRN', 0
command_name: db 'COMMAND.COM', 0
command_tail: db 2, '/P', 13
exec_block:
    dw 0
    dw command_tail, IO_SEG
    dw 005ch, ROOT_PSP
    dw 006ch, ROOT_PSP
dos_version: dw 0
exec_error: dw 0
