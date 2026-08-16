// Package store defines the persistence interfaces and shared domain
// types for call records, and provides an in-memory implementation
// suitable for development, testing, and small deployments.
package store

import "time"

// PolicyAction records what the policy engine decided to do with a call.
type PolicyAction string

const (
	ActionAllow  PolicyAction = "allow"
	ActionRedact PolicyAction = "redact"
	ActionBlock  PolicyAction = "block"
	ActionRoute  PolicyAction = "route"
)

// CallRecord is the immutable audit-log entry for a single proxied LLM
// call. Prompt/response bodies are stored redacted by default; raw
// content retention is an explicit, configurable opt-in (see
// config.AuditConfig) because it materially changes the compliance
// posture of the deployment.
type CallRecord struct {
	ID               string       `json:"id"`
	Timestamp        time.Time    `json:"timestamp"`
	Team             string       `json:"team"`
	Feature          string       `json:"feature"`
	Provider         string       `json:"provider"`
	Model            string       `json:"model"`
	PromptTokens     int          `json:"prompt_tokens"`
	CompletionTokens int          `json:"completion_tokens"`
	CostUSD          float64      `json:"cost_usd"`
	LatencyMS        int64        `json:"latency_ms"`
	StatusCode       int          `json:"status_code"`
	Error            string       `json:"error,omitempty"`
	PolicyAction     PolicyAction `json:"policy_action"`
	MatchedRules     []string     `json:"matched_rules,omitempty"`
	PIICategories    []string     `json:"pii_categories,omitempty"`
	// PromptExcerpt/ResponseExcerpt hold redacted, length-capped text
	// kept for audit context. Raw content is never persisted here.
	PromptExcerpt   string `json:"prompt_excerpt,omitempty"`
	ResponseExcerpt string `json:"response_excerpt,omitempty"`
}

// Filter narrows a call-record query. Zero values mean "no filter" for
// that field.
type Filter struct {
	Team    string
	Feature string
	Model   string
	Since   time.Time
	Limit   int
}

// Stats is an aggregate summary over a set of call records, used to
// power the cost/usage dashboard.
type Stats struct {
	TotalCalls     int                `json:"total_calls"`
	TotalCostUSD   float64            `json:"total_cost_usd"`
	TotalPromptTok int                `json:"total_prompt_tokens"`
	TotalComplTok  int                `json:"total_completion_tokens"`
	ErrorCount     int                `json:"error_count"`
	AvgLatencyMS   float64            `json:"avg_latency_ms"`
	CostByModel    map[string]float64 `json:"cost_by_model"`
	CostByTeam     map[string]float64 `json:"cost_by_team"`
	CallsByFeature map[string]int     `json:"calls_by_feature"`
	BlockedCount   int                `json:"blocked_count"`
	RedactedCount  int                `json:"redacted_count"`
}
