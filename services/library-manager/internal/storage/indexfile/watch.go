package indexfile

import (
	"context"
	"errors"
	"os"
	"time"
)

// Logger is the small interface Watch uses for lifecycle and error logs.
type Logger interface {
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
}

type nopLogger struct{}

func (nopLogger) Info(string, ...any) {}
func (nopLogger) Warn(string, ...any) {}

type WatchConfig struct {
	ProbeInterval   time.Duration
	RebuildInterval time.Duration
	LibRootDebounce time.Duration
}

// Watch attaches background freshness loops to this Index and returns
// immediately. All loops exit when ctx is cancelled. Logging goes to
// the Config.Logger the Index was constructed with.
func (i *Index) Watch(ctx context.Context, cfg WatchConfig) {
	i.libRootDebounce = cfg.LibRootDebounce
	if stat, err := os.Stat(i.root); err == nil {
		i.mu.Lock()
		i.cachedLibRootMTime = stat.ModTime()
		i.mu.Unlock()
	}
	if cfg.ProbeInterval > 0 {
		go i.probeLoop(ctx, cfg.ProbeInterval)
	}
	if cfg.RebuildInterval > 0 {
		go i.rebuildLoop(ctx, cfg.RebuildInterval)
	}
}

func (i *Index) probeLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			i.probeOnce(ctx)
		}
	}
}

func (i *Index) probeOnce(ctx context.Context) {
	if i.Rebuilding() {
		return
	}
	stat, err := os.Stat(i.root)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			i.log.Warn("indexfile: probe libroot stat", "err", err)
		}
		return
	}
	i.mu.RLock()
	cached := i.cachedLibRootMTime
	i.mu.RUnlock()
	if stat.ModTime().Equal(cached) {
		return
	}
	i.mu.Lock()
	i.cachedLibRootMTime = stat.ModTime()
	i.mu.Unlock()
	i.scheduleLibRootRebuild(ctx)
}

func (i *Index) scheduleLibRootRebuild(ctx context.Context) {
	if i.libRootDebounce <= 0 {
		i.TriggerRebuild(ctx, "rebuild_libroot")
		return
	}
	i.mu.Lock()
	if i.libRootRebuildTimer != nil {
		i.libRootRebuildTimer.Stop()
	}
	i.libRootRebuildTimer = time.AfterFunc(i.libRootDebounce, func() {
		if i.Rebuilding() {
			return
		}
		i.TriggerRebuild(ctx, "rebuild_libroot")
	})
	i.mu.Unlock()
}

func (i *Index) rebuildLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			i.TriggerRebuild(ctx, "rebuild_periodic")
		}
	}
}
