package events

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoopEmitter_ImplementsInterface(t *testing.T) {
	var emitter EventEmitter = &NoopEmitter{}
	require.NotNil(t, emitter)
}

func TestNoopEmitter_Emit_DoesNotPanic(t *testing.T) {
	emitter := &NoopEmitter{}

	assert.NotPanics(t, func() {
		emitter.Emit(nil)
	})

	assert.NotPanics(t, func() {
		emitter.Emit(&RequestEvent{
			RequestID: "test-123",
			Host:      "example.com",
			URL:       "https://example.com/page",
			EventType: "render",
			CreatedAt: time.Now(),
		})
	})
}

func TestNoopEmitter_Close_ReturnsNil(t *testing.T) {
	emitter := &NoopEmitter{}
	err := emitter.Close()
	assert.NoError(t, err)
}
