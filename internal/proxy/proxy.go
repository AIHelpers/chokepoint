// Package proxy implements the drop-in reverse-proxy handler that sits
// between an application and its LLM provider(s). Every call flows
// through Gateway.Handler, which is where PII detection, policy
// enforcement, cost accounting, latency tracking, and audit logging
// all attach — the single interception point the rest of the platform
// hangs off of.
package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/chokepoint/chokepoint/internal/cost"
	"github.com/chokepoint/chokepoint/internal/metrics"
	"github.com/chokepoint/chokepoint/internal/pii"
	"github.com/chokepoint/chokepoint/internal/policy"
	"github.com/chokepoint/chokepoint/internal/provider"
	"github.com/chokepoint/chokepoint/internal/store"
)

// Upstream sends an already-prepared request to the real provider and
// returns its response. Production code uses httpUpstream (a thin
// http.Client wrapper); tests substitute a fake to avoid real network
// calls and keep the policy/logging logic under fast, deterministic
// test coverage.
type Upstream interface {
	Do(req *http.Request) (*http.Response, error)
}

// Gateway wires together every governance component into a single
// http.Handler that can be mounted at "/proxy/".
type Gateway struct {
	Upstream Upstream
	PII      *pii.Detector
	Policy   *policy.Engine
	Cost     *cost.Calculator
	Metrics  *metrics.Recorder
	Store    store.Store
	BaseURLs map[provider.Name]string
	// MaxExcerptChars bounds how much prompt/response text is kept in
	// the audit log excerpt fields.
	MaxExcerptChars int
	// Now is overridable for deterministic tests.
	Now func() time.Time
}

// New returns a Gateway with sane defaults for any field left zero by
// the caller (Now, MaxExcerptChars).
func New(g Gateway) *Gateway {
	if g.Now == nil {
		g.Now = time.Now
	}
	if g.MaxExcerptChars == 0 {
		g.MaxExcerptChars = 200
	}
	return &g
}

