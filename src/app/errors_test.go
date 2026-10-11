//go:build !wasip1

package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/universaltill/universal-till/sdk/plugin"
)

// A hosted provider's failure tells the shop why (ut-docs#4090): key
// rejected, no credit, rate limited, unreachable — never "check that the AI
// server is running", which is written for self-hosted Ollama.

const noCreditBody = `{"error":{"message":"You have no credits remaining. Add credits to continue using the API.","code":"insufficient_quota"}}`

func openaiSettings() map[string]string {
	return map[string]string{"provider": "openai", "api_key": "sk-test-key"}
}

type failure struct {
	status int
	body   string
	err    error
}

func (f failure) http(plugin.HTTPRequest) (plugin.HTTPResponse, error) {
	if f.err != nil {
		return plugin.HTTPResponse{}, f.err
	}
	return plugin.HTTPResponse{Status: f.status, Body: []byte(f.body)}, nil
}

var hostedFailures = map[string]struct {
	f    failure
	want string
}{
	"key rejected": {failure{status: 401, body: `{"error":{"message":"Incorrect API key provided: sk-tes***-key"}}`}, "integration_ai.error.key_rejected"},
	"no credit":    {failure{status: 429, body: noCreditBody}, "integration_ai.error.no_credit"},
	"rate limited": {failure{status: 429, body: `{"error":{"code":"rate_limit_exceeded"}}`}, "integration_ai.error.rate_limited"},
	"unreachable":  {failure{err: errors.New("dial tcp: no route to host")}, "integration_ai.error.unreachable"},
	"other":        {failure{status: 500, body: "boom"}, "integration_ai.error.provider_failed"},
}

func noticeKey(t *testing.T, cs []component) (level, key string) {
	t.Helper()
	notices := ofType(cs, "notice")
	if len(notices) != 1 {
		t.Fatalf("notices = %v, want one", notices)
	}
	return notices[0]["level"].(string), notices[0]["text"].(map[string]any)["key"].(string)
}

func TestAskJob_HostedFailureNamesTheReason(t *testing.T) {
	for name, tc := range hostedFailures {
		t.Run(name, func(t *testing.T) {
			h := newHost(t, openaiSettings())
			h.HTTP = tc.f.http
			answer, err := dispatch(t, AskJobEvent, jobPayload("q"))
			if err != nil {
				t.Fatal(err)
			}
			assertOwnKeys(t, answer)
			cs := components(t, answer)
			findAskForm(t, cs)
			if level, key := noticeKey(t, cs); level != "error" || key != tc.want {
				t.Fatalf("notice %s/%s, want error/%s", level, key, tc.want)
			}
			if l := literals(cs); strings.Contains(l, "sk-") || strings.Contains(l, "credits remaining") {
				t.Fatalf("raw provider text reached the operator: %q", l)
			}
		})
	}
}

// Self-hosted keeps today's wording for a server that is down or failing,
// but a key/credit/rate answer from it still names the reason.
func TestAskJob_SelfHostedKeepsServerMessage(t *testing.T) {
	for name, f := range map[string]failure{
		"down":  {err: errors.New("connection refused")},
		"error": {status: 500, body: "model not found"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHost(t, ollamaSettings())
			h.HTTP = f.http
			answer, err := dispatch(t, AskJobEvent, jobPayload("q"))
			if err != nil {
				t.Fatal(err)
			}
			if _, key := noticeKey(t, components(t, answer)); key != "integration_ai.ask.error" {
				t.Fatalf("notice %s, want integration_ai.ask.error", key)
			}
		})
	}
	h := newHost(t, ollamaSettings())
	h.HTTP = failure{status: 429, body: `{"error":"too many"}`}.http
	answer, err := dispatch(t, AskJobEvent, jobPayload("q"))
	if err != nil {
		t.Fatal(err)
	}
	if _, key := noticeKey(t, components(t, answer)); key != "integration_ai.error.rate_limited" {
		t.Fatalf("notice %s, want rate_limited", key)
	}
}

func TestIdentify_HostedFailureAnswersNotice(t *testing.T) {
	for name, tc := range hostedFailures {
		t.Run(name, func(t *testing.T) {
			h := newHost(t, openaiSettings())
			h.Uploads[photoToken] = jpegPhoto
			h.Views = catalogViews(t, nil, nil)
			h.HTTP = tc.f.http
			answer, err := dispatch(t, "catalog.identify", photoPayload())
			if err != nil {
				t.Fatalf("a hosted failure must answer a notice, not fail the job: %v", err)
			}
			assertOwnKeys(t, answer)
			if level, key := noticeKey(t, components(t, answer)); level != "error" || key != tc.want {
				t.Fatalf("notice %s/%s, want error/%s", level, key, tc.want)
			}
		})
	}
}
