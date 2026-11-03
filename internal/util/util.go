package util

import (
	"fmt"
	"strings"
	"time"
)

// HMS formats a duration like the Kotlin bot did: 1h2m3s.
func HMS(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int64(d.Seconds())
	h, m, s := total/3600, (total%3600)/60, total%60

	var b strings.Builder
	if h > 0 {
		fmt.Fprintf(&b, "%dh", h)
	}
	if m > 0 {
		fmt.Fprintf(&b, "%dm", m)
	}
	fmt.Fprintf(&b, "%ds", s)
	return b.String()
}

// ProgressBar renders "[████────] 42%" for pos/total.
func ProgressBar(pos, total time.Duration, width int) string {
	if width < 1 {
		width = 1
	}
	ratio := 0.0
	if total > 0 {
		ratio = float64(pos) / float64(total)
	}
	ratio = min(max(ratio, 0), 1)

	filled := int(ratio * float64(width))
	return fmt.Sprintf("[%s%s] %d%%",
		strings.Repeat("█", filled),
		strings.Repeat("─", width-filled),
		int(ratio*100),
	)
}
