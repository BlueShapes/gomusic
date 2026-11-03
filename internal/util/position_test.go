package util

import (
	"testing"
	"time"
)

func TestParsePosition(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"90", 90 * time.Second, true},
		{"1:30", 90 * time.Second, true},
		{"0:05", 5 * time.Second, true},
		{"1:02:03", time.Hour + 2*time.Minute + 3*time.Second, true},
		{" 45 ", 45 * time.Second, true},
		{"", 0, false},
		{"abc", 0, false},
		{"1:xx", 0, false},
		{"-5", 0, false},  // relative seeks are not supported
		{"+10", 0, false}, // same
		{"1:2:3:4", 0, false},
	}
	for _, tt := range tests {
		got, err := ParsePosition(tt.in)
		if tt.ok && err != nil {
			t.Errorf("ParsePosition(%q) unexpected error: %v", tt.in, err)
			continue
		}
		if !tt.ok {
			if err == nil {
				t.Errorf("ParsePosition(%q) = %v, want an error", tt.in, got)
			}
			continue
		}
		if got != tt.want {
			t.Errorf("ParsePosition(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
