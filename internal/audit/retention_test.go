package audit

import (
	"context"
	"testing"
	"time"

	"github.com/chokepoint/chokepoint/internal/store"
)

func TestPruneOnce_RemovesOldRecords(t *testing.T) {
	s := store.NewMemoryStore()
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	_ = s.Append(ctx, store.CallRecord{ID: "old", Timestamp: now.Add(-100 * 24 * time.Hour)})
	_ = s.Append(ctx, store.CallRecord{ID: "new", Timestamp: now.Add(-1 * time.Hour)})

	p := NewPruner(s, 90)
	p.Now = func() time.Time { return now }
	p.pruneOnce(ctx)

	remaining, _ := s.Query(ctx, store.Filter{})
	if len(remaining) != 1 || remaining[0].ID != "new" {
		t.Fatalf("expected only 'new' to remain, got %+v", remaining)
	}
}

func TestRun_StopsOnContextCancel(t *testing.T) {
	s := store.NewMemoryStore()
	p := NewPruner(s, 90)
	p.Interval = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
		// success: Run returned promptly after cancellation
	case <-time.After(time.Second):
		t.Fatal("expected Run to return after context cancellation")
	}
}

func TestRun_NoOpWhenRetentionDisabled(t *testing.T) {
	s := store.NewMemoryStore()
	p := NewPruner(s, 0)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected Run to return immediately when RetentionDays <= 0")
	}
}
