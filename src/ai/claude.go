package ai

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// claudeMessagesURL is the Anthropic Messages API (manifest:
// net:api.anthropic.com); claudeAPIVersion is the version header core's
// anthropic-sdk-go sends.
const (
	claudeMessagesURL = "https://api.anthropic.com/v1/messages"
	claudeAPIVersion  = "2023-06-01"
)

// claudeProvider is the OPTIONAL hosted backend (provider = claude,
// ADR-0126) for shops that explicitly choose a paid API with their own key.
// Identify only — there is no Claude ask loop yet, so Ask your till hides
// itself on this provider (Service.CanAsk).
//
// Core calls the Messages API through anthropic-sdk-go; the plugin speaks
// the same JSON over the till's http_request (the SDK's own HTTP client
// cannot run in the sandbox). One difference: the SDK retried a failed
// call twice; the plugin does not, so a failure reaches the cashier at once
// ("scan or search instead") instead of after three slow attempts.
type claudeProvider struct {
	apiKey string
	model  string
}

func newClaudeProvider(apiKey, model string) *claudeProvider {
	return &claudeProvider{apiKey: apiKey, model: model}
}

func (p *claudeProvider) usesRefs() {}

var ephemeral = map[string]any{"type": "ephemeral"}

func claudeText(text string) map[string]any {
	return map[string]any{"type": "text", "text": text}
}

func claudeImage(mediaType string, data []byte) map[string]any {
	return map[string]any{"type": "image", "source": map[string]any{
		"type": "base64", "media_type": mediaType, "data": base64.StdEncoding.EncodeToString(data),
	}}
}

func (p *claudeProvider) identify(photo []byte, photoMediaType string, items []CatalogItem, refs []RefImage) (*IdentifyResult, error) {
	prompt, err := identifyPrompt(items)
	if err != nil {
		return nil, err
	}
	system := []map[string]any{claudeText(prompt)}

	// Stable prefix: labelled reference images, cache breakpoint on the last
	// one (or on the system block when there are no references) so repeated
	// identifications re-read the catalog context at ~10% of the cost.
	var prefix []map[string]any
	for _, ref := range refs {
		prefix = append(prefix,
			claudeText("Reference image for item "+ref.ItemID+":"),
			claudeImage(ref.MediaType, ref.Data),
		)
	}
	if len(prefix) > 0 {
		prefix[len(prefix)-1]["cache_control"] = ephemeral
	} else {
		system[0]["cache_control"] = ephemeral
	}

	content := append(prefix,
		claudeText("Photo taken at the till — identify this product:"),
		claudeImage(photoMediaType, photo),
	)

	status, raw, err := postJSON(claudeMessagesURL, map[string]string{
		"x-api-key":         p.apiKey,
		"anthropic-version": claudeAPIVersion,
	}, map[string]any{
		"model":      p.model,
		"max_tokens": 1024,
		"system":     system,
		"output_config": map[string]any{
			"format": map[string]any{"type": "json_schema", "schema": identifySchema},
		},
		"messages": []map[string]any{{"role": "user", "content": content}},
	})
	if err != nil {
		return nil, err
	}
	if status != 200 {
		// Same posture as openai: an authentication error never carries the
		// body into the till's log.
		if status == 401 || status == 403 {
			return nil, fmt.Errorf("claude %s: authentication failed (key rejected)", statusText(status))
		}
		return nil, fmt.Errorf("claude %s: %s", statusText(status), strings.TrimSpace(string(raw)))
	}
	var resp struct {
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse claude response: %w", err)
	}
	if resp.StopReason == "refusal" {
		return nil, fmt.Errorf("model declined the request")
	}
	var text string
	for _, block := range resp.Content {
		if block.Type == "text" {
			text = block.Text
			break
		}
	}
	if text == "" {
		return nil, fmt.Errorf("empty model response")
	}
	var out IdentifyResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, fmt.Errorf("parse model response: %w", err)
	}
	return &out, nil
}
