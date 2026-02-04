package configtest

import (
	"fmt"
	"time"

	"github.com/edgecomet/engine/pkg/types"
)

// PrintURLTestResult prints URL test results in the specified format
func PrintURLTestResult(result *URLTestResult) {
	// Check for errors (host not found)
	if result.Error != "" {
		fmt.Printf("\nERROR: %s\n", result.Error)
		return
	}

	// Print header based on URL type
	if result.IsAbsolute {
		fmt.Println() // Blank line separator
	} else {
		fmt.Printf("\nTesting URL: %s\n", result.URL)
		fmt.Printf("Checking across %d hosts...\n", len(result.HostResults))
	}

	// Print results for each host
	for _, hostResult := range result.HostResults {
		printHostTestResult(&hostResult)
	}
}

// printHostTestResult prints test result for a single host
func printHostTestResult(result *HostTestResult) {
	fmt.Printf("\n=== Host: %s (host_id: %d) ===\n", result.Host, result.HostID)
	fmt.Printf("URL: %s\n", result.OriginalURL)
	fmt.Printf("Normalized URL: %s\n", result.NormalizedURL)
	fmt.Printf("URL Hash: %s\n", result.URLHash)
	fmt.Println()

	// Print matched pattern
	if result.MatchedRule != nil {
		patterns := result.MatchedRule.GetMatchPatterns()
		if len(patterns) > 0 {
			fmt.Printf("Matched Pattern: %s\n", patterns[0])
		}
	} else {
		fmt.Println("Matched Pattern: (default)")
	}

	fmt.Printf("Action: %s\n", result.Action)

	// Print action-specific configuration
	switch result.Config.Action {
	case types.ActionRender:
		printRenderConfig(result)
	case types.ActionBypass:
		printBypassConfig(result)
	case types.ActionBlock, types.ActionStatus403, types.ActionStatus404, types.ActionStatus410, types.ActionStatus:
		printStatusConfig(result)
	}
}

// printRenderConfig prints render action configuration
func printRenderConfig(result *HostTestResult) {
	fmt.Println()
	fmt.Printf("Cache TTL: %s (%s)\n", formatDuration(result.Config.Cache.TTL), formatHumanDuration(result.Config.Cache.TTL))
	fmt.Println()
	fmt.Println("Rendering:")
	fmt.Printf("  - Timeout: %s\n", formatDuration(result.Config.Render.Timeout))
	fmt.Printf("  - Wait Until: %s\n", result.Config.Render.Events.WaitFor)

	if result.Config.Render.Events.AdditionalWait != nil {
		fmt.Printf("  - Wait Timeout: %s\n", formatDuration(time.Duration(*result.Config.Render.Events.AdditionalWait)))
	}

	fmt.Println("  - JavaScript: enabled")

	// TODO: Print extra headers if configured
}

// printBypassConfig prints bypass action configuration
func printBypassConfig(result *HostTestResult) {
	// Check if bypass cache is enabled
	if result.Config.Bypass.Cache.Enabled {
		fmt.Println()
		fmt.Println("Bypass Cache: enabled")
		fmt.Printf("Bypass Cache TTL: %s (%s)\n",
			formatDuration(result.Config.Bypass.Cache.TTL),
			formatHumanDuration(result.Config.Bypass.Cache.TTL))

		if len(result.Config.Bypass.Cache.StatusCodes) > 0 {
			fmt.Printf("Cached Status Codes: %v\n", formatStatusCodes(result.Config.Bypass.Cache.StatusCodes))
		}
	}
}

// printStatusConfig prints status action configuration
func printStatusConfig(result *HostTestResult) {
	fmt.Printf("Response: %d %s\n", result.Config.Status.Code, getStatusText(result.Config.Status.Code))

	// Print custom headers if present
	if len(result.Config.Status.Headers) > 0 {
		fmt.Println("Headers:")
		for key, value := range result.Config.Status.Headers {
			fmt.Printf("  - %s: %s\n", key, value)
		}
	}
}

// formatDuration formats a duration in seconds format
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.0fs", d.Seconds())
}

// formatHumanDuration formats a duration in human-readable format
func formatHumanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// formatStatusCodes formats status codes as comma-separated string
func formatStatusCodes(codes []int) string {
	if len(codes) == 0 {
		return ""
	}

	result := fmt.Sprintf("%d", codes[0])
	for i := 1; i < len(codes); i++ {
		result += fmt.Sprintf(", %d", codes[i])
	}
	return result
}

// getStatusText returns the standard text for HTTP status codes
func getStatusText(code int) string {
	switch code {
	case 200:
		return "OK"
	case 301:
		return "Moved Permanently"
	case 302:
		return "Found"
	case 303:
		return "See Other"
	case 307:
		return "Temporary Redirect"
	case 308:
		return "Permanent Redirect"
	case 400:
		return "Bad Request"
	case 401:
		return "Unauthorized"
	case 403:
		return "Forbidden"
	case 404:
		return "Not Found"
	case 410:
		return "Gone"
	case 429:
		return "Too Many Requests"
	case 500:
		return "Internal Server Error"
	case 502:
		return "Bad Gateway"
	case 503:
		return "Service Unavailable"
	case 504:
		return "Gateway Timeout"
	default:
		return ""
	}
}
