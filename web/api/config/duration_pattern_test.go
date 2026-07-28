package config

import (
	"regexp"
	"testing"
	"time"
)

func TestDurationPattern(t *testing.T) {
	re := regexp.MustCompile(durationPattern)

	valid := []string{
		"0h0m0s",
		"8h0m0s",
		"1h30m",
		"1m1s",
		"1h1m",
		"30s",
		"5m",
		"1h",
		"300ms",
		"1.5h30m",
		"2h3m4s5ms",
	}
	for _, s := range valid {
		if !re.MatchString(s) {
			t.Errorf("expected %q to match duration pattern", s)
		}
		if _, err := time.ParseDuration(s); err != nil {
			t.Errorf("expected %q to parse as duration: %v", s, err)
		}
	}

	invalid := []string{
		"",
		"0h0m0",
		"1h30",
		"h30m",
		"1x",
		"abc",
		"1 hour",
	}
	for _, s := range invalid {
		if re.MatchString(s) {
			t.Errorf("expected %q not to match duration pattern", s)
		}
	}
}
