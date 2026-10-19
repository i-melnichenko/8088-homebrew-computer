BITS 16
CPU 8086
ORG 0

; A stationary city alternates between night and day. Clouds drift across
; the sky; a two-column wipe replaces each scene from the right.

%include "bios_api.inc"

SCREEN_CELLS equ BIOS_LCD_COLUMNS * BIOS_LCD_ROWS
; Current BIOS has no timer service. One polling delay is roughly 1/8 second at 4.77 MHz.
TICKS_PER_SECOND equ 8
INTRO_TICKS      equ 2 * TICKS_PER_SECOND
SCENE_TICKS      equ 5 * TICKS_PER_SECOND
HINT_ON_TICKS    equ 3 * TICKS_PER_SECOND
HINT_OFF_TICKS   equ 10 * TICKS_PER_SECOND
WIPE_COLUMNS     equ 2
MOON_GLYPH   equ 0
SUN_GLYPH    equ 1
STAR_GLYPH   equ 2
CLOUD_GLYPH  equ 3
ROOF_GLYPH   equ 4
DARK_WALL    equ 0FFh            ; Built-in solid block, freeing a custom glyph.
LOW_BUILDING equ 5
LIT_WALL     equ 6
WALKER_GLYPH equ 7

start:
    cli
    cld
    mov ax, cs
    mov ds, ax

    bios_lcd_clear
    bios_lcd_glyph MOON_GLYPH, moon_glyph
    bios_lcd_glyph SUN_GLYPH, sun_glyph
    bios_lcd_glyph STAR_GLYPH, star_glyph
    bios_lcd_glyph CLOUD_GLYPH, cloud_glyph_a
    bios_lcd_glyph ROOF_GLYPH, roof_glyph
    bios_lcd_glyph LOW_BUILDING, low_building_glyph
    bios_lcd_glyph LIT_WALL, lit_wall_glyph
    bios_lcd_glyph WALKER_GLYPH, walker_glyph_a

    mov di, old_frame
    call fill_spaces
    call draw_intro
    call present_frame
    mov byte [intro_ticks_left], INTRO_TICKS
.intro:
    call delay_tick
    jc .done
    dec byte [intro_ticks_left]
    jnz .intro

    mov byte [current_scene], 0     ; 0=night, 1=day
    mov byte [scene_tick], 0
    mov byte [cloud_variant], 0
    mov byte [cloud_shift], 0
    mov byte [hold_ticks_left], SCENE_TICKS
    mov byte [wipe_boundary], BIOS_LCD_COLUMNS
    mov byte [hint_ticks_left], HINT_ON_TICKS
    mov byte [hint_visible], 1
.frame:
    call build_night
    call build_day
    call compose_frame
    call present_frame
    call delay_tick
    jc .done
    inc byte [scene_tick]
    call update_cloud_shape
    call update_walker_shape
    call update_hint
    call update_scene
    jmp .frame

.done:
    retf

draw_intro:
    mov di, next_frame
    call copy_city
    mov si, intro_title
    mov di, next_frame + BIOS_LCD_COLUMNS + 4
    mov cx, 11
.letter:
    mov al, [si]
    mov [di], al
    inc si
    inc di
    loop .letter
    ret

; DI=destination. Copy the fixed skyline to a scene buffer.
copy_city:
    mov si, city_frame
    mov cx, SCREEN_CELLS
.cell:
    mov al, [si]
    mov [di], al
    inc si
    inc di
    loop .cell
    ret

build_night:
    mov di, night_frame
    call copy_city
    mov byte [night_frame + 17], MOON_GLYPH

    ; Keep four stars in place: two shine while the others remain dim dots.
    mov byte [night_frame + 3], '.'
    mov byte [night_frame + 11], '.'
    mov byte [night_frame + BIOS_LCD_COLUMNS + 6], '.'
    mov byte [night_frame + BIOS_LCD_COLUMNS + 15], '.'
    ; Switch the bright pair every two polling ticks.
    mov al, [scene_tick]
    shr al, 1
    and al, 3
    xor bx, bx
    mov bl, al
    mov dl, [star_one_cells + bx]
    xor dh, dh
    mov si, dx
    mov byte [night_frame + si], STAR_GLYPH
    mov dl, [star_two_cells + bx]
    mov si, dx
    mov byte [night_frame + si], STAR_GLYPH

    mov byte [night_frame + 3 * BIOS_LCD_COLUMNS + 1], LIT_WALL
    mov byte [night_frame + 3 * BIOS_LCD_COLUMNS + 8], LIT_WALL
    ; One more window changes every two seconds.
    mov al, [scene_tick]
    mov cl, 4
    shr al, cl
    and al, 7
    mov bl, al
    mov dl, [window_columns + bx]
    xor dh, dh
    mov si, dx
    mov byte [night_frame + 3 * BIOS_LCD_COLUMNS + si], LIT_WALL
    ret

