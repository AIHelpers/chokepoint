package provider

import (
	"encoding/json"
	"testing"
)

func TestDetectFromPath(t *testing.T) {
	tests := []struct {
		path string
		want Name
	}{
		{"/proxy/openai/v1/chat/completions", OpenAI},
		{"/proxy/anthropic/v1/messages", Anthropic},
		{"proxy/openai/v1/chat/completions", OpenAI},
		{"/proxy/unknownvendor/v1/x", Unknown},
		{"/proxy", Unknown},
		{"/", Unknown},
	}
	for _, tt := range tests {
		if got := DetectFromPath(tt.path); got != tt.want {
			t.Errorf("DetectFromPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestUpstreamPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/proxy/openai/v1/chat/completions", "/v1/chat/completions"},
		{"/proxy/anthropic/v1/messages", "/v1/messages"},
		{"/proxy/openai", "/"},
	}
	for _, tt := range tests {
		if got := UpstreamPath(tt.path); got != tt.want {
			t.Errorf("UpstreamPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestParseRequest_OpenAI(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello there"}]}`)
	info, err := ParseRequest(OpenAI, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Model != "gpt-4o" {
		t.Errorf("expected model gpt-4o, got %s", info.Model)
	}
	if info.PromptText != "hello there\n" {
		t.Errorf("expected prompt text 'hello there\\n', got %q", info.PromptText)
	}
}

func TestParseRequest_Anthropic(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}]}`)
	info, err := ParseRequest(Anthropic, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Model != "claude-sonnet-4-6" {
		t.Errorf("expected model claude-sonnet-4-6, got %s", info.Model)
	}
}

func TestParseRequest_InvalidJSON(t *testing.T) {
	_, err := ParseRequest(OpenAI, []byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseRequest_UnsupportedProvider(t *testing.T) {
	_, err := ParseRequest(Unknown, []byte(`{}`))
	if err == nil {
		t.Error("expected error for unsupported provider")
	}
}

func TestParseResponse_OpenAI(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"hi there"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`)
	info, err := ParseResponse(OpenAI, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.PromptTokens != 10 || info.CompletionTokens != 5 {
		t.Errorf("unexpected token counts: %+v", info)
	}
	if info.ResponseText != "hi there" {
		t.Errorf("expected response text 'hi there', got %q", info.ResponseText)
	}
}

func TestParseResponse_Anthropic(t *testing.T) {
	body := []byte(`{"content":[{"text":"hello"}],"usage":{"input_tokens":8,"output_tokens":3}}`)
	info, err := ParseResponse(Anthropic, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.PromptTokens != 8 || info.CompletionTokens != 3 {
		t.Errorf("unexpected token counts: %+v", info)
	}
	if info.ResponseText != "hello" {
		t.Errorf("expected response text 'hello', got %q", info.ResponseText)
	}
}

func TestSetModel_RewritesModelField(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	out, err := SetModel(body, "gpt-4o-mini")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if m["model"] != "gpt-4o-mini" {
		t.Errorf("expected model rewritten to gpt-4o-mini, got %v", m["model"])
	}
	// original fields preserved
	if _, ok := m["messages"]; !ok {
		t.Error("expected messages field preserved")
	}
}

func TestSetModel_InvalidJSON(t *testing.T) {
	_, err := SetModel([]byte("not json"), "x")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
