package ai

import (
	"fmt"
	"time"
)

// AskTool is one read-only capability the model may call while answering.
// Guardrail by construction: the plugin only wires core read views
// (view_query), so the model cannot mutate anything no matter what it asks
// for.
type AskTool struct {
	Name        string
	Description string
	Params      map[string]any // JSON schema for the arguments object
	Run         func(args map[string]any) (any, error)
}

// ShopContext grounds the model's answers in this till's reality.
type ShopContext struct {
	StoreName        string
	CurrencyCode     string
	CurrencyDecimals int
	Locale           string
}

// asker is the optional provider capability behind "Ask your till". Only
// providers that can run a tool-use loop implement it; the panel hides
// itself otherwise.
type asker interface {
	ask(system, question string, tools []AskTool, deadline time.Time) (string, error)
}

// CanAsk reports whether the configured backend supports the tool-use loop.
func (s *Service) CanAsk() bool {
	if !s.Enabled() {
		return false
	}
	_, ok := s.p.(asker)
	return ok
}

// askTimeout is core's ask budget (several tool rounds on modest hardware).
// Core cancels its context at the deadline; the plugin checks it before
// every round, and the job deadline (limits.long_call_s) bounds the round
// in flight. Each round's HTTP call is also cut off by the till after 30 s
// without response headers (ut-docs#4035).
const askTimeout = 120 * time.Second

// maxToolRounds bounds the loop; a lost model must not spin forever.
const maxToolRounds = 6

func askSystemPrompt(shop ShopContext) string {
	return fmt.Sprintf(
		"You are the built-in assistant of a point-of-sale till in the shop %q. "+
			"You answer the shop manager's questions about sales, stock and till activity "+
			"using ONLY the provided tools — never invent figures. "+
			"All monetary amounts in tool results are integers in minor units of %s "+
			"(%d decimal places); convert them to normal readable amounts in your answer. "+
			"Today is %s. Be concise and practical: a short direct answer first, "+
			"then at most a few supporting numbers. If the data genuinely cannot answer "+
			"the question, say so plainly.",
		shop.StoreName, shop.CurrencyCode, shop.CurrencyDecimals,
		now().Format("Monday 2006-01-02"))
}

// Ask answers a manager's natural-language question via the provider's
// tool-use loop. Callers gate on CanAsk and treat errors as "unavailable".
func (s *Service) Ask(question string, shop ShopContext, tools []AskTool) (string, error) {
	if !s.Enabled() {
		return "", fmt.Errorf("ai disabled")
	}
	a, ok := s.p.(asker)
	if !ok {
		return "", fmt.Errorf("ask not supported by this ai provider")
	}
	if question == "" {
		return "", fmt.Errorf("question required")
	}
	return a.ask(askSystemPrompt(shop), question, tools, now().Add(askTimeout))
}

// pastDeadline is the between-rounds budget check.
func pastDeadline(deadline time.Time) error {
	if !deadline.IsZero() && !now().Before(deadline) {
		return fmt.Errorf("ask timed out after %s", askTimeout)
	}
	return nil
}
