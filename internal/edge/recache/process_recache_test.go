package recache

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/pkg/types"
)

func validationTestService() *RecacheService {
	return &RecacheService{
		logger: zap.NewNop(),
		configManager: &mockEGConfigManager{
			hosts: []types.Host{
				{
					ID:      1,
					Domain:  "example.com",
					Domains: []string{"example.com"},
					Dimensions: map[string]types.Dimension{
						"desktop": {ID: 0},
					},
				},
			},
		},
	}
}

// Rejected requests must not burn the daemon's three retries: only the host-not-found case is
// worth retrying, because a cluster move reaches the EGs asynchronously.
func TestProcessRecache_RequestValidationIsClassified(t *testing.T) {
	tests := []struct {
		name          string
		url           string
		hostID        int
		dimensionID   int
		mode          string
		wantPermanent bool
		wantMessage   string
	}{
		{
			name: "host not found is retryable", url: "https://example.com/page",
			hostID: 99, dimensionID: 0, wantPermanent: false, wantMessage: "host not found",
		},
		{
			name: "dimension not found is permanent", url: "https://example.com/page",
			hostID: 1, dimensionID: 7, wantPermanent: true, wantMessage: "dimension 7 not found",
		},
		{
			name: "unparseable url is permanent", url: "http://exa mple.com/\x7f",
			hostID: 1, dimensionID: 0, wantPermanent: true, wantMessage: "failed to parse recache URL",
		},
		{
			name: "private ip is permanent", url: "http://127.0.0.1/page",
			hostID: 1, dimensionID: 0, wantPermanent: true, wantMessage: "SSRF protection",
		},
		{
			name: "domain mismatch is permanent", url: "https://not-configured.com/page",
			hostID: 1, dimensionID: 0, wantPermanent: true, wantMessage: "does not match any configured domain",
		},
		{
			name: "mode render without resolved render config is permanent", url: "https://example.com/page",
			hostID: 1, dimensionID: 0, mode: types.RecacheModeRender, wantPermanent: true,
			wantMessage: "mode:render unsupported",
		},
	}

	rs := validationTestService()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := rs.ProcessRecache(context.Background(), tt.url, tt.hostID, tt.dimensionID, tt.mode)

			require.Error(t, err)
			assert.False(t, errors.Is(err, ErrRecacheSkipped), "a rejected request is a failure, not a config decline")

			var classified *recacheError
			require.True(t, errors.As(err, &classified), "every terminal failure must be classified, got: %v", err)
			assert.Equal(t, types.ErrorTypeInvalidRequest, classified.errorType)
			assert.Equal(t, noOriginStatus, classified.statusCode)
			assert.Equal(t, tt.wantPermanent, classified.permanent)
			assert.Contains(t, classified.Error(), tt.wantMessage, "the failure must come from the branch under test")
		})
	}
}
