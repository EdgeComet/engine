package yamlutil

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// UnmarshalStrict unmarshals YAML data with strict field checking enabled.
// Unknown fields in the YAML will cause an error, helping catch typos and configuration mistakes.
func UnmarshalStrict(data []byte, v interface{}) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true) // Enable strict mode to reject unknown fields

	err := decoder.Decode(v)
	if err != nil {
		// Enhance error message for unknown field errors
		errStr := err.Error()
		if strings.Contains(errStr, "field") && strings.Contains(errStr, "not found") {
			return fmt.Errorf("unknown configuration field (check for typos): %w", err)
		}
		return err
	}

	return nil
}
