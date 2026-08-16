package pii

import (
	"strings"
	"testing"
)

func TestScan_DetectsKnownCategories(t *testing.T) {
	d := New()

	tests := []struct {
		name string
		text string
		want Category
	}{
		{"email", "contact me at jane.doe@example.com please", CategoryEmail},
		{"ssn", "SSN on file: 123-45-6789", CategorySSN},
		{"phone", "call 415-555-0132 tomorrow", CategoryPhone},
		{"aws key", "AKIAABCDEFGHIJKLMNOP is a leaked key", CategoryAWSKey},
		{"api key", "use sk-abcdefghijklmnop1234 to authenticate", CategoryAPIKey},
		{"ip", "the origin was 10.0.0.42 for that request", CategoryIPAddress},
		{"jwt", "token: eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dQw4w9WgXcQ", CategoryJWT},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := d.Scan(tt.text)
			if len(findings) == 0 {
				t.Fatalf("expected at least one finding for %q", tt.text)
			}
			found := false
			for _, f := range findings {
				if f.Category == tt.want {
					found = true
				}
			}
			if !found {
				t.Errorf("expected category %s in findings, got %+v", tt.want, findings)
			}
		})
	}
}

func TestScan_NoFalsePositiveOnCleanText(t *testing.T) {
	d := New()
	clean := "The quarterly report shows revenue grew steadily across all regions."
	if findings := d.Scan(clean); len(findings) != 0 {
		t.Errorf("expected no findings, got %+v", findings)
	}
}

func TestScan_EmptyString(t *testing.T) {
	d := New()
	if findings := d.Scan(""); findings != nil {
		t.Errorf("expected nil findings for empty string, got %+v", findings)
	}
}

func TestHasSensitiveData(t *testing.T) {
	d := New()
	if !d.HasSensitiveData("email me: a@b.com") {
		t.Error("expected true for text containing an email")
	}
	if d.HasSensitiveData("nothing sensitive here") {
		t.Error("expected false for clean text")
	}
}

func TestRedact_ReplacesMatchesAndPreservesSurroundingText(t *testing.T) {
	d := New()
	text := "reach jane@example.com for details"
	redacted := d.Redact(text)

	if strings.Contains(redacted, "jane@example.com") {
		t.Errorf("expected email to be redacted, got %q", redacted)
	}
	if !strings.HasPrefix(redacted, "reach ") || !strings.HasSuffix(redacted, " for details") {
		t.Errorf("expected surrounding text preserved, got %q", redacted)
	}
	if !strings.Contains(redacted, "[REDACTED:email]") {
		t.Errorf("expected redaction placeholder, got %q", redacted)
	}
}

func TestRedact_NoFindingsReturnsOriginal(t *testing.T) {
	d := New()
	text := "nothing to redact here"
	if got := d.Redact(text); got != text {
		t.Errorf("expected unchanged text, got %q", got)
	}
}

func TestScan_FindingsAreOrderedByPosition(t *testing.T) {
	d := New()
	text := "second bob@example.com then first alice@example.com"
	findings := d.Scan(text)
	for i := 1; i < len(findings); i++ {
		if findings[i-1].Start > findings[i].Start {
			t.Errorf("findings not sorted by position: %+v", findings)
		}
	}
}
