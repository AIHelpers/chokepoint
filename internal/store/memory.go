package store

import (
	"context"
	"sort"
	"sync"
)

// MemoryStore is a thread-safe, in-process Store implementation. It is
// the default backend for development and small deployments; larger
// deployments should implement Store against ClickHouse/Postgres as
// described in the platform design doc and swap it in at startup.
type MemoryStore struct {
	mu      sync.RWMutex
	records []CallRecord
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

// Append implements Store.
func (m *MemoryStore) Append(_ context.Context, rec CallRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, rec)
	return nil
}

func matches(rec CallRecord, f Filter) bool {
	if f.Team != "" && rec.Team != f.Team {
		return false
	}
	if f.Feature != "" && rec.Feature != f.Feature {
		return false
	}
	if f.Model != "" && rec.Model != f.Model {
		return false
	}
	if !f.Since.IsZero() && rec.Timestamp.Before(f.Since) {
		return false
	}
	return true
}

// Query implements Store.
func (m *MemoryStore) Query(_ context.Context, f Filter) ([]CallRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var out []CallRecord
	for _, r := range m.records {
		if matches(r, f) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Timestamp.After(out[j].Timestamp)
	})
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

// Stats implements Store.
func (m *MemoryStore) Stats(_ context.Context, f Filter) (Stats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s := Stats{
		CostByModel:    map[string]float64{},
		CostByTeam:     map[string]float64{},
		CallsByFeature: map[string]int{},
	}
	var totalLatency int64
	for _, r := range m.records {
		if !matches(r, f) {
			continue
		}
		s.TotalCalls++
		s.TotalCostUSD += r.CostUSD
		s.TotalPromptTok += r.PromptTokens
		s.TotalComplTok += r.CompletionTokens
		totalLatency += r.LatencyMS
		if r.Error != "" || r.StatusCode >= 400 {
			s.ErrorCount++
		}
		s.CostByModel[r.Model] += r.CostUSD
		if r.Team != "" {
			s.CostByTeam[r.Team] += r.CostUSD
		}
		if r.Feature != "" {
			s.CallsByFeature[r.Feature]++
		}
		switch r.PolicyAction {
		case ActionBlock:
			s.BlockedCount++
		case ActionRedact:
			s.RedactedCount++
		}
	}
	if s.TotalCalls > 0 {
		s.AvgLatencyMS = float64(totalLatency) / float64(s.TotalCalls)
	}
	return s, nil
}

// Prune implements Store. Records with a timestamp before f.Since are
// removed; all other filter fields are ignored since retention applies
// org-wide, not per team/model.
func (m *MemoryStore) Prune(_ context.Context, f Filter) (int, error) {
	if f.Since.IsZero() {
		return 0, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	kept := m.records[:0:0]
	removed := 0
	for _, r := range m.records {
		if r.Timestamp.Before(f.Since) {
			removed++
			continue
		}
		kept = append(kept, r)
	}
	m.records = kept
	return removed, nil
}
