// Package pii provides detection of personally identifiable information
// and secrets (API keys, tokens) inside prompt and completion text, so
// that sensitive data can be flagged, redacted, or blocked before it
// leaves the organization or is written to the audit log.
package pii

import (
	"fmt"
	"regexp"
	"strings"
)

// Category identifies the class of sensitive data a Finding belongs to.
type Category string

const (
	CategoryEmail      Category = "email"
	CategoryPhone      Category = "phone"
	CategorySSN        Category = "ssn"
	CategoryCreditCard Category = "credit_card"
	CategoryIPAddress  Category = "ip_address"
	CategoryAPIKey     Category = "api_key"
	CategoryAWSKey     Category = "aws_key"
	CategoryJWT        Category = "jwt"
)

// Finding describes a single piece of sensitive data located in text.
type Finding struct {
	Category Category `json:"category"`
	// Match is the exact substring that triggered the finding. Callers
	// that need to avoid persisting raw sensitive data should use
	// Redacted() rather than logging Match directly.
	Match string `json:"match"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// rule pairs a category with the compiled pattern used to detect it.
type rule struct {
	category Category
	pattern  *regexp.Regexp
}

// defaultRules is intentionally conservative (favoring precision over
// recall for the regex layer); a production system would pair this with
// an NER model for nuanced natural-language PII. See Detector.WithRules
// to extend or override the rule set.
var defaultRules = []rule{
	{CategoryEmail, regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)},
	{CategorySSN, regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)},
	{CategoryCreditCard, regexp.MustCompile(`\b(?:\d[ -]?){13,16}\b`)},
	{CategoryPhone, regexp.MustCompile(`\b(?:\+?1[-.\s]?)?\(?\d{3}\)?[-.\s]\d{3}[-.\s]\d{4}\b`)},
	{CategoryIPAddress, regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|[01]?\d?\d)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d?\d)\b`)},
	{CategoryAWSKey, regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{CategoryAPIKey, regexp.MustCompile(`\b(?:sk|pk|rk|api)-[A-Za-z0-9]{16,}\b`)},
	{CategoryJWT, regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)},
}

// Detector scans text for PII and secrets using a configurable rule set.
type Detector struct {
	rules []rule
}

// New returns a Detector configured with the built-in default rules.
func New() *Detector {
	rules := make([]rule, len(defaultRules))
	copy(rules, defaultRules)
	return &Detector{rules: rules}
}

// Scan returns every Finding located in text across all configured
// rules, ordered by position. An empty result means no sensitive data
// was detected under the current rule set — it is not a guarantee the
// text is safe.
func (d *Detector) Scan(text string) []Finding {
	if text == "" {
		return nil
	}
	var findings []Finding
	for _, r := range d.rules {
		matches := r.pattern.FindAllStringIndex(text, -1)
		for _, m := range matches {
			findings = append(findings, Finding{
				Category: r.category,
				Match:    text[m[0]:m[1]],
				Start:    m[0],
				End:      m[1],
			})
		}
	}
	sortFindings(findings)
	return findings
}

// HasSensitiveData is a convenience wrapper around Scan for callers that
// only need a yes/no answer.
func (d *Detector) HasSensitiveData(text string) bool {
	for _, r := range d.rules {
		if r.pattern.MatchString(text) {
			return true
		}
	}
	return false
}

// Redact returns a copy of text with every finding replaced by a
// bracketed placeholder such as "[REDACTED:email]", preserving overall
// text length characteristics for downstream logging/debugging.
func (d *Detector) Redact(text string) string {
	findings := d.Scan(text)
	if len(findings) == 0 {
		return text
	}
	var b strings.Builder
	last := 0
	for _, f := range findings {
		if f.Start < last {
			// Overlapping match from a different rule; skip it rather
			// than corrupt the builder offset.
			continue
		}
		b.WriteString(text[last:f.Start])
		b.WriteString(fmt.Sprintf("[REDACTED:%s]", f.Category))
		last = f.End
	}
	b.WriteString(text[last:])
	return b.String()
}

func sortFindings(f []Finding) {
	// Simple insertion sort: finding counts per call are small, and this
	// avoids pulling in sort for a handful of elements while keeping
	// output deterministic for tests.
	for i := 1; i < len(f); i++ {
		j := i
		for j > 0 && f[j-1].Start > f[j].Start {
			f[j-1], f[j] = f[j], f[j-1]
			j--
		}
	}
}
