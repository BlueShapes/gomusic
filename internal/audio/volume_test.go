package audio

import (
	"math"
	"testing"

	kzopus "github.com/kazzmir/opus-go/opus"
	pionopus "github.com/pion/opus"
)

// encodeSine returns one 20ms opus packet with a 440Hz tone.
func encodeSine(t *testing.T) []byte {
	t.Helper()
	enc, err := kzopus.NewEncoder(SampleRate, Channels, kzopus.ApplicationAudio)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()

	pcm := make([]int16, FrameSize*Channels)
	for i := range FrameSize {
		v := int16(10000 * math.Sin(2*math.Pi*440*float64(i)/SampleRate))
		pcm[2*i], pcm[2*i+1] = v, v
	}
	out := make([]byte, MaxPacketSize)
	n, err := enc.Encode(pcm, FrameSize, out)
	if err != nil {
		t.Fatal(err)
	}
	return out[:n]
}

func decodeRMS(t *testing.T, packet []byte) float64 {
	t.Helper()
	dec, err := pionopus.NewDecoderWithOutput(SampleRate, Channels)
	if err != nil {
		t.Fatal(err)
	}
	pcm := make([]int16, FrameSize*Channels)
	n, err := dec.DecodeToInt16(packet, pcm)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("decoded no samples")
	}
	var sum float64
	for _, s := range pcm[:n*Channels] {
		sum += float64(s) * float64(s)
	}
	return math.Sqrt(sum / float64(n*Channels))
}

func TestGainerApply(t *testing.T) {
	g, err := NewGainer()
	if err != nil {
		t.Fatal(err)
	}
	packet := encodeSine(t)
	base := decodeRMS(t, packet)

	t.Run("100% keeps the audio", func(t *testing.T) {
		out, err := g.Apply(packet, 1)
		if err != nil {
			t.Fatal(err)
		}
		if d := PacketDuration(out); d != FrameDur {
			t.Fatalf("re-encoded packet is %v, want %v", d, FrameDur)
		}
		if len(out) > MaxPacketSize {
			t.Fatalf("re-encoded packet is %d bytes (> %d)", len(out), MaxPacketSize)
		}
		if rms := decodeRMS(t, out); rms < base*0.5 || rms > base*2 {
			t.Fatalf("rms changed too much: %0.f -> %0.f", base, rms)
		}
	})

	t.Run("0% fades to silence", func(t *testing.T) {
		// A fresh gainer: opus carries the previous frame in its overlap window,
		// so after dropping to 0% the first frames still hold the decaying tail.
		g, err := NewGainer()
		if err != nil {
			t.Fatal(err)
		}
		var rms float64
		for range 5 {
			out, err := g.Apply(packet, 0)
			if err != nil {
				t.Fatal(err)
			}
			rms = decodeRMS(t, out)
		}
		if rms > 50 {
			t.Fatalf("expected silence after 5 frames, rms=%0.f", rms)
		}
	})

	t.Run("non 20ms packets are rejected", func(t *testing.T) {
		if _, err := g.Apply([]byte{0x00}, 1); err == nil {
			t.Fatal("expected an error for a bogus packet")
		}
	})
}

// Boosting must not wrap around or hard clip: the limiter keeps peaks in range.
func TestApplyGainLimits(t *testing.T) {
	pcm := make([]int16, 4)
	for i := range pcm {
		pcm[i] = 32767
	}
	applyGain(pcm, 2.0)
	for i, s := range pcm {
		if s < 30000 || s > 32767 {
			t.Fatalf("sample %d = %d, want a limited peak closer to full scale", i, s)
		}
	}
}
