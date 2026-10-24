; PC/XT boot services. All persistent fields are in the reserved BIOS bank,
; never relative to a caller's stack. UART disk is a read-only 1.44-MiB floppy.
PC_MOUNTED equ 800Fh
PC_MODE equ 8010h
PC_PENDING equ 8011h
PC_REQUEST equ 8012h
PC_LBA equ 8014h
PC_RECEIVED equ 8016h
PC_ERROR equ 8018h
PC_CACHE_VALID equ 8020h
PC_CACHE_LENGTH equ 8021h
PC_BOOT_SEQUENCE equ 8023h
PC_BOOT_VALID equ 8024h
PC_CACHE_DATA equ 8200h             ; exact last DISK_DATA payload, <=125 bytes
PC_SECTOR equ 8300h


%include "pc/time.inc"
%include "pc/console.inc"
%include "pc/disk.inc"
%include "pc/disk_transport.inc"
%include "pc/boot.inc"
