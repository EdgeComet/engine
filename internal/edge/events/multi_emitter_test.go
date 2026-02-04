package events

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockEmitter tracks calls for testing
type mockEmitter struct {
	emitCalls  []*RequestEvent
	closeCalls int
	closeErr   error
}

func (m *mockEmitter) Emit(event *RequestEvent) {
	m.emitCalls = append(m.emitCalls, event)
}

func (m *mockEmitter) Close() error {
	m.closeCalls++
	return m.closeErr
}

func TestNewMultiEmitter_EmptySlice(t *testing.T) {
	emitter := NewMultiEmitter([]EventEmitter{}, zap.NewNop())
	require.NotNil(t, emitter)
	assert.Empty(t, emitter.emitters)
}

func TestNewMultiEmitter_WithEmitters(t *testing.T) {
	mock1 := &mockEmitter{}
	mock2 := &mockEmitter{}

	emitter := NewMultiEmitter([]EventEmitter{mock1, mock2}, zap.NewNop())
	require.NotNil(t, emitter)
	assert.Len(t, emitter.emitters, 2)
}

func TestMultiEmitter_Emit_CallsAllEmitters(t *testing.T) {
	mock1 := &mockEmitter{}
	mock2 := &mockEmitter{}
	mock3 := &mockEmitter{}

	emitter := NewMultiEmitter([]EventEmitter{mock1, mock2, mock3}, zap.NewNop())

	event := &RequestEvent{
		RequestID: "test-123",
		Host:      "example.com",
		CreatedAt: time.Now(),
	}

	emitter.Emit(event)

	// All emitters should have received the event
	assert.Len(t, mock1.emitCalls, 1)
	assert.Len(t, mock2.emitCalls, 1)
	assert.Len(t, mock3.emitCalls, 1)

	// Same event reference
	assert.Equal(t, event, mock1.emitCalls[0])
	assert.Equal(t, event, mock2.emitCalls[0])
	assert.Equal(t, event, mock3.emitCalls[0])
}

func TestMultiEmitter_Emit_EmptyEmitters(t *testing.T) {
	emitter := NewMultiEmitter([]EventEmitter{}, zap.NewNop())

	event := &RequestEvent{
		RequestID: "test-123",
		CreatedAt: time.Now(),
	}

	// Should not panic
	assert.NotPanics(t, func() {
		emitter.Emit(event)
	})
}

func TestMultiEmitter_Emit_MultipleEvents(t *testing.T) {
	mock := &mockEmitter{}
	emitter := NewMultiEmitter([]EventEmitter{mock}, zap.NewNop())

	for i := 0; i < 5; i++ {
		emitter.Emit(&RequestEvent{
			RequestID: "test",
			CreatedAt: time.Now(),
		})
	}

	assert.Len(t, mock.emitCalls, 5)
}

func TestMultiEmitter_Close_CallsAllEmitters(t *testing.T) {
	mock1 := &mockEmitter{}
	mock2 := &mockEmitter{}
	mock3 := &mockEmitter{}

	emitter := NewMultiEmitter([]EventEmitter{mock1, mock2, mock3}, zap.NewNop())

	err := emitter.Close()

	assert.NoError(t, err)
	assert.Equal(t, 1, mock1.closeCalls)
	assert.Equal(t, 1, mock2.closeCalls)
	assert.Equal(t, 1, mock3.closeCalls)
}

func TestMultiEmitter_Close_ReturnsNilWhenAllSucceed(t *testing.T) {
	mock1 := &mockEmitter{}
	mock2 := &mockEmitter{}

	emitter := NewMultiEmitter([]EventEmitter{mock1, mock2}, zap.NewNop())

	err := emitter.Close()
	assert.NoError(t, err)
}

func TestMultiEmitter_Close_CollectsErrors(t *testing.T) {
	err1 := errors.New("error from emitter 1")
	err2 := errors.New("error from emitter 2")

	mock1 := &mockEmitter{closeErr: err1}
	mock2 := &mockEmitter{} // no error
	mock3 := &mockEmitter{closeErr: err2}

	emitter := NewMultiEmitter([]EventEmitter{mock1, mock2, mock3}, zap.NewNop())

	err := emitter.Close()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "error from emitter 1")
	assert.Contains(t, err.Error(), "error from emitter 2")

	// All emitters should have been closed
	assert.Equal(t, 1, mock1.closeCalls)
	assert.Equal(t, 1, mock2.closeCalls)
	assert.Equal(t, 1, mock3.closeCalls)
}

func TestMultiEmitter_Close_EmptyEmitters(t *testing.T) {
	emitter := NewMultiEmitter([]EventEmitter{}, zap.NewNop())

	err := emitter.Close()
	assert.NoError(t, err)
}

func TestMultiEmitter_ImplementsInterface(t *testing.T) {
	emitter := NewMultiEmitter([]EventEmitter{}, zap.NewNop())
	var _ EventEmitter = emitter
}
