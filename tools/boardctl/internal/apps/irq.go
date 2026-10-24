package apps

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/client"
	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/protocol"
)

func runIRQTest(c *client.Client, out io.Writer) error {
	if err := checkStatusReply(c.Transact(protocol.Frame{Command: protocol.Ping})); err != nil {
		return fmt.Errorf("PING: %w", err)
	}
	frame, err := c.TransactTimeout(protocol.Frame{Command: protocol.IRQTest}, 15*time.Second)
	if err != nil {
		return fmt.Errorf("IRQ_TEST: %w", err)
	}
	if len(frame.Data) == 1 {
		switch frame.Data[0] {
		case 1:
			return errors.New("IRQ_TEST is unsupported; install BIOS with IRQ_TEST support")
		case protocol.StatusPICMissing:
			return errors.New("IRQ_TEST: PIC 8259A not detected; install/check the chip and reset")
		case protocol.StatusPITMissing:
			return errors.New("IRQ_TEST: PIT 8254 not detected; install/check the chip and reset")
		case protocol.StatusRAM0Required:
			return errors.New("IRQ_TEST requires working RAM0 for interrupt vectors")
		case protocol.StatusPITNotCounting:
			return errors.New("IRQ_TEST: PIT counter was not running at POST; check CLK/GATE and reset")
		}
	}
	if err := checkResponse(frame, nil); err != nil {
		return fmt.Errorf("IRQ_TEST: %w", err)
	}
	if len(frame.Data) != 5 || frame.Data[1] > 1 || frame.Data[4] != 8 {
		return errors.New("invalid IRQ_TEST response")
	}
	received := binary.LittleEndian.Uint16(frame.Data[2:4])
	if (frame.Data[1] == 0) != (received >= uint16(frame.Data[4])) {
		return errors.New("inconsistent IRQ_TEST result")
	}
	fmt.Fprintf(out, "IRQ path:      PIT channel 0 -> PIC IRQ0 -> CPU\nTimer:         18.2 Hz (nominal)\nInterrupts:    %d / %d\n", received, frame.Data[4])
	if frame.Data[1] != 0 {
		fmt.Fprintln(out, "Result:        TIMEOUT\nAfter test:    BIOS runtime restored")
		return errors.New("IRQ0 test timed out; check PIT OUT0, PIC IRQ0/INTR and CPU INTA wiring")
	}
	fmt.Fprintln(out, "Result:        PASS\nAfter test:    BIOS runtime restored")
	return nil
}
