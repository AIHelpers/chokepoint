package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chokepoint/chokepoint/internal/metrics"
	"github.com/chokepoint/chokepoint/internal/policy"
	"github.com/chokepoint/chokepoint/internal/store"
)

func newTestAPI(t *testing.T) (*API, *store.MemoryStore, *http.ServeMux) {
	t.Helper()
	st := store.NewMemoryStore()
	m := metrics.NewRecorder()
	p := policy.NewEngine(policy.DefaultRules())
	a := New(st, m, p)
	mux := http.NewServeMux()
	a.Register(mux)
	return a, st, mux
}

func TestHandleStats_ReturnsAggregates(t *testing.T) {
	_, st, mux := newTestAPI(t)
	_ = st.Append(context.Background(), store.CallRecord{
		ID: "1", Team: "search", Model: "gpt-4o", Timestamp: time.Now(), CostUSD: 2.5, StatusCode: 200,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var stats store.Stats
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if stats.TotalCalls != 1 || stats.TotalCostUSD != 2.5 {
		t.Errorf("unexpected stats: %+v", stats)
	}
}

func TestHandleStats_FiltersByTeam(t *testing.T) {
	_, st, mux := newTestAPI(t)
	_ = st.Append(context.Background(), store.CallRecord{ID: "1", Team: "search", Timestamp: time.Now(), CostUSD: 1})
	_ = st.Append(context.Background(), store.CallRecord{ID: "2", Team: "billing", Timestamp: time.Now(), CostUSD: 5})

	req := httptest.NewRequest(http.MethodGet, "/api/stats?team=search", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var stats store.Stats
	_ = json.NewDecoder(w.Body).Decode(&stats)
	if stats.TotalCalls != 1 || stats.TotalCostUSD != 1 {
		t.Errorf("expected filtered stats for team=search, got %+v", stats)
	}
}

func TestHandleLogs_ReturnsRecordsWithDefaultLimit(t *testing.T) {
	_, st, mux := newTestAPI(t)
	for i := 0; i < 5; i++ {
		_ = st.Append(context.Background(), store.CallRecord{ID: string(rune('a' + i)), Timestamp: time.Now()})
	}

	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var recs []store.CallRecord
	if err := json.NewDecoder(w.Body).Decode(&recs); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(recs) != 5 {
		t.Errorf("expected 5 records, got %d", len(recs))
	}
}

func TestHandleLogs_RespectsExplicitLimit(t *testing.T) {
	_, st, mux := newTestAPI(t)
	for i := 0; i < 5; i++ {
		_ = st.Append(context.Background(), store.CallRecord{ID: string(rune('a' + i)), Timestamp: time.Now()})
	}
	req := httptest.NewRequest(http.MethodGet, "/api/logs?limit=2", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var recs []store.CallRecord
	_ = json.NewDecoder(w.Body).Decode(&recs)
	if len(recs) != 2 {
		t.Errorf("expected 2 records, got %d", len(recs))
	}
}

func TestHandleLatencyMetrics_ReturnsSnapshots(t *testing.T) {
	a, _, mux := newTestAPI(t)
	a.Metrics.Record(metrics.Outcome{Provider: "openai", Model: "gpt-4o", LatencyMS: 100, StatusCode: 200})

	req := httptest.NewRequest(http.MethodGet, "/api/metrics/latency", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var snaps []metrics.Snapshot
	if err := json.NewDecoder(w.Body).Decode(&snaps); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snaps))
	}
}

func TestHandleListPolicies_ReturnsCurrentRules(t *testing.T) {
	_, _, mux := newTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/policies", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var rules []policy.Rule
	if err := json.NewDecoder(w.Body).Decode(&rules); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(rules) != len(policy.DefaultRules()) {
		t.Errorf("expected %d default rules, got %d", len(policy.DefaultRules()), len(rules))
	}
}

func TestHandleReplacePolicies_UpdatesRuleSet(t *testing.T) {
	a, _, mux := newTestAPI(t)
	newRules := []policy.Rule{{Name: "custom-block", Action: store.ActionBlock}}
	payload, _ := json.Marshal(newRules)

	req := httptest.NewRequest(http.MethodPut, "/api/policies", bytes.NewReader(payload))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(a.Policy.Rules()) != 1 || a.Policy.Rules()[0].Name != "custom-block" {
		t.Errorf("expected policy engine to be updated, got %+v", a.Policy.Rules())
	}
}

func TestHandleReplacePolicies_InvalidPayload(t *testing.T) {
	_, _, mux := newTestAPI(t)
	req := httptest.NewRequest(http.MethodPut, "/api/policies", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid payload, got %d", w.Code)
	}
}

func TestHandleHealthz(t *testing.T) {
	_, _, mux := newTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandlePrometheus_ReturnsTextFormat(t *testing.T) {
	a, _, mux := newTestAPI(t)
	a.Metrics.Record(metrics.Outcome{Provider: "openai", Model: "gpt-4o", LatencyMS: 10, StatusCode: 200})

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct == "" {
		t.Error("expected Content-Type header to be set")
	}
}
