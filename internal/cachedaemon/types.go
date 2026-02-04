package cachedaemon

// StatusResponse is the response for GET /status endpoint
type StatusResponse struct {
	Daemon        DaemonStatus             `json:"daemon"`
	InternalQueue InternalQueueStatus      `json:"internal_queue"`
	RSCapacity    RSCapacityStatus         `json:"rs_capacity"`
	Queues        map[int]HostQueuesStatus `json:"queues"` // Keyed by host_id (int)
}

// DaemonStatus represents daemon health and uptime information
type DaemonStatus struct {
	DaemonID      string `json:"daemon_id"`
	UptimeSeconds int    `json:"uptime_seconds"`
	LastTick      string `json:"last_tick"` // ISO 8601 timestamp
}

// InternalQueueStatus represents the state of the daemon's internal processing queue
type InternalQueueStatus struct {
	Size                int     `json:"size"`
	MaxSize             int     `json:"max_size"`
	CapacityUsedPercent float64 `json:"capacity_used_percent"`
}

// RSCapacityStatus represents render service capacity information
type RSCapacityStatus struct {
	TotalFreeTabs       int     `json:"total_free_tabs"`
	ReservedForOnline   int     `json:"reserved_for_online"`
	AvailableForRecache int     `json:"available_for_recache"`
	ReservationPercent  float64 `json:"reservation_percent"`
}

// HostQueuesStatus represents queue status for a specific host across all priorities
type HostQueuesStatus struct {
	High        QueueStatus `json:"high"`
	Normal      QueueStatus `json:"normal"`
	Autorecache QueueStatus `json:"autorecache"`
}

// QueueStatus represents metrics for a single recache queue
type QueueStatus struct {
	Total  int `json:"total"`   // Total entries in ZSET
	DueNow int `json:"due_now"` // Entries with score <= now
}
