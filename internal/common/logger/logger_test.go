package logger

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/edgecomet/engine/internal/common/configtypes"
)

func TestNewLogger_ConsoleOnly(t *testing.T) {
	config := configtypes.LogConfig{
		Level: "info",
		Console: configtypes.ConsoleLogConfig{
			Enabled: true,
			Format:  "console",
		},
		File: configtypes.FileLogConfig{
			Enabled: false,
		},
	}

	logger, err := NewLogger(config)
	require.NoError(t, err)
	require.NotNil(t, logger)

	logger.Info("test console logging")
}

func TestNewLogger_FileOnly(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	config := configtypes.LogConfig{
		Level: "debug",
		Console: configtypes.ConsoleLogConfig{
			Enabled: false,
		},
		File: configtypes.FileLogConfig{
			Enabled: true,
			Path:    logPath,
			Format:  "json",
			Rotation: configtypes.RotationConfig{
				MaxSize:    10,
				MaxAge:     7,
				MaxBackups: 3,
				Compress:   false,
			},
		},
	}

	logger, err := NewLogger(config)
	require.NoError(t, err)
	require.NotNil(t, logger)

	logger.Info("test file logging", zap.String("key", "value"))
	logger.Sync()

	// Check that file was created
	_, err = os.Stat(logPath)
	assert.NoError(t, err, "log file should be created")

	// Read file content
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "test file logging")
	assert.Contains(t, string(content), "value")
}

func TestNewLogger_ConsoleAndFile(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test-both.log")

	config := configtypes.LogConfig{
		Level: "info",
		Console: configtypes.ConsoleLogConfig{
			Enabled: true,
			Format:  "console",
		},
		File: configtypes.FileLogConfig{
			Enabled: true,
			Path:    logPath,
			Format:  "json",
			Rotation: configtypes.RotationConfig{
				MaxSize:    100,
				MaxAge:     30,
				MaxBackups: 10,
				Compress:   true,
			},
		},
	}

	logger, err := NewLogger(config)
	require.NoError(t, err)
	require.NotNil(t, logger)

	logger.Info("test dual logging", zap.String("output", "both"))
	logger.Sync()

	// Check that file was created
	_, err = os.Stat(logPath)
	assert.NoError(t, err, "log file should be created")

	// Read file content
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "test dual logging")
}

func TestNewLogger_DifferentFormats(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test-formats.log")

	config := configtypes.LogConfig{
		Level: "debug",
		Console: configtypes.ConsoleLogConfig{
			Enabled: true,
			Format:  "console", // Human-readable for console
		},
		File: configtypes.FileLogConfig{
			Enabled: true,
			Path:    logPath,
			Format:  "json", // Structured for file
			Rotation: configtypes.RotationConfig{
				MaxSize:    50,
				MaxAge:     14,
				MaxBackups: 5,
				Compress:   false,
			},
		},
	}

	logger, err := NewLogger(config)
	require.NoError(t, err)
	require.NotNil(t, logger)

	logger.Debug("debug message", zap.Int("count", 42))
	logger.Info("info message", zap.String("status", "ok"))
	logger.Sync()

	// Check file has JSON format
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), `"level"`)
	assert.Contains(t, string(content), `"msg"`)
	assert.Contains(t, string(content), `"count":42`)
}

func TestNewLogger_NoOutputsEnabled(t *testing.T) {
	config := configtypes.LogConfig{
		Level: "info",
		Console: configtypes.ConsoleLogConfig{
			Enabled: false,
		},
		File: configtypes.FileLogConfig{
			Enabled: false,
		},
	}

	logger, err := NewLogger(config)
	assert.Error(t, err)
	assert.Nil(t, logger)
	assert.Contains(t, err.Error(), "at least one log output")
}

func TestNewLogger_FileEnabledNoPath(t *testing.T) {
	config := configtypes.LogConfig{
		Level: "info",
		Console: configtypes.ConsoleLogConfig{
			Enabled: false,
		},
		File: configtypes.FileLogConfig{
			Enabled: true,
			Path:    "", // Empty path
			Format:  "json",
		},
	}

	logger, err := NewLogger(config)
	assert.Error(t, err)
	assert.Nil(t, logger)
	assert.Contains(t, err.Error(), "file.path must be specified")
}

