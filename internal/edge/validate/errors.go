package validate

import "fmt"

// ValidationError represents a single validation error with file location
type ValidationError struct {
	File    string
	Line    int // 0 if line number not available
	Message string
}

// ErrorCollector collects validation errors and warnings
type ErrorCollector struct {
	errors   []ValidationError
	warnings []ValidationError
}

// NewErrorCollector creates a new error collector
func NewErrorCollector() *ErrorCollector {
	return &ErrorCollector{
		errors:   make([]ValidationError, 0),
		warnings: make([]ValidationError, 0),
	}
}

// Add adds a validation error with formatted message
func (ec *ErrorCollector) Add(file string, line int, format string, args ...interface{}) {
	ec.errors = append(ec.errors, ValidationError{
		File:    file,
		Line:    line,
		Message: fmt.Sprintf(format, args...),
	})
}

// AddError adds a pre-formatted validation error
func (ec *ErrorCollector) AddError(file string, line int, message string) {
	ec.errors = append(ec.errors, ValidationError{
		File:    file,
		Line:    line,
		Message: message,
	})
}

// HasErrors returns true if any errors have been collected
func (ec *ErrorCollector) HasErrors() bool {
	return len(ec.errors) > 0
}

// Errors returns all collected errors
func (ec *ErrorCollector) Errors() []ValidationError {
	return ec.errors
}

// Count returns the number of errors collected
func (ec *ErrorCollector) Count() int {
	return len(ec.errors)
}

// AddWarning adds a validation warning with formatted message
func (ec *ErrorCollector) AddWarning(file string, line int, format string, args ...interface{}) {
	ec.warnings = append(ec.warnings, ValidationError{
		File:    file,
		Line:    line,
		Message: fmt.Sprintf(format, args...),
	})
}

// Warnings returns all collected warnings
func (ec *ErrorCollector) Warnings() []ValidationError {
	return ec.warnings
}
