package config

import (
	"fmt"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/pkg/types"
	"go.uber.org/zap"
)

// PrepareHost prepares a host by applying global inheritance, expanding aliases,
// compiling patterns, and sorting URL rules.
// globalRender can be nil if no inheritance needed (e.g., standalone tests).
// contextPath is used for error messages (e.g., "hosts.yaml:host_id=1" or "mysql:host_id=1").
func PrepareHost(host *types.Host, globalRender *configtypes.GlobalRenderConfig, contextPath string, logger *zap.Logger) error {
	if host == nil {
		return fmt.Errorf("host cannot be nil")
	}
	if logger == nil {
		return fmt.Errorf("logger is required")
	}

	// Track if dimensions were inherited (already expanded and compiled at global level)
	dimensionsInherited := false

	// Step 1: Apply global inheritance (if globalRender provided)
	if globalRender != nil {
		// Inherit dimensions if host has none and global has some
		if len(host.Render.Dimensions) == 0 && len(globalRender.Dimensions) > 0 {
			host.Render.Dimensions = make(map[string]types.Dimension, len(globalRender.Dimensions))
			for dimName, dim := range globalRender.Dimensions {
				host.Render.Dimensions[dimName] = dim
			}
			dimensionsInherited = true
			logger.Debug("Host inherited global dimensions",
				zap.String("context", contextPath),
				zap.Int("count", len(globalRender.Dimensions)),
			)
		}

		// Inherit events (field-level merge)
		if globalRender.Events.WaitFor != "" {
			inheritedFields := []string{}

			if host.Render.Events.WaitFor == "" {
				host.Render.Events.WaitFor = globalRender.Events.WaitFor
				inheritedFields = append(inheritedFields, "wait_for")
			}

			if host.Render.Events.AdditionalWait == nil && globalRender.Events.AdditionalWait != nil {
				host.Render.Events.AdditionalWait = globalRender.Events.AdditionalWait
				inheritedFields = append(inheritedFields, "additional_wait")
			}

			if len(inheritedFields) > 0 {
				logger.Debug("Host inherited global events fields",
					zap.String("context", contextPath),
					zap.Strings("inherited_fields", inheritedFields),
					zap.String("wait_for", host.Render.Events.WaitFor),
				)
			}
		}
	}

	// Step 2: Expand and compile dimensions (skip if inherited - already done at global level)
	if !dimensionsInherited && host.Render.Dimensions != nil {
		if err := ExpandDimensionAliases(host.Render.Dimensions, contextPath, logger); err != nil {
			return fmt.Errorf("failed to expand dimension aliases: %w", err)
		}

		for dimName, dim := range host.Render.Dimensions {
			if err := dim.CompileMatchUAPatterns(); err != nil {
				return fmt.Errorf("dimension '%s': %w", dimName, err)
			}
			host.Render.Dimensions[dimName] = dim
		}
	}

	// Step 3: Expand bot aliases in host-level bothit_recache
	if host.BothitRecache != nil && len(host.BothitRecache.MatchUA) > 0 {
		expanded, err := ExpandBotAliases(host.BothitRecache.MatchUA, contextPath)
		if err != nil {
			return fmt.Errorf("failed to expand bothit_recache aliases: %w", err)
		}
		host.BothitRecache.MatchUA = expanded
	}

	// Step 4: Compile host-level bothit_recache patterns
	if host.BothitRecache != nil {
		if err := host.BothitRecache.CompileMatchUAPatterns(); err != nil {
			return fmt.Errorf("bothit_recache: %w", err)
		}
	}

	// Step 5: Process URL rules - expand aliases and compile bothit_recache
	for i := range host.URLRules {
		rule := &host.URLRules[i]
		ruleContext := fmt.Sprintf("%s:url_rule[%d]", contextPath, i)

		if rule.BothitRecache != nil && len(rule.BothitRecache.MatchUA) > 0 {
			expanded, err := ExpandBotAliases(rule.BothitRecache.MatchUA, ruleContext)
			if err != nil {
				return fmt.Errorf("url_rule[%d] bothit_recache: %w", i, err)
			}
			rule.BothitRecache.MatchUA = expanded
		}

		if rule.BothitRecache != nil {
			if err := rule.BothitRecache.CompileMatchUAPatterns(); err != nil {
				return fmt.Errorf("url_rule[%d] bothit_recache: %w", i, err)
			}
		}
	}

	// Step 6: Sort URL rules by specificity (includes CompilePatterns)
	if len(host.URLRules) > 0 {
		sorted, err := SortURLRules(host.URLRules)
		if err != nil {
			return fmt.Errorf("failed to sort URL rules: %w", err)
		}
		host.URLRules = sorted
	}

	return nil
}
