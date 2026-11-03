package audio

import (
	"errors"
	"io"
	"os"
	"time"

	"github.com/kazzmir/opus-go/ogg"
)

const (
	SampleRate = 48000
	Channels   = 2
	// FrameSize is samples per channel in a 20ms frame.
	FrameSize = 960
	// FrameDur is the only frame size Discord accepts for voice audio.
	FrameDur = 20 * time.Millisecond
	// MaxPacketSize is the largest opus packet Discord will take in one frame.
	MaxPacketSize = 1275
)

// PacketDuration returns the play duration of an opus packet from its TOC byte
// (RFC 6716 3.1). Discord requires 20ms frames, so passthrough playback has to
// check this instead of trusting the source.
func PacketDuration(packet []byte) time.Duration {
	if len(packet) == 0 {
		return 0
	}
	toc := packet[0]
	config := int(toc >> 3)
	code := toc & 0x03

	var frameMs float64
	switch {
	case config < 12: // SILK: 10/20/40/60ms
		frameMs = [...]float64{10, 20, 40, 60}[config&0x03]
	case config < 16: // Hybrid: 10/20ms
		frameMs = [...]float64{10, 20}[config&0x01]
	default: // CELT: 2.5/5/10/20ms
		frameMs = [...]float64{2.5, 5, 10, 20}[config&0x03]
	}

	frames := 1
	switch code {
	case 1, 2:
		frames = 2
	case 3:
		if len(packet) < 2 {
			return 0
		}
		frames = int(packet[1] & 0x3F)
	}
	return time.Duration(frameMs * float64(frames) * float64(time.Millisecond))
}

// Reader yields the opus packets of a cached ogg file, one 20ms frame at a time.
type Reader struct {
	f    *os.File
	op   *ogg.OpusReader
	pos  time.Duration
	path string
}

// Open opens a cached ogg/opus file for packet-wise playback.
func Open(path string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	r := &Reader{f: f, path: path}
	if err := r.rewind(); err != nil {
		f.Close()
		return nil, err
	}
	return r, nil
}

func (r *Reader) rewind() error {
	if _, err := r.f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	op, err := ogg.NewOpusReader(r.f)
	if err != nil {
		return err
	}
	r.op = op
	r.pos = 0
	return nil
}

// ReadPacket returns the next opus frame. It returns io.EOF on a clean end.
func (r *Reader) ReadPacket() ([]byte, error) {
	p, err := r.op.ReadAudioPacket()
	if err != nil {
		return nil, err
	}
	r.pos += PacketDuration(p.Data)
	return p.Data, nil
}

// Seek moves to d by replaying packets from the start. A 10 minute track costs
// ~50ms this way (measured: 0.83ms per 10s of audio), so a granule index is not
// worth the extra code yet.
func (r *Reader) Seek(d time.Duration) error {
	if err := r.rewind(); err != nil {
		return err
	}
	for r.pos < d {
		if _, err := r.ReadPacket(); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
	return nil
}

// Position is the playback position of the next packet.
func (r *Reader) Position() time.Duration { return r.pos }

func (r *Reader) Close() error { return r.f.Close() }

// verifyDiscordReady checks the head of a file: every packet must be a 20ms
// frame small enough for Discord. Non-opus or unusual sources fail here instead
// of sounding broken later.
func verifyDiscordReady(path string) error {
	r, err := Open(path)
	if err != nil {
		return err
	}
	defer r.Close()

	for i := range 5 {
		p, err := r.ReadPacket()
		if errors.Is(err, io.EOF) {
			if i == 0 {
				return errors.New("no audio packets")
			}
			return nil
		}
		if err != nil {
			return err
		}
		if d := PacketDuration(p); d != FrameDur {
			return errors.New("source frames are not 20ms")
		}
		if len(p) > MaxPacketSize {
			return errors.New("source packets exceed Discord's frame limit")
		}
	}
	return nil
}
