// Package audit runs the retention policy for the audit log: it
// periodically removes records older than the configured retention
// window so the store doesn't grow unbounded, per the platform's
// "configurable retention" requirement.
package audit

import (
	"context"
	"log"
	"time"

	"github.com/chokepoint/chokepoint/internal/store"
)

// Pruner periodically removes call records older than RetentionDays.
type Pruner struct {
	Store         store.Store
	RetentionDays int
	Interval      time.Duration
	Now           func() time.Time
}

// NewPruner returns a Pruner with a default check interval of one
// hour, sufficient for a "days" granularity retention window without
// needing configuration for typical deployments.
func NewPruner(s store.Store, retentionDays int) *Pruner {
	return &Pruner{
		Store:         s,
		RetentionDays: retentionDays,
		Interval:      time.Hour,
		Now:           time.Now,
	}
}

// Run blocks, pruning on Interval until ctx is cancelled. Callers
// typically launch it with `go pruner.Run(ctx)` at startup.
func (p *Pruner) Run(ctx context.Context) {
	if p.RetentionDays <= 0 {
		return
	}
	ticker := time.NewTicker(p.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.pruneOnce(ctx)
		}
	}
}

func (p *Pruner) pruneOnce(ctx context.Context) {
	cutoff := p.Now().Add(-time.Duration(p.RetentionDays) * 24 * time.Hour)
	removed, err := p.Store.Prune(ctx, store.Filter{Since: cutoff})
	if err != nil {
		log.Printf("audit: retention prune failed: %v", err)
		return
	}
	if removed > 0 {
		log.Printf("audit: pruned %d records older than %d days", removed, p.RetentionDays)
	}
}
