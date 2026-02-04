package configtypes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestEventLoggingConfig_Parse(t *testing.T) {
	tests := []struct {
		name           string
		yaml           string
		wantEnabled    bool
		wantPath       string
		wantTemplate   string
		wantMaxSize    int
		wantMaxAge     int
		wantMaxBackups int
		wantCompress   bool
	}{
		{
			name: "full config",
			yaml: `
event_logging:
  file:
    enabled: true
    path: "/tmp/test-access.log"
    template: "{timestamp} {host} {url}"
    rotation:
      max_size: 100
      max_age: 30
      max_backups: 5
      compress: true
`,
			wantEnabled:    true,
			wantPath:       "/tmp/test-access.log",
			wantTemplate:   "{timestamp} {host} {url}",
			wantMaxSize:    100,
			wantMaxAge:     30,
			wantMaxBackups: 5,
			wantCompress:   true,
		},
		{
			name: "minimal config",
			yaml: `
event_logging:
  file:
    enabled: true
    path: "/var/log/access.log"
    template: "{url}"
`,
			wantEnabled:  true,
			wantPath:     "/var/log/access.log",
			wantTemplate: "{url}",
		},
		{
			name: "disabled config",
			yaml: `
event_logging:
  file:
    enabled: false
`,
			wantEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg EgConfig
			err := yaml.Unmarshal([]byte(tt.yaml), &cfg)
			require.NoError(t, err)
			require.NotNil(t, cfg.EventLogging)

			assert.Equal(t, tt.wantEnabled, cfg.EventLogging.File.Enabled)
			assert.Equal(t, tt.wantPath, cfg.EventLogging.File.Path)
			assert.Equal(t, tt.wantTemplate, cfg.EventLogging.File.Template)
			assert.Equal(t, tt.wantMaxSize, cfg.EventLogging.File.Rotation.MaxSize)
			assert.Equal(t, tt.wantMaxAge, cfg.EventLogging.File.Rotation.MaxAge)
			assert.Equal(t, tt.wantMaxBackups, cfg.EventLogging.File.Rotation.MaxBackups)
			assert.Equal(t, tt.wantCompress, cfg.EventLogging.File.Rotation.Compress)
		})
	}
}

func TestEventLoggingConfig_NilWhenMissing(t *testing.T) {
	yamlStr := `
server:
  listen: ":8080"
`
	var cfg EgConfig
	err := yaml.Unmarshal([]byte(yamlStr), &cfg)
	require.NoError(t, err)
	assert.Nil(t, cfg.EventLogging)
}
