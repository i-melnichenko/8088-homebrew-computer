package apps

import (
	"fmt"
	"strconv"
	"strings"
)

// INFO is human-readable firmware text. Keep unfamiliar fields visible so
// older boardctl builds can still show information from newer BIOS versions.
func formatInfo(summary string) string {
	var out strings.Builder
	row := func(label, value string) {
		fmt.Fprintf(&out, "%-14s %s\n", label+":", value)
	}
	fields := strings.Split(summary, ";")
	pitDetected := false
	runtimeFlags := -1
	timerText := "IRQ0 / active"
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "protocol 2.0" {
			timerText = "IRQ0 / 18.2 Hz"
		}
		if strings.HasPrefix(field, "IRQ ") {
			if flags, err := strconv.Atoi(strings.TrimPrefix(field, "IRQ ")); err == nil && flags >= 0 && flags <= 3 {
				runtimeFlags = flags
			}
		}
		if field == "PIT 1" {
			pitDetected = true
		}
	}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		switch {
		case strings.HasPrefix(field, "BIOS "):
			row("BIOS", strings.TrimPrefix(field, "BIOS "))
		case field == "rev2":
			row("Board", "8088 mainboard / revision 2")
		case strings.HasPrefix(field, "ROM "):
			row("ROM base", "0x"+strings.TrimPrefix(field, "ROM "))
		case strings.HasPrefix(field, "SRAM "):
			row("SRAM", strings.TrimSuffix(strings.TrimPrefix(field, "SRAM "), "K")+" KiB")
		case strings.HasPrefix(field, "UART "):
			port, config, _ := strings.Cut(strings.TrimPrefix(field, "UART "), " ")
			row("UART", "0x"+port+" / "+config)
		case strings.HasPrefix(field, "protocol "):
			row("Protocol", strings.TrimPrefix(field, "protocol "))
		case strings.HasPrefix(field, "API "):
			row("BIOS API", strings.TrimPrefix(field, "API "))
		case strings.HasPrefix(field, "RAM mask "):
			maskText := strings.TrimPrefix(field, "RAM mask ")
			mask, err := strconv.ParseUint(maskText, 16, 8)
			if err != nil || mask & ^uint64(0x1f) != 0 {
				row("RAM mask", maskText)
				continue
			}
			var banks []string
			for bank := 0; bank < 5; bank++ {
				if mask&(1<<bank) != 0 {
					banks = append(banks, strconv.Itoa(bank))
				}
			}
			row("RAM banks", strings.Join(banks, ", ")+" (mask 0x"+maskText+")")
		case strings.HasPrefix(field, "work "):
			row("Working bank", strings.TrimPrefix(field, "work "))
		case field == "INT 1":
			row("Software INT", "available")
		case field == "INT 0":
			row("Software INT", "unavailable (RAM0 required)")
		case field == "PIC 1":
			if runtimeFlags > 0 {
				row("PIC 8259A", "detected (IRQs active)")
			} else {
				row("PIC 8259A", "detected (IRQs masked)")
			}
		case field == "PIC 0":
			row("PIC 8259A", "not detected")
		case field == "PIT 1":
			row("PIT 8254", "detected")
		case field == "PIT 0":
			row("PIT 8254", "not detected")
		case field == "CLK 1":
			row("PIT counter", "running (channel 0)")
		case field == "CLK 0":
			if pitDetected {
				row("PIT counter", "no counting observed (check CLK/GATE)")
			} else {
				row("PIT counter", "unavailable")
			}
		case strings.HasPrefix(field, "IRQ ") && runtimeFlags >= 0:
			if runtimeFlags&1 != 0 {
				row("System timer", timerText)
			} else {
				row("System timer", "software fallback")
			}
			if runtimeFlags&2 != 0 {
				row("UART receive", "IRQ4 / buffered")
			} else {
				row("UART receive", "polling")
			}
		case field != "":
			fmt.Fprintln(&out, field)
		}
	}
	return out.String()
}
