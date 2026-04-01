package types

// CacheURLItem represents a single cached URL entry returned by the cache daemon API.
// Shared between OSS cache daemon and enterprise cluster manager.
type CacheURLItem struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Dimension   string `json:"dimension"`
	Status      string `json:"status"`
	CacheAge    int64  `json:"cache_age"`
	Size        int64  `json:"size"`
	DiskSize    int64  `json:"disk_size"`
	LastAccess  int64  `json:"last_access"`
	CacheKey    string `json:"cache_key"`
	CreatedAt   int64  `json:"created_at"`
	ExpiresAt   int64  `json:"expires_at"`
	StatusCode  int    `json:"status_code"`
	Source      string `json:"source"`
	IndexStatus int    `json:"index_status"`
	LastBotHit  *int64 `json:"last_bot_hit,omitempty"`
}
