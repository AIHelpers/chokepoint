package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chokepoint/chokepoint/internal/cost"
	"github.com/chokepoint/chokepoint/internal/metrics"
	"github.com/chokepoint/chokepoint/internal/pii"
	"github.com/chokepoint/chokepoint/internal/policy"
	"github.com/chokepoint/chokepoint/internal/provider"
	"github.com/chokepoint/chokepoint/internal/store"
)

// fakeUpstream lets tests control the upstream response without any
// real network access, and records the last request it received so
// tests can assert on what was actually forwarded (e.g. redacted
// content, rewritten model).
type fakeUpstream struct {
	statusCode  int
	respBody    string
	err         error
	lastReq     *http.Request
	lastReqBody []byte
}

func (f *fakeUpstream) Do(req *http.Request) (*http.Response, error) {
	f.lastReq = req
	if req.Body != nil {
		f.lastReqBody, _ = io.ReadAll(req.Body)
	}
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{
		StatusCode: f.statusCode,
		Body:       io.NopCloser(bytes.NewReader([]byte(f.respBody))),
		Header:     http.Header{},
	}, nil
}

func newTestGateway(up Upstream) (*Gateway, *store.MemoryStore) {
	st := store.NewMemoryStore()
	gw := New(Gateway{
		Upstream: up,
		PII:      pii.New(),
		Policy:   policy.NewEngine(policy.DefaultRules()),
		Cost:     cost.NewCalculator(),
		Metrics:  metrics.NewRecorder(),
		Store:    st,
		BaseURLs: map[provider.Name]string{
			provider.OpenAI:    "https://api.openai.com",
			provider.Anthropic: "https://api.anthropic.com",
		},
	})
	return gw, st
}

func openAIChatRequest(model, content string) []byte {
	body, _ := json.Marshal(map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": content},
		},
	})
	return body
}

