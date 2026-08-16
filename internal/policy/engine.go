// Package policy implements the rule engine that decides whether a
// proxied LLM call is allowed, redacted, blocked, or rerouted to a
// different model — the enforcement layer that differentiates this
// platform from analytics-only observability tools.
package policy

import (
	"strings"

	"github.com/chokepoint/chokepoint/internal/pii"
	"github.com/chokepoint/chokepoint/internal/store"
)

// Condition is a single predicate evaluated against a call's context.
// Exactly one comparison field should be set per Condition; New rule
// authors should prefer the constructor helpers (e.g. WhenPII) over
// building Conditions by hand.
type Condition struct {
	// PIICategories, when non-empty, matches if the prompt contains any
	// finding from one of these categories. An empty slice with
	// AnyPII=true matches on any detected category.
	PIICategories []pii.Category
	AnyPII        bool
	// ModelEquals matches calls targeting this exact model name.
	ModelEquals string
	// ModelPrefix matches calls whose model name has this prefix (e.g.
	// "gpt-4" to match all GPT-4 variants).
	ModelPrefix string
	// PromptTokensOver matches calls whose prompt exceeds this many
	// tokens — used for routing long-context calls to cheaper models.
	PromptTokensOver int
	// TeamEquals matches calls from this team.
	TeamEquals string
}

// evaluate reports whether a condition holds given the call context.
func (c Condition) evaluate(ctx EvalContext) bool {
	if c.AnyPII && len(ctx.PIIFindings) == 0 {
		return false
	}
	if c.AnyPII && len(ctx.PIIFindings) > 0 {
		return true
	}
	if len(c.PIICategories) > 0 {
		found := false
		for _, want := range c.PIICategories {
			for _, f := range ctx.PIIFindings {
				if f.Category == want {
					found = true
				}
			}
		}
		if !found {
			return false
		}
	}
	if c.ModelEquals != "" && ctx.Model != c.ModelEquals {
		return false
	}
	if c.ModelPrefix != "" && !strings.HasPrefix(ctx.Model, c.ModelPrefix) {
		return false
	}
	if c.PromptTokensOver > 0 && ctx.PromptTokens <= c.PromptTokensOver {
		return false
	}
	if c.TeamEquals != "" && ctx.Team != c.TeamEquals {
		return false
	}
	return true
}

// Rule is a named policy rule: if Condition holds, Action is applied.
// Rules are evaluated in the order they appear in the Engine and the
// first match wins, mirroring firewall/ACL semantics that platform
// teams are already familiar with.
type Rule struct {
	Name        string
	Description string
	Condition   Condition
	Action      store.PolicyAction
	// RouteToModel is used only when Action == store.ActionRoute, and
	// names the model the call should be redirected to.
	RouteToModel string
}

// EvalContext carries the per-call facts a Rule's Condition is
// evaluated against.
type EvalContext struct {
	Team         string
	Model        string
	PromptTokens int
	PIIFindings  []pii.Finding
}

// Decision is the outcome of evaluating all rules against a call.
type Decision struct {
	Action       store.PolicyAction
	MatchedRule  string
	RouteToModel string
}

// Engine holds an ordered set of rules and evaluates calls against
// them. It is safe for concurrent read use; use SetRules to replace
// the rule set atomically (e.g. on config reload).
type Engine struct {
	rules []Rule
}

// NewEngine returns an Engine with the given initial rule set.
func NewEngine(rules []Rule) *Engine {
	e := &Engine{}
	e.SetRules(rules)
	return e
}

// SetRules atomically replaces the engine's rule set.
func (e *Engine) SetRules(rules []Rule) {
	cp := make([]Rule, len(rules))
	copy(cp, rules)
	e.rules = cp
}

// Rules returns a copy of the currently configured rules.
func (e *Engine) Rules() []Rule {
	cp := make([]Rule, len(e.rules))
	copy(cp, e.rules)
	return cp
}

// Evaluate runs the call context through the rule set and returns the
// first matching rule's decision. If no rule matches, the call is
// allowed by default — policy engines that fail closed instead should
// add an explicit catch-all rule.
func (e *Engine) Evaluate(ctx EvalContext) Decision {
	for _, r := range e.rules {
		if r.Condition.evaluate(ctx) {
			return Decision{
				Action:       r.Action,
				MatchedRule:  r.Name,
				RouteToModel: r.RouteToModel,
			}
		}
	}
	return Decision{Action: store.ActionAllow}
}

// DefaultRules returns a starter rule set covering the most common
// enterprise policies: block PII/secrets from leaving the org via
// external models, and route large prompts to a cheaper model.
func DefaultRules() []Rule {
	return []Rule{
		{
			Name:        "block-secrets",
			Description: "Block calls whose prompt contains API keys, AWS keys, or JWTs",
			Condition: Condition{
				PIICategories: []pii.Category{pii.CategoryAPIKey, pii.CategoryAWSKey, pii.CategoryJWT},
			},
			Action: store.ActionBlock,
		},
		{
			Name:        "redact-pii",
			Description: "Redact common PII (email, phone, SSN, credit card) before sending",
			Condition: Condition{
				PIICategories: []pii.Category{pii.CategoryEmail, pii.CategoryPhone, pii.CategorySSN, pii.CategoryCreditCard},
			},
			Action: store.ActionRedact,
		},
		{
			Name:         "route-long-context-to-cheaper-model",
			Description:  "Route very long prompts to a cheaper, higher-context model",
			Condition:    Condition{PromptTokensOver: 50000},
			Action:       store.ActionRoute,
			RouteToModel: "gpt-4o-mini",
		},
	}
}
