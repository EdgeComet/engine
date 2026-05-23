package types

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// Duration wraps time.Duration with extended YAML parsing support for days and weeks
type Duration time.Duration

// UnmarshalYAML implements yaml.Unmarshaler for extended duration formats
func (d *Duration) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}

	// Try standard parsing first (handles: ns, us, ms, s, m, h)
	dur, err := time.ParseDuration(s)
	if err == nil {
		*d = Duration(dur)
		return nil
	}

	// Parse extended formats: d (days), w (weeks)
	dur, err = parseExtendedDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(dur)
	return nil
}

// MarshalYAML implements yaml.Marshaler
func (d Duration) MarshalYAML() (interface{}, error) {
	return time.Duration(d).String(), nil
}

// UnmarshalJSON implements json.Unmarshaler for Duration.
// Accepts both numbers (nanoseconds, backward-compatible) and strings ("15s", "24h", "30d", "2w").
func (d *Duration) UnmarshalJSON(data []byte) error {
	var ns int64
	if err := json.Unmarshal(data, &ns); err == nil {
		*d = Duration(ns)
		return nil
	}

	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("duration must be a string or number, got %s", string(data))
	}

	dur, err := time.ParseDuration(s)
	if err == nil {
		*d = Duration(dur)
		return nil
	}

	dur, err = parseExtendedDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(dur)
	return nil
}

// MarshalJSON implements json.Marshaler for Duration.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// ToDuration converts types.Duration to time.Duration
func (d Duration) ToDuration() time.Duration {
	return time.Duration(d)
}

// String implements fmt.Stringer for Duration
func (d Duration) String() string {
	return time.Duration(d).String()
}

// parseExtendedDuration parses duration strings with extended suffixes: d (days), w (weeks)
// Examples: "30d", "2w", "1.5d"
func parseExtendedDuration(s string) (time.Duration, error) {
	re := regexp.MustCompile(`^(-?)(\d+(?:\.\d+)?)(d|w)$`)
	matches := re.FindStringSubmatch(s)
	if matches == nil {
		return 0, fmt.Errorf("invalid format, expected format like '30d' or '2w'")
	}

	sign := matches[1]
	valueStr := matches[2]
	suffix := matches[3]

	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid numeric value: %w", err)
	}

	if sign == "-" {
		value = -value
	}

	var duration time.Duration
	switch suffix {
	case "d":
		duration = time.Duration(value * float64(24*time.Hour))
	case "w":
		duration = time.Duration(value * float64(7*24*time.Hour))
	default:
		return 0, fmt.Errorf("unsupported suffix %q", suffix)
	}

	return duration, nil
}
