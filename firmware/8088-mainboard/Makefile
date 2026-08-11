NASM ?= nasm

.PHONY: all clean

all: demo.bin

demo.bin: demo.asm
	$(NASM) -f bin -o $@ $<

clean:
	rm -f demo.bin