build_day:
    mov di, day_frame
    call copy_city
    mov dh, 0
    mov dl, 4
    call draw_cloud
    mov dh, 1
    mov dl, 8
    call draw_cloud
    mov dl, 14
    call draw_cloud
    ; Keep the sun fixed even when a cloud crosses its cell.
    mov byte [day_frame + 17], SUN_GLYPH
    ret

; DH=row, DL=starting column. Wrap the drifting cloud at the left edge.
draw_cloud:
    sub dl, [cloud_shift]
    jnc .column
    add dl, BIOS_LCD_COLUMNS
.column:
    xor bx, bx
    mov bl, dl
    cmp dh, 0
    je .store
    add bx, BIOS_LCD_COLUMNS
.store:
    mov byte [day_frame + bx], CLOUD_GLYPH
    ret

compose_frame:
    ; Columns to the right of the boundary belong to the incoming scene.
    mov si, night_frame
    mov bx, day_frame
    cmp byte [current_scene], 0
    je .pointers
    xchg si, bx
.pointers:
    mov di, next_frame
    xor dx, dx                   ; DL=column within each LCD row.
    mov cx, SCREEN_CELLS
.cell:
    cmp dl, [wipe_boundary]
    jb .old_scene
    mov al, [bx]
    jmp short .write
.old_scene:
    mov al, [si]
.write:
    mov [di], al
    inc si
    inc bx
    inc di
    inc dl
    cmp dl, BIOS_LCD_COLUMNS
    jb .next
    xor dl, dl
.next:
    loop .cell

    call draw_walker
    cmp byte [hint_visible], 0
    je .done
    mov si, hint_text
    mov di, next_frame + 3 * BIOS_LCD_COLUMNS + 12
    mov cx, 8
.hint:
    mov al, [si]
    mov [di], al
    inc si
    inc di
    loop .hint
.done:
    ret

; One pedestrian paces between the two left-hand buildings.
draw_walker:
    mov al, [scene_tick]
    mov cl, 2
    shr al, cl
    and al, 1
    add al, 4                    ; Alternate between columns 4 and 5.
    xor ah, ah
    mov si, ax
    mov byte [next_frame + 3 * BIOS_LCD_COLUMNS + si], WALKER_GLYPH
    ret

update_scene:
    cmp byte [wipe_boundary], BIOS_LCD_COLUMNS
    jne .wiping
    dec byte [hold_ticks_left]
    jnz .done
    mov byte [wipe_boundary], BIOS_LCD_COLUMNS - WIPE_COLUMNS
    ret
.wiping:
    cmp byte [wipe_boundary], 0
    je .finish
    sub byte [wipe_boundary], WIPE_COLUMNS
    ret
.finish:
    xor byte [current_scene], 1
    mov byte [wipe_boundary], BIOS_LCD_COLUMNS
    mov byte [hold_ticks_left], SCENE_TICKS
.done:
    ret

update_hint:
    dec byte [hint_ticks_left]
    jnz .done
    xor byte [hint_visible], 1
    cmp byte [hint_visible], 0
    je .hide
    mov byte [hint_ticks_left], HINT_ON_TICKS
    ret
.hide:
    mov byte [hint_ticks_left], HINT_OFF_TICKS
.done:
    ret

update_cloud_shape:
    mov al, [scene_tick]
    and al, TICKS_PER_SECOND - 1
    jnz .done
    inc byte [cloud_shift]
    cmp byte [cloud_shift], BIOS_LCD_COLUMNS
    jb .shifted
    mov byte [cloud_shift], 0
.shifted:
    xor byte [cloud_variant], 1
    cmp byte [cloud_variant], 0
    je .first
    bios_lcd_glyph CLOUD_GLYPH, cloud_glyph_b
    ret
.first:
    bios_lcd_glyph CLOUD_GLYPH, cloud_glyph_a
.done:
    ret

update_walker_shape:
    mov al, [scene_tick]
    and al, 3
    jnz .done
    test byte [scene_tick], 4
    jz .first
    bios_lcd_glyph WALKER_GLYPH, walker_glyph_b
    ret
