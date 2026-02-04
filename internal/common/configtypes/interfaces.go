package configtypes

import "github.com/edgecomet/engine/pkg/types"

// EGConfigManager provides access to Edge Gateway configuration.
// Implementations must be safe for concurrent use.
// Returned pointers are read-only - callers must not modify them.
type EGConfigManager interface {
	// GetConfig returns the main EG configuration (read-only)
	GetConfig() *EgConfig

	// GetHosts returns all configured hosts (read-only slice)
	GetHosts() []types.Host

	// GetHostByDomain returns the host for a domain, or nil if not found.
	// Domain matching is case-insensitive.
	// Returned pointer is read-only - do not modify.
	GetHostByDomain(domain string) *types.Host
}
