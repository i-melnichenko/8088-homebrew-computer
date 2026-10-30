NASM ?= nasm
GO ?= go
EMULATOR_IMAGE ?= 8088-mainboard-emulator
BIOS_VERSION ?= dev

BUILD_DIR := build

.PHONY: all clean bios os serial serial-ports keyboard test test-images FORCE

all: bios os

bios: $(BUILD_DIR)/bios.bin

$(BUILD_DIR):
	mkdir -p $@

BIOS_INCLUDES := $(wildcard bios/*.inc bios/uart/*.inc bios/utils/*.inc bios/pc/*.inc)
# Rebuild this small ROM on each invocation so a changed CI version cannot
# leave an old image behind. Examples retain normal incremental builds.
FORCE:

$(BUILD_DIR)/flasher.bin: bios/programs/flasher/main.asm bios/programs/flasher/read.inc bios/programs/flasher/write.inc bios/programs/flasher/range.inc bios/programs/flasher/screen.inc bios/state.inc bios/uart/constants.inc bios/uart/framing.inc lib/memory.inc lib/uart16550.inc FORCE | $(BUILD_DIR)
	$(NASM) -f bin -I lib/ -I bios/ -I bios/programs/flasher/ -o $@ $<

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

# One ROM image serves boards with and without the optional LCD module.
test-images:
	$(MAKE) bios os

test:
	docker build --build-arg BIOS_VERSION=$(BIOS_VERSION) -f tests/Dockerfile -t $(EMULATOR_IMAGE) .
	docker run --rm $(EMULATOR_IMAGE)

clean:
	rm -rf $(BUILD_DIR)

.PHONY: os
os: $(BUILD_DIR)/home8088.img

$(BUILD_DIR)/os-boot.bin: os/home8088/boot.asm | $(BUILD_DIR)
	$(NASM) -f bin -o $@ $<

$(BUILD_DIR)/os-kernel.bin: os/home8088/kernel.asm | $(BUILD_DIR)
	$(NASM) -f bin -l $(BUILD_DIR)/os-kernel.lst -o $@ $<

$(BUILD_DIR)/%.com: os/home8088/programs/%/main.asm lib/bios_api.inc | $(BUILD_DIR)
	$(NASM) -f bin -I lib/ -o $@ $<

$(BUILD_DIR)/home8088.img: $(BUILD_DIR)/os-boot.bin $(BUILD_DIR)/os-kernel.bin $(BUILD_DIR)/city.com $(BUILD_DIR)/timer.com $(BUILD_DIR)/terminal.com scripts/make-os-image.py
	python3 scripts/make-os-image.py $@ $(filter %.bin %.com,$^)

$(BUILD_DIR)/city.com: os/home8088/programs/citylights/main.asm lib/bios_api.inc | $(BUILD_DIR)
	$(NASM) -f bin -I lib/ -o $@ $<

# Explicit opt-in: fetch pinned Microsoft files, or supply a local checkout.
.PHONY: msdos
msdos: $(BUILD_DIR)/msdos-boot.bin $(BUILD_DIR)/msdos-io.bin $(BUILD_DIR)/doshello.com
	python3 scripts/make-msdos-image.py $(if $(MSDOS_SOURCE_DIR),--source "$(MSDOS_SOURCE_DIR)",)

.PHONY: msdos-zork
msdos-zork: $(BUILD_DIR)/msdos-boot.bin $(BUILD_DIR)/msdos-io.bin $(BUILD_DIR)/doshello.com
	python3 scripts/make-msdos-image.py --zork $(if $(MSDOS_SOURCE_DIR),--source "$(MSDOS_SOURCE_DIR)",)

$(BUILD_DIR)/msdos-boot.bin: os/msdos/boot.asm | $(BUILD_DIR)
	$(NASM) -f bin -o $@ $<

$(BUILD_DIR)/msdos-io.bin: os/msdos/io.asm | $(BUILD_DIR)
	$(NASM) -f bin -l $(BUILD_DIR)/msdos-io.lst -o $@ $<

$(BUILD_DIR)/doshello.com: os/msdos/programs/hello/main.asm | $(BUILD_DIR)
	$(NASM) -f bin -o $@ $<
