package apps

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/client"
	"github.com/i-melnichenko/8088-homebrew-computer/tools/boardctl/internal/protocol"
)

func TestProgramList(t *testing.T) {
	data := make([]byte, 25)
	data[1], data[2] = 1, 1
	binary.LittleEndian.PutUint32(data[3:], 0x8830)
	binary.LittleEndian.PutUint16(data[7:], 17)
	copy(data[9:], "Penguins")
	programs, err := decodePrograms(data)
	if err != nil || len(programs) != 1 || programs[0].Name != "Penguins" || programs[0].Address != 0x8830 {
		t.Fatalf("decode: %v, %v", programs, err)
	}
	for _, index := range []int{0, 1, 2, 3, 8, 9} {
		bad := append([]byte(nil), data...)
		bad[index] = 255
		if _, err := decodePrograms(bad); err == nil {
			t.Errorf("accepted invalid list at %d", index)
		}
	}
	if _, err := decodePrograms([]byte{0, 0}); err != nil {
		t.Fatal(err)
	}
	if name := defaultProgramName("/tmp/my demo.bin"); name != "my demo" {
		t.Fatal(name)
	}
}

func TestUploadTransactionAndAbort(t *testing.T) {
	for _, scenario := range []string{"commit", "write_error", "read_error", "short_read", "hash_mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			host, board := net.Pipe()
			defer host.Close()
			defer board.Close()
			_ = host.SetDeadline(time.Now().Add(5 * time.Second))
			_ = board.SetDeadline(time.Now().Add(5 * time.Second))
			image := make([]byte, 257)
			for i := range image {
				image[i] = byte(i)
			}
			done := make(chan error, 1)
			go func() {
				reader := bufio.NewReader(board)
				offset, readOffset, stage := 0, 0, 0
				for {
					_, err := reader.ReadBytes(0)
					if err != nil {
						done <- err
						return
					}
					packet, err := reader.ReadBytes(0)
					if err != nil {
						done <- err
						return
					}
					f, err := protocol.Decode(append([]byte{0}, packet...))
					if err != nil {
						done <- err
						return
					}
					status := []byte{0}
					switch stage {
					case 0:
						if f.Command != protocol.ProgramUploadBegin || len(f.Data) != 10 || binary.LittleEndian.Uint32(f.Data) != uint32(len(image)) || binary.LittleEndian.Uint16(f.Data[4:]) != protocol.CRC16(image) || string(f.Data[6:]) != "Demo" {
							done <- errors.New("invalid BEGIN")
							return
						}
						status = []byte{0, 2, 0x30, 0x88, 0, 0}
						stage = 1
					case 1:
						n := min(protocol.MaxUploadDataLength, len(image)-offset)
						if f.Command != protocol.ProgramUploadData || len(f.Data) != n || !bytes.Equal(f.Data, image[offset:offset+n]) {
							done <- errors.New("invalid WRITE")
							return
						}
						offset += len(f.Data)
						if scenario == "write_error" {
							status = []byte{3}
							stage = 4
						} else if offset == len(image) {
							stage = 2
						}
					case 2:
						n := min(protocol.MaxMemoryReadLength, len(image)-readOffset)
						if f.Command != protocol.MemoryRead || len(f.Data) != 6 || binary.LittleEndian.Uint32(f.Data) != 0x8830+uint32(readOffset) || int(binary.LittleEndian.Uint16(f.Data[4:])) != n {
							done <- errors.New("invalid verification READ or premature COMMIT")
							return
						}
						status = append(status, image[readOffset:readOffset+n]...)
						readOffset += n
						if scenario == "read_error" {
							status, stage = []byte{14}, 4
						} else if scenario == "short_read" {
							status, stage = status[:len(status)-1], 4
						} else if readOffset == len(image) {
							stage = 3
							if scenario == "hash_mismatch" {
								status[len(status)-1] ^= 1
								stage = 4
							}
						}
					case 3, 4:
						expected := protocol.ProgramUploadCommit
						if stage == 4 {
							expected = protocol.ProgramUploadAbort
						}
						if f.Command != expected || !bytes.Equal(f.Data, []byte{2}) {
							done <- errors.New("invalid COMMIT/ABORT")
							return
						}
						stage = 5
					}
					reply, err := protocol.Encode(protocol.Frame{Command: f.Command | 0x80, Sequence: f.Sequence, Data: status})
					if err != nil {
						done <- err
						return
					}
					if _, err := board.Write(reply); err != nil {
						done <- err
						return
					}
					if stage == 5 {
						done <- nil
						return
					}
				}
			}()
			progress := 0
			p, err := uploadData(client.New(host, nil), "Demo", image, func(n, total int) {
				if n <= progress || total != len(image) {
					t.Errorf("invalid progress %d/%d", n, total)
				}
				progress = n
			})
			if (err != nil) != (scenario != "commit") {
				t.Fatalf("upload error: %v", err)
			}
			if scenario == "commit" && (p.ID != 2 || p.Address != 0x8830 || progress != len(image)) {
				t.Fatalf("allocation/progress: %+v %d", p, progress)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if scenario == "hash_mismatch" {
				changed := append([]byte(nil), image...)
				changed[len(changed)-1] ^= 1
				want := fmt.Sprintf("RAM SHA-256 mismatch: PC=%x RAM=%x", sha256.Sum256(image), sha256.Sum256(changed))
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("hash diagnostic: %v, want %q", err, want)
				}
			}
		})
	}
}

