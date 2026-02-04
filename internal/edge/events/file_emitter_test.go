package events

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/configtypes"
)

func TestNewFileEmitter_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	nestedPath := filepath.Join(tmpDir, "nested", "dir", "access.log")

	config := configtypes.EventFileConfig{
		Enabled:  true,
		Path:     nestedPath,
		Template: "{request_id}",
	}

	emitter, err := NewFileEmitter(config, zap.NewNop())
	require.NoError(t, err)
	defer emitter.Close()

	// Verify directory was created
	dir := filepath.Dir(nestedPath)
	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestNewFileEmitter_InvalidTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "access.log")

	config := configtypes.EventFileConfig{
		Enabled:  true,
		Path:     logPath,
		Template: "{invalid_field}",
	}

	emitter, err := NewFileEmitter(config, zap.NewNop())
	assert.Error(t, err)
	assert.Nil(t, emitter)
	assert.Contains(t, err.Error(), "invalid_field")
}

func TestNewFileEmitter_EmptyTemplate_UsesDefault(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "access.log")

	config := configtypes.EventFileConfig{
		Enabled:  true,
		Path:     logPath,
		Template: "", // Empty template should use default
	}

	emitter, err := NewFileEmitter(config, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, emitter)
	defer emitter.Close()

	// Verify default template was applied by checking formatter exists
	assert.NotNil(t, emitter.formatter)
}

func TestNewFileEmitter_AppliesDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "access.log")

	config := configtypes.EventFileConfig{
		Enabled:  true,
		Path:     logPath,
		Template: "{request_id}",
		Rotation: configtypes.RotationConfig{
			// All zeros - should use defaults
		},
	}

	emitter, err := NewFileEmitter(config, zap.NewNop())
	require.NoError(t, err)
	defer emitter.Close()

	// Verify defaults were applied
	assert.Equal(t, DefaultMaxSize, emitter.writer.MaxSize)
	assert.Equal(t, DefaultMaxAge, emitter.writer.MaxAge)
	assert.Equal(t, DefaultMaxBackups, emitter.writer.MaxBackups)
}

func TestNewFileEmitter_UsesProvidedRotationConfig(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "access.log")

	config := configtypes.EventFileConfig{
		Enabled:  true,
		Path:     logPath,
		Template: "{request_id}",
		Rotation: configtypes.RotationConfig{
			MaxSize:    50,
			MaxAge:     7,
			MaxBackups: 3,
			Compress:   true,
		},
	}

	emitter, err := NewFileEmitter(config, zap.NewNop())
	require.NoError(t, err)
	defer emitter.Close()

	assert.Equal(t, 50, emitter.writer.MaxSize)
	assert.Equal(t, 7, emitter.writer.MaxAge)
	assert.Equal(t, 3, emitter.writer.MaxBackups)
	assert.True(t, emitter.writer.Compress)
}

func TestFileEmitter_Emit_WritesToFile(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "access.log")

	config := configtypes.EventFileConfig{
		Enabled:  true,
		Path:     logPath,
		Template: "{request_id} {host} {status_code}",
	}

	emitter, err := NewFileEmitter(config, zap.NewNop())
	require.NoError(t, err)

	event := &RequestEvent{
		RequestID:  "req-123",
		Host:       "example.com",
		StatusCode: 200,
		CreatedAt:  time.Now(),
	}

	emitter.Emit(event)
	err = emitter.Close()
	require.NoError(t, err)

	// Read the file and verify content
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)

	line := strings.TrimSpace(string(content))
	assert.Equal(t, `"req-123" "example.com" 200`, line)
}

func TestFileEmitter_Emit_MultipleLines(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "access.log")

	config := configtypes.EventFileConfig{
		Enabled:  true,
		Path:     logPath,
		Template: "{request_id}",
	}

	emitter, err := NewFileEmitter(config, zap.NewNop())
	require.NoError(t, err)

	events := []*RequestEvent{
		{RequestID: "req-001", CreatedAt: time.Now()},
		{RequestID: "req-002", CreatedAt: time.Now()},
		{RequestID: "req-003", CreatedAt: time.Now()},
	}

	for _, event := range events {
		emitter.Emit(event)
	}
	err = emitter.Close()
	require.NoError(t, err)

	// Read and verify all lines
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, `"req-001"`, lines[0])
	assert.Equal(t, `"req-002"`, lines[1])
	assert.Equal(t, `"req-003"`, lines[2])
}

func TestFileEmitter_Close(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "access.log")

	config := configtypes.EventFileConfig{
		Enabled:  true,
		Path:     logPath,
		Template: "{request_id}",
	}

	emitter, err := NewFileEmitter(config, zap.NewNop())
	require.NoError(t, err)

	// Write something to ensure file is opened
	emitter.Emit(&RequestEvent{RequestID: "test", CreatedAt: time.Now()})

	// Close should not error
	err = emitter.Close()
	assert.NoError(t, err)
}

func TestFileEmitter_ImplementsInterface(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "access.log")

	config := configtypes.EventFileConfig{
		Enabled:  true,
		Path:     logPath,
		Template: "{request_id}",
	}

	emitter, err := NewFileEmitter(config, zap.NewNop())
	require.NoError(t, err)
	defer emitter.Close()

	// Verify it implements EventEmitter
	var _ EventEmitter = emitter
}
