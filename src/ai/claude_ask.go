package ai

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// claudeAskMaxTokens bounds one ask round. Answers are short (the system
// prompt asks for a direct answer and a few numbers), but on models that
// think by default the budget covers the reasoning too.
const claudeAskMaxTokens = 8192

// claudeBlock is the part of a Messages API content block the loop reads.
// The assistant's blocks themselves are echoed back as the raw JSON
// received (thinking blocks must keep their signature, tool_use its input).
type claudeBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type claudeMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string (the question) or []block
}

// claudeToolDefs is the Messages API "tools" array. A tool without a
// parameter schema still gets an object schema: the API rejects a tool
// without input_schema.
func claudeToolDefs(tools []AskTool) ([]map[string]any, map[string]AskTool) {
	defs := make([]map[string]any, 0, len(tools))
	byName := make(map[string]AskTool, len(tools))
	for _, t := range tools {
		byName[t.Name] = t
		schema := t.Params
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		defs = append(defs, map[string]any{
			"name":         t.Name,
			"description":  t.Description,
			"input_schema": schema,
		})
	}
	return defs, byName
}

// ask runs the Messages API tool-use loop (ut-docs#2835, #1792): on
// stop_reason tool_use the assistant turn goes back verbatim and every
// requested tool's result follows in ONE user turn of tool_result blocks;
// any other stop reason ends the loop with the text blocks joined. Bounded
// by the shared maxToolRounds and the ask budget, like ollama and openai.
func (p *claudeProvider) ask(system, question string, tools []AskTool, deadline time.Time) (string, error) {
	defs, byName := claudeToolDefs(tools)
	messages := []claudeMessage{{Role: "user", Content: question}}
	for range maxToolRounds {
		if err := pastDeadline(deadline); err != nil {
			return "", err
		}
		payload := map[string]any{
			"model":      p.model,
			"max_tokens": claudeAskMaxTokens,
			"system":     system,
			"messages":   messages,
		}
		if len(defs) > 0 {
			payload["tools"] = defs
		}
		status, raw, err := p.post(payload)
		if err := claudeStatusErr(status, raw, err); err != nil {
			return "", err
		}
		var resp struct {
			StopReason string            `json:"stop_reason"`
			Content    []json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return "", fmt.Errorf("parse claude response: %w", err)
		}
		blocks := make([]claudeBlock, len(resp.Content))
		for i, rb := range resp.Content {
			if err := json.Unmarshal(rb, &blocks[i]); err != nil {
				return "", fmt.Errorf("parse claude response: %w", err)
			}
		}
		switch resp.StopReason {
		case "refusal":
			return "", fmt.Errorf("model declined the request")
		case "max_tokens":
			// A cut-off sentence must never reach the manager as the answer.
			return "", fmt.Errorf("model answer cut off at %d tokens", claudeAskMaxTokens)
		}
		if resp.StopReason != "tool_use" {
			var texts []string
			for _, b := range blocks {
				if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
					texts = append(texts, b.Text)
				}
			}
			if len(texts) == 0 {
				return "", fmt.Errorf("empty model response")
			}
			return strings.Join(texts, "\n"), nil
		}
		var results []map[string]any
		for _, b := range blocks {
			if b.Type != "tool_use" {
				continue
			}
			content, failed := runClaudeToolUse(byName, b)
			result := map[string]any{
				"type":        "tool_result",
				"tool_use_id": b.ID,
				"content":     content,
			}
			if failed {
				result["is_error"] = true
			}
			results = append(results, result)
		}
		if len(results) == 0 {
			return "", fmt.Errorf("model asked for tools but named none")
		}
		messages = append(messages,
			claudeMessage{Role: "assistant", Content: resp.Content},
			claudeMessage{Role: "user", Content: results},
		)
	}
	return "", fmt.Errorf("model did not answer within %d tool rounds", maxToolRounds)
}

// runClaudeToolUse decodes a tool_use block's input object and dispatches
// through the shared runAskTool; a bad input goes back to the model as text.
// failed marks the result is_error, so the model can tell a failure from
// data (runAskTool's failures all start "error: ").
func runClaudeToolUse(byName map[string]AskTool, b claudeBlock) (content string, failed bool) {
	var args map[string]any
	if len(b.Input) > 0 && string(b.Input) != "null" {
		if err := json.Unmarshal(b.Input, &args); err != nil {
			return "error: invalid tool arguments: " + err.Error(), true
		}
	}
	content = runAskTool(byName, b.Name, args)
	return content, strings.HasPrefix(content, "error: ")
}
