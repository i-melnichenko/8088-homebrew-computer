; Validate physical DX:SI with CX bytes inside the canonical EEPROM window.
; Callers enforce nonzero command-specific sizes. CF=0 returns SI as the
; EEPROM-relative offset; CF=1 rejects the range before any access.
eeprom_validate_range:
    cmp dx, (ROM_SEGMENT * 16) >> 16
    jne .error
    cmp si, (ROM_SEGMENT * 16) & 0FFFFh
    jb .error
    sub si, (ROM_SEGMENT * 16) & 0FFFFh
    mov ax, si
    add ax, cx
    jc .error
    cmp ax, 8000h
    ja .error
    clc
    ret
.error:
    stc
    ret
