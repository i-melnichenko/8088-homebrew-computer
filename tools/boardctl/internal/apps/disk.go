package apps

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"

	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/client"
	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/protocol"
)

const diskImageSize = 2880 * 512

// Keep an immutable snapshot: neither DOS nor retries can modify the host file.
func prepareDisk(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read disk image: %w", err)
	}
	if len(data) != diskImageSize {
		return nil, errors.New("disk image must be a raw 1.44-MiB floppy (1474560 bytes)")
	}
	return data, nil
}

func serviceDisk(ctx context.Context, c *client.Client, image []byte) error {
	if len(image) != diskImageSize {
		return errors.New("invalid disk image size")
	}
	f, err := c.TransactAttemptsContext(ctx, protocol.Frame{Command: protocol.DiskPoll}, 3)
	if err := checkResponse(f, err); err != nil {
		return err
	}
	if len(f.Data) != 8 || f.Data[1] > 1 {
		return errors.New("invalid disk poll response")
	}
	if f.Data[1] == 0 {
		return nil
	}
	id := binary.LittleEndian.Uint16(f.Data[2:4])
	lba := int(binary.LittleEndian.Uint16(f.Data[4:6]))
	offset := int(binary.LittleEndian.Uint16(f.Data[6:8]))
	if lba >= 2880 || offset >= 512 {
		return errors.New("invalid disk sector request")
	}
	for offset < 512 {
		n := min(120, 512-offset)
		data := make([]byte, 5+n)
		binary.LittleEndian.PutUint16(data, id)
		binary.LittleEndian.PutUint16(data[2:], uint16(offset))
		copy(data[5:], image[lba*512+offset:lba*512+offset+n])
		f, err = c.TransactAttemptsContext(ctx, protocol.Frame{Command: protocol.DiskData, Data: data}, 3)
		if err := checkStatusReply(f, err); err != nil {
			return err
		}
		offset += n
	}
	return nil
}
