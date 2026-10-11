//go:build !wasip1

package ai

import (
	"errors"
	"strings"
	"testing"

	"github.com/universaltill/universal-till/sdk/plugin"
)

// Provider failures are classified so the plugin can tell the shop why a
// hosted provider failed (ut-docs#4090) instead of "check the AI server".

func TestErrorKind_HostedProviderAnswers(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   ErrorKind
	}{
		{"openai key rejected", 401, `{"error":{"message":"Incorrect API key provided: sk-proj-***abcd"}}`, KindKeyRejected},
		{"forbidden", 403, `{}`, KindKeyRejected},
		{"openai no credit", 429, `{"error":{"message":"You have no credits remaining. Add credits to continue using the API.","type":"insufficient_quota","code":"insufficient_quota"}}`, KindNoCredit},
		{"openai quota exceeded", 429, `{"error":{"message":"You exceeded your current quota, please check your plan and billing details.","code":"insufficient_quota"}}`, KindNoCredit},
		{"claude credit balance", 400, `{"type":"error","error":{"type":"invalid_request_error","message":"Your credit balance is too low to access the Anthropic API. Please go to Plans & Billing to upgrade or purchase credits."}}`, KindNoCredit},
		{"payment required", 402, `{}`, KindNoCredit},
		{"rate limit linking billing", 429, `{"error":{"message":"Rate limit reached for gpt-4o-mini in organization org-x on requests per min (RPM): Limit 3, Used 3, Requested 1. Please try again in 20s. Visit https://platform.openai.com/account/rate-limits to learn more. You can increase your rate limit by adding a payment method to your account at https://platform.openai.com/account/billing.","type":"requests","param":null,"code":"rate_limit_exceeded"}}`, KindRateLimited},
		{"bad request mentioning billing", 400, `{"error":{"message":"see billing docs","code":"invalid_request"}}`, KindOther},
		{"rate limited", 429, `{"error":{"message":"Rate limit reached for gpt-4o-mini","code":"rate_limit_exceeded"}}`, KindRateLimited},
		{"claude overloaded", 529, `{"type":"error","error":{"type":"overloaded_error"}}`, KindRateLimited},
		{"server error", 500, `oops`, KindOther},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for name, call := range map[string]func() error{
				"openai": func() error {
					p := testOpenAIProvider(t, func(*testing.T, plugin.HTTPRequest) plugin.HTTPResponse { return rawResponse(tc.status, tc.body) })
					_, err := p.ask("sys", "q", nil, now().Add(askTimeout))
					return err
				},
				"claude": func() error {
					p := testClaudeProvider(t, func(*testing.T, plugin.HTTPRequest) plugin.HTTPResponse { return rawResponse(tc.status, tc.body) })
					_, err := p.ask("sys", "q", nil, now().Add(askTimeout))
					return err
				},
			} {
				err := call()
				if err == nil {
					t.Fatalf("%s: no error", name)
				}
				if got := Kind(err); got != tc.want {
					t.Errorf("%s: Kind(%v) = %v, want %v", name, err, got, tc.want)
				}
				if tc.status == 401 && strings.Contains(err.Error(), "sk-proj") {
					t.Errorf("%s: a 401 body reached the error: %v", name, err)
				}
			}
		})
	}
}

func TestErrorKind_UnreachableAndPlainErrors(t *testing.T) {
	useDownServer(t)
	p := newOpenAIProvider("k", "v", "a")
	_, err := p.ask("sys", "q", nil, now().Add(askTimeout))
	if Kind(err) != KindUnreachable {
		t.Fatalf("down server: Kind(%v) = %v, want unreachable", err, Kind(err))
	}
	if inner := errors.Unwrap(err); inner == nil || inner.Error() != err.Error() {
		t.Fatalf("the transport error must stay wrapped and be the log text: %v", err)
	}
	if Kind(nil) != KindOther {
		t.Fatal("Kind(nil) must be KindOther")
	}
}

func TestService_Hosted(t *testing.T) {
	for provider, want := range map[string]bool{"claude": true, "openai": true, "ollama": false, "openai_compatible": false} {
		s := New(Config{Provider: provider, APIKey: "k", Endpoint: "http://x.local:1"})
		if s.Hosted() != want {
			t.Errorf("%s: Hosted() = %v, want %v", provider, s.Hosted(), want)
		}
	}
	if (*Service)(nil).Hosted() {
		t.Error("nil service must not be hosted")
	}
}
