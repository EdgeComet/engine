package types

// Recache mode override values for RecacheMember.Mode / RecacheAPIRequest.Mode.
// Empty string means "respect the configured action" (dimension / url-rule).
const (
	RecacheModeRender = "render" // force a Chrome render, store as render cache
	RecacheModeBypass = "bypass" // force an origin fetch, store as bypass cache
)

// Recache outcome values for RecacheOutcomeData.Outcome, the machine-readable result of a
// single-URL recache on the edge gateway's internal API.
const (
	RecacheOutcomeCached  = "cached"  // content was fetched or rendered and written to cache
	RecacheOutcomeSkipped = "skipped" // the resolved configuration declines to cache this URL
	RecacheOutcomeFailed  = "failed"  // the attempt failed; ErrorType names the class
)

// RecacheOutcomeData is the data payload of the edge gateway's single-URL recache response.
// The HTTP status carries retryability (200 terminal-ok, 422 permanent failure, 5xx worth
// retrying) and this payload names the outcome, so the cache daemon decides from a field
// instead of parsing an error string.
type RecacheOutcomeData struct {
	Outcome   string `json:"outcome"`
	Reason    string `json:"reason,omitempty"`     // skipped: which configuration decision declined
	ErrorType string `json:"error_type,omitempty"` // failed: outcome taxonomy value
	// Permanent is meaningful only for RecacheOutcomeFailed. It is always serialized: a
	// retry instruction that can be absent is a retry instruction that gets misread.
	Permanent bool `json:"permanent"`
}

// RecacheMember represents a ZSET member for recache queues
type RecacheMember struct {
	URL         string `json:"url"`            // Normalized URL
	DimensionID int    `json:"dimension_id"`   // Integer dimension ID (1, 2, 3...)
	Mode        string `json:"mode,omitempty"` // Optional action override: render | bypass (empty = respect config)
}

// RecacheAPIRequest is the request body for POST /internal/cache/recache
type RecacheAPIRequest struct {
	HostID       int      `json:"host_id"`        // Host identifier from hosts.yaml
	URLs         []string `json:"urls"`           // URLs to recache (1-10000)
	DimensionIDs []int    `json:"dimension_ids"`  // Dimension IDs (optional, empty = all)
	Priority     string   `json:"priority"`       // "high" or "normal"
	Mode         string   `json:"mode,omitempty"` // Optional action override: render | bypass (empty = respect config)
}

// RecacheAPIData is the data payload for POST /internal/cache/recache response
type RecacheAPIData struct {
	HostID            int    `json:"host_id"`
	URLsCount         int    `json:"urls_count"`
	DimensionIDsCount int    `json:"dimension_ids_count"`
	EntriesEnqueued   int    `json:"entries_enqueued"`
	Priority          string `json:"priority"`
}

// InvalidateAPIRequest is the request body for POST /internal/cache/invalidate
type InvalidateAPIRequest struct {
	HostID       int      `json:"host_id"`
	URLs         []string `json:"urls"`
	DimensionIDs []int    `json:"dimension_ids"` // Optional, empty = all
}

// InvalidateAPIData is the data payload for POST /internal/cache/invalidate response
type InvalidateAPIData struct {
	HostID             int `json:"host_id"`
	URLsCount          int `json:"urls_count"`
	DimensionIDsCount  int `json:"dimension_ids_count"`
	EntriesInvalidated int `json:"entries_invalidated"`
}

// InvalidateAllAPIRequest is the request body for POST /internal/cache/invalidate-all
type InvalidateAllAPIRequest struct {
	HostID       int   `json:"host_id"`
	DimensionIDs []int `json:"dimension_ids"` // Optional, empty = all
}

// InvalidateAllAPIData is the data payload for POST /internal/cache/invalidate-all response
type InvalidateAllAPIData struct {
	HostID             int `json:"host_id"`
	DimensionIDsCount  int `json:"dimension_ids_count"`
	EntriesInvalidated int `json:"entries_invalidated"`
}
