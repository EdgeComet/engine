package cachedaemon

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/pkg/types"
)

func newLimiter(globalDefault int, overrides map[int]int) *HostConcurrencyLimiter {
	var eg *configtypes.EgConfig
	if globalDefault > 0 {
		eg = &configtypes.EgConfig{
			Recache: &types.RecacheLimitConfig{MaxConcurrent: globalDefault},
		}
	} else {
		eg = &configtypes.EgConfig{}
	}
	hosts := make([]types.Host, 0, len(overrides))
	for hostID, max := range overrides {
		hosts = append(hosts, types.Host{
			ID:      hostID,
			Recache: &types.RecacheLimitConfig{MaxConcurrent: max},
		})
	}
	return NewHostConcurrencyLimiter(eg, hosts)
}

func TestConcurrencyLimiter_AcquireUntilFullThenRelease(t *testing.T) {
	l := newLimiter(3, nil)

	slots := make([]Slot, 0, 3)
	for i := 0; i < 3; i++ {
		s, ok := l.TryAcquire(42)
		require.True(t, ok, "expected acquire %d to succeed", i)
		slots = append(slots, s)
	}

	_, ok := l.TryAcquire(42)
	assert.False(t, ok, "fourth acquire must be denied")

	stats := l.Stats(42)
	assert.Equal(t, int64(3), stats.InFlight)
	assert.EqualValues(t, 3, stats.AcquiredTotal)
	assert.EqualValues(t, 1, stats.DeniedTotal)

	l.Release(slots[0])

	s, ok := l.TryAcquire(42)
	require.True(t, ok, "acquire after one release must succeed")
	l.Release(s)
	for _, s := range slots[1:] {
		l.Release(s)
	}

	stats = l.Stats(42)
	assert.Equal(t, int64(0), stats.InFlight)
}

func TestConcurrencyLimiter_DefaultFallback(t *testing.T) {
	// Empty EG config → DefaultMaxConcurrent.
	l := NewHostConcurrencyLimiter(&configtypes.EgConfig{}, nil)
	for i := 0; i < DefaultMaxConcurrent; i++ {
		_, ok := l.TryAcquire(7)
		require.True(t, ok)
	}
	_, ok := l.TryAcquire(7)
	assert.False(t, ok)
}

func TestConcurrencyLimiter_PerHostOverride(t *testing.T) {
	l := newLimiter(2, map[int]int{99: 5})

	// Host 99 (override=5) accepts 5 acquires.
	for i := 0; i < 5; i++ {
		_, ok := l.TryAcquire(99)
		require.True(t, ok, "host 99 acquire %d must succeed", i)
	}
	_, ok := l.TryAcquire(99)
	assert.False(t, ok)

	// Host 1 (default=2) accepts only 2.
	for i := 0; i < 2; i++ {
		_, ok := l.TryAcquire(1)
		require.True(t, ok)
	}
	_, ok = l.TryAcquire(1)
	assert.False(t, ok)
}

func TestConcurrencyLimiter_ReloadPreservesCountersAndDoesNotBlockOldSlot(t *testing.T) {
	l := newLimiter(10, nil)

	s, ok := l.TryAcquire(5)
	require.True(t, ok)

	preReload := l.Stats(5)
	require.EqualValues(t, 1, preReload.AcquiredTotal)

	// Reload to a smaller capacity. The old channel is replaced for new acquires
	// but the slot we hold still references the old channel.
	eg := &configtypes.EgConfig{Recache: &types.RecacheLimitConfig{MaxConcurrent: 3}}
	l.Reload(eg, nil)

	postReloadStats := l.Stats(5)
	assert.EqualValues(t, 1, postReloadStats.AcquiredTotal, "acquired_total must persist across reload")
	assert.Equal(t, 3, postReloadStats.MaxConcurrent, "max_concurrent must reflect new config")

	// Releasing the pre-reload slot must not block — it operates on the old channel.
	done := make(chan struct{})
	go func() {
		l.Release(s)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Release on pre-reload Slot blocked")
	}

	finalStats := l.Stats(5)
	// Pre-reload Slot's release decremented the counter on the old hostCounters,
	// which is the same counter we still track (counters are preserved on reload).
	assert.Equal(t, int64(0), finalStats.InFlight)

	// New acquires use the new (smaller) channel.
	for i := 0; i < 3; i++ {
		_, ok := l.TryAcquire(5)
		require.True(t, ok, "acquire %d on reloaded host must succeed", i)
	}
	_, ok = l.TryAcquire(5)
	assert.False(t, ok)
}

