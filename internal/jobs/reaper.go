package jobs

import "time"

// startReaper launches the reaper goroutine if both ReaperInterval
// and Retention are configured. Called by NewRegistry.
func (r *Registry) startReaper() {
	if r.cfg.ReaperInterval <= 0 || r.cfg.Retention <= 0 {
		return
	}
	r.wg.Add(1)
	go r.reaperLoop()
}

func (r *Registry) reaperLoop() {
	defer r.wg.Done()
	t := time.NewTicker(r.cfg.ReaperInterval)
	defer t.Stop()
	for {
		select {
		case <-r.parentCtx.Done():
			return
		case now := <-t.C:
			r.evict(now)
		}
	}
}

// evict removes terminal entries whose endedAt + retention < now.
// Logs aggregate count when any are evicted.
func (r *Registry) evict(now time.Time) {
	cutoff := now.Add(-r.cfg.Retention)
	var removed []string
	r.mu.Lock()
	for id, e := range r.byID {
		e.mu.RLock()
		isTerminal := e.state != StatusRunning
		ended := e.endedAt
		e.mu.RUnlock()
		if isTerminal && ended.Before(cutoff) {
			delete(r.byID, id)
			removed = append(removed, id)
		}
	}
	r.mu.Unlock()
	if len(removed) > 0 {
		r.log.Info("reaper evicted terminal jobs", "count", len(removed))
	}
}
