package rsclient

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/pkg/types"
)

// Pool exhaustion, a dead Chrome and a hard timeout are reported as non-200 answers, so the
// structured type has to be recovered from the body or those causes are lost to every caller.
func TestNewServiceError_RecoversStructuredErrorType(t *testing.T) {
	body, err := json.Marshal(types.RenderResponse{
		RequestID: "req-1",
		Success:   false,
		Error:     "Failed to acquire instance: pool exhausted",
		ErrorType: types.ErrorTypePoolUnavailable,
	})
	require.NoError(t, err)

	svcErr := newServiceError(503, body)

	assert.Equal(t, 503, svcErr.HTTPStatus)
	assert.Equal(t, types.ErrorTypePoolUnavailable, svcErr.ErrorType)
	assert.Contains(t, svcErr.Error(), "render service returned status 503")
	assert.Contains(t, svcErr.Error(), "pool exhausted")
}

func TestNewServiceError_NonRenderBodyLeavesErrorTypeEmpty(t *testing.T) {
	svcErr := newServiceError(502, []byte("<html>502 Bad Gateway</html>"))

	assert.Empty(t, svcErr.ErrorType)
	assert.Equal(t, "render service returned status 502: <html>502 Bad Gateway</html>", svcErr.Error())
}