func TestNewLogger_LogLevels(t *testing.T) {
	tests := []struct {
		level         string
		expectedLevel zapcore.Level
	}{
		{"debug", zap.DebugLevel},
		{"info", zap.InfoLevel},
		{"warn", zap.WarnLevel},
		{"error", zap.ErrorLevel},
		{"invalid", zap.InfoLevel}, // Default to info
		{"", zap.InfoLevel},        // Default to info
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			tmpDir := t.TempDir()
			logPath := filepath.Join(tmpDir, "test-level.log")

			config := configtypes.LogConfig{
				Level: tt.level,
				Console: configtypes.ConsoleLogConfig{
					Enabled: false,
				},
				File: configtypes.FileLogConfig{
					Enabled: true,
					Path:    logPath,
					Format:  "json",
				},
			}

			logger, err := NewLogger(config)
			require.NoError(t, err)
			require.NotNil(t, logger)

			// Test all log levels
			logger.Debug("debug message")
			logger.Info("info message")
			logger.Warn("warn message")
			logger.Error("error message")
			logger.Sync()

			content, err := os.ReadFile(logPath)
			require.NoError(t, err)

			// Check which messages appear based on level
			switch tt.expectedLevel {
			case zap.DebugLevel:
				assert.Contains(t, string(content), "debug message")
				assert.Contains(t, string(content), "info message")
			case zap.InfoLevel:
				assert.NotContains(t, string(content), "debug message")
				assert.Contains(t, string(content), "info message")
			case zap.WarnLevel:
				assert.NotContains(t, string(content), "debug message")
				assert.NotContains(t, string(content), "info message")
				assert.Contains(t, string(content), "warn message")
			case zap.ErrorLevel:
				assert.NotContains(t, string(content), "debug message")
				assert.NotContains(t, string(content), "info message")
				assert.NotContains(t, string(content), "warn message")
				assert.Contains(t, string(content), "error message")
			}
		})
	}
}

func TestNewDefaultLogger(t *testing.T) {
	logger, err := NewDefaultLogger()
	require.NoError(t, err)
	require.NotNil(t, logger)

	logger.Debug("default logger test")
}

func TestLogRotation(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test-rotation.log")

	config := configtypes.LogConfig{
		Level: "info",
		Console: configtypes.ConsoleLogConfig{
			Enabled: false,
		},
		File: configtypes.FileLogConfig{
			Enabled: true,
			Path:    logPath,
			Format:  "json",
			Rotation: configtypes.RotationConfig{
				MaxSize:    1, // 1MB - small size to trigger rotation
				MaxAge:     7,
				MaxBackups: 3,
				Compress:   false,
			},
		},
	}

	logger, err := NewLogger(config)
	require.NoError(t, err)
	require.NotNil(t, logger)

	// Write multiple messages
	for i := 0; i < 100; i++ {
		logger.Info("test message", zap.Int("iteration", i), zap.String("data", "some extra data to fill up the log"))
	}
	logger.Sync()

	// Check that file exists
	_, err = os.Stat(logPath)
	assert.NoError(t, err)
}

func TestNewLogger_TextFormat_NoColorCodes(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test-text.log")

	config := configtypes.LogConfig{
		Level: "info",
		Console: configtypes.ConsoleLogConfig{
			Enabled: false,
		},
		File: configtypes.FileLogConfig{
			Enabled: true,
			Path:    logPath,
			Format:  "text", // Plain text format without colors
			Rotation: configtypes.RotationConfig{
				MaxSize:    100,
				MaxAge:     30,
				MaxBackups: 10,
				Compress:   false,
			},
		},
	}

	logger, err := NewLogger(config)
	require.NoError(t, err)
	require.NotNil(t, logger)

	logger.Info("test text format", zap.String("key", "value"))
	logger.Warn("warning message")
	logger.Error("error message")
	logger.Sync()

	// Read file content
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)

	contentStr := string(content)

	// Verify content is present
	assert.Contains(t, contentStr, "test text format")
	assert.Contains(t, contentStr, "warning message")
	assert.Contains(t, contentStr, "error message")
	assert.Contains(t, contentStr, "key")
	assert.Contains(t, contentStr, "value")

	// Verify NO ANSI color codes (e.g., \x1b[34m, \x1b[0m, etc.)
	assert.NotContains(t, contentStr, "\x1b[", "text format should not contain ANSI color codes")
	assert.NotContains(t, contentStr, "\033[", "text format should not contain ANSI color codes (octal)")

	// Verify level names are present in plain text
	assert.Contains(t, contentStr, "INFO")
	assert.Contains(t, contentStr, "WARN")
	assert.Contains(t, contentStr, "ERROR")
}

