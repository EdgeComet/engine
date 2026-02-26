package chrome

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChromeStatus_String(t *testing.T) {
	tests := []struct {
		status   ChromeStatus
		expected string
	}{
		{ChromeStatusIdle, "idle"},
		{ChromeStatusRendering, "rendering"},
		{ChromeStatusRestarting, "restarting"},
		{ChromeStatusDead, "dead"},
		{ChromeStatus(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.status.String())
		})
	}
}
