// Package api exposes the HTTP endpoints that power the cost/usage
// dashboard, audit-log browsing, live policy management, and
// health/metrics probes described in the platform's MVP feature list.
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/chokepoint/chokepoint/internal/metrics"
	"github.com/chokepoint/chokepoint/internal/policy"
	"github.com/chokepoint/chokepoint/internal/store"
)

// API bundles the dependencies the dashboard endpoints read from.
type API struct {
	Store   store.Store
	Metrics *metrics.Recorder
	Policy  *policy.Engine
}

// New returns an API ready to have its routes registered.
func New(s store.Store, m *metrics.Recorder, p *policy.Engine) *API {
	return &API{Store: s, Metrics: m, Policy: p}
}

// Register mounts every dashboard endpoint onto mux.
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/stats", a.handleStats)
	mux.HandleFunc("GET /api/logs", a.handleLogs)
	mux.HandleFunc("GET /api/metrics/latency", a.handleLatencyMetrics)
	mux.HandleFunc("GET /api/policies", a.handleListPolicies)
	mux.HandleFunc("PUT /api/policies", a.handleReplacePolicies)
	mux.HandleFunc("GET /healthz", a.handleHealthz)
	mux.HandleFunc("GET /metrics", a.handlePrometheus)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// parseFilter builds a store.Filter from common query parameters
// shared by the stats and logs endpoints.
func parseFilter(r *http.Request) store.Filter {
	q := r.URL.Query()
	f := store.Filter{
		Team:    q.Get("team"),
		Feature: q.Get("feature"),
		Model:   q.Get("model"),
	}
	if since := q.Get("since_hours"); since != "" {
		if hrs, err := strconv.Atoi(since); err == nil && hrs > 0 {
			f.Since = time.Now().Add(-time.Duration(hrs) * time.Hour)
		}
	}
	if limit := q.Get("limit"); limit != "" {
		if n, err := strconv.Atoi(limit); err == nil {
			f.Limit = n
		}
	}
	return f
}

// handleStats implements GET /api/stats — the cost & usage dashboard's
// primary data source, filterable by team/feature/model/time window.
func (a *API) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := a.Store.Stats(r.Context(), parseFilter(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to compute stats")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// handleLogs implements GET /api/logs — the audit log browser.
func (a *API) handleLogs(w http.ResponseWriter, r *http.Request) {
	f := parseFilter(r)
	if f.Limit == 0 {
		f.Limit = 100
	}
	recs, err := a.Store.Query(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query logs")
		return
	}
	writeJSON(w, http.StatusOK, recs)
}

// handleLatencyMetrics implements GET /api/metrics/latency — per
// provider/model latency and error-rate breakdown.
func (a *API) handleLatencyMetrics(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.Metrics.Snapshots())
}

// handleListPolicies implements GET /api/policies.
func (a *API) handleListPolicies(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.Policy.Rules())
}

// handleReplacePolicies implements PUT /api/policies, allowing the
// rule set to be updated at runtime without a redeploy.
func (a *API) handleReplacePolicies(w http.ResponseWriter, r *http.Request) {
	var rules []policy.Rule
	if err := json.NewDecoder(r.Body).Decode(&rules); err != nil {
		writeError(w, http.StatusBadRequest, "invalid policy payload")
		return
	}
	a.Policy.SetRules(rules)
	writeJSON(w, http.StatusOK, a.Policy.Rules())
}

// handleHealthz implements GET /healthz for load-balancer/readiness
// probes.
func (a *API) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handlePrometheus implements GET /metrics in Prometheus text
// exposition format.
func (a *API) handlePrometheus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = w.Write([]byte(a.Metrics.PrometheusText()))
}
