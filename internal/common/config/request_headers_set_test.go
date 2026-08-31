package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/pkg/types"
)

const testHeadersSetURL = "https://example.com/api/orders"

func buildHeadersSetResolver(global, host map[string]string, rules []types.URLRule) *ConfigResolver {
	var globalHeaders *types.HeadersConfig
	if global != nil {
		globalHeaders = &types.HeadersConfig{RequestHeadersSet: global}
	}

	testHost := buildTestHost()
	if host != nil {
		testHost.Headers = &types.HeadersConfig{RequestHeadersSet: host}
	}
	testHost.URLRules = rules

	return NewConfigResolver(buildTestGlobalRender(), buildTestGlobalBypass(), nil, nil, nil, globalHeaders, "", testHost)
}

func TestResolveHeaders_RequestHeadersSetMerge(t *testing.T) {
	tests := []struct {
		name     string
		global   map[string]string
		host     map[string]string
		rules    []types.URLRule
		expected map[string]string
	}{
		{
			name:     "nothing set anywhere",
			expected: nil,
		},
		{
			name:     "global only",
			global:   map[string]string{"X-Api-Key": "global"},
			expected: map[string]string{"X-Api-Key": "global"},
		},
		{
			name:     "host only",
			host:     map[string]string{"X-Api-Key": "host"},
			expected: map[string]string{"X-Api-Key": "host"},
		},
		{
			name:     "host overrides one global key and inherits another",
			global:   map[string]string{"X-Api-Key": "global", "X-Tenant-ID": "acme"},
			host:     map[string]string{"X-Api-Key": "host"},
			expected: map[string]string{"X-Api-Key": "host", "X-Tenant-ID": "acme"},
		},
		{
			name: "rule overrides one host key and inherits another",
			host: map[string]string{"X-Api-Key": "host", "X-Tenant-ID": "acme"},
			rules: []types.URLRule{
				{
					Match:   "/api/*",
					Action:  types.ActionRender,
					Headers: &types.HeadersConfig{RequestHeadersSet: map[string]string{"X-Api-Key": "rule"}},
				},
			},
			expected: map[string]string{"X-Api-Key": "rule", "X-Tenant-ID": "acme"},
		},
		{
			name: "rule overrides a host key in a different case",
			host: map[string]string{"X-Api-Key": "host"},
			rules: []types.URLRule{
				{
					Match:   "/api/*",
					Action:  types.ActionRender,
					Headers: &types.HeadersConfig{RequestHeadersSet: map[string]string{"x-api-key": "rule"}},
				},
			},
			expected: map[string]string{"x-api-key": "rule"},
		},
		{
			name:   "all three levels merge per key",
			global: map[string]string{"X-Global": "g", "X-Api-Key": "global"},
			host:   map[string]string{"X-Host": "h", "X-Api-Key": "host"},
			rules: []types.URLRule{
				{
					Match:   "/api/*",
					Action:  types.ActionRender,
					Headers: &types.HeadersConfig{RequestHeadersSet: map[string]string{"X-Api-Key": "rule"}},
				},
			},
			expected: map[string]string{"X-Global": "g", "X-Host": "h", "X-Api-Key": "rule"},
		},
		{
			name:   "rule that does not match leaves the host value",
			global: map[string]string{"X-Api-Key": "global"},
			host:   map[string]string{"X-Api-Key": "host"},
			rules: []types.URLRule{
				{
					Match:   "/blog/*",
					Action:  types.ActionRender,
					Headers: &types.HeadersConfig{RequestHeadersSet: map[string]string{"X-Api-Key": "rule"}},
				},
			},
			expected: map[string]string{"X-Api-Key": "host"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := buildHeadersSetResolver(tt.global, tt.host, tt.rules)

			resolved := resolver.ResolveForURL(testHeadersSetURL)
			assert.Equal(t, tt.expected, resolved.RequestHeadersSet)

			renderResolved := resolver.ResolveRenderForURL(testHeadersSetURL)
			assert.Equal(t, tt.expected, renderResolved.RequestHeadersSet,
				"ResolveRenderForURL must resolve headers too")
		})
	}
}

