BITS 16
CPU 8086
ORG 0

; Supplied by Make/CI, never a release number hardcoded in the ROM sources.
%ifndef BIOS_VERSION
    %error "BIOS_VERSION is required; build with make"
%endif
%strlen BIOS_VERSION_LENGTH BIOS_VERSION
%if BIOS_VERSION_LENGTH < 1 || BIOS_VERSION_LENGTH > 9
    %error "BIOS_VERSION must be 1..9 characters to fit the LCD Info row"
%endif
%assign BIOS_VERSION_INDEX 1
%rep BIOS_VERSION_LENGTH
    %substr BIOS_VERSION_CHAR BIOS_VERSION BIOS_VERSION_INDEX, 1
    %if !((BIOS_VERSION_CHAR >= 'a' && BIOS_VERSION_CHAR <= 'z') || (BIOS_VERSION_CHAR >= 'A' && BIOS_VERSION_CHAR <= 'Z') || (BIOS_VERSION_CHAR >= '0' && BIOS_VERSION_CHAR <= '9') || BIOS_VERSION_CHAR = '.' || BIOS_VERSION_CHAR = '-' || BIOS_VERSION_CHAR = '_')
        %error "BIOS_VERSION may contain only ASCII letters, digits, dots, hyphens or underscores"
    %endif
    %assign BIOS_VERSION_INDEX BIOS_VERSION_INDEX + 1
%endrep

; The reset vector jumps here at F800:0000. The monitor executes in ROM;
; only its packet buffers, upload state, and user programs occupy SRAM.
jmp start

%include "lcd.inc"
%include "memory.inc"
%include "uart16550.inc"

; Initialize revision-1 hardware and enter the menu without a splash screen.
start:
    cli
    cld
    xor ax, ax
    mov ss, ax
    mov sp, SRAM_STACK_TOP
    mov ax, cs
    mov ds, ax

    call lcd_initialize
    call uart_initialize
    call api_initialize
    jmp monitor_ready

%include "state.inc"
%include "monitor.inc"
%include "uart/framing.inc"
%include "uart/protocol.inc"
%include "keyboard.inc"
%include "uart/keyboard.inc"
%include "programs.inc"
%include "uart/programs.inc"
%include "uart/memory_read.inc"
%include "uart/memory_write.inc"
%include "menu.inc"
%include "boot.inc"
%include "utils/memory_check.inc"
%include "uart/info.inc"
%include "info.inc"
%include "api.inc"
%include "uart/flasher_mode.inc"

; 8088 reset vector at physical FFFF0h = EEPROM offset 7FF0h.
times 7FF0h - ($ - $$) db 0FFh
    jmp ROM_SEGMENT:0000h

; Complete 32 KiB image for the 28C256 EEPROM.
times 8000h - ($ - $$) db 0FFh
