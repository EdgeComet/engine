package events

import (
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/edgecomet/engine/internal/common/configtypes"
)

const (
	DefaultMaxSize    = 100 // MB
	DefaultMaxAge     = 30  // days
	DefaultMaxBackups = 10  // files
	defaultTemplate   = "{timestamp}\t{host}\t{url}\t{status_code}\t{event_type}\t{source}\t{serve_time}\t{render_time}\t{cache_age}\t{client_ip}"
)

// FileEmitter writes events to a log file with rotation support.
type FileEmitter struct {
	writer    *lumberjack.Logger
	formatter *TemplateFormatter
	logger    *zap.Logger
}

// NewFileEmitter creates a new file-based event emitter.
// Returns error if the template is invalid or directory creation fails.
func NewFileEmitter(config configtypes.EventFileConfig, logger *zap.Logger) (*FileEmitter, error) {
	// Create parent directory if it doesn't exist
	dir := filepath.Dir(config.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create log directory %s: %w", dir, err)
	}

	// Apply default template if not specified
	template := config.Template
	if template == "" {
		template = defaultTemplate
	}

	// Initialize template formatter
	formatter, err := NewTemplateFormatter(template)
	if err != nil {
		return nil, fmt.Errorf("invalid template for event log %s: %w", config.Path, err)
	}

	// Apply rotation defaults
	maxSize := config.Rotation.MaxSize
	if maxSize == 0 {
		maxSize = DefaultMaxSize
	}

	maxAge := config.Rotation.MaxAge
	if maxAge == 0 {
		maxAge = DefaultMaxAge
	}

	maxBackups := config.Rotation.MaxBackups
	if maxBackups == 0 {
		maxBackups = DefaultMaxBackups
	}

	// Initialize lumberjack logger
	writer := &lumberjack.Logger{
		Filename:   config.Path,
		MaxSize:    maxSize,
		MaxAge:     maxAge,
		MaxBackups: maxBackups,
		Compress:   config.Rotation.Compress,
	}

	return &FileEmitter{
		writer:    writer,
		formatter: formatter,
		logger:    logger,
	}, nil
}

// Emit formats the event and writes it to the log file.
// Fire-and-forget: errors are logged but not returned.
func (f *FileEmitter) Emit(event *RequestEvent) {
	line := f.formatter.Format(event)
	_, err := f.writer.Write([]byte(line + "\n"))
	if err != nil {
		f.logger.Warn("failed to write event to log file",
			zap.Error(err),
			zap.String("request_id", event.RequestID),
		)
	}
}

// Close closes the underlying file handle.
func (f *FileEmitter) Close() error {
	return f.writer.Close()
}
