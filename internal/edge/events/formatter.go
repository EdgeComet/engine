package events

import (
	"fmt"
	"strings"
	"time"

	"github.com/edgecomet/engine/pkg/types"
)

// TemplateFormatter formats RequestEvent using a template string
type TemplateFormatter struct {
	template     string
	placeholders []placeholder
}

type placeholder struct {
	raw       string   // e.g., "{metrics.total_requests}"
	fieldPath []string // e.g., ["metrics", "total_requests"]
	start     int
	end       int
}

// validFields contains all known placeholder names
var validFields = map[string]bool{
	"timestamp":                     true,
	"request_id":                    true,
	"host":                          true,
	"host_id":                       true,
	"url":                           true,
	"url_hash":                      true,
	"event_type":                    true,
	"dimension":                     true,
	"user_agent":                    true,
	"client_ip":                     true,
	"matched_rule":                  true,
	"status_code":                   true,
	"page_size":                     true,
	"serve_time":                    true,
	"source":                        true,
	"render_service_id":             true,
	"render_time":                   true,
	"chrome_id":                     true,
	"title":                         true,
	"index_status":                  true,
	"cache_age":                     true,
	"cache_key":                     true,
	"error_type":                    true,
	"error_message":                 true,
	"eg_instance_id":                true,
	"metrics.final_url":             true,
	"metrics.total_requests":        true,
	"metrics.total_bytes":           true,
	"metrics.same_origin_requests":  true,
	"metrics.same_origin_bytes":     true,
	"metrics.third_party_requests":  true,
	"metrics.third_party_bytes":     true,
	"metrics.third_party_domains":   true,
	"metrics.blocked_count":         true,
	"metrics.failed_count":          true,
	"metrics.timed_out":             true,
	"metrics.console_messages":      true,
	"metrics.error_count":           true,
	"metrics.warning_count":         true,
	"metrics.time_to_first_request": true,
	"metrics.time_to_last_response": true,
}

// NewTemplateFormatter parses and validates the template.
// Returns error if any placeholder is unknown or template is empty.
func NewTemplateFormatter(template string) (*TemplateFormatter, error) {
	if template == "" {
		return nil, fmt.Errorf("template cannot be empty")
	}

	placeholders, err := parsePlaceholders(template)
	if err != nil {
		return nil, err
	}

	return &TemplateFormatter{
		template:     template,
		placeholders: placeholders,
	}, nil
}

// parsePlaceholders extracts and validates all placeholders from the template
func parsePlaceholders(template string) ([]placeholder, error) {
	var placeholders []placeholder
	i := 0

	for i < len(template) {
		// Find opening brace
		start := strings.Index(template[i:], "{")
		if start == -1 {
			break
		}
		start += i

		// Find closing brace
		end := strings.Index(template[start:], "}")
		if end == -1 {
			return nil, fmt.Errorf("unclosed placeholder at position %d", start)
		}
		end += start

		// Extract field name (without braces)
		fieldName := template[start+1 : end]
		if fieldName == "" {
			return nil, fmt.Errorf("empty placeholder at position %d", start)
		}

		// Validate field name
		if !validFields[fieldName] {
			return nil, fmt.Errorf("unknown placeholder {%s}", fieldName)
		}

		// Parse field path (e.g., "metrics.total_requests" -> ["metrics", "total_requests"])
		fieldPath := strings.Split(fieldName, ".")

		placeholders = append(placeholders, placeholder{
			raw:       template[start : end+1],
			fieldPath: fieldPath,
			start:     start,
			end:       end + 1,
		})

		i = end + 1
	}

	return placeholders, nil
}

// Template returns the original template string
func (f *TemplateFormatter) Template() string {
	return f.template
}

// Placeholders returns the parsed placeholders (for testing)
func (f *TemplateFormatter) Placeholders() []placeholder {
	return f.placeholders
}

// Format renders the event using the template
func (f *TemplateFormatter) Format(event *RequestEvent) string {
	if len(f.placeholders) == 0 {
		return f.template
	}

	result := f.template
	// Process placeholders in reverse order to maintain correct positions
	for i := len(f.placeholders) - 1; i >= 0; i-- {
		p := f.placeholders[i]
		value := f.getFieldValue(event, p.fieldPath)
		result = result[:p.start] + value + result[p.end:]
	}

	return result
}

// getFieldValue retrieves and formats a field value from the event
func (f *TemplateFormatter) getFieldValue(event *RequestEvent, fieldPath []string) string {
	if len(fieldPath) == 0 {
		return "-"
	}

	// Handle nested metrics fields
	if fieldPath[0] == "metrics" {
		if event.Metrics == nil {
			return "-"
		}
		if len(fieldPath) < 2 {
			return "-"
		}
		return f.getMetricsFieldValue(event.Metrics, fieldPath[1])
	}

	// Handle top-level fields
	return f.getTopLevelFieldValue(event, fieldPath[0])
}

