package events

// EventEmitter defines the interface for event logging backends.
// Implementations should be fire-and-forget, non-blocking.
type EventEmitter interface {
	// Emit sends an event. Fire-and-forget, non-blocking.
	// Errors are logged internally, never returned to caller.
	Emit(event *RequestEvent)

	// Close gracefully shuts down the emitter.
	Close() error
}

// NoopEmitter is a no-op implementation for testing and disabled logging.
type NoopEmitter struct{}

// Emit does nothing.
func (n *NoopEmitter) Emit(event *RequestEvent) {}

// Close returns nil.
func (n *NoopEmitter) Close() error { return nil }
