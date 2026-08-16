// Package metrics tracks per-provider/per-model latency and error
// rates in-process and renders them in Prometheus text exposition
// format, so the gateway can be scraped without a separate metrics
// pipeline for basic operational visibility.
package metrics

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// key identifies a metrics bucket by provider and model.
type key struct {
	provider string
	model    string
}

// bucket accumulates counters and latency samples for one key. Latency
// is summarized as count/sum so p50/p99-style percentiles can be added
// later without changing the wire format, while keeping this
// implementation allocation-light for the hot request path.
type bucket struct {
	requests     int64
	errors       int64
	timeouts     int64
	rateLimited  int64
	latencySumMS int64
	latencyMax   int64
}

// Recorder collects call outcomes. It is safe for concurrent use.
type Recorder struct {
	mu      sync.Mutex
	buckets map[key]*bucket
	started time.Time
}

// NewRecorder returns an empty Recorder.
func NewRecorder() *Recorder {
	return &Recorder{buckets: make(map[key]*bucket), started: time.Now()}
}

// Outcome describes how a single proxied call finished.
type Outcome struct {
	Provider    string
	Model       string
	LatencyMS   int64
	StatusCode  int
	IsTimeout   bool
	IsRateLimit bool
}

// Record adds one call outcome to the running totals for its
// provider/model bucket.
func (r *Recorder) Record(o Outcome) {
	r.mu.Lock()
	defer r.mu.Unlock()

	k := key{provider: o.Provider, model: o.Model}
	b, ok := r.buckets[k]
	if !ok {
		b = &bucket{}
		r.buckets[k] = b
	}
	b.requests++
	b.latencySumMS += o.LatencyMS
	if o.LatencyMS > b.latencyMax {
		b.latencyMax = o.LatencyMS
	}
	if o.StatusCode >= 400 {
		b.errors++
	}
	if o.IsTimeout {
		b.timeouts++
	}
	if o.IsRateLimit {
		b.rateLimited++
	}
}

// Snapshot is a point-in-time, read-only view of one provider/model
// bucket's counters.
type Snapshot struct {
	Provider     string  `json:"provider"`
	Model        string  `json:"model"`
	Requests     int64   `json:"requests"`
	Errors       int64   `json:"errors"`
	Timeouts     int64   `json:"timeouts"`
	RateLimited  int64   `json:"rate_limited"`
	AvgLatencyMS float64 `json:"avg_latency_ms"`
	MaxLatencyMS int64   `json:"max_latency_ms"`
	ErrorRate    float64 `json:"error_rate"`
}

// Snapshots returns a stable-ordered snapshot of every tracked bucket.
func (r *Recorder) Snapshots() []Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]Snapshot, 0, len(r.buckets))
	for k, b := range r.buckets {
		s := Snapshot{
			Provider:     k.provider,
			Model:        k.model,
			Requests:     b.requests,
			Errors:       b.errors,
			Timeouts:     b.timeouts,
			RateLimited:  b.rateLimited,
			MaxLatencyMS: b.latencyMax,
		}
		if b.requests > 0 {
			s.AvgLatencyMS = float64(b.latencySumMS) / float64(b.requests)
			s.ErrorRate = float64(b.errors) / float64(b.requests)
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].Model < out[j].Model
	})
	return out
}

// UptimeSeconds returns how long this Recorder has been collecting.
func (r *Recorder) UptimeSeconds() float64 {
	return time.Since(r.started).Seconds()
}

// PrometheusText renders current counters in Prometheus text
// exposition format for scraping at /metrics.
func (r *Recorder) PrometheusText() string {
	var b strings.Builder
	b.WriteString("# HELP chokepoint_requests_total Total proxied LLM requests\n")
	b.WriteString("# TYPE chokepoint_requests_total counter\n")
	for _, s := range r.Snapshots() {
		fmt.Fprintf(&b, "chokepoint_requests_total{provider=%q,model=%q} %d\n", s.Provider, s.Model, s.Requests)
	}
	b.WriteString("# HELP chokepoint_errors_total Total proxied LLM requests that errored\n")
	b.WriteString("# TYPE chokepoint_errors_total counter\n")
	for _, s := range r.Snapshots() {
		fmt.Fprintf(&b, "chokepoint_errors_total{provider=%q,model=%q} %d\n", s.Provider, s.Model, s.Errors)
	}
	b.WriteString("# HELP chokepoint_latency_avg_ms Average latency in milliseconds\n")
	b.WriteString("# TYPE chokepoint_latency_avg_ms gauge\n")
	for _, s := range r.Snapshots() {
		fmt.Fprintf(&b, "chokepoint_latency_avg_ms{provider=%q,model=%q} %.2f\n", s.Provider, s.Model, s.AvgLatencyMS)
	}
	return b.String()
}