func TestNewLogger_ConsoleFormat_HasColorCodes(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test-console.log")

	config := configtypes.LogConfig{
		Level: "info",
		Console: configtypes.ConsoleLogConfig{
			Enabled: false,
		},
		File: configtypes.FileLogConfig{
			Enabled: true,
			Path:    logPath,
			Format:  "console", // Console format with colors
			Rotation: configtypes.RotationConfig{
				MaxSize:    100,
				MaxAge:     30,
				MaxBackups: 10,
				Compress:   false,
			},
		},
	}

	logger, err := NewLogger(config)
	require.NoError(t, err)
	require.NotNil(t, logger)

	logger.Info("test console format with colors")
	logger.Sync()

	// Read file content
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)

	contentStr := string(content)

	// Verify content is present
	assert.Contains(t, contentStr, "test console format with colors")

	// Verify ANSI color codes ARE present for console format
	assert.Contains(t, contentStr, "\x1b[", "console format should contain ANSI color codes")
}

func TestNewLogger_PerOutputLogLevels(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test-per-output.log")

	config := configtypes.LogConfig{
		Level: "info", // Global level (fallback)
		Console: configtypes.ConsoleLogConfig{
			Enabled: true,
			Format:  "console",
			Level:   "warn", // Console only shows warn and error
		},
		File: configtypes.FileLogConfig{
			Enabled: true,
			Path:    logPath,
			Format:  "json",
			Level:   "debug", // File captures everything
			Rotation: configtypes.RotationConfig{
				MaxSize:    10,
				MaxAge:     7,
				MaxBackups: 3,
				Compress:   false,
			},
		},
	}

	logger, err := NewLogger(config)
	require.NoError(t, err)
	require.NotNil(t, logger)

	// Log messages at different levels
	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")
	logger.Sync()

	// Read file content
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	contentStr := string(content)

	// File should have all messages (debug level)
	assert.Contains(t, contentStr, "debug message", "file should contain debug message")
	assert.Contains(t, contentStr, "info message", "file should contain info message")
	assert.Contains(t, contentStr, "warn message", "file should contain warn message")
	assert.Contains(t, contentStr, "error message", "file should contain error message")

	// Note: Console output goes to stdout, harder to capture in test
	// But we can verify the logger was created successfully with different levels
}

func TestNewLogger_ConsoleLevel_OverridesGlobal(t *testing.T) {
	config := configtypes.LogConfig{
		Level: "debug", // Global level
		Console: configtypes.ConsoleLogConfig{
			Enabled: true,
			Format:  "console",
			Level:   "error", // Override to error
		},
		File: configtypes.FileLogConfig{
			Enabled: false,
		},
	}

	logger, err := NewLogger(config)
	require.NoError(t, err)
	require.NotNil(t, logger)

	// Logger should be created with error level for console
	// (We can't easily test console output, but we verify creation succeeds)
	logger.Error("error message should appear")
}

