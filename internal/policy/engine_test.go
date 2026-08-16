package policy

import (
	"testing"

	"github.com/chokepoint/chokepoint/internal/pii"
	"github.com/chokepoint/chokepoint/internal/store"
)

func TestEvaluate_NoRulesAllowsByDefault(t *testing.T) {
	e := NewEngine(nil)
	d := e.Evaluate(EvalContext{Model: "gpt-4o"})
	if d.Action != store.ActionAllow {
		t.Errorf("expected allow with no rules, got %s", d.Action)
	}
}

func TestEvaluate_FirstMatchWins(t *testing.T) {
	e := NewEngine([]Rule{
		{Name: "first", Condition: Condition{ModelEquals: "gpt-4o"}, Action: store.ActionBlock},
		{Name: "second", Condition: Condition{ModelEquals: "gpt-4o"}, Action: store.ActionRedact},
	})
	d := e.Evaluate(EvalContext{Model: "gpt-4o"})
	if d.Action != store.ActionBlock || d.MatchedRule != "first" {
		t.Errorf("expected first rule to win, got %+v", d)
	}
}

func TestEvaluate_ModelPrefix(t *testing.T) {
	e := NewEngine([]Rule{
		{Name: "gpt4-rule", Condition: Condition{ModelPrefix: "gpt-4"}, Action: store.ActionBlock},
	})
	d := e.Evaluate(EvalContext{Model: "gpt-4-turbo"})
	if d.Action != store.ActionBlock {
		t.Errorf("expected block for gpt-4-turbo, got %s", d.Action)
	}
	d2 := e.Evaluate(EvalContext{Model: "claude-sonnet-4-6"})
	if d2.Action != store.ActionAllow {
		t.Errorf("expected allow for claude model, got %s", d2.Action)
	}
}

func TestEvaluate_PIICategoryMatch(t *testing.T) {
	e := NewEngine([]Rule{
		{Name: "block-ssn", Condition: Condition{PIICategories: []pii.Category{pii.CategorySSN}}, Action: store.ActionBlock},
	})
	d := e.Evaluate(EvalContext{PIIFindings: []pii.Finding{{Category: pii.CategorySSN}}})
	if d.Action != store.ActionBlock {
		t.Errorf("expected block on SSN finding, got %s", d.Action)
	}
	d2 := e.Evaluate(EvalContext{PIIFindings: []pii.Finding{{Category: pii.CategoryEmail}}})
	if d2.Action != store.ActionAllow {
		t.Errorf("expected allow when only unrelated PII present, got %s", d2.Action)
	}
}

func TestEvaluate_AnyPII(t *testing.T) {
	e := NewEngine([]Rule{
		{Name: "flag-any-pii", Condition: Condition{AnyPII: true}, Action: store.ActionRedact},
	})
	d := e.Evaluate(EvalContext{PIIFindings: []pii.Finding{{Category: pii.CategoryIPAddress}}})
	if d.Action != store.ActionRedact {
		t.Errorf("expected redact for any PII, got %s", d.Action)
	}
	d2 := e.Evaluate(EvalContext{})
	if d2.Action != store.ActionAllow {
		t.Errorf("expected allow with no PII, got %s", d2.Action)
	}
}

func TestEvaluate_PromptTokensOver(t *testing.T) {
	e := NewEngine([]Rule{
		{Name: "route-big", Condition: Condition{PromptTokensOver: 1000}, Action: store.ActionRoute, RouteToModel: "cheap-model"},
	})
	d := e.Evaluate(EvalContext{PromptTokens: 5000})
	if d.Action != store.ActionRoute || d.RouteToModel != "cheap-model" {
		t.Errorf("expected route to cheap-model, got %+v", d)
	}
	d2 := e.Evaluate(EvalContext{PromptTokens: 10})
	if d2.Action != store.ActionAllow {
		t.Errorf("expected allow for small prompt, got %s", d2.Action)
	}
}

func TestEvaluate_TeamEquals(t *testing.T) {
	e := NewEngine([]Rule{
		{Name: "block-legal-team", Condition: Condition{TeamEquals: "legal"}, Action: store.ActionBlock},
	})
	d := e.Evaluate(EvalContext{Team: "legal"})
	if d.Action != store.ActionBlock {
		t.Errorf("expected block for legal team, got %s", d.Action)
	}
	d2 := e.Evaluate(EvalContext{Team: "marketing"})
	if d2.Action != store.ActionAllow {
		t.Errorf("expected allow for marketing team, got %s", d2.Action)
	}
}

func TestSetRules_ReplacesAtomically(t *testing.T) {
	e := NewEngine([]Rule{{Name: "a", Condition: Condition{ModelEquals: "x"}, Action: store.ActionBlock}})
	e.SetRules([]Rule{{Name: "b", Condition: Condition{ModelEquals: "x"}, Action: store.ActionRedact}})
	d := e.Evaluate(EvalContext{Model: "x"})
	if d.MatchedRule != "b" {
		t.Errorf("expected rule set to be replaced, got matched rule %s", d.MatchedRule)
	}
}

func TestRules_ReturnsDefensiveCopy(t *testing.T) {
	e := NewEngine([]Rule{{Name: "a"}})
	got := e.Rules()
	got[0].Name = "mutated"
	if e.Rules()[0].Name != "a" {
		t.Error("expected Rules() to return a defensive copy")
	}
}

func TestDefaultRules_BlocksSecretsAndRedactsPII(t *testing.T) {
	e := NewEngine(DefaultRules())

	secretCtx := EvalContext{PIIFindings: []pii.Finding{{Category: pii.CategoryAPIKey}}}
	if d := e.Evaluate(secretCtx); d.Action != store.ActionBlock {
		t.Errorf("expected default rules to block API keys, got %s", d.Action)
	}

	emailCtx := EvalContext{PIIFindings: []pii.Finding{{Category: pii.CategoryEmail}}}
	if d := e.Evaluate(emailCtx); d.Action != store.ActionRedact {
		t.Errorf("expected default rules to redact email PII, got %s", d.Action)
	}

	longCtx := EvalContext{PromptTokens: 100000}
	if d := e.Evaluate(longCtx); d.Action != store.ActionRoute {
		t.Errorf("expected default rules to route long prompts, got %s", d.Action)
	}

	cleanCtx := EvalContext{PromptTokens: 10}
	if d := e.Evaluate(cleanCtx); d.Action != store.ActionAllow {
		t.Errorf("expected default rules to allow clean short prompts, got %s", d.Action)
	}
}
