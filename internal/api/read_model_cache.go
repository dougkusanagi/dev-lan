package api

import (
	"context"
	"sync"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/application"
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
	value application.SystemHealthSnapshot
	at    time.Time
}

// ReadModelCache owns the materialized read snapshots for one API consumer.
// It is intentionally constructed and held by Server instead of being indexed
// by *app.App in a package-level registry. That keeps cache lifetime explicit
// and prevents closed services from being retained by the API package.
type ReadModelCache struct {
	mu   sync.Mutex
	hot  cachedHotRuntime
	cold cachedColdSnapshot
}

func NewReadModelCache() *ReadModelCache {
	return &ReadModelCache{}
}

// InvalidateHotReadModelCache is used by HMR mutations. It preserves the
// administrative snapshot so a start/stop does not immediately re-run PHP,
// firewall, Hyper-V and CA checks.
func (c *ReadModelCache) InvalidateHotReadModelCache() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.hot = cachedHotRuntime{}
	c.mu.Unlock()
}

// InvalidateColdReadModelCache expires only the slower administrative layer.
func (c *ReadModelCache) InvalidateColdReadModelCache() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.cold = cachedColdSnapshot{}
	c.mu.Unlock()
}

// InvalidateReadModelCache is called after a mutation that can change both
// project runtime and administrative health state.
func (c *ReadModelCache) InvalidateReadModelCache() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.hot = cachedHotRuntime{}
	c.cold = cachedColdSnapshot{}
	c.mu.Unlock()
}

func (s *Server) InvalidateHotReadModelCache() {
	if s != nil {
		s.readModelCache.InvalidateHotReadModelCache()
	}
}

func (s *Server) InvalidateColdReadModelCache() {
	if s != nil {
		s.readModelCache.InvalidateColdReadModelCache()
	}
}

func (s *Server) InvalidateReadModelCache() {
	if s != nil {
		s.readModelCache.InvalidateReadModelCache()
	}
}

func (c *ReadModelCache) cachedHot(ctx context.Context, queries *application.Queries, now time.Time) (*projectViewRuntime, bool, error) {
	if c == nil {
		c = NewReadModelCache()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hot.runtime != nil && now.Sub(c.hot.at) < hotSnapshotTTL {
		runtime := c.hot.runtime
		return runtime, true, nil
	}

	runtime, err := loadProjectViewRuntimeUncached(ctx, queries)
	if err != nil {
		return nil, false, err
	}
	c.hot = cachedHotRuntime{runtime: runtime, at: now}
	return runtime, false, nil
}

func (c *ReadModelCache) cachedCold(ctx context.Context, queries *application.Queries, now time.Time, caddyStatus application.CaddyStatus) (application.SystemHealthSnapshot, bool) {
	if c == nil {
		c = NewReadModelCache()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.cold.at.IsZero() && now.Sub(c.cold.at) < coldSnapshotTTL {
		value := c.cold.value
		return value, true
	}

	value := loadSystemHealthSnapshot(ctx, queries, caddyStatus)
	c.cold = cachedColdSnapshot{value: value, at: now}
	return value, false
}

func (c *ReadModelCache) ages(now time.Time) (int64, int64) {
	if c == nil {
		return 0, 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var hotAge, coldAge int64
	if !c.hot.at.IsZero() {
		hotAge = now.Sub(c.hot.at).Milliseconds()
	}
	if !c.cold.at.IsZero() {
		coldAge = now.Sub(c.cold.at).Milliseconds()
	}
	return hotAge, coldAge
}
