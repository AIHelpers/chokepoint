package cost

import (
	"errors"
	"testing"
)

func TestCalculate_KnownModel(t *testing.T) {
	c := NewCalculator()
	c.Register(Pricing{Model: "test-model", InputPerMillion: 1.0, OutputPerMillion: 2.0})

	res, err := c.Calculate("test-model", Usage{PromptTokens: 1_000_000, CompletionTokens: 500_000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.InputCost != 1.0 {
		t.Errorf("expected input cost 1.0, got %v", res.InputCost)
	}
	if res.OutputCost != 1.0 {
		t.Errorf("expected output cost 1.0, got %v", res.OutputCost)
	}
	if res.TotalCost != 2.0 {
		t.Errorf("expected total cost 2.0, got %v", res.TotalCost)
	}
}

func TestCalculate_UnknownModel(t *testing.T) {
	c := NewCalculator()
	_, err := c.Calculate("does-not-exist", Usage{PromptTokens: 100})
	if !errors.Is(err, ErrUnknownModel) {
		t.Errorf("expected ErrUnknownModel, got %v", err)
	}
}

func TestCalculate_ZeroTokens(t *testing.T) {
	c := NewCalculator()
	c.Register(Pricing{Model: "zero-model", InputPerMillion: 5.0, OutputPerMillion: 5.0})
	res, err := c.Calculate("zero-model", Usage{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TotalCost != 0 {
		t.Errorf("expected zero cost, got %v", res.TotalCost)
	}
}

func TestRegister_OverridesExistingPricing(t *testing.T) {
	c := NewCalculator()
	c.Register(Pricing{Model: "gpt-4o", InputPerMillion: 100, OutputPerMillion: 100})
	res, err := c.Calculate("gpt-4o", Usage{PromptTokens: 1_000_000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.InputCost != 100 {
		t.Errorf("expected override to take effect, got %v", res.InputCost)
	}
}

func TestDefaultPricing_IsRegistered(t *testing.T) {
	c := NewCalculator()
	if _, err := c.Calculate("claude-sonnet-4-6", Usage{PromptTokens: 1000}); err != nil {
		t.Errorf("expected default pricing for claude-sonnet-4-6, got error: %v", err)
	}
}

func TestIsAnomaly(t *testing.T) {
	th := DefaultAnomalyThreshold()
	history := []float64{10, 11, 9, 10, 10}

	if IsAnomaly(history, 12, th) {
		t.Error("12 should not be flagged as anomaly against ~10 average")
	}
	if !IsAnomaly(history, 100, th) {
		t.Error("100 should be flagged as anomaly against ~10 average")
	}
}

func TestIsAnomaly_InsufficientHistory(t *testing.T) {
	th := DefaultAnomalyThreshold()
	history := []float64{10, 10}
	if IsAnomaly(history, 1000, th) {
		t.Error("should not flag anomaly with insufficient history")
	}
}

func TestIsAnomaly_ZeroAverage(t *testing.T) {
	th := DefaultAnomalyThreshold()
	history := []float64{0, 0, 0, 0, 0}
	if IsAnomaly(history, 5, th) {
		t.Error("should not flag anomaly when historical average is zero")
	}
}
