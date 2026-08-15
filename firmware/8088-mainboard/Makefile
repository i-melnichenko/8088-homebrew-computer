NASM ?= nasm

.PHONY: all clean demo

all: bios.bin

bios.bin: bios.asm ram-program.bin
	$(NASM) -f bin -o $@ bios.asm

ram-program.bin: ram-program.asm
	$(NASM) -f bin -o $@ $<

demo.bin: demo.asm
	$(NASM) -f bin -o $@ $<

demo: demo.bin

clean:
	rm -f bios.bin ram-program.bin demo.bin
