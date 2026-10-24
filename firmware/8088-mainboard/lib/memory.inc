; Rev2 has five optional, distinct 128-KiB banks at 00000h..9FFFFh.
; Offsets below are relative to the BIOS bank selected by POST or relocated
; at OS boot. Application stacks do not determine this bank.
RAM_END         equ 0A0000h
RAM_KIB         equ 640

RAM_SEGMENT      equ 0850h
BIOS_STATE_BASE  equ RAM_SEGMENT * 16
BIOS_STATE_SIZE  equ 0200h
STATE_END_OFFSET  equ 0300h
FLASHER_BASE    equ BIOS_STATE_BASE + STATE_END_OFFSET
SRAM_STACK_TOP   equ 0FFFEh
SRAM_STACK_BOTTOM equ 0FC00h

; 0000..03FF IVT, 0400..04FF BDA; 8000..81FF protected IRQ runtime state.
; 8500..86FF monitor state; 8700..87FF API state; 8800..FBFF flasher/diagnostic scratch.
; FC00h..FFFFh is reserved for BIOS stack use.
FLASHER_MAX_SIZE equ SRAM_STACK_BOTTOM - FLASHER_BASE

ROM_SEGMENT equ 0F800h

; BDA extension holds the BIOS bank segment. Before DOS boot this is the
; selected POST bank; boot relocates its upper 32 KiB above conventional RAM.
; Missing RAM0 retains the monitor-only SS-relative fallback (no INT services).
BIOS_BANK_POINTER equ 04F0h
BIOS_BANK_SIGNATURE equ 04F2h
%macro LOAD_BANK 1
    pushf
    push es
    xor %1, %1
    mov es, %1
    cmp word [es:BIOS_BANK_SIGNATURE], 0B105h
    jne %%legacy
    mov %1, [es:BIOS_BANK_POINTER]
    jmp %%done
%%legacy:
    mov %1, ss
%%done:
    pop es
    popf
%endmacro
%macro LOAD_STATE 1
    LOAD_BANK %1
    add %1, RAM_SEGMENT
%endmacro
; Runtime routines use ES=bank without relying on the application's SS.
%macro RUNTIME_ENTER 0
    push es
    push ax
    LOAD_BANK ax
    mov es, ax
    pop ax
%endmacro
%macro RUNTIME_LEAVE 0
    pop es
%endmacro

%macro LOAD_BDA 1
    pushf
    push es
    xor %1, %1
    mov es, %1
    cmp word [es:BIOS_BANK_SIGNATURE], 0B105h
    je %%done
    mov %1, ss
%%done:
    pop es
    popf
%endmacro
