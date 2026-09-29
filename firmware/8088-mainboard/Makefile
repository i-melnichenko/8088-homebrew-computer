NASM ?= nasm
GO ?= go
EMULATOR_IMAGE ?= 8088-mainboard-emulator
BIOS_VERSION ?= dev

BUILD_DIR := build

.PHONY: all clean bios citylights terminal serial serial-ports keyboard test FORCE

all: bios

bios: $(BUILD_DIR)/bios.bin

$(BUILD_DIR):
	mkdir -p $@

$(BUILD_DIR)/terminal.bin: programs/terminal/main.asm lib/bios_api.inc | $(BUILD_DIR)
	$(NASM) -f bin -I lib/ -o $@ $<

$(BUILD_DIR)/citylights.bin: programs/citylights/main.asm lib/bios_api.inc | $(BUILD_DIR)
	$(NASM) -f bin -I lib/ -o $@ $<

citylights: $(BUILD_DIR)/citylights.bin

terminal: $(BUILD_DIR)/terminal.bin

BIOS_INCLUDES := $(wildcard bios/*.inc bios/uart/*.inc bios/utils/*.inc)
# Rebuild this small ROM on each invocation so a changed CI version cannot
# leave an old image behind. Examples retain normal incremental builds.
FORCE:

$(BUILD_DIR)/flasher.bin: programs/flasher/main.asm programs/flasher/read.inc programs/flasher/write.inc programs/flasher/range.inc programs/flasher/screen.inc bios/state.inc bios/uart/constants.inc bios/uart/framing.inc lib/memory.inc lib/uart16550.inc | $(BUILD_DIR)
	$(NASM) -f bin -I lib/ -I bios/ -I programs/flasher/ -o $@ $<

$(BUILD_DIR)/bios.bin: bios/main.asm $(BIOS_INCLUDES) $(BUILD_DIR)/flasher.bin lib/lcd.inc lib/memory.inc lib/uart16550.inc lib/bios_api.inc scripts/rom-size.awk FORCE | $(BUILD_DIR)
	$(NASM) -f bin -I lib/ -I bios/ -D 'BIOS_VERSION="$(BIOS_VERSION)"' -l $(BUILD_DIR)/bios.lst -o $@ $<
	@awk -v image_size="$$(wc -c < "$@")" -f scripts/rom-size.awk $(BUILD_DIR)/bios.lst

# Usage: make serial SERIAL_PORT=/dev/cu.usbserial-XXXX
# boardctl opens the device directly and does not create cu lock files.
serial:
ifndef SERIAL_PORT
	$(error Set SERIAL_PORT, e.g. make serial SERIAL_PORT=/dev/cu.usbserial-XXXX)
endif
	$(GO) -C ../../tools/boardctl run ./cmd -port $(SERIAL_PORT) console

serial-ports:
	@ls -1 /dev/cu.* 2>/dev/null || true

keyboard:
ifndef SERIAL_PORT
	$(error Set SERIAL_PORT, e.g. make keyboard SERIAL_PORT=/dev/cu.usbserial-XXXX)
endif
	$(GO) -C ../../tools/boardctl run ./cmd -port $(SERIAL_PORT) keyboard

test:
	docker build --build-arg BIOS_VERSION=$(BIOS_VERSION) -f tests/Dockerfile -t $(EMULATOR_IMAGE) .
	docker run --rm $(EMULATOR_IMAGE)

clean:
	rm -rf $(BUILD_DIR)
