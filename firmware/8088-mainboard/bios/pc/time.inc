pc_null_interrupt:
    iret

pc_equipment:
    API_ENTER
    xor ax, ax
    mov ds, ax
    mov ax, [0410h]
    mov [ss:bp - 2], ax
    jmp api_return

pc_memory:
    API_ENTER
    xor ax, ax
    mov ds, ax
    mov ax, [0413h]
    mov [ss:bp - 2], ax
    jmp api_return

pc_time:
    API_ENTER
    xor bx, bx
    mov ds, bx
    cmp ah, 0
    je .read
    cmp ah, 1
    jne api_error
    cmp cx, 0018h
    ja api_error
    jb .set
    cmp dx, 00B0h
    jae api_error
.set:
    cli
    mov [046Ch], dx
    mov [046Eh], cx
    mov byte [0470h], 0
    jmp api_board_ok
.read:
    cli
    mov ax, [046Eh]
    mov [ss:bp - 6], ax
    mov ax, [046Ch]
    mov [ss:bp - 8], ax
    xor ax, ax
    xchg al, [0470h]
    mov [ss:bp - 2], ax
    jmp api_board_ok