func TestNewLogger_FileLevel_OverridesGlobal(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test-file-override.log")

	config := configtypes.LogConfig{
		Level: "warn", // Global level
		Console: configtypes.ConsoleLogConfig{
			Enabled: false,
		},
		File: configtypes.FileLogConfig{
			Enabled: true,
			Path:    logPath,
			Format:  "json",
			Level:   "debug", // Override to debug
			Rotation: configtypes.RotationConfig{
				MaxSize:    10,
				MaxAge:     7,
				MaxBackups: 3,
				Compress:   false,
			},
		},
	}

	logger, err := NewLogger(config)
	require.NoError(t, err)
	require.NotNil(t, logger)

	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Sync()

	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	contentStr := string(content)

	// File should have all messages because it uses debug level
	assert.Contains(t, contentStr, "debug message", "file should contain debug message with debug level")
	assert.Contains(t, contentStr, "info message", "file should contain info message with debug level")
	assert.Contains(t, contentStr, "warn message", "file should contain warn message with debug level")
}

func TestNewLogger_FallbackToGlobalLevel(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test-fallback.log")

	config := configtypes.LogConfig{
		Level: "warn", // Global level
		Console: configtypes.ConsoleLogConfig{
			Enabled: true,
			Format:  "console",
			// Level not specified - should use global
		},
		File: configtypes.FileLogConfig{
			Enabled: true,
			Path:    logPath,
			Format:  "json",
			// Level not specified - should use global
			Rotation: configtypes.RotationConfig{
				MaxSize:    10,
				MaxAge:     7,
				MaxBackups: 3,
				Compress:   false,
			},
		},
	}

	logger, err := NewLogger(config)
	require.NoError(t, err)
	require.NotNil(t, logger)

	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")
	logger.Sync()

	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	contentStr := string(content)

	// Both outputs should use warn level (global)
	assert.NotContains(t, contentStr, "debug message", "file should not contain debug with warn level")
	assert.NotContains(t, contentStr, "info message", "file should not contain info with warn level")
	assert.Contains(t, contentStr, "warn message", "file should contain warn message")
	assert.Contains(t, contentStr, "error message", "file should contain error message")
}

func TestNewLogger_MixedLevelConfiguration(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test-mixed.log")

	config := configtypes.LogConfig{
		Level: "info", // Global level
		Console: configtypes.ConsoleLogConfig{
			Enabled: true,
			Format:  "console",
			Level:   "error", // Console has explicit level
		},
		File: configtypes.FileLogConfig{
			Enabled: true,
			Path:    logPath,
			Format:  "json",
			// File uses global level (info)
			Rotation: configtypes.RotationConfig{
				MaxSize:    10,
				MaxAge:     7,
				MaxBackups: 3,
				Compress:   false,
			},
		},
	}

	logger, err := NewLogger(config)
	require.NoError(t, err)
	require.NotNil(t, logger)

	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")
	logger.Sync()

	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	contentStr := string(content)

	// File should use global info level
	assert.NotContains(t, contentStr, "debug message")
	assert.Contains(t, contentStr, "info message", "file should contain info message with global level")
	assert.Contains(t, contentStr, "warn message")
	assert.Contains(t, contentStr, "error message")
}

func TestResolveLogLevel(t *testing.T) {
	tests := []struct {
		name          string
		outputLevel   string
		globalLevel   zapcore.Level
		expectedLevel zapcore.Level
	}{
		{
			name:          "output level specified - debug",
			outputLevel:   "debug",
			globalLevel:   zap.InfoLevel,
			expectedLevel: zap.DebugLevel,
		},
		{
			name:          "output level specified - error",
			outputLevel:   "error",
			globalLevel:   zap.InfoLevel,
			expectedLevel: zap.ErrorLevel,
		},
		{
			name:          "output level not specified - fallback to global",
			outputLevel:   "",
			globalLevel:   zap.WarnLevel,
			expectedLevel: zap.WarnLevel,
		},
		{
			name:          "output level empty - fallback to global debug",
			outputLevel:   "",
			globalLevel:   zap.DebugLevel,
			expectedLevel: zap.DebugLevel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveLogLevel(tt.outputLevel, tt.globalLevel)
			assert.Equal(t, tt.expectedLevel, result)
		})
	}
}

