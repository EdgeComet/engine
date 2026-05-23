package types

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RecacheLimitConfig configures per-host recache concurrency limits.
// Controls the maximum number of in-flight recache requests the cache daemon
// will dispatch for a single host at any time.
type RecacheLimitConfig struct {
	MaxConcurrent int `yaml:"max_concurrent,omitempty" json:"max_concurrent,omitempty"`
}

// HeadersConfig defines safe request and response headers configuration.
// Supports both replacement (safe_*) and additive (safe_*_add) directives.
// At each config level, only ONE of safe_request/safe_request_add can be used (same for response).
type HeadersConfig struct {
	// SafeRequest replaces parent's request headers list
	SafeRequest []string `yaml:"safe_request,omitempty" json:"safe_request,omitempty"`
	// SafeRequestAdd adds to parent's request headers list
	SafeRequestAdd []string `yaml:"safe_request_add,omitempty" json:"safe_request_add,omitempty"`
	// SafeResponse replaces parent's response headers list
	SafeResponse []string `yaml:"safe_response,omitempty" json:"safe_response,omitempty"`
	// SafeResponseAdd adds to parent's response headers list
	SafeResponseAdd []string `yaml:"safe_response_add,omitempty" json:"safe_response_add,omitempty"`
}

// ClientIPConfig defines HTTP headers for extracting the client's real IP address.
type ClientIPConfig struct {
	Headers []string `yaml:"headers,omitempty" json:"headers,omitempty"`
}

// Host represents a domain configuration
type Host struct {
	ID                 int                          `yaml:"id" json:"id"`
	Domain             string                       `yaml:"-" json:"-"`
	Domains            []string                     `yaml:"-" json:"domain"`
	RenderKey          string                       `yaml:"render_key" json:"render_key"`
	Enabled            bool                         `yaml:"enabled" json:"enabled"`
	Dimensions         map[string]Dimension         `yaml:"dimensions" json:"dimensions"`
	UnmatchedDimension string                       `yaml:"unmatched_dimension" json:"unmatched_dimension"`
	Render             RenderConfig                 `yaml:"render" json:"render"`
	Bypass             *BypassConfig                `yaml:"bypass,omitempty" json:"bypass,omitempty"`                   // Host-level bypass override (optional, pointer for override detection)
	TrackingParams     *TrackingParamsConfig        `yaml:"tracking_params,omitempty" json:"tracking_params,omitempty"` // Host-level tracking params override
	CacheSharding      *CacheShardingBehaviorConfig `yaml:"cache_sharding,omitempty" json:"cache_sharding,omitempty"`   // Host-level cache sharding override (behavioral settings only)
	BothitRecache      *BothitRecacheConfig         `yaml:"bothit_recache,omitempty" json:"bothit_recache,omitempty"`   // Host-level bot hit recache override
	Headers            *HeadersConfig               `yaml:"headers,omitempty" json:"headers,omitempty"`                 // Host-level headers override
	ClientIP           *ClientIPConfig              `yaml:"client_ip,omitempty" json:"client_ip,omitempty"`             // Host-level client IP override
	Recache            *RecacheLimitConfig          `yaml:"recache,omitempty" json:"recache,omitempty"`                 // Host-level per-origin recache concurrency override
	URLRules           []URLRule                    `yaml:"url_rules,omitempty" json:"url_rules,omitempty"`             // URL pattern rules
}

// UnmarshalYAML implements custom YAML unmarshaling for Host.
// Handles both string and array formats for domain field and strips trailing dots (FQDN normalization).
func (h *Host) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type hostAlias Host
	type hostRaw struct {
		hostAlias `yaml:",inline"`
		Domain    interface{} `yaml:"domain"`
	}

	var raw hostRaw
	if err := unmarshal(&raw); err != nil {
		return err
	}

	*h = Host(raw.hostAlias)

	switch v := raw.Domain.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed != "" {
			h.Domains = []string{strings.TrimSuffix(trimmed, ".")}
		}
	case []interface{}:
		var domains []string
		for _, d := range v {
			if s, ok := d.(string); ok {
				trimmed := strings.TrimSpace(s)
				if trimmed != "" {
					domains = append(domains, strings.TrimSuffix(trimmed, "."))
				}
			}
		}
		h.Domains = domains
	case nil:
		// Domain not specified, leave Domains empty
	default:
		return fmt.Errorf("domain must be a string or array of strings, got %T", raw.Domain)
	}

	if len(h.Domains) > 0 {
		h.Domain = h.Domains[0]
	}

	return nil
}

// MarshalYAML implements yaml.Marshaler for Host.
// Outputs Domains as "domain" field (single string if one domain, array if multiple).
func (h Host) MarshalYAML() (interface{}, error) {
	type hostAlias Host

	result := struct {
		hostAlias `yaml:",inline"`
		Domain    interface{} `yaml:"domain,omitempty"`
	}{
		hostAlias: hostAlias(h),
	}

	switch len(h.Domains) {
	case 0:
		result.Domain = nil
	case 1:
		result.Domain = h.Domains[0]
	default:
		result.Domain = h.Domains
	}

	return result, nil
}

// UnmarshalJSON implements json.Unmarshaler for Host.
// Handles both string and array formats for domain field and strips trailing dots.
func (h *Host) UnmarshalJSON(data []byte) error {
	type hostAlias Host
	type hostRaw struct {
		hostAlias
		Domain json.RawMessage `json:"domain"`
	}

	var raw hostRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*h = Host(raw.hostAlias)

	if len(raw.Domain) == 0 || string(raw.Domain) == "null" {
		return nil
	}

	var single string
	if err := json.Unmarshal(raw.Domain, &single); err == nil {
		trimmed := strings.TrimSpace(single)
		if trimmed != "" {
			h.Domains = []string{strings.TrimSuffix(trimmed, ".")}
		}
	} else {
		var arr []string
		if err := json.Unmarshal(raw.Domain, &arr); err != nil {
			return fmt.Errorf("domain must be a string or array of strings")
		}
		var domains []string
		for _, d := range arr {
			trimmed := strings.TrimSpace(d)
			if trimmed != "" {
				domains = append(domains, strings.TrimSuffix(trimmed, "."))
			}
		}
		h.Domains = domains
	}

	if len(h.Domains) > 0 {
		h.Domain = h.Domains[0]
	}

	return nil
}

// Unmatched dimension behavior constants
const (
	UnmatchedDimensionBlock  = "block"  // Return 403 Forbidden
	UnmatchedDimensionBypass = "bypass" // Fetch from origin (default)
)

// Bypass dimension constants
const (
	BypassDimensionName = "bypass"
	BypassDimensionID   = 0
)