// ServeHTTP implements http.Handler. Requests are expected at
// "/proxy/{provider}/{upstream-path}", matching how the OpenAI and
// Anthropic SDKs can be pointed at a custom base URL with a one-line
// change (see README "Drop-in integration").
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := g.Now()

	provName := provider.DetectFromPath(r.URL.Path)
	if provName == provider.Unknown {
		http.Error(w, `{"error":"unknown provider in proxy path"}`, http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadRequest)
		return
	}

	reqInfo, err := provider.ParseRequest(provName, body)
	if err != nil {
		// Fail open on parse errors: an unparseable body (e.g. a new
		// endpoint shape) shouldn't take down the app's LLM calls, but
		// it does mean we can't apply content-based policy to it.
		reqInfo = provider.RequestInfo{}
	}

	findings := g.PII.Scan(reqInfo.PromptText)
	team := r.Header.Get("X-Chokepoint-Team")
	feature := r.Header.Get("X-Chokepoint-Feature")

	decision := g.Policy.Evaluate(policy.EvalContext{
		Team:         team,
		Model:        reqInfo.Model,
		PromptTokens: estimateTokens(reqInfo.PromptText),
		PIIFindings:  findings,
	})

	rec := store.CallRecord{
		ID:            newID(),
		Timestamp:     start,
		Team:          team,
		Feature:       feature,
		Provider:      string(provName),
		Model:         reqInfo.Model,
		PolicyAction:  decision.Action,
		PromptExcerpt: excerpt(g.PII.Redact(reqInfo.PromptText), g.MaxExcerptChars),
	}
	for _, f := range findings {
		rec.PIICategories = appendUnique(rec.PIICategories, string(f.Category))
	}
	if decision.MatchedRule != "" {
		rec.MatchedRules = []string{decision.MatchedRule}
	}

	if decision.Action == store.ActionBlock {
		rec.StatusCode = http.StatusForbidden
		rec.LatencyMS = time.Since(start).Milliseconds()
		g.persist(r.Context(), rec)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(fmt.Sprintf(
			`{"error":"blocked by policy","rule":%q}`, decision.MatchedRule)))
		return
	}

	outBody := body
	targetModel := reqInfo.Model
	if decision.Action == store.ActionRoute && decision.RouteToModel != "" {
		targetModel = decision.RouteToModel
		if rewritten, err := provider.SetModel(body, targetModel); err == nil {
			outBody = rewritten
		}
	}
	if decision.Action == store.ActionRedact {
		// Redact detected PII from the outgoing prompt content for
		// every message, not just what we log, so the provider never
		// receives the raw sensitive data either.
		if redacted, err := redactRequestBody(provName, outBody, g.PII); err == nil {
			outBody = redacted
		}
	}

	upstreamReq, err := g.buildUpstreamRequest(r, provName, outBody)
	if err != nil {
		http.Error(w, `{"error":"failed to build upstream request"}`, http.StatusInternalServerError)
		return
	}

	resp, err := g.Upstream.Do(upstreamReq)
	latency := time.Since(start)
	rec.LatencyMS = latency.Milliseconds()

	if err != nil {
		rec.StatusCode = http.StatusBadGateway
		rec.Error = err.Error()
		g.Metrics.Record(metrics.Outcome{
			Provider: string(provName), Model: targetModel,
			LatencyMS: latency.Milliseconds(), StatusCode: http.StatusBadGateway,
			IsTimeout: isTimeout(err),
		})
		g.persist(r.Context(), rec)
		http.Error(w, `{"error":"upstream request failed"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		respBody = nil
	}

	rec.StatusCode = resp.StatusCode
	if respInfo, err := provider.ParseResponse(provName, respBody); err == nil {
		rec.PromptTokens = respInfo.PromptTokens
		rec.CompletionTokens = respInfo.CompletionTokens
		rec.ResponseExcerpt = excerpt(g.PII.Redact(respInfo.ResponseText), g.MaxExcerptChars)
		if costResult, err := g.Cost.Calculate(targetModel, cost.Usage{
			PromptTokens: respInfo.PromptTokens, CompletionTokens: respInfo.CompletionTokens,
		}); err == nil {
			rec.CostUSD = costResult.TotalCost
		}
	}

	g.Metrics.Record(metrics.Outcome{
		Provider: string(provName), Model: targetModel,
		LatencyMS: latency.Milliseconds(), StatusCode: resp.StatusCode,
		IsRateLimit: resp.StatusCode == http.StatusTooManyRequests,
	})
	g.persist(r.Context(), rec)

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)
}

func (g *Gateway) persist(ctx context.Context, rec store.CallRecord) {
	// Audit logging failures must never break the caller's LLM
	// response; they're swallowed here on purpose. A production
	// deployment should pair this with a metrics counter/alert on
	// persist failures rather than surfacing them to the app.
	_ = g.Store.Append(ctx, rec)
}

func (g *Gateway) buildUpstreamRequest(orig *http.Request, provName provider.Name, body []byte) (*http.Request, error) {
	base, ok := g.BaseURLs[provName]
	if !ok {
		return nil, fmt.Errorf("proxy: no base URL configured for provider %q", provName)
	}
	target := strings.TrimRight(base, "/") + provider.UpstreamPath(orig.URL.Path)

	req, err := http.NewRequestWithContext(orig.Context(), orig.Method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, vv := range orig.Header {
		if strings.HasPrefix(k, "X-Chokepoint-") {
			continue
		}
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}
	req.ContentLength = int64(len(body))
	return req, nil
}

// redactRequestBody rewrites every message's content field with PII
// redacted, for the two supported provider schemas.
func redactRequestBody(p provider.Name, body []byte, d *pii.Detector) ([]byte, error) {
	info, err := provider.ParseRequest(p, body)
	if err != nil {
		return nil, err
	}
	if !d.HasSensitiveData(info.PromptText) {
		return body, nil
	}
	// Simplest correct approach: re-run SetModel-style generic
	// round-trip isn't sufficient here since messages is nested; do a
	// targeted generic walk instead.
	return redactGenericMessages(body, d)
}

func estimateTokens(text string) int {
	// Rough heuristic (~4 chars/token) used only for routing decisions
	// where an approximate size is sufficient; actual billed tokens
	// come from the provider's usage response.
	return len(text) / 4
}

func excerpt(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func appendUnique(list []string, v string) []string {
	for _, existing := range list {
		if existing == v {
			return list
		}
	}
	return append(list, v)
}

func isTimeout(err error) bool {
	type timeoutErr interface{ Timeout() bool }
	if te, ok := err.(timeoutErr); ok {
		return te.Timeout()
	}
	return false
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
