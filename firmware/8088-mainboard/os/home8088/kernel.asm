BITS 16
CPU 8086
ORG 0
; A deliberately small single-task shell. Uses PC BIOS, owns its own stack,
; reads FAT12 and starts COM programs with a separate segment/stack and PSP.
KERNEL_SEG equ 1000h
ROOT_SEG equ 0200h
FAT_SEG equ 0400h
APP_SEG equ 0600h
DATA_LBA equ 49                    ; 17 reserved + 2*9 FAT + 14 root
start:
    cli
    mov ax, cs
    mov ss, ax
    mov sp, 7FFEh
    mov ds, ax
    mov es, ax
    cld
    xor ax, ax
    mov es, ax
    mov word [es:20h*4], app_exit
    mov word [es:20h*4+2], cs
    mov ax, cs
    mov es, ax
    sti
    mov ax, 3
    int 10h
    mov si, welcome
    call puts
shell:
    mov ax, cs
    mov ds, ax
    mov es, ax
    cld
    mov si, prompt
    call puts
    mov di, line
    xor cx, cx
.input:
    xor ah, ah
    int 16h
    cmp al, 13
    je .enter
    cmp al, 8
    je .backspace
    cmp al, ' '
    jb .input
    cmp al, 126
    ja .input
    cmp cx, 47
    jae .input
    cmp al, 'a'
    jb .store
    cmp al, 'z'
    ja .store
    sub al, 32
.store:
    stosb
    inc cx
    call putc
    jmp .input
.backspace:
    jcxz .input
    dec di
    dec cx
    mov si, erase
    call puts
    jmp .input
.enter:
    mov byte [di], 0
    mov si, newline
    call puts
    mov si, line
    mov di, cmd_help
    call equal
    je help
    mov di, cmd_mem
    call equal
    je memory
    mov di, cmd_dir
    call equal
    je directory
    mov di, cmd_reboot
    call equal
    je reboot
    cmp word [line], 'RU'
    jne .unknown
    cmp word [line+2], 'N '
    je run_program
.unknown:
    cmp byte [line], 0
    je shell
    mov si, unknown
    call puts
    jmp shell
help:
    mov si, commands
    call puts
    jmp shell
memory:
    int 12h
    call decimal
    mov si, memory_suffix
    call puts
    jmp shell
reboot:
    jmp 0FFFFh:0000h

; Read one sector AX=LBA, ES:BX destination. Preserve all except AX/FLAGS.
read_sector:
    push bx
    push cx
    push dx
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
    push bp
    mov bp, sp
    mov bx, [ss:bp+6]              ; original BX below saved CX/DX
    mov ax, 0201h
    int 13h
    pop bp
    pop dx
    pop cx
    pop bx
    ret

load_root:
    mov ax, ROOT_SEG
    mov es, ax
    xor bx, bx
    mov ax, 35
    mov cx, 14
.loop:
    push ax
    push cx
    call read_sector
    pop cx
    pop ax
    jc disk_error
    inc ax
    add bx, 512
    loop .loop
    xor di, di
    ret

directory:
    call load_root
    mov cx, 224
.next:
    cmp byte [es:di], 0
    je .done
    cmp byte [es:di], 0E5h
    je .skip
    test byte [es:di+11], 18h
    jnz .skip
    push cx
    mov cx, 8
    xor bx, bx
.name:
    mov al, [es:di+bx]
    cmp al, ' '
    je .name_next
    call putc
.name_next:
    inc bx
    loop .name
    mov al, '.'
    call putc
    mov cx, 3
.ext:
    mov al, [es:di+bx]
    call putc
    inc bx
    loop .ext
    mov si, newline
    call puts
    pop cx
.skip:
    add di, 32
    loop .next
.done:
    jmp shell

run_program:
    mov di, filename
    mov cx, 11
    mov al, ' '
    push ds
    pop es
    rep stosb
    mov word [filename+8], 'CO'
    mov byte [filename+10], 'M'
    mov si, line+4
    mov di, filename
    xor cx, cx
.name:
    lodsb
    test al, al
    jz .find
    cmp al, '.'
    je .extension
    cmp al, ' '
    jbe bad_name
    cmp al, '/'
    je bad_name
    cmp al, '\'
    je bad_name
    cmp cx, 8
    jae bad_name
    stosb
    inc cx
    jmp .name
.extension:
    test cx, cx
    jz bad_name
    mov di, filename+8
    mov cx, 3
.ext:
    lodsb
    cmp al, ' '
    jbe bad_name
    stosb
    loop .ext
    cmp byte [si], 0
    jne bad_name
    mov cx, 1
.find:
    test cx, cx
    jz bad_name
    call load_root
    mov cx, 224
