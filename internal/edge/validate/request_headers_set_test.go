package validate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/pkg/types"
)

const testHeadersLevel = "host[0] (example.com)"

func TestValidateHeadersConfig_RequestHeadersSetValid(t *testing.T) {
	headers := &types.HeadersConfig{
		SafeRequestAdd: []string{"Authorization"},
		RequestHeadersSet: map[string]string{
			"X-Api-Key":     "abc123",
			"X-Tenant-ID":   "acme",
			"X-Render-From": "edgecomet",
		},
	}

	require.NoError(t, validateHeadersConfig(headers, testHeadersLevel))
}

func TestValidateHeadersConfig_RequestHeadersSetOverCap(t *testing.T) {
	headersSet := make(map[string]string, maxRequestHeadersSet+1)
	for i := 0; i <= maxRequestHeadersSet; i++ {
		headersSet["X-Header-"+string(rune('a'+i))] = "value"
	}

	err := validateHeadersConfig(&types.HeadersConfig{RequestHeadersSet: headersSet}, testHeadersLevel)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request_headers_set: 21 entries exceeds maximum of 20")
}

func TestValidateHeadersConfig_RequestHeadersSetDenyListed(t *testing.T) {
	for name := range requestHeadersDenyList {
		t.Run(name, func(t *testing.T) {
			err := validateHeadersConfig(&types.HeadersConfig{
				RequestHeadersSet: map[string]string{name: "value"},
			}, testHeadersLevel)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "request_headers_set["+name+"]")
			assert.Contains(t, err.Error(), "blocked for security reasons")
		})
	}
}

func TestValidateHeadersConfig_RequestHeadersSetDeniedPrefix(t *testing.T) {
	for _, prefix := range requestHeadersDenyListPrefixes {
		name := prefix + "authorization"
		t.Run(name, func(t *testing.T) {
			err := validateHeadersConfig(&types.HeadersConfig{
				RequestHeadersSet: map[string]string{name: "value"},
			}, testHeadersLevel)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "request_headers_set["+name+"]")
			assert.Contains(t, err.Error(), "prefix")
		})
	}
}

func TestValidateHeadersConfig_RequestHeadersSetReserved(t *testing.T) {
	// Reserved names must be refused in any spelling, including the canonical one.
	names := []string{
		types.HeaderRenderKey,
		strings.ToLower(types.HeaderRenderKey),
		types.HeaderEdgeRender,
		headerUserAgent,
		strings.ToUpper(headerUserAgent),
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			err := validateHeadersConfig(&types.HeadersConfig{
				RequestHeadersSet: map[string]string{name: "value"},
			}, testHeadersLevel)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "is reserved and set by the engine")
		})
	}
}

func TestValidateHeadersConfig_RequestHeadersSetCaseDuplicate(t *testing.T) {
	err := validateHeadersConfig(&types.HeadersConfig{
		RequestHeadersSet: map[string]string{
			"X-Api-Key": "abc123",
			"x-api-key": "def456",
		},
	}, testHeadersLevel)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "differ only in case")
	assert.Contains(t, err.Error(), `"X-Api-Key"`)
	assert.Contains(t, err.Error(), `"x-api-key"`)
}

func TestValidateHeadersConfig_RequestHeadersSetValues(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		expectedErr string
	}{
		{
			name:        "empty value",
			value:       "",
			expectedErr: "value cannot be empty",
		},
		{
			name:        "over-long value",
			value:       strings.Repeat("a", maxRequestHeaderValueLength+1),
			expectedErr: "value is 2001 bytes, maximum is 2000",
		},
		{
			name:        "carriage return and line feed",
			value:       "abc\r\nX-Injected: yes",
			expectedErr: "value contains control character 0x0d at position 3",
		},
		{
			name:        "bare control byte",
			value:       "abc\x00def",
			expectedErr: "value contains control character 0x00 at position 3",
		},
		{
			name:        "delete byte",
			value:       "abc\x7f",
			expectedErr: "value contains control character 0x7f at position 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateHeadersConfig(&types.HeadersConfig{
				RequestHeadersSet: map[string]string{"X-Api-Key": tt.value},
			}, testHeadersLevel)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "host[0] (example.com) request_headers_set[X-Api-Key]: ")
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

