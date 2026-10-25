BITS 16
CPU 8086
ORG 7C00h
    jmp short start
    nop
    db 'HB8088  '
    dw 512
    db 1
    dw 17                         ; boot sector + 16-sector kernel
    db 2
    dw 224
    dw 2880
    db 0F0h
    dw 9
    dw 18
    dw 2
    dd 0
    dd 0
    db 0, 0, 29h
    dd 080880001h
    db 'HOME8088 OS'
    db 'FAT12   '
start:
    cli
    xor ax, ax
    mov ss, ax
    mov sp, 7C00h
    mov ds, ax
    sti
    cld
    mov ax, 1000h
    mov es, ax
    xor bx, bx
    mov ax, 0210h                 ; 16 kernel sectors, one track
    mov cx, 2
    xor dx, dx
    int 13h
    jc failed
    jmp 1000h:0000h
failed:
    mov si, error
.next:
    lodsb
    test al, al
    jz .halt
    mov ah, 0Eh
    xor bx, bx
    int 10h
    jmp .next
.halt:
    sti
    hlt
    jmp .halt
error: db 13, 10, 'Kernel read failed. Reset to retry.', 13, 10, 0
    times 510-($-$$) db 0
    dw 0AA55h
