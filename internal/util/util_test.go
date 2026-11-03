package util

import (
	"testing"
	"time"
)

func TestHMS(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{0, "0s"},
		{45 * time.Second, "45s"},
		{90 * time.Second, "1m30s"},
		{time.Hour + 2*time.Minute + 3*time.Second, "1h2m3s"},
		{-5 * time.Second, "0s"},
	}
	for _, tt := range tests {
		if got := HMS(tt.in); got != tt.want {
			t.Errorf("HMS(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestProgressBar(t *testing.T) {
	tests := []struct {
		name  string
		pos   time.Duration
		total time.Duration
		width int
		want  string
	}{
		{"empty", 0, 100 * time.Second, 10, "[──────────] 0%"},
		{"half", 50 * time.Second, 100 * time.Second, 10, "[█████─────] 50%"},
		{"full", 100 * time.Second, 100 * time.Second, 10, "[██████████] 100%"},
		// a source without duration must not divide by zero
		{"unknown total", 10 * time.Second, 0, 4, "[────] 0%"},
		{"past the end", 200 * time.Second, 100 * time.Second, 4, "[████] 100%"},
		{"zero width", 0, 100 * time.Second, 0, "[─] 0%"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ProgressBar(tt.pos, tt.total, tt.width); got != tt.want {
				t.Errorf("ProgressBar = %q, want %q", got, tt.want)
			}
		})
	}
}
