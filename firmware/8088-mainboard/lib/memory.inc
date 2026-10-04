; Rev2 has distinct RAM at 00000h..9FFFFh. Keep monitor state and ORG-0
; program allocations in the first 64 KiB until the registry supports u32.
RAM_END         equ 0A0000h
RAM_KIB         equ 640

RAM_SEGMENT      equ 0850h
BIOS_STATE_BASE  equ RAM_SEGMENT * 16
BIOS_STATE_SIZE  equ 0200h
PROGRAM_OFFSET  equ 0300h
PROGRAM_BASE    equ BIOS_STATE_BASE + PROGRAM_OFFSET
SRAM_STACK_TOP   equ 0FFFEh
SRAM_STACK_BOTTOM equ 0FC00h
PROGRAM_STACK_TOP equ 0FDFEh       ; lower 512 bytes of stack reservation

; 0000..03FF IVT, 0400..04FF BDA; 8000..84FF is separate RAM.
; 8500..86FF monitor state; 8700..87FF API state; 8800..FBFF programs.
; FC00h..FDFFh is the shared program stack, FE00h..FFFFh the BIOS stack.
MAX_PROGRAM_SIZE equ SRAM_STACK_BOTTOM - PROGRAM_BASE

ROM_SEGMENT equ 0F800h