func TestConcurrencyLimiter_ReloadIntroducesOverride(t *testing.T) {
	l := newLimiter(2, nil)

	for i := 0; i < 2; i++ {
		_, ok := l.TryAcquire(11)
		require.True(t, ok)
	}
	_, ok := l.TryAcquire(11)
	assert.False(t, ok)

	// Reload introducing a per-host override of 4.
	eg := &configtypes.EgConfig{Recache: &types.RecacheLimitConfig{MaxConcurrent: 2}}
	hosts := []types.Host{{ID: 11, Recache: &types.RecacheLimitConfig{MaxConcurrent: 4}}}
	l.Reload(eg, hosts)

	// New channel has capacity 4 and is empty (the 2 in-flight slots are on the old channel).
	for i := 0; i < 4; i++ {
		_, ok := l.TryAcquire(11)
		require.True(t, ok, "acquire %d after reload override must succeed", i)
	}
	_, ok = l.TryAcquire(11)
	assert.False(t, ok)
}

func TestConcurrencyLimiter_ZeroSlotReleaseIsNoOp(t *testing.T) {
	l := newLimiter(2, nil)
	require.NotPanics(t, func() {
		l.Release(Slot{})
	})
	stats := l.Stats(1)
	assert.Equal(t, int64(0), stats.InFlight)
}

func TestConcurrencyLimiter_DoubleReleaseIsIdempotent(t *testing.T) {
	l := newLimiter(2, nil)

	s, ok := l.TryAcquire(7)
	require.True(t, ok)

	// First Release drains the captured channel, decrements inFlight.
	l.Release(s)
	assert.Equal(t, int64(0), l.Stats(7).InFlight)

	// Second Release on the same Slot must be a no-op — must not block,
	// must not drive inFlight negative.
	done := make(chan struct{})
	go func() {
		l.Release(s)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("second Release blocked")
	}
	assert.Equal(t, int64(0), l.Stats(7).InFlight, "inFlight must not go negative on double Release")

	// A copy of the released Slot must also be a safe no-op.
	dup := s
	require.NotPanics(t, func() { l.Release(dup) })
	assert.Equal(t, int64(0), l.Stats(7).InFlight)

	// Subsequent legitimate acquires continue to work.
	s2, ok := l.TryAcquire(7)
	require.True(t, ok)
	l.Release(s2)
}

func TestConcurrencyLimiter_ConcurrentAcquireRelease(t *testing.T) {
	const goroutines = 32
	const ops = 200

	l := newLimiter(8, nil)

	var wg sync.WaitGroup
	var deniedCount int64

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				slot, ok := l.TryAcquire(7)
				if !ok {
					atomic.AddInt64(&deniedCount, 1)
					continue
				}
				l.Release(slot)
			}
		}()
	}
	wg.Wait()

	stats := l.Stats(7)
	assert.Equal(t, int64(0), stats.InFlight, "no slot may leak after every Release returns")
	assert.GreaterOrEqual(t, stats.AcquiredTotal+stats.DeniedTotal, uint64(goroutines*ops))
}

func TestConcurrencyLimiter_AllStatsContainsTrackedHosts(t *testing.T) {
	l := newLimiter(3, map[int]int{42: 10})

	_, _ = l.TryAcquire(42)
	_, _ = l.TryAcquire(7)

	all := l.AllStats()
	assert.Contains(t, all, 42)
	assert.Contains(t, all, 7)
	assert.Equal(t, 10, all[42].MaxConcurrent)
	assert.Equal(t, 3, all[7].MaxConcurrent)
}

func TestConcurrencyLimiter_MaxConcurrentAccessor(t *testing.T) {
	t.Run("default fallback when no global config and no override", func(t *testing.T) {
		l := NewHostConcurrencyLimiter(&configtypes.EgConfig{}, nil)
		assert.Equal(t, DefaultMaxConcurrent, l.MaxConcurrent(123))
	})

	t.Run("global config value applies to hosts without override", func(t *testing.T) {
		l := newLimiter(7, nil)
		assert.Equal(t, 7, l.MaxConcurrent(1))
		assert.Equal(t, 7, l.MaxConcurrent(99))
	})

	t.Run("per-host override takes precedence over global", func(t *testing.T) {
		l := newLimiter(3, map[int]int{42: 11})
		assert.Equal(t, 11, l.MaxConcurrent(42))
		assert.Equal(t, 3, l.MaxConcurrent(7), "non-overridden hosts use global")
	})

	t.Run("reload updates resolved value", func(t *testing.T) {
		l := newLimiter(2, nil)
		assert.Equal(t, 2, l.MaxConcurrent(50))

		eg := &configtypes.EgConfig{Recache: &types.RecacheLimitConfig{MaxConcurrent: 8}}
		hosts := []types.Host{{ID: 50, Recache: &types.RecacheLimitConfig{MaxConcurrent: 13}}}
		l.Reload(eg, hosts)

		assert.Equal(t, 13, l.MaxConcurrent(50), "override after reload")
		assert.Equal(t, 8, l.MaxConcurrent(99), "global default after reload for non-overridden host")
	})
}