func TestEnsureInfoLevelForShutdown(t *testing.T) {
	t.Run("console level higher than INFO - should lower to INFO", func(t *testing.T) {
		config := configtypes.LogConfig{
			Level: configtypes.LogLevelError,
			Console: configtypes.ConsoleLogConfig{
				Enabled: true,
				Format:  configtypes.LogFormatConsole,
			},
			File: configtypes.FileLogConfig{
				Enabled: false,
			},
		}

		logger, err := NewLogger(config)
		require.NoError(t, err)
		require.NotNil(t, logger)

		assert.Equal(t, zap.ErrorLevel, logger.consoleLevel.Level())

		logger.EnsureInfoLevelForShutdown()

		assert.Equal(t, zap.InfoLevel, logger.consoleLevel.Level())
	})

	t.Run("file level higher than INFO - should lower to INFO", func(t *testing.T) {
		tmpDir := t.TempDir()
		logPath := filepath.Join(tmpDir, "test.log")

		config := configtypes.LogConfig{
			Level: configtypes.LogLevelWarn,
			Console: configtypes.ConsoleLogConfig{
				Enabled: false,
			},
			File: configtypes.FileLogConfig{
				Enabled: true,
				Path:    logPath,
				Format:  configtypes.LogFormatText,
				Rotation: configtypes.RotationConfig{
					MaxSize:    10,
					MaxAge:     7,
					MaxBackups: 3,
					Compress:   false,
				},
			},
		}

		logger, err := NewLogger(config)
		require.NoError(t, err)
		require.NotNil(t, logger)

		assert.Equal(t, zap.WarnLevel, logger.fileLevel.Level())

		logger.EnsureInfoLevelForShutdown()

		assert.Equal(t, zap.InfoLevel, logger.fileLevel.Level())
	})

	t.Run("both console and file levels higher - should lower both to INFO", func(t *testing.T) {
		tmpDir := t.TempDir()
		logPath := filepath.Join(tmpDir, "test.log")

		config := configtypes.LogConfig{
			Level: configtypes.LogLevelError,
			Console: configtypes.ConsoleLogConfig{
				Enabled: true,
				Format:  configtypes.LogFormatConsole,
			},
			File: configtypes.FileLogConfig{
				Enabled: true,
				Path:    logPath,
				Format:  configtypes.LogFormatText,
				Rotation: configtypes.RotationConfig{
					MaxSize:    10,
					MaxAge:     7,
					MaxBackups: 3,
					Compress:   false,
				},
			},
		}

		logger, err := NewLogger(config)
		require.NoError(t, err)
		require.NotNil(t, logger)

		assert.Equal(t, zap.ErrorLevel, logger.consoleLevel.Level())
		assert.Equal(t, zap.ErrorLevel, logger.fileLevel.Level())

		logger.EnsureInfoLevelForShutdown()

		assert.Equal(t, zap.InfoLevel, logger.consoleLevel.Level())
		assert.Equal(t, zap.InfoLevel, logger.fileLevel.Level())
	})

	t.Run("level already at INFO - no change needed", func(t *testing.T) {
		config := configtypes.LogConfig{
			Level: configtypes.LogLevelInfo,
			Console: configtypes.ConsoleLogConfig{
				Enabled: true,
				Format:  configtypes.LogFormatConsole,
			},
			File: configtypes.FileLogConfig{
				Enabled: false,
			},
		}

		logger, err := NewLogger(config)
		require.NoError(t, err)
		require.NotNil(t, logger)

		assert.Equal(t, zap.InfoLevel, logger.consoleLevel.Level())

		logger.EnsureInfoLevelForShutdown()

		assert.Equal(t, zap.InfoLevel, logger.consoleLevel.Level())
	})

	t.Run("level at DEBUG - should not change", func(t *testing.T) {
		config := configtypes.LogConfig{
			Level: configtypes.LogLevelDebug,
			Console: configtypes.ConsoleLogConfig{
				Enabled: true,
				Format:  configtypes.LogFormatConsole,
			},
			File: configtypes.FileLogConfig{
				Enabled: false,
			},
		}

		logger, err := NewLogger(config)
		require.NoError(t, err)
		require.NotNil(t, logger)

		assert.Equal(t, zap.DebugLevel, logger.consoleLevel.Level())

		logger.EnsureInfoLevelForShutdown()

		assert.Equal(t, zap.DebugLevel, logger.consoleLevel.Level())
	})
}
