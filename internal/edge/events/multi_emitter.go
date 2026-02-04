package events

import (
	"errors"

	"go.uber.org/zap"
)

// MultiEmitter dispatches events to multiple backends.
type MultiEmitter struct {
	emitters []EventEmitter
	logger   *zap.Logger
}

// NewMultiEmitter creates a new multi-emitter that dispatches to all provided emitters.
func NewMultiEmitter(emitters []EventEmitter, logger *zap.Logger) *MultiEmitter {
	return &MultiEmitter{
		emitters: emitters,
		logger:   logger,
	}
}

// Emit sends the event to all registered emitters.
func (m *MultiEmitter) Emit(event *RequestEvent) {
	for _, e := range m.emitters {
		e.Emit(event)
	}
}

// Close closes all emitters and returns any errors combined.
func (m *MultiEmitter) Close() error {
	var errs []error
	for _, e := range m.emitters {
		if err := e.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
