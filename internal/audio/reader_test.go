package audio

import (
	"testing"
	"time"
)

// PacketDuration decides whether a source can be passed through or must be
// re-encoded, so the TOC parsing is worth pinning down.
func TestPacketDuration(t *testing.T) {
	toc := func(config, stereo, code int) byte {
		return byte(config<<3 | stereo<<2 | code)
	}

	tests := []struct {
		name   string
		packet []byte
		want   time.Duration
	}{
		{"empty", nil, 0},
		{"SILK NB 10ms", []byte{toc(0, 0, 0), 0}, 10 * time.Millisecond},
		{"SILK NB 20ms", []byte{toc(1, 0, 0), 0}, 20 * time.Millisecond},
		{"SILK NB 40ms", []byte{toc(2, 0, 0), 0}, 40 * time.Millisecond},
		{"SILK WB 60ms", []byte{toc(11, 0, 0), 0}, 60 * time.Millisecond},
		{"Hybrid SWB 10ms", []byte{toc(12, 0, 0), 0}, 10 * time.Millisecond},
		{"Hybrid FB 20ms", []byte{toc(15, 0, 0), 0}, 20 * time.Millisecond},
		{"CELT FB 2.5ms", []byte{toc(16, 0, 0), 0}, 2500 * time.Microsecond},
		{"CELT FB 20ms (youtube)", []byte{toc(19, 1, 0), 0}, 20 * time.Millisecond},
		{"2 frames of 20ms", []byte{toc(19, 1, 1), 0}, 40 * time.Millisecond},
		{"code 3: 3 frames of 20ms", []byte{toc(19, 1, 3), 3}, 60 * time.Millisecond},
		{"code 3 truncated", []byte{toc(19, 1, 3)}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PacketDuration(tt.packet); got != tt.want {
				t.Fatalf("PacketDuration = %v, want %v", got, tt.want)
			}
		})
	}
}
