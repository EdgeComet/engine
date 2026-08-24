package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsPrerenderWait(t *testing.T) {
	for _, value := range []string{WaitForPrerenderReady, WaitForPrerenderContentReady} {
		assert.True(t, IsPrerenderWait(value), "%s selects the prerender wait", value)
	}

	for _, value := range []string{
		LifecycleEventDOMContentLoaded,
		LifecycleEventLoad,
		LifecycleEventNetworkIdle,
		LifecycleEventNetworkAlmostIdle,
	} {
		assert.False(t, IsPrerenderWait(value), "%s selects the lifecycle wait", value)
	}

	assert.False(t, IsPrerenderWait(""), "an unset wait_for is not a prerender wait")
	assert.False(t, IsPrerenderWait("prerender"), "an unknown value is not a prerender wait")
	assert.False(t, IsPrerenderWait("PrerenderReady"), "the match is exact, mirroring the validator")
}

func TestValidWaitForValues(t *testing.T) {
	// Both readiness values must be here or a host configured with one fails validation, and the
	// order is what the validator's error message reads back.
	assert.Equal(t, []string{
		LifecycleEventDOMContentLoaded,
		LifecycleEventLoad,
		LifecycleEventNetworkIdle,
		LifecycleEventNetworkAlmostIdle,
		WaitForPrerenderReady,
		WaitForPrerenderContentReady,
	}, ValidWaitForValues())

	values := ValidWaitForValues()
	values[0] = "mutated"
	assert.Equal(t, LifecycleEventDOMContentLoaded, ValidWaitForValues()[0], "a caller cannot mutate the list")
}
