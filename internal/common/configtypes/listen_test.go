package configtypes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseListenAddress(t *testing.T) {
	tests := []struct {
		name        string
		listen      string
		wantHost    string
		wantPort    int
		wantErr     bool
		errContains string
	}{
		// Valid cases
		{
			name:     "port only with colon",
			listen:   ":8080",
			wantHost: "",
			wantPort: 8080,
			wantErr:  false,
		},
		{
			name:     "port only without colon",
			listen:   "8080",
			wantHost: "",
			wantPort: 8080,
			wantErr:  false,
		},
		{
			name:     "localhost with port",
			listen:   "localhost:9090",
			wantHost: "localhost",
			wantPort: 9090,
			wantErr:  false,
		},
		{
			name:     "all interfaces",
			listen:   "0.0.0.0:10070",
			wantHost: "0.0.0.0",
			wantPort: 10070,
			wantErr:  false,
		},
		{
			name:     "specific IP",
			listen:   "192.168.1.1:8080",
			wantHost: "192.168.1.1",
			wantPort: 8080,
			wantErr:  false,
		},
		{
			name:     "loopback IP",
			listen:   "127.0.0.1:3000",
			wantHost: "127.0.0.1",
			wantPort: 3000,
			wantErr:  false,
		},
		{
			name:     "min valid port",
			listen:   ":1",
			wantHost: "",
			wantPort: 1,
			wantErr:  false,
		},
		{
			name:     "max valid port",
			listen:   ":65535",
			wantHost: "",
			wantPort: 65535,
			wantErr:  false,
		},

		// Invalid cases
		{
			name:        "empty string",
			listen:      "",
			wantErr:     true,
			errContains: "listen address is empty",
		},
		{
			name:        "invalid format no port",
			listen:      "localhost",
			wantErr:     true,
			errContains: "invalid listen address format",
		},
		{
			name:        "non-numeric port",
			listen:      "localhost:abc",
			wantErr:     true,
			errContains: "invalid port",
		},
		{
			name:        "too many colons",
			listen:      "::8080",
			wantErr:     true,
			errContains: "invalid listen address format",
		},
		{
			name:        "double colon with host",
			listen:      "host:8080:extra",
			wantErr:     true,
			errContains: "invalid listen address format",
		},
		{
			name:        "only colon",
			listen:      ":",
			wantErr:     true,
			errContains: "invalid port",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port, err := ParseListenAddress(tt.listen)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantHost, host)
				assert.Equal(t, tt.wantPort, port)
			}
		})
	}
}

func TestValidateListenAddress(t *testing.T) {
	tests := []struct {
		name        string
		listen      string
		wantErr     bool
		errContains string
	}{
		// Valid cases
		{
			name:    "valid port only",
			listen:  ":8080",
			wantErr: false,
		},
		{
			name:    "valid with host",
			listen:  "localhost:9090",
			wantErr: false,
		},
		{
			name:    "valid all interfaces",
			listen:  "0.0.0.0:10070",
			wantErr: false,
		},
		{
			name:    "min valid port",
			listen:  ":1",
			wantErr: false,
		},
		{
			name:    "max valid port",
			listen:  ":65535",
			wantErr: false,
		},

		// Invalid cases
		{
			name:        "empty string",
			listen:      "",
			wantErr:     true,
			errContains: "listen address is empty",
		},
		{
			name:        "port zero",
			listen:      ":0",
			wantErr:     true,
			errContains: "port must be between 1 and 65535, got 0",
		},
		{
			name:        "negative port",
			listen:      ":-1",
			wantErr:     true,
			errContains: "port must be between 1 and 65535, got -1",
		},
		{
			name:        "port too large",
			listen:      ":65536",
			wantErr:     true,
			errContains: "port must be between 1 and 65535, got 65536",
		},
		{
			name:        "port way too large",
			listen:      ":70000",
			wantErr:     true,
			errContains: "port must be between 1 and 65535, got 70000",
		},
		{
			name:        "invalid format",
			listen:      "invalid",
			wantErr:     true,
			errContains: "invalid listen address format",
		},
		{
			name:        "non-numeric port",
			listen:      ":abc",
			wantErr:     true,
			errContains: "invalid port",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateListenAddress(tt.listen)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGetPortFromListen(t *testing.T) {
	tests := []struct {
		name     string
		listen   string
		wantPort int
		wantErr  bool
	}{
		{
			name:     "port only",
			listen:   ":8080",
			wantPort: 8080,
			wantErr:  false,
		},
		{
			name:     "with host",
			listen:   "localhost:9090",
			wantPort: 9090,
			wantErr:  false,
		},
		{
			name:     "all interfaces",
			listen:   "0.0.0.0:3000",
			wantPort: 3000,
			wantErr:  false,
		},
		{
			name:     "port without colon",
			listen:   "8080",
			wantPort: 8080,
			wantErr:  false,
		},
		{
			name:    "empty string",
			listen:  "",
			wantErr: true,
		},
		{
			name:    "invalid format",
			listen:  "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port, err := GetPortFromListen(tt.listen)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantPort, port)
			}
		})
	}
}

func TestNormalizeListen(t *testing.T) {
	tests := []struct {
		name       string
		listen     string
		wantResult string
		wantErr    bool
	}{
		{
			name:       "port only with colon",
			listen:     ":8080",
			wantResult: ":8080",
			wantErr:    false,
		},
		{
			name:       "port only without colon",
			listen:     "8080",
			wantResult: ":8080",
			wantErr:    false,
		},
		{
			name:       "with host",
			listen:     "localhost:9090",
			wantResult: "localhost:9090",
			wantErr:    false,
		},
		{
			name:       "all interfaces",
			listen:     "0.0.0.0:10070",
			wantResult: "0.0.0.0:10070",
			wantErr:    false,
		},
		{
			name:       "IP address",
			listen:     "192.168.1.1:3000",
			wantResult: "192.168.1.1:3000",
			wantErr:    false,
		},
		{
			name:    "empty string",
			listen:  "",
			wantErr: true,
		},
		{
			name:    "invalid format",
			listen:  "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := NormalizeListen(tt.listen)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantResult, result)
			}
		})
	}
}