.entry:
    push cx
    push di
    mov si, filename
    mov cx, 11
    repe cmpsb
    pop di
    pop cx
    je .found
    add di, 32
    loop .entry
    mov si, not_found
    jmp message
.found:
    test byte [es:di+11], 18h
    jnz bad_name
    cmp word [es:di+30], 0
    jne too_large
    mov ax, [es:di+28]
    test ax, ax
    jz too_large
    cmp ax, 7B00h
    ja too_large
    add ax, 511
    mov cl, 9
    shr ax, cl
    mov [sectors_left], ax
    mov ax, [es:di+26]
    mov [cluster], ax
    ; Read FAT1. The loader follows chains, including fragmented files.
    mov ax, FAT_SEG
    mov es, ax
    xor bx, bx
    mov ax, 17
    mov cx, 9
.fat:
    push ax
    push cx
    call read_sector
    pop cx
    pop ax
    jc disk_error
    inc ax
    add bx, 512
    loop .fat
    mov ax, APP_SEG
    mov es, ax
    mov bx, 0100h
.load:
    mov ax, [cluster]
    cmp ax, 2
    jb disk_error
    cmp ax, 2833
    jae disk_error
    add ax, DATA_LBA-2
    call read_sector
    jc disk_error
    add bx, 512
    dec word [sectors_left]
    jz .loaded
    push es
    mov ax, FAT_SEG
    mov es, ax
    mov ax, [cluster]
    mov di, ax
    shr di, 1
    add di, ax
    mov ax, [es:di]
    test word [cluster], 1
    jz .even
    mov cl, 4
    shr ax, cl
.even:
    and ax, 0FFFh
    mov [cluster], ax
    pop es
    jmp .load
.loaded:
    xor ax, ax
    xor di, di
    mov cx, 128
    rep stosw
    mov word [es:0], 20CDh          ; PSP: INT 20h
    mov word [es:2], APP_SEG+0800h
    mov word [es:80h], 0D00h        ; empty command tail
    mov [saved_sp], sp
    cli
    mov ax, APP_SEG
    mov ss, ax
    mov sp, 7FFCh
    mov word [ss:7FFCh], 0          ; near RET lands at PSP INT 20h
    mov ds, ax
    mov es, ax
    cld
    sti
    jmp APP_SEG:0100h

app_exit:
    cli
    mov ax, KERNEL_SEG
    mov ss, ax
    mov sp, [cs:saved_sp]
    mov ds, ax
    mov es, ax
    cld
    sti
    mov ax, 3
    int 10h
    jmp shell
bad_name:
    mov si, name_error
    jmp message
too_large:
    mov si, size_error
    jmp message
disk_error:
    ; Disk failure may occur inside a helper with temporary stack entries.
    mov ax, KERNEL_SEG
    mov ss, ax
    mov sp, 7FFEh
    mov ds, ax
    mov si, io_error
message:
    call puts
    jmp shell

equal:
    push si
    push di
.next:
    mov al, [si]
    cmp al, [di]
    jne .done
    inc si
    inc di
    test al, al
    jnz .next
.done:
    pop di
    pop si
    ret
putc:
    push ax
    push bx
    mov ah, 0Eh
    xor bx, bx
    int 10h
    pop bx
    pop ax
    ret
puts:
    lodsb
    test al, al
    jz .done
    call putc
    jmp puts
.done:
    ret
decimal:
    xor cx, cx
    mov bx, 10
.next:
    xor dx, dx
    div bx
    push dx
    inc cx
    test ax, ax
    jnz .next
.print:
    pop ax
    add al, '0'
    call putc
    loop .print
    ret
welcome: db 'HOME8088 OS 0.1',13,10,'PC BIOS / FAT12 / single task',13,10,'Type HELP for commands.',13,10,0
prompt: db 'A> ',0
newline: db 13,10,0
erase: db 8,' ',8,0
commands: db 'HELP  MEM  DIR  RUN name[.COM]  REBOOT',13,10,0
memory_suffix: db ' KiB conventional RAM',13,10,0
unknown: db 'Unknown command. Type HELP.',13,10,0
not_found: db 'Program not found.',13,10,0
name_error: db 'Use RUN name[.COM], an 8.3 filename.',13,10,0
size_error: db 'Program must be 1..31488 bytes.',13,10,0
io_error: db 'Disk read failed.',13,10,0
cmd_help: db 'HELP',0
cmd_mem: db 'MEM',0
cmd_dir: db 'DIR',0
cmd_reboot: db 'REBOOT',0
line: times 48 db 0
filename: times 11 db ' '
cluster: dw 0
sectors_left: dw 0
saved_sp: dw 0