.first:
    bios_lcd_glyph WALKER_GLYPH, walker_glyph_a
.done:
    ret

; DI=destination. Preserve the caller's other registers except AX/CX/DI.
fill_spaces:
    mov cx, SCREEN_CELLS
    mov al, ' '
.cell:
    mov [di], al
    inc di
    loop .cell
    ret

; Write only LCD cells that changed. INT 60h/AH=04h preserves SI, DI and DX.
present_frame:
    mov si, next_frame
    mov di, old_frame
    xor dx, dx
.cell:
    mov al, [si]
    cmp al, [di]
    je .next
    mov [di], al
    mov ah, 04h
    int BIOS_INT_BOARD
.next:
    inc si
    inc di
    inc dl
    cmp dl, BIOS_LCD_COLUMNS
    jb .cell
    xor dl, dl
    inc dh
    cmp dh, BIOS_LCD_ROWS
    jb .cell
    ret

delay_tick:
    ; Poll between short delay slices: BIOS does not enable keyboard IRQs.
    mov dx, 128
.chunk:
    mov cx, 256
.wait:
    loop .wait
    bios_key_peek
    jz .next
    bios_key_read
    cmp al, 1Bh
    je .cancel
.next:
    dec dx
    jnz .chunk
    clc
    ret
.cancel:
    stc
    ret

intro_ticks_left: db 0
current_scene:    db 0
scene_tick:       db 0
cloud_variant:    db 0
cloud_shift:      db 0
hold_ticks_left:  db 0
wipe_boundary:    db 0
hint_ticks_left:  db 0
hint_visible:     db 0
intro_title: db 'CITY LIGHTS'
hint_text:   db 'ESC:BACK'
star_one_cells: db 3, 11, 3, 11
star_two_cells: db 26, 35, 35, 26
window_columns: db 1, 2, 7, 8, 9, 14, 15, 16

; The upper two rows are sky; the lower two form a fixed skyline.
city_frame:
    times BIOS_LCD_COLUMNS db ' '
    times BIOS_LCD_COLUMNS db ' '
    db ' ', ROOF_GLYPH, ROOF_GLYPH, ' ', ' ', ' ', ' '
    db ROOF_GLYPH, ROOF_GLYPH, ROOF_GLYPH, ' ', ' ', ' ', ' '
    db ROOF_GLYPH, ROOF_GLYPH, ROOF_GLYPH, ' ', ' ', ' '
    db ' ', DARK_WALL, DARK_WALL, ' ', ' ', ' ', ' '
    db DARK_WALL, DARK_WALL, DARK_WALL, ' ', LOW_BUILDING, LOW_BUILDING, ' '
    db DARK_WALL, DARK_WALL, DARK_WALL, ' ', LOW_BUILDING, LOW_BUILDING

old_frame:   times SCREEN_CELLS db ' '
next_frame:  times SCREEN_CELLS db ' '
night_frame: times SCREEN_CELLS db ' '
day_frame:   times SCREEN_CELLS db ' '

; Each glyph is one 5x8 LCD cell.
moon_glyph:
    db 00110b, 01100b, 11000b, 11000b, 11000b, 01100b, 00110b, 00000b
sun_glyph:
    db 00100b, 10101b, 01110b, 11111b, 01110b, 10101b, 00100b, 00000b
star_glyph:
    db 00100b, 00100b, 10101b, 01110b, 10101b, 00100b, 00100b, 00000b
cloud_glyph_a:
    db 00000b, 00110b, 01111b, 11111b, 11111b, 01110b, 00000b, 00000b
cloud_glyph_b:
    db 00000b, 01100b, 11110b, 11111b, 11111b, 00111b, 00000b, 00000b
roof_glyph:
    db 00000b, 00000b, 11111b, 11111b, 10101b, 11111b, 10101b, 11111b
low_building_glyph:
    db 00000b, 00000b, 00000b, 11111b, 11111b, 11111b, 11111b, 11111b
lit_wall_glyph:
    db 11111b, 11111b, 10001b, 11111b, 11111b, 10001b, 11111b, 11111b
walker_glyph_a:
    db 00000b, 00100b, 00100b, 01110b, 00100b, 01010b, 10001b, 00000b
walker_glyph_b:
    db 00000b, 00100b, 00100b, 01110b, 00100b, 01010b, 01010b, 00000b
