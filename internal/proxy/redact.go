package proxy

import (
	"encoding/json"
	"fmt"

	"github.com/chokepoint/chokepoint/internal/pii"
)

// redactGenericMessages walks a request body's top-level "messages"
// array (the shape shared by both OpenAI and Anthropic request
// schemas) and redacts PII from each message's string "content"
// field, leaving every other field untouched. It operates on the
// generic map form rather than a typed struct so it works across
// provider schemas without per-vendor duplication.
func redactGenericMessages(body []byte, d *pii.Detector) ([]byte, error) {
	var generic map[string]interface{}
	if err := json.Unmarshal(body, &generic); err != nil {
		return nil, fmt.Errorf("proxy: redact request: %w", err)
	}

	rawMessages, ok := generic["messages"]
	if !ok {
		return body, nil
	}
	messages, ok := rawMessages.([]interface{})
	if !ok {
		return body, nil
	}

	for _, m := range messages {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := msg["content"].(string)
		if !ok {
			continue
		}
		msg["content"] = d.Redact(content)
	}

	return json.Marshal(generic)
}
