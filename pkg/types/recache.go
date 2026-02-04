package types

// RecacheMember represents a ZSET member for recache queues
type RecacheMember struct {
	URL         string `json:"url"`          // Normalized URL
	DimensionID int    `json:"dimension_id"` // Integer dimension ID (1, 2, 3...)
}

// RecacheAPIRequest is the request body for POST /internal/cache/recache
type RecacheAPIRequest struct {
	HostID       int      `json:"host_id"`       // Host identifier from hosts.yaml
	URLs         []string `json:"urls"`          // URLs to recache (1-10000)
	DimensionIDs []int    `json:"dimension_ids"` // Dimension IDs (optional, empty = all)
	Priority     string   `json:"priority"`      // "high" or "normal"
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
