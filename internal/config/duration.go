package config

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"time"
)

var durationRE = regexp.MustCompile(`^(\d+(?:\.\d+)?)(ms|s|m|h)$`)
var bareIntRE = regexp.MustCompile(`^\d+$`)

// ParseDuration parses a config duration: a number with ms, s, m or h, or a
// bare integer meaning seconds. Zero is rejected unless allowZero is set.
func ParseDuration(s string, allowZero bool) (time.Duration, error) {
	d, err := parseDurationSyntax(s)
	if err != nil {
		return 0, err
	}
	if d == 0 && !allowZero {
		return 0, fmt.Errorf("duration must be greater than 0 (got %q)", s)
	}
	return d, nil
}

func parseDurationSyntax(s string) (time.Duration, error) {
	num, unit := s, "s"
	if !bareIntRE.MatchString(s) {
		m := durationRE.FindStringSubmatch(s)
		if m == nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		num, unit = m[1], m[2]
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	var mult time.Duration
	switch unit {
	case "ms":
		mult = time.Millisecond
	case "s":
		mult = time.Second
	case "m":
		mult = time.Minute
	case "h":
		mult = time.Hour
	}
	v := math.Round(f * float64(mult))
	if v >= math.MaxInt64 {
		return 0, fmt.Errorf("invalid duration %q: too large", s)
	}
	return time.Duration(v), nil
}
