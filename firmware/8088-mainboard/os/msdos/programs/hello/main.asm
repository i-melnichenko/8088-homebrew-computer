BITS 16
CPU 8086
ORG 100h
; A real DOS application: output and termination use DOS INT 21h.
    mov dx,message
    mov ah,9
    int 21h
    mov ax,4c00h
    int 21h
message: db 'Hello from Microsoft MS-DOS!',13,10,'$'