func TestResolveHeaders_SingleLevelReturnedByReference(t *testing.T) {
	globalSet := map[string]string{"X-Api-Key": "global"}
	resolver := buildHeadersSetResolver(globalSet, nil, nil)

	resolved := resolver.ResolveForURL(testHeadersSetURL)
	require.Len(t, resolved.RequestHeadersSet, 1)

	// The single defining level is handed back as-is: no copy, so the map identity is the
	// configuration's own. That is what makes it read-only for every consumer.
	resolved.RequestHeadersSet["X-Injected"] = "boom"
	assert.Len(t, globalSet, 2, "the resolved map is expected to alias the configuration map")

	delete(globalSet, "X-Injected")
	assert.Equal(t, map[string]string{"X-Api-Key": "global"}, globalSet)
}

func TestResolveHeaders_MergedResultDoesNotTouchSourceMaps(t *testing.T) {
	globalSet := map[string]string{"X-Api-Key": "global", "X-Tenant-ID": "acme"}
	hostSet := map[string]string{"X-Api-Key": "host"}
	resolver := buildHeadersSetResolver(globalSet, hostSet, nil)

	resolved := resolver.ResolveForURL(testHeadersSetURL)
	resolved.RequestHeadersSet["X-Injected"] = "boom"

	assert.Equal(t, map[string]string{"X-Api-Key": "global", "X-Tenant-ID": "acme"}, globalSet)
	assert.Equal(t, map[string]string{"X-Api-Key": "host"}, hostSet)
}

func TestApplyRequestHeaders(t *testing.T) {
	tests := []struct {
		name          string
		set           map[string]string
		clientHeaders map[string][]string
		expected      map[string][]string
	}{
		{
			name:          "nothing configured returns input unchanged",
			clientHeaders: map[string][]string{"Authorization": {"Bearer token"}},
			expected:      map[string][]string{"Authorization": {"Bearer token"}},
		},
		{
			name:     "nil client headers",
			set:      map[string]string{"X-Api-Key": "abc123"},
			expected: map[string][]string{"X-Api-Key": {"abc123"}},
		},
		{
			name:          "non-conflicting forwarded header is kept",
			set:           map[string]string{"X-Api-Key": "abc123"},
			clientHeaders: map[string][]string{"Authorization": {"Bearer token"}},
			expected: map[string][]string{
				"Authorization": {"Bearer token"},
				"X-Api-Key":     {"abc123"},
			},
		},
		{
			name:          "forwarded header differing only in case is replaced",
			set:           map[string]string{"X-Api-Key": "abc123"},
			clientHeaders: map[string][]string{"x-api-key": {"forwarded"}},
			expected:      map[string][]string{"X-Api-Key": {"abc123"}},
		},
		{
			name:          "forwarded multi-value header is replaced by the single configured value",
			set:           map[string]string{"X-Api-Key": "abc123"},
			clientHeaders: map[string][]string{"X-API-KEY": {"one", "two", "three"}},
			expected:      map[string][]string{"X-Api-Key": {"abc123"}},
		},
		{
			name:          "empty client headers map",
			set:           map[string]string{"X-Api-Key": "abc123"},
			clientHeaders: map[string][]string{},
			expected:      map[string][]string{"X-Api-Key": {"abc123"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := &ResolvedConfig{RequestHeadersSet: tt.set}
			assert.Equal(t, tt.expected, rc.ApplyRequestHeaders(tt.clientHeaders))
		})
	}
}

func TestApplyRequestHeaders_NilInputWithNothingConfigured(t *testing.T) {
	rc := &ResolvedConfig{}
	assert.Nil(t, rc.ApplyRequestHeaders(nil))
}

func TestApplyRequestHeaders_DoesNotMutateInputs(t *testing.T) {
	set := map[string]string{"X-Api-Key": "abc123"}
	clientHeaders := map[string][]string{
		"x-api-key":     {"forwarded"},
		"Authorization": {"Bearer token"},
	}
	rc := &ResolvedConfig{RequestHeadersSet: set}

	result := rc.ApplyRequestHeaders(clientHeaders)

	assert.Equal(t, map[string]string{"X-Api-Key": "abc123"}, set)
	assert.Equal(t, map[string][]string{
		"x-api-key":     {"forwarded"},
		"Authorization": {"Bearer token"},
	}, clientHeaders)

	// Writing into the result must not reach either input.
	result["X-Injected"] = []string{"boom"}
	assert.NotContains(t, clientHeaders, "X-Injected")
	assert.NotContains(t, set, "X-Injected")
}
