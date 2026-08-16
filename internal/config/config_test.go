package config

import (
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	c := Load()
	if c.ListenAddr != ":8080" {
		t.Errorf("expected default listen addr :8080, got %s", c.ListenAddr)
	}
	if c.UpstreamTimeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", c.UpstreamTimeout)
	}
	if c.RetentionDays != 90 {
		t.Errorf("expected default retention 90 days, got %d", c.RetentionDays)
	}
	if c.RetainRawContent {
		t.Error("expected RetainRawContent to default to false")
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv("CHOKEPOINT_LISTEN_ADDR", ":9090")
	t.Setenv("CHOKEPOINT_UPSTREAM_TIMEOUT", "5s")
	t.Setenv("CHOKEPOINT_RETENTION_DAYS", "30")
	t.Setenv("CHOKEPOINT_RETAIN_RAW_CONTENT", "true")

	c := Load()
	if c.ListenAddr != ":9090" {
		t.Errorf("expected overridden listen addr :9090, got %s", c.ListenAddr)
	}
	if c.UpstreamTimeout != 5*time.Second {
		t.Errorf("expected overridden timeout 5s, got %v", c.UpstreamTimeout)
	}
	if c.RetentionDays != 30 {
		t.Errorf("expected overridden retention 30, got %d", c.RetentionDays)
	}
	if !c.RetainRawContent {
		t.Error("expected RetainRawContent true")
	}
}

func TestLoad_InvalidIntFallsBackToDefault(t *testing.T) {
	t.Setenv("CHOKEPOINT_RETENTION_DAYS", "not-a-number")
	c := Load()
	if c.RetentionDays != 90 {
		t.Errorf("expected fallback to default on invalid int, got %d", c.RetentionDays)
	}
}

func TestLoad_InvalidDurationFallsBackToDefault(t *testing.T) {
	t.Setenv("CHOKEPOINT_UPSTREAM_TIMEOUT", "not-a-duration")
	c := Load()
	if c.UpstreamTimeout != 30*time.Second {
		t.Errorf("expected fallback to default on invalid duration, got %v", c.UpstreamTimeout)
	}
}