func TestProgramListAllowsReusedHolesButRejectsOverlap(t *testing.T) {
	data := make([]byte, 2+2*23)
	data[1] = 2
	for i, address := range []uint32{0x8840, 0x8800} {
		record := data[2+i*23 : 2+(i+1)*23]
		record[0] = byte(i + 1)
		binary.LittleEndian.PutUint32(record[1:], address)
		binary.LittleEndian.PutUint16(record[5:], 17)
		copy(record[7:], fmt.Sprintf("Program %d", i+1))
	}
	if _, err := decodePrograms(data); err != nil {
		t.Fatalf("valid first-fit registry: %v", err)
	}
	binary.LittleEndian.PutUint32(data[2+23+1:], 0x8850)
	if _, err := decodePrograms(data); err == nil {
		t.Fatal("overlapping registry accepted")
	}
}

type registrySerial struct {
	requests []protocol.Frame
	packet   []byte
	response []byte
	dropACK  bool
	dropped  bool
}

func (s *registrySerial) Write(p []byte) (int, error) {
	f, err := protocol.Decode(p)
	if err != nil {
		return 0, err
	}
	s.requests = append(s.requests, f)
	if s.dropACK && !s.dropped && f.Command == protocol.ProgramDelete {
		s.dropped = true
		return len(p), nil
	}
	data := []byte{0}
	if s.response != nil && f.Command != protocol.Ping {
		data = s.response
	}
	s.packet, err = protocol.Encode(protocol.Frame{Command: f.Command | 0x80, Sequence: f.Sequence, Data: data})
	return len(p), err
}

func (s *registrySerial) Read(p []byte) (int, error) {
	if len(s.packet) == 0 {
		return 0, errors.New("simulated lost registry ACK")
	}
	n := copy(p, s.packet)
	s.packet = s.packet[n:]
	return n, nil
}

func (s *registrySerial) Close() error { return nil }

func TestProgramRegistryCommandsAndDeleteRetry(t *testing.T) {
	s := &registrySerial{dropACK: true}
	if err := deleteProgram(client.New(s, nil), 2); err != nil {
		t.Fatal(err)
	}
	if len(s.requests) != 3 || s.requests[0].Command != protocol.Ping ||
		s.requests[1].Command != protocol.ProgramDelete || !bytes.Equal(s.requests[1].Data, []byte{2}) ||
		s.requests[1].Sequence != s.requests[2].Sequence || !bytes.Equal(s.requests[1].Data, s.requests[2].Data) {
		t.Fatalf("delete retry: %+v", s.requests)
	}
	s = &registrySerial{}
	if err := renameProgram(client.New(s, nil), 3, "My program"); err != nil {
		t.Fatal(err)
	}
	if len(s.requests) != 1 || s.requests[0].Command != protocol.ProgramRename || !bytes.Equal(s.requests[0].Data, []byte("\x03My program")) {
		t.Fatalf("rename payload: %+v", s.requests)
	}
	for _, response := range [][]byte{{8}, {10}, {11}, {15}, {16}, {}, {0, 0}} {
		for _, deleting := range []bool{false, true} {
			s := &registrySerial{response: response}
			c := client.New(s, nil)
			var err error
			if deleting {
				err = deleteProgram(c, 1)
			} else {
				err = renameProgram(c, 1, "Name")
			}
			if err == nil {
				t.Fatalf("accepted registry response %x", response)
			}
		}
	}
	for _, args := range [][]string{{"delete", "2"}, {"rename", "3", "My program"}} {
		s := &registrySerial{}
		if err := runWithSerial(args, func(string) (io.ReadWriteCloser, error) { return s, nil }); err != nil {
			t.Fatal(err)
		}
	}
}
