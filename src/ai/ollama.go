package ai

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ollamaProvider talks to a self-hosted Ollama server (/api/chat) running an
// open vision model — no account, no metering, nothing leaves the shop.
// Reference images are deliberately NOT sent: the small open vision models
// this targets lose accuracy when dozens of images share one request, so the
// photo + catalog text does the matching and the ai_ref corpus is kept for
// training a custom per-shop matcher later.
type ollamaProvider struct {
	endpoint string
	model    string // vision model (camera identify)
	askModel string // tool-capable text model ("Ask your till")
}

func newOllamaProvider(endpoint, model, askModel string) *ollamaProvider {
	return &ollamaProvider{
		endpoint: strings.TrimSuffix(endpoint, "/"),
		model:    model,
		askModel: askModel,
	}
}

func (p *ollamaProvider) identify(photo []byte, _ string, items []CatalogItem, _ []RefImage) (*IdentifyResult, error) {
	prompt, err := identifyPrompt(items)
	if err != nil {
		return nil, err
	}
	status, raw, err := postJSON(p.endpoint+"/api/chat", nil, map[string]any{
		"model":  p.model,
		"stream": false,
		"format": identifySchema,
		"messages": []map[string]any{{
			"role":    "user",
			"content": prompt + "\n\nIdentify the product in the attached photo.",
			"images":  []string{base64.StdEncoding.EncodeToString(photo)},
		}},
	})
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("ollama %s: %s", statusText(status), strings.TrimSpace(string(raw)))
	}
	var envelope struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("parse ollama response: %w", err)
	}
	if envelope.Message.Content == "" {
		return nil, fmt.Errorf("empty model response")
	}
	var out IdentifyResult
	if err := json.Unmarshal([]byte(envelope.Message.Content), &out); err != nil {
		return nil, fmt.Errorf("parse model response: %w", err)
	}
	return &out, nil
}

// ollamaMessage keeps the assistant's raw tool_calls so they can be echoed
// back verbatim on the next round, as the chat contract expects.
type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
	ToolName  string           `json:"tool_name,omitempty"`
}

type ollamaToolCall struct {
	Function struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"function"`
}

// toolDefs is the "tools" array both Ollama and OpenAI accept.
func toolDefs(tools []AskTool) ([]map[string]any, map[string]AskTool) {
	defs := make([]map[string]any, 0, len(tools))
	byName := make(map[string]AskTool, len(tools))
	for _, t := range tools {
		byName[t.Name] = t
		defs = append(defs, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Params,
			},
		})
	}
	return defs, byName
}

// ask runs the standard Ollama tool-use loop (/api/chat with "tools"): the
// model either answers or requests tool calls; results go back as role
// "tool" messages until it answers. Requires a tool-capable text model
// (llama3.2, qwen2.5, …) — vision models generally don't support tools,
// hence the separate ask model.
func (p *ollamaProvider) ask(system, question string, tools []AskTool, deadline time.Time) (string, error) {
	defs, byName := toolDefs(tools)
	messages := []ollamaMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: question},
	}
	for range maxToolRounds {
		if err := pastDeadline(deadline); err != nil {
			return "", err
		}
		msg, err := p.chat(p.askModel, messages, defs)
		if err != nil {
			return "", err
		}
		messages = append(messages, msg)
		if len(msg.ToolCalls) == 0 {
			if strings.TrimSpace(msg.Content) == "" {
				return "", fmt.Errorf("empty model response")
			}
			return msg.Content, nil
		}
		for _, tc := range msg.ToolCalls {
			messages = append(messages, ollamaMessage{
				Role:     "tool",
				ToolName: tc.Function.Name,
				Content:  runAskTool(byName, tc.Function.Name, tc.Function.Arguments),
			})
		}
	}
	return "", fmt.Errorf("model did not answer within %d tool rounds", maxToolRounds)
}

// runAskTool executes one requested tool; errors go back to the model as
// text so it can recover (ask differently or apologise) instead of aborting.
func runAskTool(byName map[string]AskTool, name string, args map[string]any) string {
	tool, ok := byName[name]
	if !ok {
		return fmt.Sprintf("error: unknown tool %q", name)
	}
	result, err := tool.Run(args)
	if err != nil {
		return "error: " + err.Error()
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return "error: " + err.Error()
	}
	return string(raw)
}

// chat is one non-streaming /api/chat exchange.
func (p *ollamaProvider) chat(model string, messages []ollamaMessage, tools []map[string]any) (ollamaMessage, error) {
	payload := map[string]any{
		"model":    model,
		"stream":   false,
		"messages": messages,
	}
	if len(tools) > 0 {
		payload["tools"] = tools
	}
	status, raw, err := postJSON(p.endpoint+"/api/chat", nil, payload)
	if err != nil {
		return ollamaMessage{}, err
	}
	if status != 200 {
		return ollamaMessage{}, fmt.Errorf("ollama %s: %s", statusText(status), strings.TrimSpace(string(raw)))
	}
	var envelope struct {
		Message ollamaMessage `json:"message"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return ollamaMessage{}, fmt.Errorf("parse ollama response: %w", err)
	}
	return envelope.Message, nil
}
