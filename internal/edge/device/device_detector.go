package device

import (
	"sort"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/pkg/pattern"
)

// DeviceDetector handles user agent analysis and dimension detection
type DeviceDetector struct{}

// NewDeviceDetector creates a new DeviceDetector instance
func NewDeviceDetector() *DeviceDetector {
	return &DeviceDetector{}
}

// DetectDimension analyzes User-Agent and returns the dimension name and whether it was matched
// Returns: (dimension name, true if matched a pattern)
func (dd *DeviceDetector) DetectDimension(renderCtx *edgectx.RenderContext) (string, bool) {
	userAgent := string(renderCtx.HTTPCtx.UserAgent())

	// Collect all patterns with their compiled pattern objects
	type patternInfo struct {
		patternStr      string
		compiledPattern *pattern.Pattern
		dimension       string
	}

	var allPatterns []patternInfo
	for dimensionName, dimension := range renderCtx.Host.Render.Dimensions {
		for i := range dimension.MatchUA {
			info := patternInfo{
				patternStr:      dimension.MatchUA[i],
				compiledPattern: dimension.CompiledPatterns[i],
				dimension:       dimensionName,
			}

			allPatterns = append(allPatterns, info)
		}
	}

	// Sort by pattern type (Regexp > Exact > Wildcard), then by pattern length (descending)
	sort.Slice(allPatterns, func(i, j int) bool {
		iType := allPatterns[i].compiledPattern.Type
		jType := allPatterns[j].compiledPattern.Type

		// Assign priority: Regexp=3, Exact=2, Wildcard=1
		iPriority := getPatternPriority(iType)
		jPriority := getPatternPriority(jType)

		if iPriority != jPriority {
			return iPriority > jPriority
		}

		// Same type, sort by length (longest first)
		return len(allPatterns[i].patternStr) > len(allPatterns[j].patternStr)
	})

	// Check patterns in order of specificity
	for _, info := range allPatterns {
		matched := info.compiledPattern.Match(userAgent)

		if matched {
			renderCtx.Logger.Debug("Detected dimension from User-Agent",
				zap.String("user_agent", userAgent),
				zap.String("pattern", info.patternStr),
				zap.String("dimension", info.dimension))
			return info.dimension, true
		}
	}

	// No pattern matched - server will handle using UnmatchedDimension config
	renderCtx.Logger.Debug("No User-Agent match found",
		zap.String("user_agent", userAgent))

	return "", false
}

// getPatternPriority returns priority for sorting (higher = checked first)
func getPatternPriority(pType pattern.PatternType) int {
	switch pType {
	case pattern.PatternTypeRegexp:
		return 3 // Check regexp first (most specific)
	case pattern.PatternTypeExact:
		return 2 // Then exact matches
	case pattern.PatternTypeWildcard:
		return 1 // Then wildcards (least specific)
	default:
		return 0
	}
}
