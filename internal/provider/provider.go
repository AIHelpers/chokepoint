// Package provider abstracts over the wire formats of different LLM
// vendors (OpenAI, Anthropic, ...) so the gateway can extract the
// model name, prompt text, and token usage without caring which
// backend a given call targets. This is what lets Chokepoint be
// multi-provider from day one instead of locked to a single vendor.
package provider

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Name identifies a supported upstream provider.
type Name string

const (
	OpenAI    Name = "openai"
	Anthropic Name = "anthropic"
	Unknown   Name = "unknown"
)

// DetectFromPath infers the provider from the incoming proxy path,
// e.g. "/proxy/openai/v1/chat/completions" -> OpenAI.
func DetectFromPath(path string) Name {
	p := strings.Trim(path, "/")
	parts := strings.SplitN(p, "/", 3)
	if len(parts) < 2 {
		return Unknown
	}
	switch strings.ToLower(parts[1]) {
	case "openai":
		return OpenAI
	case "anthropic":
		return Anthropic
	default:
		return Unknown
	}
}

// UpstreamPath strips the "/proxy/{provider}" prefix, returning the
// path to forward to the upstream API, e.g.
// "/proxy/openai/v1/chat/completions" -> "/v1/chat/completions".
func UpstreamPath(path string) string {
	p := strings.Trim(path, "/")
	parts := strings.SplitN(p, "/", 3)
	if len(parts) < 3 {
		return "/"
	}
	return "/" + parts[2]
}

// RequestInfo is the vendor-agnostic view of an outgoing call extracted
// from the request body.
type RequestInfo struct {
	Model      string
	PromptText string
}

// ResponseInfo is the vendor-agnostic view of usage extracted from the
// response body.
type ResponseInfo struct {
	PromptTokens     int
	CompletionTokens int
	ResponseText     string
}

// openAIChatRequest matches the subset of the Chat Completions request
// body Chokepoint needs to inspect.
type openAIChatRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// anthropicMessagesRequest matches the subset of the Messages API
// request body Chokepoint needs to inspect.
type anthropicMessagesRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

type anthropicMessagesResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// ParseRequest extracts the model and concatenated prompt text from a
// provider-specific request body. It returns an error if the body
// isn't valid JSON; callers should treat that as "cannot inspect" and
// decide their own fail-open/fail-closed policy rather than crash.
func ParseRequest(p Name, body []byte) (RequestInfo, error) {
	switch p {
	case OpenAI:
		var req openAIChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return RequestInfo{}, fmt.Errorf("provider: parse openai request: %w", err)
		}
		var sb strings.Builder
		for _, m := range req.Messages {
			sb.WriteString(m.Content)
			sb.WriteString("\n")
		}
		return RequestInfo{Model: req.Model, PromptText: sb.String()}, nil
	case Anthropic:
		var req anthropicMessagesRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return RequestInfo{}, fmt.Errorf("provider: parse anthropic request: %w", err)
		}
		var sb strings.Builder
		for _, m := range req.Messages {
			sb.WriteString(m.Content)
			sb.WriteString("\n")
		}
		return RequestInfo{Model: req.Model, PromptText: sb.String()}, nil
	default:
		return RequestInfo{}, fmt.Errorf("provider: unsupported provider %q", p)
	}
}

// ParseResponse extracts token usage and completion text from a
// provider-specific response body.
func ParseResponse(p Name, body []byte) (ResponseInfo, error) {
	switch p {
	case OpenAI:
		var resp openAIChatResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return ResponseInfo{}, fmt.Errorf("provider: parse openai response: %w", err)
		}
		text := ""
		if len(resp.Choices) > 0 {
			text = resp.Choices[0].Message.Content
		}
		return ResponseInfo{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			ResponseText:     text,
		}, nil
	case Anthropic:
		var resp anthropicMessagesResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return ResponseInfo{}, fmt.Errorf("provider: parse anthropic response: %w", err)
		}
		text := ""
		if len(resp.Content) > 0 {
			text = resp.Content[0].Text
		}
		return ResponseInfo{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			ResponseText:     text,
		}, nil
	default:
		return ResponseInfo{}, fmt.Errorf("provider: unsupported provider %q", p)
	}
}

// SetModel rewrites the model field in a request body, used by the
// policy engine's "route to cheaper model" action. It round-trips
// through the generic form so it works for either vendor's schema
// without needing a full typed re-encode.
func SetModel(body []byte, model string) ([]byte, error) {
	var generic map[string]interface{}
	if err := json.Unmarshal(body, &generic); err != nil {
		return nil, fmt.Errorf("provider: set model: %w", err)
	}
	generic["model"] = model
	return json.Marshal(generic)
}
