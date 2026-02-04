package bot

import (
	"github.com/edgecomet/engine/internal/common/config"
)

// IsBotRequest checks if User-Agent matches any bot patterns
// Supports 4 pattern types: exact, wildcard, case-sensitive regexp (~), case-insensitive regexp (~*)
// Returns true if User-Agent matches any pattern (first match wins)
func IsBotRequest(userAgent string, cfg *config.ResolvedBothitRecache) bool {
	if len(cfg.CompiledPatterns) == 0 {
		return false
	}

	for _, compiledPattern := range cfg.CompiledPatterns {
		if compiledPattern.Match(userAgent) {
			return true
		}
	}

	return false
}
