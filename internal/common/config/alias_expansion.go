package config

import (
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/pkg/types"
)

const maxAliasNestingDepth = 1

// ExpandDimensionAliases replaces bot alias references (prefixed with $) in dimension
// match_ua arrays with the actual patterns from BotAliases.
// Supports composite aliases that reference other aliases (single level nesting).
// Returns error with detailed context if an alias is referenced but not found.
func ExpandDimensionAliases(dimensions map[string]types.Dimension, configPath string, logger *zap.Logger) error {
	if len(dimensions) == 0 {
		return nil
	}

	for dimensionName, dimension := range dimensions {
		if len(dimension.MatchUA) == 0 {
			continue
		}

		context := fmt.Sprintf("dimension %q at %s", dimensionName, configPath)
		expandedPatterns, err := expandPatternsWithNesting(dimension.MatchUA, 0, context)
		if err != nil {
			return err
		}

		// Log expansion for top-level aliases only
		/*for _, pattern := range dimension.MatchUA {
			if strings.HasPrefix(pattern, "$") {
				aliasName := strings.TrimPrefix(pattern, "$")
				if aliasPatterns, exists := GetBotAlias(aliasName); exists {
					// Count actual patterns after potential nested expansion
					actualCount := countExpandedPatterns(aliasPatterns)
					logger.Debug("Expanded bot alias",
						zap.String("alias", aliasName),
						zap.Int("pattern_count", actualCount),
						zap.String("dimension", dimensionName),
						zap.String("config_path", configPath))
				}
			}
		}*/

		dimension.MatchUA = expandedPatterns
		dimensions[dimensionName] = dimension
	}

	return nil
}

// countExpandedPatterns returns the count of final patterns after nested expansion
func countExpandedPatterns(patterns []string) int {
	count := 0
	for _, p := range patterns {
		if strings.HasPrefix(p, "$") {
			aliasName := strings.TrimPrefix(p, "$")
			if nestedPatterns, exists := GetBotAlias(aliasName); exists {
				count += len(nestedPatterns)
			} else {
				count++ // Unknown alias counts as 1
			}
		} else {
			count++
		}
	}
	return count
}

// ExpandBotAliases expands bot alias references (prefixed with $) in pattern array
// to their underlying patterns from BotAliases map.
// Supports composite aliases that reference other aliases (single level nesting).
// Returns expanded pattern array and error if unknown alias encountered.
// Collects ALL unknown aliases before returning error for better UX.
func ExpandBotAliases(patterns []string, context string) ([]string, error) {
	return expandPatternsWithNesting(patterns, 0, context)
}

// expandPatternsWithNesting expands patterns that may contain alias references.
// Supports single level of nesting (composite aliases referencing base aliases).
func expandPatternsWithNesting(patterns []string, depth int, context string) ([]string, error) {
	if len(patterns) == 0 {
		return patterns, nil
	}

	if depth > maxAliasNestingDepth {
		return nil, fmt.Errorf("alias nesting exceeds maximum depth of %d in %s", maxAliasNestingDepth, context)
	}

	expandedPatterns := []string{}
	unknownAliases := []string{}

	for _, pattern := range patterns {
		if strings.HasPrefix(pattern, "$") {
			aliasName := strings.TrimPrefix(pattern, "$")

			aliasPatterns, exists := GetBotAlias(aliasName)
			if !exists {
				unknownAliases = append(unknownAliases, pattern)
				continue
			}

			// Check if alias contains nested references (composite alias)
			hasNestedRefs := containsAliasReferences(aliasPatterns)

			if hasNestedRefs {
				nestedExpanded, err := expandPatternsWithNesting(aliasPatterns, depth+1, context)
				if err != nil {
					return nil, err
				}
				expandedPatterns = append(expandedPatterns, nestedExpanded...)
			} else {
				expandedPatterns = append(expandedPatterns, aliasPatterns...)
			}
		} else {
			expandedPatterns = append(expandedPatterns, pattern)
		}
	}

	if len(unknownAliases) > 0 {
		return nil, buildUnknownAliasesError(unknownAliases, context)
	}

	return expandedPatterns, nil
}

// containsAliasReferences checks if any pattern in the slice starts with $
func containsAliasReferences(patterns []string) bool {
	for _, p := range patterns {
		if strings.HasPrefix(p, "$") {
			return true
		}
	}
	return false
}

// buildUnknownAliasesError creates error message for multiple unknown aliases
func buildUnknownAliasesError(unknownAliases []string, context string) error {
	availableAliases := GetAvailableAliases()

	var aliasesStr string
	if len(unknownAliases) == 1 {
		aliasesStr = fmt.Sprintf("unknown bot alias %q", unknownAliases[0])
	} else {
		quotedAliases := make([]string, len(unknownAliases))
		for i, alias := range unknownAliases {
			quotedAliases[i] = fmt.Sprintf("%q", alias)
		}
		aliasesStr = fmt.Sprintf("unknown bot aliases %s", strings.Join(quotedAliases, ", "))
	}

	var hint string
	if len(availableAliases) == 0 {
		hint = "\n\nNo bot aliases are currently defined"
	} else {
		const maxDisplayed = 5
		displayed := availableAliases
		remaining := 0

		if len(availableAliases) > maxDisplayed {
			displayed = availableAliases[:maxDisplayed]
			remaining = len(availableAliases) - maxDisplayed
		}

		aliasesWithPrefix := make([]string, len(displayed))
		for i, alias := range displayed {
			aliasesWithPrefix[i] = "$" + alias
		}

		hint = "\n\nAvailable aliases: " + strings.Join(aliasesWithPrefix, ", ")
		if remaining > 0 {
			hint += fmt.Sprintf(" ... and %d more", remaining)
		}
	}

	return fmt.Errorf("%s at %s%s", aliasesStr, context, hint)
}
