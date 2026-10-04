; Rev1 selects SRAM throughout 00000h..1FFFFh, mirrored every 32 KiB.
; Use its 08000h alias for the monitor, but reserve the low-memory structures.

RAM_SEGMENT      equ 0850h
SRAM_TEST_BASE   equ 08000h        ; one complete 32 KiB SRAM alias
SRAM_TEST_SIZE   equ 08000h
BIOS_STATE_BASE  equ RAM_SEGMENT * 16
BIOS_STATE_SIZE  equ 0200h
PROGRAM_OFFSET  equ 0300h
PROGRAM_BASE    equ BIOS_STATE_BASE + PROGRAM_OFFSET
SRAM_STACK_TOP   equ 0FFFEh
SRAM_STACK_BOTTOM equ 0FC00h
PROGRAM_STACK_TOP equ 0FDFEh       ; lower 512 bytes of stack reservation

; 0000..03FF IVT, 0400..04FF BDA (aliases 8000..84FF).
; 8500..86FF monitor state; 8700..87FF API state; 8800..FBFF programs.
; FC00h..FDFFh is the shared program stack, FE00h..FFFFh the BIOS stack.
MAX_PROGRAM_SIZE equ SRAM_STACK_BOTTOM - PROGRAM_BASE

ROM_SEGMENT equ 0F800h
