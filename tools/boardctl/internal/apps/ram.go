package apps

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/client"
	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/protocol"
)

const ramBankSize = 128 * 1024

type ramMap struct {
	mask, work, flags, testState byte
	total                        uint32
	conventional                 uint16
}

type ramTestStatus struct {
	state, mask, work, phase              byte
	current, ok, bad, total, first, units uint32
	expected, actual                      byte
}

func validRAMBanks(mask, work byte) bool {
	return mask != 0 && mask&^byte(0x1f) == 0 && work < 5 && mask&(1<<work) != 0
}

func decodeRAMMap(data []byte) (ramMap, error) {
	if len(data) != 11 || data[0] != 0 {
		return ramMap{}, errors.New("invalid RAM_INFO response")
	}
	m := ramMap{mask: data[1], work: data[2], flags: data[3], total: binary.LittleEndian.Uint32(data[4:]), conventional: binary.LittleEndian.Uint16(data[8:]), testState: data[10]}
	expected := uint16(0)
	for bank := byte(0); bank < 5 && m.mask&(1<<bank) != 0; bank++ {
		expected += 128
	}
	if !validRAMBanks(m.mask, m.work) || m.flags > 3 || m.testState > 3 || m.total != uint32(bits.OnesCount8(m.mask)*ramBankSize) || m.conventional != expected || (m.flags&1 != 0) != (m.testState == 1) {
		return ramMap{}, errors.New("invalid Rev.2 RAM map")
	}
	return m, nil
}

func checkRAMResponse(command string, frame protocol.Frame, err error) error {
	if responseErr := checkResponse(frame, err); responseErr != nil {
		if err == nil && len(frame.Data) == 1 && frame.Data[0] == 1 {
			return fmt.Errorf("%s is unsupported by this BIOS; update the board firmware: %w", command, responseErr)
		}
		return fmt.Errorf("%s: %w", command, responseErr)
	}
	return nil
}

func showRAM(c *client.Client, out io.Writer) error {
	frame, err := c.Transact(protocol.Frame{Command: protocol.RAMInfo})
	if err := checkRAMResponse("RAM_INFO", frame, err); err != nil {
		return err
	}
	m, err := decodeRAMMap(frame.Data)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "Bank  Physical range  POST         Role")
	for bank := byte(0); bank < 5; bank++ {
		status, role := "FAIL/ABSENT", ""
		if m.mask&(1<<bank) != 0 {
			status = "OK"
		}
		if bank == m.work {
			role = "BIOS"
		}
		start := uint32(bank) * ramBankSize
		fmt.Fprintf(out, "RAM%d  %05X-%05X     %-11s  %s\n", bank, start, start+ramBankSize-1, status, role)
	}
	fmt.Fprintf(out, "Detected RAM: %d KiB\n", m.total/1024)
	fmt.Fprintf(out, "Conventional RAM: %d KiB\n", m.conventional)
	fmt.Fprintf(out, "RAM test: %s\n", []string{"IDLE", "RUNNING", "COMPLETED", "CANCELLED"}[m.testState])
	return nil
}

func decodeRAMTestStatus(data []byte) (ramTestStatus, error) {
	if len(data) != 31 || data[0] != 0 {
		return ramTestStatus{}, errors.New("invalid RAM_TEST response")
	}
	s := ramTestStatus{state: data[1], mask: data[2], work: data[3], phase: data[4],
		current: binary.LittleEndian.Uint32(data[5:]), ok: binary.LittleEndian.Uint32(data[9:]),
		bad: binary.LittleEndian.Uint32(data[13:]), total: binary.LittleEndian.Uint32(data[17:]),
		first: binary.LittleEndian.Uint32(data[21:]), expected: data[25], actual: data[26], units: binary.LittleEndian.Uint32(data[27:])}
	if !validRAMBanks(s.mask, s.work) || s.state > 3 || s.phase > 10 ||
		s.total != uint32(bits.OnesCount8(s.mask)*ramBankSize*5) || uint64(s.ok)+uint64(s.bad) > uint64(s.total) ||
		s.units > s.total/8 || s.current >= 0xa0000 || s.current&15 != 0 ||
		(s.units != 0 && s.mask&(1<<(s.current/ramBankSize)) == 0) ||
		(s.bad != 0 && (s.first >= 0xa0000 || s.mask&(1<<(s.first/ramBankSize)) == 0)) ||
		(s.state == 0 && (s.phase != 0 || s.units != 0 || s.ok != 0 || s.bad != 0)) ||
		(s.state == 1 && s.phase >= 10) ||
		(s.state == 2 && (s.phase != 10 || s.units != s.total/8 || uint64(s.ok)+uint64(s.bad) != uint64(s.total))) {
		return ramTestStatus{}, errors.New("invalid Rev.2 RAM diagnostic status")
	}
	return s, nil
}

