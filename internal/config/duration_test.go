package config

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	cases := []struct {
		in        string
		allowZero bool
		want      time.Duration
		ok        bool
	}{
		{"500ms", false, 500 * time.Millisecond, true},
		{"750ms", false, 750 * time.Millisecond, true},
		{"1.5m", false, 90 * time.Second, true},
		{"30", false, 30 * time.Second, true},
		{"30s", false, 30 * time.Second, true},
		{"2h", false, 2 * time.Hour, true},
		{"0.5s", false, 500 * time.Millisecond, true},
		{"5 s", false, 0, false},
		{"5", false, 5 * time.Second, true},
		{"-5s", false, 0, false},
		{"5sec", false, 0, false},
		{"1.s", false, 0, false},
		{".5s", false, 0, false},
		{"", false, 0, false},
		{"0s", false, 0, false},
		{"0", false, 0, false},
		{"0s", true, 0, true},
		{"0ms", true, 0, true},
		{"99999999999999h", false, 0, false},
	}
	for _, c := range cases {
		got, err := parseDuration(c.in, c.allowZero)
		if (err == nil) != c.ok {
			t.Errorf("ParseDuration(%q, %v) err = %v, want ok=%v", c.in, c.allowZero, err, c.ok)
			continue
		}
		if c.ok && got != c.want {
			t.Errorf("ParseDuration(%q) = %v, want %v", c.in, got, c.want)
		}
	}
	if _, err := parseDuration("5 s", false); err == nil || err.Error() != `invalid duration "5 s"` {
		t.Errorf("error = %v", err)
	}
}
