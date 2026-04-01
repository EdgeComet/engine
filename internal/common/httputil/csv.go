package httputil

import (
	"fmt"
	"strings"
)

// ParseCSVFilter splits a comma-separated string, trims whitespace from each part,
// optionally validates against an allowed set, and returns the non-empty values.
// If allowed is nil, validation is skipped.
func ParseCSVFilter(value string, allowed map[string]bool, fieldName string) ([]string, error) {
	if value == "" {
		return nil, nil
	}

	parts := strings.Split(value, ",")
	var result []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		if allowed != nil {
			if !allowed[trimmed] {
				return nil, fmt.Errorf("invalid %s value: %s", fieldName, trimmed)
			}
		}
		result = append(result, trimmed)
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}