// Internal protocol operations: the CLI exposes only the combined ram-test.
func exchangeRAMTest(ctx context.Context, c *client.Client, action byte, attempts int) (ramTestStatus, error) {
	if action > 2 {
		return ramTestStatus{}, errors.New("invalid RAM test action")
	}
	data := []byte{action}
	if action == 1 {
		frame, err := c.TransactAttemptsContext(ctx, protocol.Frame{Command: protocol.Ping}, attempts)
		if err := checkStatusReply(frame, err); err != nil {
			return ramTestStatus{}, err
		}
		data = append(data, protocol.RAMTestConfirm)
	}
	frame, err := c.TransactAttemptsContext(ctx, protocol.Frame{Command: protocol.RAMTest, Data: data}, attempts)
	if err := checkRAMResponse("RAM_TEST", frame, err); err != nil {
		return ramTestStatus{}, err
	}
	return decodeRAMTestStatus(frame.Data)
}

func printRAMTestStatus(s ramTestStatus, out io.Writer) error {
	state := []string{"IDLE", "RUNNING", "PASSED", "CANCELLED"}[s.state]
	if s.state == 2 && s.bad != 0 {
		state = "FAILED"
	}
	progress := uint64(s.units) * 1000 / uint64(s.total/8)
	fmt.Fprintf(out, "RAM TEST: %s %d.%d%%\n", state, progress/10, progress%10)
	if s.state != 0 {
		phase := min(int(s.phase)+1, 10)
		fmt.Fprintf(out, "Phase: %d/10; checked: OK=%d BAD=%d / %d\n", phase, s.ok, s.bad, s.total)
		if s.units != 0 {
			fmt.Fprintf(out, "Last block: RAM%d %05X-%05X\n", s.current/ramBankSize, s.current, s.current+15)
		}
	}
	if s.bad != 0 {
		fmt.Fprintf(out, "First error: RAM%d %05X expected=%02X actual=%02X\n", s.first/ramBankSize, s.first, s.expected, s.actual)
	}
	if s.state == 2 && s.bad != 0 {
		return errors.New("RAM test found memory errors")
	}
	return nil
}

// Confirmation happens before opening UART. Empty/negative input does nothing.
func confirmRAMTest(in io.Reader, out io.Writer) (bool, error) {
	fmt.Fprint(out, "Full RAM test erases RAM contents. Start? [y/N]: ")
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer != "y" && answer != "yes" {
		fmt.Fprintln(out, "RAM test not started.")
		return false, nil
	}
	return true, nil
}

func runRAMTest(c *client.Client, out io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	return watchRAMTest(ctx, c, out, 250*time.Millisecond)
}

func watchRAMTest(ctx context.Context, c *client.Client, out io.Writer, interval time.Duration) (resultErr error) {
	needsCancel := false
	progressShown := false
	// A cancelled/lost START reply may still mean the board accepted START.
	// Always use a fresh bounded context for cancellation, never the stopped one.
	defer func() {
		if !needsCancel {
			return
		}
		if progressShown {
			fmt.Fprintln(out)
		}
		cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		final, err := exchangeRAMTest(cleanup, c, 2, 2)
		if err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("cancellation not acknowledged; BIOS will stop the test when its heartbeat budget expires: %w", err))
			return
		}
		finalErr := printRAMTestStatus(final, out)
		if errors.Is(resultErr, context.Canceled) {
			resultErr = finalErr
		} else {
			resultErr = errors.Join(resultErr, finalErr)
		}
	}()

	frame, err := c.TransactAttemptsContext(ctx, protocol.Frame{Command: protocol.RAMInfo}, 3)
	if err := checkRAMResponse("RAM_INFO", frame, err); err != nil {
		return err
	}
	info, err := decodeRAMMap(frame.Data)
	if err != nil {
		return err
	}
	if info.flags&protocol.RAMFlagHeartbeat == 0 {
		return errors.New("this BIOS has no RAM-test heartbeat watchdog; update the board firmware before running ram-test")
	}
	status, err := exchangeRAMTest(ctx, c, 0, 3)
	if err != nil {
		return err
	}
	if status.state == 1 {
		needsCancel = true
		fmt.Fprintln(out, "Following the running RAM test. Ctrl+C cancels.")
	} else {
		if status.state != 0 {
			fmt.Fprintln(out, "Previous RAM test:")
			_ = printRAMTestStatus(status, out)
		}
		needsCancel = true
		status, err = exchangeRAMTest(ctx, c, 1, 3)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, "RAM test started. Ctrl+C cancels.")
	}
	for {
		if status.state != 1 {
			if status.state == 0 {
				return errors.New("board returned to idle before the RAM test completed")
			}
			needsCancel = false
			if progressShown {
				fmt.Fprintln(out)
			}
			return printRAMTestStatus(status, out)
		}
		percent := uint64(status.units) * 1000 / uint64(status.total/8)
		line := fmt.Sprintf("RAM TEST: RUNNING %d.%d%%  phase %d/10  OK=%d BAD=%d", percent/10, percent%10, status.phase+1, status.ok, status.bad)
		fmt.Fprintf(out, "\r%-100s", line)
		progressShown = true
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		status, err = exchangeRAMTest(ctx, c, 0, 3)
		if err != nil {
			return err
		}
	}
}
