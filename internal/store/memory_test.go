package store

import (
	"context"
	"testing"
	"time"
)

func TestAppendAndQuery(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()

	now := time.Now()
	_ = s.Append(ctx, CallRecord{ID: "1", Team: "search", Model: "gpt-4o", Timestamp: now, CostUSD: 1.0})
	_ = s.Append(ctx, CallRecord{ID: "2", Team: "billing", Model: "gpt-4o-mini", Timestamp: now.Add(time.Second), CostUSD: 0.1})

	all, err := s.Query(ctx, Filter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 records, got %d", len(all))
	}
	// newest first
	if all[0].ID != "2" {
		t.Errorf("expected newest record first, got %s", all[0].ID)
	}
}

func TestQuery_FiltersByTeamAndModel(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	_ = s.Append(ctx, CallRecord{ID: "1", Team: "search", Model: "gpt-4o", Timestamp: time.Now()})
	_ = s.Append(ctx, CallRecord{ID: "2", Team: "billing", Model: "gpt-4o", Timestamp: time.Now()})
	_ = s.Append(ctx, CallRecord{ID: "3", Team: "search", Model: "claude-sonnet-4-6", Timestamp: time.Now()})

	res, err := s.Query(ctx, Filter{Team: "search"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 records for team search, got %d", len(res))
	}

	res, err = s.Query(ctx, Filter{Team: "search", Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 1 || res[0].ID != "1" {
		t.Fatalf("expected single record id=1, got %+v", res)
	}
}

func TestQuery_RespectsLimit(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		_ = s.Append(ctx, CallRecord{ID: string(rune('a' + i)), Timestamp: time.Now()})
	}
	res, err := s.Query(ctx, Filter{Limit: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 3 {
		t.Fatalf("expected 3 records, got %d", len(res))
	}
}

func TestQuery_SinceExcludesOlderRecords(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	cutoff := time.Now()
	_ = s.Append(ctx, CallRecord{ID: "old", Timestamp: cutoff.Add(-time.Hour)})
	_ = s.Append(ctx, CallRecord{ID: "new", Timestamp: cutoff.Add(time.Hour)})

	res, err := s.Query(ctx, Filter{Since: cutoff})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 1 || res[0].ID != "new" {
		t.Fatalf("expected only the new record, got %+v", res)
	}
}

func TestStats_AggregatesCorrectly(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	_ = s.Append(ctx, CallRecord{
		ID: "1", Team: "search", Feature: "autocomplete", Model: "gpt-4o",
		Timestamp: time.Now(), CostUSD: 1.5, PromptTokens: 100, CompletionTokens: 50,
		LatencyMS: 200, StatusCode: 200, PolicyAction: ActionAllow,
	})
	_ = s.Append(ctx, CallRecord{
		ID: "2", Team: "search", Feature: "autocomplete", Model: "gpt-4o",
		Timestamp: time.Now(), CostUSD: 2.5, PromptTokens: 200, CompletionTokens: 80,
		LatencyMS: 400, StatusCode: 500, Error: "timeout", PolicyAction: ActionBlock,
	})

	stats, err := s.Stats(ctx, Filter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.TotalCalls != 2 {
		t.Errorf("expected 2 total calls, got %d", stats.TotalCalls)
	}
	if stats.TotalCostUSD != 4.0 {
		t.Errorf("expected total cost 4.0, got %v", stats.TotalCostUSD)
	}
	if stats.ErrorCount != 1 {
		t.Errorf("expected 1 error, got %d", stats.ErrorCount)
	}
	if stats.AvgLatencyMS != 300 {
		t.Errorf("expected avg latency 300, got %v", stats.AvgLatencyMS)
	}
	if stats.BlockedCount != 1 {
		t.Errorf("expected 1 blocked call, got %d", stats.BlockedCount)
	}
	if stats.CostByModel["gpt-4o"] != 4.0 {
		t.Errorf("expected cost by model 4.0, got %v", stats.CostByModel["gpt-4o"])
	}
	if stats.CallsByFeature["autocomplete"] != 2 {
		t.Errorf("expected 2 calls for autocomplete feature, got %d", stats.CallsByFeature["autocomplete"])
	}
}

func TestStats_EmptyStore(t *testing.T) {
	s := NewMemoryStore()
	stats, err := s.Stats(context.Background(), Filter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.TotalCalls != 0 || stats.AvgLatencyMS != 0 {
		t.Errorf("expected zero-value stats, got %+v", stats)
	}
}

func TestPrune_RemovesOldRecordsOnly(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	cutoff := time.Now()
	_ = s.Append(ctx, CallRecord{ID: "old", Timestamp: cutoff.Add(-48 * time.Hour)})
	_ = s.Append(ctx, CallRecord{ID: "new", Timestamp: cutoff.Add(time.Hour)})

	removed, err := s.Prune(ctx, Filter{Since: cutoff})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected 1 removed, got %d", removed)
	}

	remaining, _ := s.Query(ctx, Filter{})
	if len(remaining) != 1 || remaining[0].ID != "new" {
		t.Fatalf("expected only 'new' to remain, got %+v", remaining)
	}
}

func TestPrune_NoOpWithoutSince(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	_ = s.Append(ctx, CallRecord{ID: "1", Timestamp: time.Now()})
	removed, err := s.Prune(ctx, Filter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed != 0 {
		t.Errorf("expected no-op prune, removed %d", removed)
	}
}
