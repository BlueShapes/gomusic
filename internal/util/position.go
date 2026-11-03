package util

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParsePosition parses "90", "1:30" or "1:02:03" into a duration. Only absolute
// positions are accepted; relative forms like "+10" are intentionally not.
func ParsePosition(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty position")
	}
	parts := strings.Split(s, ":")
	if len(parts) > 3 {
		return 0, fmt.Errorf("invalid position %q", s)
	}

	var total time.Duration
	for _, part := range parts {
		// digits only: rejects relative forms like "+10"/"-5" as well as junk
		for _, c := range part {
			if c < '0' || c > '9' {
				return 0, fmt.Errorf("invalid position %q", s)
			}
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return 0, fmt.Errorf("invalid position %q", s)
		}
		total = total*60 + time.Duration(n)*time.Second
	}
	return total, nil
}
