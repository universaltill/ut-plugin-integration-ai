package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrorKind says why a provider call failed, so the plugin can tell the
// shop the reason in its own language (ut-docs#4090). The error text itself
// stays for the till's log only.
type ErrorKind int

const (
	KindOther       ErrorKind = iota // anything else: a server error, a bad answer
	KindUnreachable                  // the request never got an answer (network, DNS, TLS)
	KindKeyRejected                  // 401/403: the provider refused the key
	KindNoCredit                     // the provider account has no credit or quota left
	KindRateLimited                  // 429 / overloaded: try again shortly
)

func (k ErrorKind) String() string {
	switch k {
	case KindUnreachable:
		return "unreachable"
	case KindKeyRejected:
		return "key rejected"
	case KindNoCredit:
		return "no credit"
	case KindRateLimited:
		return "rate limited"
	default:
		return "other"
	}
}

// ProviderError is a classified provider failure. Error() is the log text,
// unchanged from before the classification.
type ProviderError struct {
	Kind ErrorKind
	msg  string
	err  error
}

func (e *ProviderError) Error() string { return e.msg }
func (e *ProviderError) Unwrap() error { return e.err }

// Kind is err's ErrorKind; KindOther for nil or an unclassified error.
func Kind(err error) ErrorKind {
	var pe *ProviderError
	if errors.As(err, &pe) {
		return pe.Kind
	}
	return KindOther
}

// unreachable wraps a transport error: the request got no answer.
func unreachable(err error) error {
	return &ProviderError{Kind: KindUnreachable, msg: err.Error(), err: err}
}

// statusError is the error for a non-200 answer from provider name. A
// 401/403 body never reaches the message: OpenAI echoes a masked fragment
// of the caller's own key, and this text reaches the till's log.
func statusError(name string, status int, raw []byte) error {
	if status == 401 || status == 403 {
		return &ProviderError{Kind: KindKeyRejected, msg: fmt.Sprintf("%s %s: authentication failed (key rejected)", name, statusText(status))}
	}
	body := strings.TrimSpace(string(raw))
	return &ProviderError{Kind: statusKind(status, body), msg: fmt.Sprintf("%s %s: %s", name, statusText(status), body)}
}

// statusKind classifies a non-200, non-401/403 answer. "No credit" is
// matched on what the providers really send: OpenAI's structured
// insufficient_quota code (429 "You have no credits remaining" / "exceeded
// your current quota") and Anthropic's 400 "Your credit balance is too
// low". Loose words like "billing" are not used: OpenAI's ordinary 429
// rate-limit message links its billing page too (review, ut-docs#4090).
func statusKind(status int, body string) ErrorKind {
	if status == 402 {
		return KindNoCredit
	}
	if status >= 400 && status < 500 {
		var envelope struct {
			Error struct {
				Code string `json:"code"`
				Type string `json:"type"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(body), &envelope) == nil &&
			(envelope.Error.Code == "insufficient_quota" || envelope.Error.Type == "insufficient_quota") {
			return KindNoCredit
		}
		if strings.Contains(strings.ToLower(body), "credit balance is too low") {
			return KindNoCredit
		}
	}
	if status == 429 || status == 529 { // 529: Anthropic "overloaded"
		return KindRateLimited
	}
	return KindOther
}