func TestServeHTTP_UnknownProviderReturnsBadRequest(t *testing.T) {
	gw, _ := newTestGateway(&fakeUpstream{})
	req := httptest.NewRequest(http.MethodPost, "/proxy/notarealvendor/v1/x", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()
	gw.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestServeHTTP_AllowedCallForwardsAndLogs(t *testing.T) {
	up := &fakeUpstream{
		statusCode: 200,
		respBody:   `{"choices":[{"message":{"content":"hi back"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
	}
	gw, st := newTestGateway(up)

	body := openAIChatRequest("gpt-4o", "what's the weather today")
	req := httptest.NewRequest(http.MethodPost, "/proxy/openai/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("X-Chokepoint-Team", "search")
	req.Header.Set("X-Chokepoint-Feature", "weather-bot")
	w := httptest.NewRecorder()

	gw.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if up.lastReq == nil {
		t.Fatal("expected upstream to be called")
	}
	if up.lastReq.URL.String() != "https://api.openai.com/v1/chat/completions" {
		t.Errorf("unexpected upstream URL: %s", up.lastReq.URL.String())
	}

	recs, err := st.Query(context.Background(), store.Filter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(recs))
	}
	r := recs[0]
	if r.Team != "search" || r.Feature != "weather-bot" {
		t.Errorf("expected team/feature captured, got %+v", r)
	}
	if r.PromptTokens != 10 || r.CompletionTokens != 5 {
		t.Errorf("expected token usage captured, got %+v", r)
	}
	if r.CostUSD <= 0 {
		t.Errorf("expected nonzero cost, got %v", r.CostUSD)
	}
	if r.PolicyAction != store.ActionAllow {
		t.Errorf("expected allow action, got %s", r.PolicyAction)
	}
}

func TestServeHTTP_BlocksCallWithSecret(t *testing.T) {
	up := &fakeUpstream{statusCode: 200, respBody: `{}`}
	gw, st := newTestGateway(up)

	body := openAIChatRequest("gpt-4o", "here is my key sk-abcdefghijklmnop1234, use it")
	req := httptest.NewRequest(http.MethodPost, "/proxy/openai/v1/chat/completions", bytes.NewReader(body))
	w := httptest.NewRecorder()

	gw.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if up.lastReq != nil {
		t.Error("expected upstream NOT to be called for a blocked request")
	}

	recs, _ := st.Query(context.Background(), store.Filter{})
	if len(recs) != 1 || recs[0].PolicyAction != store.ActionBlock {
		t.Fatalf("expected a blocked audit record, got %+v", recs)
	}
}

func TestServeHTTP_RedactsPIIBeforeForwarding(t *testing.T) {
	up := &fakeUpstream{statusCode: 200, respBody: `{}`}
	gw, _ := newTestGateway(up)

	body := openAIChatRequest("gpt-4o", "my email is jane@example.com, help me write a reply")
	req := httptest.NewRequest(http.MethodPost, "/proxy/openai/v1/chat/completions", bytes.NewReader(body))
	w := httptest.NewRecorder()

	gw.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if strings.Contains(string(up.lastReqBody), "jane@example.com") {
		t.Errorf("expected email to be redacted from forwarded body, got: %s", up.lastReqBody)
	}
	if !strings.Contains(string(up.lastReqBody), "REDACTED") {
		t.Errorf("expected redaction marker in forwarded body, got: %s", up.lastReqBody)
	}
}

func TestServeHTTP_RoutesLongPromptToCheaperModel(t *testing.T) {
	up := &fakeUpstream{statusCode: 200, respBody: `{}`}
	gw, st := newTestGateway(up)

	longText := strings.Repeat("word ", 60000) // ~60000*5/4 tokens estimate, well over 50000 threshold
	body := openAIChatRequest("gpt-4o", longText)
	req := httptest.NewRequest(http.MethodPost, "/proxy/openai/v1/chat/completions", bytes.NewReader(body))
	w := httptest.NewRecorder()

	gw.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var sent map[string]interface{}
	if err := json.Unmarshal(up.lastReqBody, &sent); err != nil {
		t.Fatalf("forwarded body not valid JSON: %v", err)
	}
	if sent["model"] != "gpt-4o-mini" {
		t.Errorf("expected model rewritten to gpt-4o-mini, got %v", sent["model"])
	}

	recs, _ := st.Query(context.Background(), store.Filter{})
	if len(recs) != 1 || recs[0].PolicyAction != store.ActionRoute {
		t.Fatalf("expected a route audit record, got %+v", recs)
	}
}

func TestServeHTTP_UpstreamErrorReturnsBadGateway(t *testing.T) {
	up := &fakeUpstream{err: &fakeTimeoutErr{}}
	gw, st := newTestGateway(up)

	body := openAIChatRequest("gpt-4o", "hello")
	req := httptest.NewRequest(http.MethodPost, "/proxy/openai/v1/chat/completions", bytes.NewReader(body))
	w := httptest.NewRecorder()

	gw.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
	recs, _ := st.Query(context.Background(), store.Filter{})
	if len(recs) != 1 || recs[0].Error == "" {
		t.Fatalf("expected audit record with error captured, got %+v", recs)
	}
}

func TestServeHTTP_TeamAndFeatureHeadersNotForwardedUpstream(t *testing.T) {
	up := &fakeUpstream{statusCode: 200, respBody: `{}`}
	gw, _ := newTestGateway(up)

	body := openAIChatRequest("gpt-4o", "hello")
	req := httptest.NewRequest(http.MethodPost, "/proxy/openai/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("X-Chokepoint-Team", "search")
	w := httptest.NewRecorder()

	gw.ServeHTTP(w, req)

	if up.lastReq.Header.Get("X-Chokepoint-Team") != "" {
		t.Error("expected internal X-Chokepoint-* headers to be stripped before forwarding upstream")
	}
}

type fakeTimeoutErr struct{}

func (e *fakeTimeoutErr) Error() string   { return "context deadline exceeded (fake)" }
func (e *fakeTimeoutErr) Timeout() bool   { return true }
func (e *fakeTimeoutErr) Temporary() bool { return true }

func TestNew_AppliesDefaults(t *testing.T) {
	gw := New(Gateway{})
	if gw.Now == nil {
		t.Error("expected default Now to be set")
	}
	if gw.MaxExcerptChars != 200 {
		t.Errorf("expected default MaxExcerptChars 200, got %d", gw.MaxExcerptChars)
	}
	if gw.Now().IsZero() {
		t.Error("expected Now() to return a real time")
	}
	_ = time.Now
}
