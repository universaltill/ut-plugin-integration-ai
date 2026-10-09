package ai

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// openaiBaseURL is the Chat Completions API (manifest: net:api.openai.com).
const openaiBaseURL = "https://api.openai.com/v1"

// openaiProvider is the second OPTIONAL hosted backend (provider = openai,
// ADR-0126) for shops that explicitly choose a paid API with their own key.
// It implements BOTH identify and ask — OpenAI's Chat Completions API
// supports vision input and function/tool calling on the same model.
type openaiProvider struct {
	apiKey      string
	visionModel string // camera identify
	askModel    string // tool-capable model for "Ask your till"
	baseURL     string
}

func newOpenAIProvider(apiKey, visionModel, askModel string) *openaiProvider {
	return &openaiProvider{apiKey: apiKey, visionModel: visionModel, askModel: askModel, baseURL: openaiBaseURL}
}

func (p *openaiProvider) usesRefs() {}

// openAIToolCall is OpenAI's real wire shape for a requested tool call.
// LOAD-BEARING DETAIL: Arguments is a JSON-encoded STRING, not an object
// (Ollama's shape uses an object) — get this wrong and every tool call
// silently fails to decode.
type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// openAIMessage covers every message role this provider sends or receives:
// system/user (Content only), assistant (Content and/or ToolCalls, which
// must be echoed back verbatim on the next round per the API contract), and
// tool (ToolCallID + Content = the tool's result text).
type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	Refusal    string           `json:"refusal,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

// dataURL builds the data: URL form of an image_url part OpenAI's multimodal
// content array expects.
func dataURL(mediaType string, data []byte) string {
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func (p *openaiProvider) identify(photo []byte, photoMediaType string, items []CatalogItem, refs []RefImage) (*IdentifyResult, error) {
	prompt, err := identifyPrompt(items)
	if err != nil {
		return nil, err
	}

	content := make([]map[string]any, 0, len(refs)*2+2)
	for _, ref := range refs {
		content = append(content,
			map[string]any{"type": "text", "text": "Reference image for item " + ref.ItemID + ":"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": dataURL(ref.MediaType, ref.Data)}},
		)
	}
	content = append(content,
		map[string]any{"type": "text", "text": "Photo taken at the till — identify this product:"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": dataURL(photoMediaType, photo)}},
	)

	msg, err := p.chatCompletion(map[string]any{
		"model": p.visionModel,
		"messages": []map[string]any{
			{"role": "system", "content": prompt},
			{"role": "user", "content": content},
		},
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "identify_result",
				"schema": identifySchema,
				"strict": true,
			},
		},
	})
	if err != nil {
		return nil, err
	}
	if msg.Refusal != "" {
		return nil, fmt.Errorf("model declined the request")
	}
	if msg.Content == "" {
		return nil, fmt.Errorf("empty model response")
	}
	var out IdentifyResult
	if err := json.Unmarshal([]byte(msg.Content), &out); err != nil {
		return nil, fmt.Errorf("parse model response: %w", err)
	}
	return &out, nil
}

// ask runs OpenAI's tool-use loop (POST /chat/completions with "tools"),
// bounded by the shared maxToolRounds and the ask budget.
func (p *openaiProvider) ask(system, question string, tools []AskTool, deadline time.Time) (string, error) {
	defs, byName := toolDefs(tools)
	messages := []openAIMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: question},
	}
	for range maxToolRounds {
		if err := pastDeadline(deadline); err != nil {
			return "", err
		}
		payload := map[string]any{
			"model":    p.askModel,
			"messages": messages,
		}
		if len(defs) > 0 {
			payload["tools"] = defs
		}
		msg, err := p.chatCompletion(payload)
		if err != nil {
			return "", err
		}
		if msg.Refusal != "" {
			return "", fmt.Errorf("model declined the request")
		}
		messages = append(messages, msg)
		if len(msg.ToolCalls) == 0 {
			if strings.TrimSpace(msg.Content) == "" {
				return "", fmt.Errorf("empty model response")
			}
			return msg.Content, nil
		}
		for _, tc := range msg.ToolCalls {
			messages = append(messages, openAIMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    runOpenAIToolCall(byName, tc),
			})
		}
	}
	return "", fmt.Errorf("model did not answer within %d tool rounds", maxToolRounds)
}

// runOpenAIToolCall decodes OpenAI's JSON-string arguments and dispatches
// through the shared runAskTool.
func runOpenAIToolCall(byName map[string]AskTool, tc openAIToolCall) string {
	var args map[string]any
	if s := strings.TrimSpace(tc.Function.Arguments); s != "" {
		if err := json.Unmarshal([]byte(s), &args); err != nil {
			return "error: invalid tool arguments: " + err.Error()
		}
	}
	return runAskTool(byName, tc.Function.Name, args)
}

// chatCompletion is one non-streaming POST /chat/completions exchange,
// shared by identify and the ask loop.
func (p *openaiProvider) chatCompletion(payload map[string]any) (openAIMessage, error) {
	status, raw, err := postJSON(p.baseURL+"/chat/completions",
		map[string]string{"Authorization": "Bearer " + p.apiKey}, payload)
	if err != nil {
		return openAIMessage{}, err
	}
	if status != 200 {
		// 401/403 bodies from OpenAI echo a masked fragment of the caller's
		// own API key back (e.g. "Incorrect API key provided: sk-proj-***abcd")
		// — this error reaches the till's log, so never the body here.
		if status == 401 || status == 403 {
			return openAIMessage{}, fmt.Errorf("openai %s: authentication failed (key rejected)", statusText(status))
		}
		return openAIMessage{}, fmt.Errorf("openai %s: %s", statusText(status), strings.TrimSpace(string(raw)))
	}
	var envelope struct {
		Choices []struct {
			Message openAIMessage `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return openAIMessage{}, fmt.Errorf("parse openai response: %w", err)
	}
	if len(envelope.Choices) == 0 {
		return openAIMessage{}, fmt.Errorf("empty model response")
	}
	return envelope.Choices[0].Message, nil
}
