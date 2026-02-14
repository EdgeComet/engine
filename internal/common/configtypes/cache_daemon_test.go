package configtypes

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/pkg/types"
)

func TestCacheDaemonConfig_Validate(t *testing.T) {
	validConfig := &CacheDaemonConfig{
		EgConfig: "/path/to/edge-gateway.yaml",
		DaemonID: "daemon-1",
		Redis: RedisConfig{
			Addr: "localhost:6379",
			DB:   0,
		},
		Scheduler: CacheDaemonScheduler{
			TickInterval:        types.Duration(1 * time.Second),
			NormalCheckInterval: types.Duration(60 * time.Second),
		},
		InternalQueue: CacheDaemonInternalQueue{
			MaxSize:    1000,
			MaxRetries: 3,
		},
		Recache: CacheDaemonRecache{
			RSCapacityReserved: 0.30,
			TimeoutPerURL:      types.Duration(60 * time.Second),
		},
		HTTPApi: CacheDaemonHTTPApi{
			Enabled:        true,
			Listen:         ":10090",
			RequestTimeout: types.Duration(30 * time.Second),
		},
		Logging: CacheDaemonLogging{
			Level: "info",
			Console: ConsoleLogConfig{
				Enabled: false,
				Format:  "console",
			},
			File: FileLogConfig{
				Enabled: true,
				Path:    "/var/log/daemon.log",
				Format:  "json",
				Rotation: RotationConfig{
					MaxSize:    100,
					MaxAge:     30,
					MaxBackups: 10,
					Compress:   true,
				},
			},
		},
		Metrics: MetricsConfig{
			Enabled:   true,
			Listen:    ":9090",
			Path:      "/metrics",
			Namespace: "edgecomet",
		},
	}

	tests := []struct {
		name    string
		config  *CacheDaemonConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil config is valid",
			config:  nil,
			wantErr: false,
		},
		{
			name:    "valid config",
			config:  validConfig,
			wantErr: false,
		},
		{
			name: "tick_interval < 100ms should fail",
			config: &CacheDaemonConfig{
				EgConfig: "/path/to/edge-gateway.yaml",
				DaemonID: "daemon-1",
				Redis: RedisConfig{
					Addr: "localhost:6379",
					DB:   0,
				},
				Scheduler: CacheDaemonScheduler{
					TickInterval:        types.Duration(50 * time.Millisecond),
					NormalCheckInterval: types.Duration(60 * time.Second),
				},
				InternalQueue: CacheDaemonInternalQueue{
					MaxSize:    1000,
					MaxRetries: 3,
				},
				Recache: CacheDaemonRecache{
					RSCapacityReserved: 0.30,
				},
			},
			wantErr: true,
			errMsg:  "tick_interval must be >= 100ms",
		},
		{
			name: "normal_check_interval not multiple of tick_interval should fail",
			config: &CacheDaemonConfig{
				EgConfig: "/path/to/edge-gateway.yaml",
				DaemonID: "daemon-1",
				Redis: RedisConfig{
					Addr: "localhost:6379",
					DB:   0,
				},
				Scheduler: CacheDaemonScheduler{
					TickInterval:        types.Duration(3 * time.Second),
					NormalCheckInterval: types.Duration(10 * time.Second), // 10 % 3 != 0
				},
				InternalQueue: CacheDaemonInternalQueue{
					MaxSize:    1000,
					MaxRetries: 3,
				},
				Recache: CacheDaemonRecache{
					RSCapacityReserved: 0.30,
				},
			},
			wantErr: true,
			errMsg:  "must be a multiple of tick_interval",
		},
		{
			name: "max_size <= 0 should fail",
			config: &CacheDaemonConfig{
				EgConfig: "/path/to/edge-gateway.yaml",
				DaemonID: "daemon-1",
				Redis: RedisConfig{
					Addr: "localhost:6379",
					DB:   0,
				},
				Scheduler: CacheDaemonScheduler{
					TickInterval:        types.Duration(1 * time.Second),
					NormalCheckInterval: types.Duration(60 * time.Second),
				},
				InternalQueue: CacheDaemonInternalQueue{
					MaxSize:    0,
					MaxRetries: 3,
				},
				Recache: CacheDaemonRecache{
					RSCapacityReserved: 0.30,
				},
			},
			wantErr: true,
			errMsg:  "max_size must be > 0",
		},
		{
			name: "max_retries < 1 should fail",
			config: &CacheDaemonConfig{
				EgConfig: "/path/to/edge-gateway.yaml",
				DaemonID: "daemon-1",
				Redis: RedisConfig{
					Addr: "localhost:6379",
					DB:   0,
				},
				Scheduler: CacheDaemonScheduler{
					TickInterval:        types.Duration(1 * time.Second),
					NormalCheckInterval: types.Duration(60 * time.Second),
				},
				InternalQueue: CacheDaemonInternalQueue{
					MaxSize:    1000,
					MaxRetries: 0,
				},
				Recache: CacheDaemonRecache{
					RSCapacityReserved: 0.30,
				},
			},
			wantErr: true,
			errMsg:  "max_retries must be >= 1",
		},
		{
			name: "rs_capacity_reserved < 0.0 should fail",
			config: &CacheDaemonConfig{
				EgConfig: "/path/to/edge-gateway.yaml",
				DaemonID: "daemon-1",
				Redis: RedisConfig{
					Addr: "localhost:6379",
					DB:   0,
				},
				Scheduler: CacheDaemonScheduler{
					TickInterval:        types.Duration(1 * time.Second),
					NormalCheckInterval: types.Duration(60 * time.Second),
				},
				InternalQueue: CacheDaemonInternalQueue{
					MaxSize:    1000,
					MaxRetries: 3,
				},
				Recache: CacheDaemonRecache{
					RSCapacityReserved: -0.1,
				},
			},
			wantErr: true,
			errMsg:  "rs_capacity_reserved must be between 0.0 and 1.0",
		},
		{
			name: "rs_capacity_reserved > 1.0 should fail",
			config: &CacheDaemonConfig{
				EgConfig: "/path/to/edge-gateway.yaml",
				DaemonID: "daemon-1",
				Redis: RedisConfig{
					Addr: "localhost:6379",
					DB:   0,
				},
				Scheduler: CacheDaemonScheduler{
					TickInterval:        types.Duration(1 * time.Second),
					NormalCheckInterval: types.Duration(60 * time.Second),
				},
				InternalQueue: CacheDaemonInternalQueue{
					MaxSize:    1000,
					MaxRetries: 3,
				},
				Recache: CacheDaemonRecache{
					RSCapacityReserved: 1.1,
				},
			},
			wantErr: true,
			errMsg:  "rs_capacity_reserved must be between 0.0 and 1.0",
		},
		{
			name: "timeout_per_url = 0 should fail",
			config: &CacheDaemonConfig{
				EgConfig: "/path/to/edge-gateway.yaml",
				DaemonID: "daemon-1",
				Redis: RedisConfig{
					Addr: "localhost:6379",
					DB:   0,
				},
				Scheduler: CacheDaemonScheduler{
					TickInterval:        types.Duration(1 * time.Second),
					NormalCheckInterval: types.Duration(60 * time.Second),
				},
				InternalQueue: CacheDaemonInternalQueue{
					MaxSize:    1000,
					MaxRetries: 3,
				},
				Recache: CacheDaemonRecache{
					RSCapacityReserved: 0.30,
					TimeoutPerURL:      0,
				},
			},
			wantErr: true,
			errMsg:  "recache.timeout_per_url must be > 0",
		},
		{
			name: "request_timeout = 0 when http_api enabled should fail",
			config: &CacheDaemonConfig{
				EgConfig: "/path/to/edge-gateway.yaml",
				DaemonID: "daemon-1",
				Redis: RedisConfig{
					Addr: "localhost:6379",
					DB:   0,
				},
				Scheduler: CacheDaemonScheduler{
					TickInterval:        types.Duration(1 * time.Second),
					NormalCheckInterval: types.Duration(60 * time.Second),
				},
				InternalQueue: CacheDaemonInternalQueue{
					MaxSize:    1000,
					MaxRetries: 3,
				},
				Recache: CacheDaemonRecache{
					RSCapacityReserved: 0.30,
					TimeoutPerURL:      types.Duration(60 * time.Second),
				},
				HTTPApi: CacheDaemonHTTPApi{
					Enabled: true,
					Listen:  ":10090",
				},
			},
			wantErr: true,
			errMsg:  "http_api.request_timeout must be > 0 when http_api is enabled",
		},
		{
			name: "invalid port (too low) should fail",
			config: &CacheDaemonConfig{
				EgConfig: "/path/to/edge-gateway.yaml",
				DaemonID: "daemon-1",
				Redis: RedisConfig{
					Addr: "localhost:6379",
					DB:   0,
				},
				Scheduler: CacheDaemonScheduler{
					TickInterval:        types.Duration(1 * time.Second),
					NormalCheckInterval: types.Duration(60 * time.Second),
				},
				InternalQueue: CacheDaemonInternalQueue{
					MaxSize:    1000,
					MaxRetries: 3,
				},
				Recache: CacheDaemonRecache{
					RSCapacityReserved: 0.30,
					TimeoutPerURL:      types.Duration(60 * time.Second),
				},
				HTTPApi: CacheDaemonHTTPApi{
					Enabled: true,
					Listen:  ":0",
				},
			},
			wantErr: true,
			errMsg:  "port must be between 1 and 65535",
		},
		{
			name: "invalid port (too high) should fail",
			config: &CacheDaemonConfig{
				EgConfig: "/path/to/edge-gateway.yaml",
				DaemonID: "daemon-1",
				Redis: RedisConfig{
					Addr: "localhost:6379",
					DB:   0,
				},
				Scheduler: CacheDaemonScheduler{
					TickInterval:        types.Duration(1 * time.Second),
					NormalCheckInterval: types.Duration(60 * time.Second),
				},
				InternalQueue: CacheDaemonInternalQueue{
					MaxSize:    1000,
					MaxRetries: 3,
				},
				Recache: CacheDaemonRecache{
					RSCapacityReserved: 0.30,
					TimeoutPerURL:      types.Duration(60 * time.Second),
				},
				HTTPApi: CacheDaemonHTTPApi{
					Enabled: true,
					Listen:  ":70000",
				},
			},
			wantErr: true,
			errMsg:  "port must be between 1 and 65535",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
