package metrics

import (
	"strings"
	"sync"
	"testing"
)

func TestRecord_AccumulatesPerBucket(t *testing.T) {
	r := NewRecorder()
	r.Record(Outcome{Provider: "openai", Model: "gpt-4o", LatencyMS: 100, StatusCode: 200})
	r.Record(Outcome{Provider: "openai", Model: "gpt-4o", LatencyMS: 300, StatusCode: 500})

	snaps := r.Snapshots()
	if len(snaps) != 1 {
		t.Fatalf("expected 1 bucket, got %d", len(snaps))
	}
	s := snaps[0]
	if s.Requests != 2 {
		t.Errorf("expected 2 requests, got %d", s.Requests)
	}
	if s.Errors != 1 {
		t.Errorf("expected 1 error, got %d", s.Errors)
	}
	if s.AvgLatencyMS != 200 {
		t.Errorf("expected avg latency 200, got %v", s.AvgLatencyMS)
	}
	if s.MaxLatencyMS != 300 {
		t.Errorf("expected max latency 300, got %d", s.MaxLatencyMS)
	}
	if s.ErrorRate != 0.5 {
		t.Errorf("expected error rate 0.5, got %v", s.ErrorRate)
	}
}

func TestRecord_SeparatesBucketsByModel(t *testing.T) {
	r := NewRecorder()
	r.Record(Outcome{Provider: "openai", Model: "gpt-4o", LatencyMS: 100, StatusCode: 200})
	r.Record(Outcome{Provider: "anthropic", Model: "claude-sonnet-4-6", LatencyMS: 50, StatusCode: 200})

	snaps := r.Snapshots()
	if len(snaps) != 2 {
		t.Fatalf("expected 2 buckets, got %d", len(snaps))
	}
}

func TestRecord_TracksTimeoutsAndRateLimits(t *testing.T) {
	r := NewRecorder()
	r.Record(Outcome{Provider: "openai", Model: "gpt-4o", IsTimeout: true, StatusCode: 504})
	r.Record(Outcome{Provider: "openai", Model: "gpt-4o", IsRateLimit: true, StatusCode: 429})

	s := r.Snapshots()[0]
	if s.Timeouts != 1 {
		t.Errorf("expected 1 timeout, got %d", s.Timeouts)
	}
	if s.RateLimited != 1 {
		t.Errorf("expected 1 rate-limited, got %d", s.RateLimited)
	}
}

func TestSnapshots_EmptyRecorder(t *testing.T) {
	r := NewRecorder()
	if snaps := r.Snapshots(); len(snaps) != 0 {
		t.Errorf("expected no snapshots, got %+v", snaps)
	}
}

func TestSnapshots_SortedByProviderThenModel(t *testing.T) {
	r := NewRecorder()
	r.Record(Outcome{Provider: "openai", Model: "z-model", StatusCode: 200})
	r.Record(Outcome{Provider: "anthropic", Model: "a-model", StatusCode: 200})
	r.Record(Outcome{Provider: "openai", Model: "a-model", StatusCode: 200})

	snaps := r.Snapshots()
	if snaps[0].Provider != "anthropic" {
		t.Errorf("expected anthropic first, got %+v", snaps)
	}
	if snaps[1].Provider != "openai" || snaps[1].Model != "a-model" {
		t.Errorf("expected openai/a-model second, got %+v", snaps)
	}
}

func TestPrometheusText_ContainsExpectedMetrics(t *testing.T) {
	r := NewRecorder()
	r.Record(Outcome{Provider: "openai", Model: "gpt-4o", LatencyMS: 150, StatusCode: 200})

	text := r.PrometheusText()
	for _, want := range []string{"chokepoint_requests_total", "chokepoint_errors_total", "chokepoint_latency_avg_ms", `provider="openai"`, `model="gpt-4o"`} {
		if !strings.Contains(text, want) {
			t.Errorf("expected prometheus output to contain %q, got:\n%s", want, text)
		}
	}
}

func TestRecord_ConcurrentSafe(t *testing.T) {
	r := NewRecorder()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Record(Outcome{Provider: "openai", Model: "gpt-4o", LatencyMS: 10, StatusCode: 200})
		}()
	}
	wg.Wait()
	s := r.Snapshots()[0]
	if s.Requests != 100 {
		t.Errorf("expected 100 requests recorded, got %d", s.Requests)
	}
}
