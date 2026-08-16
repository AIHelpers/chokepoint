// Package config loads Chokepoint gateway configuration from environment
// variables, with sane defaults so the service can be started with
// zero configuration for local development.
package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all runtime-tunable settings for the gateway.
type Config struct {
	// ListenAddr is the address the HTTP server binds to.
	ListenAddr string
	// UpstreamTimeout bounds how long the proxy waits on the upstream
	// provider before treating the call as a timeout.
	UpstreamTimeout time.Duration
	// RetentionDays controls how long audit records are kept before
	// Prune removes them. Zero disables automatic pruning.
	RetentionDays int
	// RetainRawContent, when true, stores full prompt/response text in
	// the audit log instead of redacted excerpts. Off by default: this
	// is a compliance-relevant opt-in, not a default posture.
	RetainRawContent bool
	// MaxExcerptChars bounds how much prompt/response text is kept per
	// audit record when RetainRawContent is false.
	MaxExcerptChars int
	// OpenAIBaseURL / AnthropicBaseURL let the upstream target be
	// overridden, primarily for testing against a fake server.
	OpenAIBaseURL    string
	AnthropicBaseURL string
}

// Load builds a Config from environment variables, falling back to
// defaults for anything unset.
func Load() Config {
	return Config{
		ListenAddr:       getEnv("CHOKEPOINT_LISTEN_ADDR", ":8080"),
		UpstreamTimeout:  getEnvDuration("CHOKEPOINT_UPSTREAM_TIMEOUT", 30*time.Second),
		RetentionDays:    getEnvInt("CHOKEPOINT_RETENTION_DAYS", 90),
		RetainRawContent: getEnvBool("CHOKEPOINT_RETAIN_RAW_CONTENT", false),
		MaxExcerptChars:  getEnvInt("CHOKEPOINT_MAX_EXCERPT_CHARS", 200),
		OpenAIBaseURL:    getEnv("CHOKEPOINT_OPENAI_BASE_URL", "https://api.openai.com"),
		AnthropicBaseURL: getEnv("CHOKEPOINT_ANTHROPIC_BASE_URL", "https://api.anthropic.com"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
