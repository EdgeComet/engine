package validate

import (
	"errors"
	"fmt"
	"strings"

	"github.com/edgecomet/engine/pkg/types"
)

var (
	errMatchUAEmptyList      = errors.New("match_ua must list at least one pattern; omit it to match every client")
	errMatchUATrackingParams = errors.New("tracking_params is not allowed on a rule with match_ua; set it at host level")
)

// ValidateURLRuleMatchUA checks match_ua on every URL rule. It must run on the submitted rule
// list, before PrepareHost sorts and expands it, so the error names the submitted index.
// Store-loaded hosts never pass file validation; this is their only match_ua check.
func ValidateURLRuleMatchUA(host *types.Host) error {
	if host == nil {
		return nil
	}

	for i := range host.URLRules {
		if err := validateMatchUAList(&host.URLRules[i]); err != nil {
			return fmt.Errorf("url_rule[%d] %w", i, err)
		}
	}

	return nil
}

// validateMatchUAList returns the first match_ua problem of one rule; a rule without match_ua passes.
func validateMatchUAList(rule *types.URLRule) error {
	if rule.MatchUA == nil {
		return nil
	}

	if len(rule.MatchUA) == 0 {
		return errMatchUAEmptyList
	}

	for i, p := range rule.MatchUA {
		if p == "" {
			return fmt.Errorf("match_ua[%d] must not be empty", i)
		}
		// Runs after the empty check: "" also trims to "".
		if strings.Trim(p, "*") == "" {
			return fmt.Errorf("match_ua[%d] '%s' matches every client; omit match_ua instead", i, p)
		}
		if err := validatePatternSyntax(p, userAgentPatternContext); err != nil {
			return fmt.Errorf("match_ua[%d]: %w", i, err)
		}
	}

	if rule.TrackingParams != nil {
		return errMatchUATrackingParams
	}

	return nil
}
