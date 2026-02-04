package logger

import (
	"fmt"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/edgecomet/engine/internal/common/configtypes"
)

// DynamicLogger wraps zap.Logger with ability to switch levels at runtime
type DynamicLogger struct {
	*zap.Logger
	consoleLevel     *zap.AtomicLevel
	fileLevel        *zap.AtomicLevel
	configuredConfig configtypes.LogConfig
}

// SwitchToConfiguredLevel switches logger to the originally configured level
func (dl *DynamicLogger) SwitchToConfiguredLevel() {
	globalLevel := parseLogLevel(dl.configuredConfig.Level)

	dl.Info("Switching logger to configured level", zap.String("level", dl.configuredConfig.Level))

	if dl.consoleLevel != nil {
		configuredLevel := resolveLogLevel(dl.configuredConfig.Console.Level, globalLevel)
		dl.consoleLevel.SetLevel(configuredLevel)
	}

	if dl.fileLevel != nil {
		configuredLevel := resolveLogLevel(dl.configuredConfig.File.Level, globalLevel)
		dl.fileLevel.SetLevel(configuredLevel)
	}
}

// EnsureInfoLevelForShutdown ensures both console and file loggers are at INFO level
// to guarantee visibility of shutdown sequence logs
func (dl *DynamicLogger) EnsureInfoLevelForShutdown() {
	levelChanged := false

	if dl.consoleLevel != nil && dl.consoleLevel.Level() > zap.InfoLevel {
		dl.consoleLevel.SetLevel(zap.InfoLevel)
		levelChanged = true
	}

	if dl.fileLevel != nil && dl.fileLevel.Level() > zap.InfoLevel {
		dl.fileLevel.SetLevel(zap.InfoLevel)
		levelChanged = true
	}

	if levelChanged {
		dl.Info("Switched to INFO level for shutdown visibility")
	}
}

// NewLogger creates a new Zap logger with appropriate configuration
func NewLogger(config configtypes.LogConfig) (*DynamicLogger, error) {
	// Parse global log level (fallback for outputs without explicit level)
	globalLevel := parseLogLevel(config.Level)

	// Collect enabled cores
	var cores []zapcore.Core
	var consoleLevel *zap.AtomicLevel
	var fileLevel *zap.AtomicLevel

	// Add console output if enabled
	if config.Console.Enabled {
		level := zap.NewAtomicLevelAt(resolveLogLevel(config.Console.Level, globalLevel))
		consoleLevel = &level
		consoleEncoder := createEncoder(config.Console.Format)
		consoleWriter := zapcore.Lock(os.Stdout)
		cores = append(cores, zapcore.NewCore(consoleEncoder, consoleWriter, consoleLevel))
	}

	// Add file output if enabled
	if config.File.Enabled {
		if config.File.Path == "" {
			return nil, fmt.Errorf("file.path must be specified when file logging is enabled")
		}

		level := zap.NewAtomicLevelAt(resolveLogLevel(config.File.Level, globalLevel))
		fileLevel = &level
		fileEncoder := createEncoder(config.File.Format)
		fileWriter := createFileWriter(config.File.Path, config.File.Rotation)
		cores = append(cores, zapcore.NewCore(fileEncoder, fileWriter, fileLevel))
	}

	// Ensure at least one output is enabled
	if len(cores) == 0 {
		return nil, fmt.Errorf("at least one log output (console or file) must be enabled")
	}

	// Create logger with tee core if multiple outputs, or single core if only one
	var core zapcore.Core
	if len(cores) == 1 {
		core = cores[0]
	} else {
		core = zapcore.NewTee(cores...)
	}

	return &DynamicLogger{
		Logger:           zap.New(core),
		consoleLevel:     consoleLevel,
		fileLevel:        fileLevel,
		configuredConfig: config,
	}, nil
}

// NewLoggerWithStartupOverride creates a logger that starts at INFO level if configured level is higher,
// then can be switched to configured level using SwitchToConfiguredLevel()
func NewLoggerWithStartupOverride(config configtypes.LogConfig) (*DynamicLogger, error) {
	configuredLevel := parseLogLevel(config.Level)
	startupLevel := zap.InfoLevel

	// If configured level is INFO or lower (DEBUG), use it directly - no override needed
	if configuredLevel <= startupLevel {
		return NewLogger(config)
	}

	// Configure level is higher than INFO (WARN, ERROR, etc.)
	// Create temporary config with INFO level for startup
	startupConfig := config
	startupConfig.Level = configtypes.LogLevelInfo

	// Override console and file levels only if they were using global level
	if startupConfig.Console.Enabled && startupConfig.Console.Level == "" {
		startupConfig.Console.Level = configtypes.LogLevelInfo
	}
	if startupConfig.File.Enabled && startupConfig.File.Level == "" {
		startupConfig.File.Level = configtypes.LogLevelInfo
	}

	dynamicLogger, err := NewLogger(startupConfig)
	if err != nil {
		return nil, err
	}

	// Store the original configured config for later switching
	dynamicLogger.configuredConfig = config

	return dynamicLogger, nil
}

// parseLogLevel converts string level to zapcore.Level
func parseLogLevel(level string) zapcore.Level {
	switch level {
	case configtypes.LogLevelDebug:
		return zap.DebugLevel
	case configtypes.LogLevelInfo:
		return zap.InfoLevel
	case configtypes.LogLevelWarn:
		return zap.WarnLevel
	case configtypes.LogLevelError:
		return zap.ErrorLevel
	default:
		return zap.InfoLevel
	}
}

// resolveLogLevel determines the effective log level for an output
// If outputLevel is specified, use it; otherwise fall back to globalLevel
func resolveLogLevel(outputLevel string, globalLevel zapcore.Level) zapcore.Level {
	if outputLevel != "" {
		return parseLogLevel(outputLevel)
	}
	return globalLevel
}

// createEncoder creates a zapcore.Encoder based on format
func createEncoder(format string) zapcore.Encoder {
	if format == configtypes.LogFormatJSON {
		return zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
	}

	// Console or text format
	encoderConfig := zap.NewDevelopmentEncoderConfig()

	if format == configtypes.LogFormatText {
		// Plain text without color codes (for files)
		encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	} else {
		// Console format with color codes (for terminals)
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	return zapcore.NewConsoleEncoder(encoderConfig)
}

// createFileWriter creates a zapcore.WriteSyncer with rotation support
func createFileWriter(path string, rotation configtypes.RotationConfig) zapcore.WriteSyncer {
	lumberLogger := &lumberjack.Logger{
		Filename:   path,
		MaxSize:    rotation.MaxSize,
		MaxAge:     rotation.MaxAge,
		MaxBackups: rotation.MaxBackups,
		Compress:   rotation.Compress,
	}
	return zapcore.AddSync(lumberLogger)
}

// NewDefaultLogger creates a default logger for initial startup logging
func NewDefaultLogger() (*DynamicLogger, error) {
	config := configtypes.LogConfig{
		Level: configtypes.LogLevelDebug,
		Console: configtypes.ConsoleLogConfig{
			Enabled: true,
			Format:  configtypes.LogFormatConsole,
		},
		File: configtypes.FileLogConfig{
			Enabled: false,
			Format:  configtypes.LogFormatText,
		},
	}
	return NewLogger(config)
}