func TestValidateHeadersConfig_RequestHeadersSetAcceptsMultiByteValue(t *testing.T) {
	// Every byte of a valid multi-byte sequence sits above the control range.
	err := validateHeadersConfig(&types.HeadersConfig{
		RequestHeadersSet: map[string]string{"X-Tenant-Name": "Ünïcodé"},
	}, testHeadersLevel)

	require.NoError(t, err)
}

func TestValidateHeadersConfigGlobal_RequestHeadersSetWarns(t *testing.T) {
	collector := NewErrorCollector()
	cfg := &configtypes.EgConfig{
		Headers: &types.HeadersConfig{
			RequestHeadersSet: map[string]string{
				"X-Api-Key":   "abc123",
				"X-Tenant-ID": "acme",
			},
		},
	}

	validateHeadersConfigGlobal(cfg, "edge-gateway.yaml", collector)

	assert.False(t, collector.HasErrors())
	warnings := collector.Warnings()
	require.Len(t, warnings, 2)
	assert.Contains(t, warnings[0].Message, "global headers.request_headers_set[X-Api-Key]")
	assert.Contains(t, warnings[1].Message, "global headers.request_headers_set[X-Tenant-ID]")
}

func TestValidateHeadersConfigGlobal_NoRequestHeadersSetNoWarning(t *testing.T) {
	collector := NewErrorCollector()
	cfg := &configtypes.EgConfig{
		Headers: &types.HeadersConfig{SafeRequestAdd: []string{"Authorization"}},
	}

	validateHeadersConfigGlobal(cfg, "edge-gateway.yaml", collector)

	assert.False(t, collector.HasErrors())
	assert.Empty(t, collector.Warnings())
}

func TestValidateHostHeaders(t *testing.T) {
	t.Run("valid host and rules", func(t *testing.T) {
		host := &types.Host{
			Domain:  "example.com",
			Headers: &types.HeadersConfig{RequestHeadersSet: map[string]string{"X-Api-Key": "abc123"}},
			URLRules: []types.URLRule{
				{Match: "/blog/*"},
				{Match: "/api/*", Headers: &types.HeadersConfig{RequestHeadersSet: map[string]string{"X-Api-Key": "def456"}}},
			},
		}
		require.NoError(t, ValidateHostHeaders(host))
	})

	t.Run("invalid host block", func(t *testing.T) {
		host := &types.Host{
			Domain:  "example.com",
			Headers: &types.HeadersConfig{RequestHeadersSet: map[string]string{types.HeaderRenderKey: "abc123"}},
		}
		err := ValidateHostHeaders(host)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "headers request_headers_set["+types.HeaderRenderKey+"]")
	})

	t.Run("invalid rule block names its index", func(t *testing.T) {
		host := &types.Host{
			Domain: "example.com",
			URLRules: []types.URLRule{
				{Match: "/blog/*"},
				{Match: "/api/*", Headers: &types.HeadersConfig{RequestHeadersSet: map[string]string{"X-Api-Key": ""}}},
			},
		}
		err := ValidateHostHeaders(host)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "url_rules[1].headers request_headers_set[X-Api-Key]")
	})

	t.Run("nil host", func(t *testing.T) {
		require.NoError(t, ValidateHostHeaders(nil))
	})
}

func TestValidateHeaders_ExportedWrapper(t *testing.T) {
	require.NoError(t, ValidateHeaders(nil, "headers"))

	err := ValidateHeaders(&types.HeadersConfig{
		RequestHeadersSet: map[string]string{"X-Api-Key": "abc\r\n"},
	}, "url_rules[2].headers")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "url_rules[2].headers request_headers_set[X-Api-Key]")
}

// Accept-Encoding and Range make the origin answer with a body the bypass path cannot use: it
// stores what the origin sends verbatim, without decompressing or reassembling it. Neither the
// forwarding allow-list nor an explicit value may name them.
func TestValidateHeadersConfig_RepresentationChangingHeadersRejected(t *testing.T) {
	for _, name := range []string{"Accept-Encoding", "Range"} {
		t.Run(name, func(t *testing.T) {
			err := validateHeadersConfig(&types.HeadersConfig{SafeRequest: []string{name}}, testHeadersLevel)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "safe_request[0]")

			err = validateHeadersConfig(&types.HeadersConfig{SafeRequestAdd: []string{name}}, testHeadersLevel)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "safe_request_add[0]")

			err = validateHeadersConfig(&types.HeadersConfig{
				RequestHeadersSet: map[string]string{name: "value"},
			}, testHeadersLevel)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "request_headers_set["+name+"]")
		})
	}
}
