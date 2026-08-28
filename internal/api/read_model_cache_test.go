package api

import (
	"sync"
	"testing"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/app"
)

func TestServerOwnsReadModelCache(t *testing.T) {
	service := app.New(t.TempDir())
	first := New(service)
	second := New(service)

	if first.readModelCache == nil || second.readModelCache == nil {
		t.Fatal("cada servidor deveria criar um cache de read model")
	}
	if first.readModelCache == second.readModelCache {
		t.Fatal("servidores não deveriam compartilhar cache por meio de um registry global")
	}

	now := time.Now()
	first.readModelCache.hot = cachedHotRuntime{runtime: &projectViewRuntime{}, at: now}
	if hotAge, _ := second.readModelCache.ages(now); hotAge != 0 {
		t.Fatalf("cache do segundo servidor foi contaminado pelo primeiro: %dms", hotAge)
	}
}

func TestReadModelCacheConcurrentInvalidationAndAge(t *testing.T) {
	cache := NewReadModelCache()
	now := time.Now()
	cache.hot = cachedHotRuntime{runtime: &projectViewRuntime{}, at: now}
	cache.cold = cachedColdSnapshot{at: now}

	const workers = 32
	var wait sync.WaitGroup
	start := make(chan struct{})
	wait.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func(index int) {
			defer wait.Done()
			<-start
			switch index % 4 {
			case 0:
				cache.InvalidateHotReadModelCache()
			case 1:
				cache.InvalidateColdReadModelCache()
			case 2:
				cache.InvalidateReadModelCache()
			default:
				cache.ages(now)
			}
		}(worker)
	}
	close(start)
	wait.Wait()

	hotAge, coldAge := cache.ages(now)
	if hotAge != 0 || coldAge != 0 {
		t.Fatalf("invalidação concorrente deixou snapshots ativos: hot=%dms cold=%dms", hotAge, coldAge)
	}
}
