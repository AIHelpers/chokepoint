// Package cost computes the dollar cost of an LLM call from its token
// usage and model pricing, and provides simple spend-anomaly detection
// used by the alerting layer.
package cost

import (
	"errors"
	"fmt"
	"sync"
)

// ErrUnknownModel is returned when a cost lookup is requested for a
// model that has no registered pricing.
var ErrUnknownModel = errors.New("cost: unknown model")

// Pricing holds per-million-token pricing for a single model, expressed
// in USD, matching how providers publish their price lists.
type Pricing struct {
	Model            string
	InputPerMillion  float64
	OutputPerMillion float64
}

// Usage represents token counts for a single completed LLM call.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

// Result is the computed cost breakdown for a call.
type Result struct {
	Model      string  `json:"model"`
	InputCost  float64 `json:"input_cost_usd"`
	OutputCost float64 `json:"output_cost_usd"`
	TotalCost  float64 `json:"total_cost_usd"`
}

// Calculator computes call costs against a registry of model pricing.
// It is safe for concurrent use.
type Calculator struct {
	mu      sync.RWMutex
	pricing map[string]Pricing
}

// NewCalculator returns a Calculator seeded with a reasonable set of
// default prices for well-known models. Callers can add or override
// entries with Register.
func NewCalculator() *Calculator {
	c := &Calculator{pricing: make(map[string]Pricing)}
	for _, p := range defaultPricing() {
		c.Register(p)
	}
	return c
}

// defaultPricing returns a starter price list. Prices drift frequently,
// so operators are expected to keep this current via Register or a
// config file loaded at startup.
func defaultPricing() []Pricing {
	return []Pricing{
		{Model: "gpt-4o", InputPerMillion: 2.50, OutputPerMillion: 10.00},
		{Model: "gpt-4o-mini", InputPerMillion: 0.15, OutputPerMillion: 0.60},
		{Model: "gpt-4-turbo", InputPerMillion: 10.00, OutputPerMillion: 30.00},
		{Model: "claude-sonnet-4-6", InputPerMillion: 3.00, OutputPerMillion: 15.00},
		{Model: "claude-haiku-4-5", InputPerMillion: 0.80, OutputPerMillion: 4.00},
		{Model: "claude-opus-4-8", InputPerMillion: 15.00, OutputPerMillion: 75.00},
	}
}

// Register adds or overwrites the pricing entry for a model.
func (c *Calculator) Register(p Pricing) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pricing[p.Model] = p
}

// Calculate returns the cost of a call given its model and token usage.
// It returns ErrUnknownModel if no pricing has been registered for the
// model, so callers can decide whether to fall back to a default rate
// or surface a configuration warning.
func (c *Calculator) Calculate(model string, u Usage) (Result, error) {
	c.mu.RLock()
	p, ok := c.pricing[model]
	c.mu.RUnlock()
	if !ok {
		return Result{}, fmt.Errorf("%w: %s", ErrUnknownModel, model)
	}

	in := float64(u.PromptTokens) / 1_000_000 * p.InputPerMillion
	out := float64(u.CompletionTokens) / 1_000_000 * p.OutputPerMillion

	return Result{
		Model:      model,
		InputCost:  round4(in),
		OutputCost: round4(out),
		TotalCost:  round4(in + out),
	}, nil
}

func round4(v float64) float64 {
	// Cost figures are typically fractions of a cent; keep 4 decimal
	// places so per-call costs remain meaningful when summed.
	const factor = 10000
	return float64(int64(v*factor+0.5)) / factor
}

// AnomalyThreshold configures spend-spike detection for a rolling
// window (see IsAnomaly).
type AnomalyThreshold struct {
	// MultipleOfAverage flags a new spend value as anomalous when it
	// exceeds the historical average by this multiple (e.g. 3.0 = 3x).
	MultipleOfAverage float64
	// MinSamples is the minimum history length required before anomaly
	// detection activates, to avoid flagging noise on a cold start.
	MinSamples int
}

// DefaultAnomalyThreshold is a conservative starting point: flag spend
// that is 3x the trailing average once at least 5 data points exist.
func DefaultAnomalyThreshold() AnomalyThreshold {
	return AnomalyThreshold{MultipleOfAverage: 3.0, MinSamples: 5}
}

// IsAnomaly reports whether latest is a spend anomaly relative to the
// trailing history, using a simple average-multiple heuristic. It is
// intentionally simple and explainable rather than statistically
// sophisticated, since alert fatigue is the primary risk in this space.
func IsAnomaly(history []float64, latest float64, t AnomalyThreshold) bool {
	if len(history) < t.MinSamples {
		return false
	}
	var sum float64
	for _, v := range history {
		sum += v
	}
	avg := sum / float64(len(history))
	if avg <= 0 {
		return false
	}
	return latest > avg*t.MultipleOfAverage
}
