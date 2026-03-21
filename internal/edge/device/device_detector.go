package device

import (
	"sort"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/pkg/pattern"
	"github.com/edgecomet/engine/pkg/types"
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
		dimensionID     int
		isBlock         bool
		literalLen      int
	}

	var allPatterns []patternInfo
	for dimensionName, dimension := range renderCtx.Host.Dimensions {
		isBlock := dimension.Action == types.ActionBlock
		for i := range dimension.MatchUA {
			info := patternInfo{
				patternStr:      dimension.MatchUA[i],
				compiledPattern: dimension.CompiledPatterns[i],
				dimension:       dimensionName,
				dimensionID:     dimension.ID,
				isBlock:         isBlock,
				literalLen:      countLiteralChars(dimension.MatchUA[i]),
			}

			allPatterns = append(allPatterns, info)
		}
	}

	// Sort by specificity aligned with URL rule approach:
	// 1. Block dimensions first
	// 2. Pattern type: Exact > Wildcard > Regexp
	// 3. Literal character count (more = more specific)
	// 4. Dimension ID (deterministic tie-break)
	sort.Slice(allPatterns, func(i, j int) bool {
		// Block dimensions always come first
		if allPatterns[i].isBlock != allPatterns[j].isBlock {
			return allPatterns[i].isBlock
		}

		iType := allPatterns[i].compiledPattern.Type
		jType := allPatterns[j].compiledPattern.Type

		if iType != jType {
			return pattern.TypePriority(iType) > pattern.TypePriority(jType)
		}

		// More literal characters = more specific
		if allPatterns[i].literalLen != allPatterns[j].literalLen {
			return allPatterns[i].literalLen > allPatterns[j].literalLen
		}

		// Deterministic tie-break
		return allPatterns[i].dimensionID < allPatterns[j].dimensionID
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

// countLiteralChars counts non-wildcard characters in a pattern string
func countLiteralChars(s string) int {
	count := 0
	for _, c := range s {
		if c != '*' {
			count++
		}
	}
	return count
}
