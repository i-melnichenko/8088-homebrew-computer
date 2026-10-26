package apps

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/client"
	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/protocol"
)

const diskImageSize = 2880 * 512

// Read-only mounts serve this snapshot. Writable mounts refresh it after locking.
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

type diskService struct {
	image  []byte
	writer sectorWriter
}

type sectorWriter interface {
	WriteAt([]byte, int64) (int, error)
	ReadAt([]byte, int64) (int, error)
	Sync() error
}

func openWritableDisk(path string, image []byte) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open writable disk: %w", err)
	}
	fail := func(err error) (*os.File, error) { f.Close(); return nil, err }
	stat, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	if !stat.Mode().IsRegular() || stat.Size() != diskImageSize {
		return fail(errors.New("writable disk must be a regular 1.44-MiB image"))
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fail(fmt.Errorf("lock writable disk: %w", err))
	}
	if _, err := f.ReadAt(image, 0); err != nil {
		return fail(err)
	}
	return f, nil
}

func serviceDisk(ctx context.Context, c *client.Client, image []byte, writers ...sectorWriter) error {
	if len(image) != diskImageSize {
		return errors.New("invalid disk image size")
	}
	f, err := c.TransactAttemptsContext(ctx, protocol.Frame{Command: protocol.DiskPoll}, 3)
	if err := checkResponse(f, err); err != nil {
		return err
	}
	if len(f.Data) != 8 || f.Data[1] > 2 {
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
	if f.Data[1] == 2 {
		if offset != 0 {
			return errors.New("invalid write sector offset")
		}
		var writer sectorWriter
		if len(writers) > 0 {
			writer = writers[0]
		}
		return receiveDiskWrite(ctx, c, image, writer, id, lba)
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

func receiveDiskWrite(ctx context.Context, c *client.Client, image []byte, writer sectorWriter, id uint16, lba int) error {
	status := byte(3) // A read-only host must never silently accept a write.
	var commitErr error
	if writer != nil {
		sector := make([]byte, 512)
		for offset := 0; offset < 512; {
			n := min(120, 512-offset)
			request := make([]byte, 4)
			binary.LittleEndian.PutUint16(request, id)
			binary.LittleEndian.PutUint16(request[2:], uint16(offset))
			reply, err := c.TransactAttemptsContext(ctx, protocol.Frame{Command: protocol.DiskWriteData, Data: request}, 3)
			if err := checkResponse(reply, err); err != nil {
				return err
			}
			if len(reply.Data) != 5+n || !bytes.Equal(reply.Data[1:5], request) {
				return errors.New("invalid disk write data response")
			}
			copy(sector[offset:], reply.Data[5:])
			offset += n
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := writer.WriteAt(sector, int64(lba*512))
		if err == nil && n != 512 {
			err = io.ErrShortWrite
		}
		if err == nil {
			err = writer.Sync()
		}
		if err == nil {
			verified := make([]byte, 512)
			n, err = writer.ReadAt(verified, int64(lba*512))
			if err == nil && (n != 512 || !bytes.Equal(verified, sector)) {
				err = errors.New("disk sector readback mismatch")
			}
		}
		commitErr = err
		status = 0
		if err != nil {
			status = 0x20
		} else {
			copy(image[lba*512:], sector)
		}
	}
	ack := []byte{byte(id), byte(id >> 8), status}
	err := checkStatusReply(c.TransactAttemptsContext(ctx, protocol.Frame{Command: protocol.DiskWriteAck, Data: ack}, 3))
	return errors.Join(commitErr, err)
}
