package audio

import (
	"errors"
	"fmt"
	"math"

	kzopus "github.com/kazzmir/opus-go/opus"
	pionopus "github.com/pion/opus"
)

// Gainer applies volume to opus packets in process. Volume cannot be changed
// without decoding, so this is only used when the volume is not 100%: plain
// playback stays a zero-cost passthrough.
//
// Decoding uses pion (1 alloc/frame, ~410x realtime) and encoding uses kazzmir
// (the only pure Go encoder, ~50x realtime). Measured: ~410µs per 20ms frame,
// i.e. ~2% of one core per guild at volume != 100%.
type Gainer struct {
	dec *pionopus.Decoder
	enc *kzopus.Encoder
	pcm []int16
	buf []byte
}

func NewGainer() (*Gainer, error) {
	dec, err := pionopus.NewDecoderWithOutput(SampleRate, Channels)
	if err != nil {
		return nil, fmt.Errorf("create opus decoder: %w", err)
	}
	enc, err := kzopus.NewEncoder(SampleRate, Channels, kzopus.ApplicationAudio)
	if err != nil {
		return nil, fmt.Errorf("create opus encoder: %w", err)
	}
	if err := enc.SetBitrate(128000); err != nil {
		return nil, fmt.Errorf("set bitrate: %w", err)
	}
	return &Gainer{
		dec: &dec,
		enc: enc,
		pcm: make([]int16, 5760*Channels), // max opus frame (120ms)
		buf: make([]byte, MaxPacketSize),
	}, nil
}

// Apply returns packet scaled by gain (1.0 = unchanged). The result is a new
// slice because the caller hands it to the voice sender.
func (g *Gainer) Apply(packet []byte, gain float64) ([]byte, error) {
	n, err := g.dec.DecodeToInt16(packet, g.pcm)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if n != FrameSize {
		return nil, fmt.Errorf("unexpected frame size %d, want %d", n, FrameSize)
	}

	samples := g.pcm[:n*Channels]
	applyGain(samples, gain)

	size, err := g.enc.Encode(samples, n, g.buf)
	if err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	if size == 0 {
		return nil, errors.New("encoder produced an empty packet")
	}
	out := make([]byte, size)
	copy(out, g.buf[:size])
	return out, nil
}

// applyGain scales interleaved s16 PCM in place. Above -3dBFS it bends towards
// full scale instead of clipping, so boosting an already loud master distorts
// much less than a hard clamp.
func applyGain(pcm []int16, gain float64) {
	const knee = 0.7
	for i, s := range pcm {
		v := float64(s) / 32768 * gain
		if a := math.Abs(v); a > knee {
			sign := 1.0
			if v < 0 {
				sign = -1
			}
			v = sign * (knee + (1-knee)*math.Tanh((a-knee)/(1-knee)))
		}
		switch {
		case v > 0.999969:
			v = 0.999969
		case v < -1:
			v = -1
		}
		pcm[i] = int16(math.Round(v * 32768))
	}
}