// getTopLevelFieldValue retrieves and formats a top-level field
func (f *TemplateFormatter) getTopLevelFieldValue(event *RequestEvent, field string) string {
	switch field {
	case "timestamp":
		return formatTime(event.CreatedAt)
	case "request_id":
		return formatString(event.RequestID)
	case "host":
		return formatString(event.Host)
	case "host_id":
		return formatInt(event.HostID)
	case "url":
		return formatString(event.URL)
	case "url_hash":
		return formatString(event.URLHash)
	case "event_type":
		return formatString(event.EventType)
	case "dimension":
		return formatString(event.Dimension)
	case "user_agent":
		return formatString(event.UserAgent)
	case "client_ip":
		return formatString(event.ClientIP)
	case "matched_rule":
		return formatString(event.MatchedRule)
	case "status_code":
		return formatInt(event.StatusCode)
	case "page_size":
		return formatInt64(event.PageSize)
	case "serve_time":
		return formatFloat(event.ServeTime)
	case "source":
		return formatString(event.Source)
	case "render_service_id":
		return formatString(event.RenderServiceID)
	case "render_time":
		return formatFloat(event.RenderTime)
	case "chrome_id":
		return formatString(event.ChromeID)
	case "title":
		if event.PageSEO != nil {
			return formatString(event.PageSEO.Title)
		}
		return formatString("")
	case "index_status":
		if event.PageSEO != nil {
			return formatInt(event.PageSEO.IndexStatus)
		}
		return formatInt(0)
	case "cache_age":
		return formatInt(event.CacheAge)
	case "cache_key":
		return formatString(event.CacheKey)
	case "error_type":
		return formatString(event.ErrorType)
	case "error_message":
		return formatString(event.ErrorMessage)
	case "eg_instance_id":
		return formatString(event.EGInstanceID)
	default:
		return "-"
	}
}

// getMetricsFieldValue retrieves and formats a metrics field
func (f *TemplateFormatter) getMetricsFieldValue(metrics *PageMetricsEvent, field string) string {
	switch field {
	case "final_url":
		return formatString(metrics.FinalURL)
	case "total_requests":
		return formatInt(metrics.TotalRequests)
	case "total_bytes":
		return formatInt64(metrics.TotalBytes)
	case "same_origin_requests":
		return formatInt(metrics.SameOriginRequests)
	case "same_origin_bytes":
		return formatInt64(metrics.SameOriginBytes)
	case "third_party_requests":
		return formatInt(metrics.ThirdPartyRequests)
	case "third_party_bytes":
		return formatInt64(metrics.ThirdPartyBytes)
	case "third_party_domains":
		return formatInt(metrics.ThirdPartyDomains)
	case "blocked_count":
		return formatInt(metrics.BlockedCount)
	case "failed_count":
		return formatInt(metrics.FailedCount)
	case "timed_out":
		return formatBool(metrics.TimedOut)
	case "console_messages":
		return formatConsoleMessages(metrics.ConsoleMessages)
	case "error_count":
		return formatInt(metrics.ErrorCount)
	case "warning_count":
		return formatInt(metrics.WarningCount)
	case "time_to_first_request":
		return formatFloat(metrics.TimeToFirstRequest)
	case "time_to_last_response":
		return formatFloat(metrics.TimeToLastResponse)
	default:
		return "-"
	}
}

// formatConsoleMessages formats console messages for log output
func formatConsoleMessages(messages []types.ConsoleError) string {
	if len(messages) == 0 {
		return "-"
	}

	// Format as JSON array for structured logging
	parts := make([]string, len(messages))
	for i, msg := range messages {
		parts[i] = fmt.Sprintf(`{"type":%q,"source_url":%q,"source_location":%q,"message":%q}`,
			msg.Type, msg.SourceURL, msg.SourceLocation, msg.Message)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// escapeString escapes special characters in a string for log output
func escapeString(s string) string {
	escaped := strings.ReplaceAll(s, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
	escaped = strings.ReplaceAll(escaped, "\n", "\\n")
	escaped = strings.ReplaceAll(escaped, "\t", "\\t")
	escaped = strings.ReplaceAll(escaped, "\r", "\\r")
	return escaped
}

// formatString formats a string value with quotes and escaping
func formatString(s string) string {
	if s == "" {
		return "-"
	}
	return "\"" + escapeString(s) + "\""
}

// formatInt formats an integer
func formatInt(i int) string {
	return fmt.Sprintf("%d", i)
}

// formatInt64 formats an int64
func formatInt64(i int64) string {
	return fmt.Sprintf("%d", i)
}

// formatFloat formats a float64 with 3 decimal places
func formatFloat(f float64) string {
	return fmt.Sprintf("%.3f", f)
}

// formatBool formats a boolean
func formatBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// formatTime formats a time in ISO 8601 format
func formatTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}
