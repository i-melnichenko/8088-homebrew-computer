pc_boot_interrupt:
    cli
    xor ax, ax
    mov ds, ax
    mov ss, ax
    mov sp, 7C00h
    jmp pc_boot

; Reserve the upper 32 KiB of contiguous RAM and move live BIOS state there.
; The entire used half-bank, including the current monitor stack, is copied.
pc_boot:
    cli
    xor ax, ax
    mov ds, ax
    mov ax, [0413h]
    cmp ax, 96
    jb .failed
    LOAD_BANK dx
    test dx, dx
    jnz .relocated
    sub ax, 32
    mov [0413h], ax
    sub ax, 32
    mov cl, 6
    shl ax, cl
    mov es, ax
    mov si, 8000h
    mov di, si
    mov cx, 4000h
    rep movsw
    mov [BIOS_BANK_POINTER], ax
    mov ss, ax                  ; same SP, copied return frames
.relocated:
    mov ax, cs
    mov ds, ax
    LOAD_STATE ax
    mov es, ax
    mov byte [es:OS_RUNNING], 1
    RUNTIME_ENTER
    mov byte [es:PC_MODE], 1
    RUNTIME_LEAVE
    xor ax, ax
    mov ds, ax
    mov word [0410h], 0001h        ; one floppy
    mov byte [0449h], 3
    mov word [044Ah], 80
    mov word [044Ch], 4000
    mov byte [0484h], 24
    mov word [0450h], 0
    mov ax, cs
    mov ds, ax
    call runtime_enable
    call pc_lcd_status
    xor ax, ax
    call pc_read_sector
    jc .failed
    push ds
    LOAD_BANK ax
    mov ds, ax
    xor ax, ax
    mov es, ax
    mov si, PC_SECTOR
    mov di, 7C00h
    mov cx, 256
    rep movsw
    pop ds
    cmp word [es:7DFEh], 0AA55h
    jne .failed
    cli
    xor ax, ax
    mov ds, ax
    mov es, ax
    mov ss, ax
    mov sp, 7C00h
    xor dx, dx                  ; boot drive A:
    sti
    jmp 0000h:7C00h
.failed:
    mov ax, cs
    mov ds, ax
    LOAD_STATE ax
    mov es, ax
    mov si, pc_boot_error
    call pc_print_rom
.failure_wait:
    call runtime_pump
    jmp .failure_wait
pc_boot_error: db 13, 10, 'BOOT FAILED - check disk image; press Reset', 13, 10, 0

