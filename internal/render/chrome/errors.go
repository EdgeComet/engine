package chrome

import "errors"

// Render errors - returned during page rendering
var (
	ErrWaitTimeout      = errors.New("wait timeout exceeded")
	ErrNavigateFailed   = errors.New("navigation failed")
	ErrExtractHTML      = errors.New("HTML extraction failed")
	ErrStatusCapture    = errors.New("status capture failed")
	ErrResponseTooLarge = errors.New("response exceeds maximum size limit")
)

// Pool errors - returned during Chrome instance management
var (
	ErrPoolShutdown  = errors.New("pool is shutting down")
	ErrInstanceDead  = errors.New("chrome instance is dead")
	ErrRestartFailed = errors.New("chrome restart failed")
)
