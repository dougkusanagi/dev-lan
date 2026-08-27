package api

import (
	"context"
	"sync"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/app"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

const (
	hotSnapshotTTL  = 3 * time.Second
	coldSnapshotTTL = 45 * time.Second
)

type cachedHotRuntime struct {
	runtime *projectViewRuntime
	at      time.Time
}

type cachedColdSnapshot struct {
	value systemHealthSnapshot
	at    time.Time
}

type serviceReadModelCache struct {
	mu   sync.Mutex
	hot  cachedHotRuntime
	cold cachedColdSnapshot
}

var readModelCaches sync.Map // map[*app.App]*serviceReadModelCache

func cacheFor(service *app.App) *serviceReadModelCache {
	if value, ok := readModelCaches.Load(service); ok {
		return value.(*serviceReadModelCache)
	}
	created := &serviceReadModelCache{}
	actual, _ := readModelCaches.LoadOrStore(service, created)
	return actual.(*serviceReadModelCache)
}

// InvalidateHotReadModelCache is used by HMR mutations. It preserves the
// administrative snapshot so a start/stop does not immediately re-run PHP,
// firewall, Hyper-V and CA checks.
func InvalidateHotReadModelCache(service *app.App) {
	cache := cacheFor(service)
	cache.mu.Lock()
	cache.hot = cachedHotRuntime{}
	cache.mu.Unlock()
}

// InvalidateColdReadModelCache expires only the slower administrative layer.
func InvalidateColdReadModelCache(service *app.App) {
	cache := cacheFor(service)
	cache.mu.Lock()
	cache.cold = cachedColdSnapshot{}
	cache.mu.Unlock()
}

// InvalidateReadModelCache is called after a mutation that can change both
// project runtime and administrative health state.
func InvalidateReadModelCache(service *app.App) {
	cache := cacheFor(service)
	cache.mu.Lock()
	cache.hot = cachedHotRuntime{}
	cache.cold = cachedColdSnapshot{}
	cache.mu.Unlock()
}

func cachedHot(ctx context.Context, service *app.App, now time.Time) (*projectViewRuntime, bool, error) {
	cache := cacheFor(service)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.hot.runtime != nil && now.Sub(cache.hot.at) < hotSnapshotTTL {
		runtime := cache.hot.runtime
		return runtime, true, nil
	}

	runtime, err := loadProjectViewRuntimeUncached(ctx, service)
	if err != nil {
		return nil, false, err
	}
	cache.hot = cachedHotRuntime{runtime: runtime, at: now}
	return runtime, false, nil
}

func cachedCold(ctx context.Context, service *app.App, now time.Time, caddyStatus platform.CaddyServiceStatus) (systemHealthSnapshot, bool) {
	cache := cacheFor(service)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if !cache.cold.at.IsZero() && now.Sub(cache.cold.at) < coldSnapshotTTL {
		value := cache.cold.value
		return value, true
	}

	value := loadSystemHealthSnapshot(ctx, service, caddyStatus)
	cache.cold = cachedColdSnapshot{value: value, at: now}
	return value, false
}

func readModelCacheAges(service *app.App, now time.Time) (int64, int64) {
	cache := cacheFor(service)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	var hotAge, coldAge int64
	if !cache.hot.at.IsZero() {
		hotAge = now.Sub(cache.hot.at).Milliseconds()
	}
	if !cache.cold.at.IsZero() {
		coldAge = now.Sub(cache.cold.at).Milliseconds()
	}
	return hotAge, coldAge
}
