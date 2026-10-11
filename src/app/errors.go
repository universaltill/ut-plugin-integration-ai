package app

import "github.com/universaltill/ut-plugin-integration-ai/src/ai"

// failureKey is the notice key that tells the shop why a provider call
// failed (ut-docs#4090), or "" to keep the caller's own wording. Key,
// credit and rate answers are named for every provider. A hosted provider
// (claude, openai) that is unreachable or fails otherwise gets its own
// wording too: "check that the AI server is running" is written for the
// shop's own server and is wrong for a vendor's API.
func failureKey(svc *ai.Service, err error) string {
	switch ai.Kind(err) {
	case ai.KindKeyRejected:
		return "integration_ai.error.key_rejected"
	case ai.KindNoCredit:
		return "integration_ai.error.no_credit"
	case ai.KindRateLimited:
		return "integration_ai.error.rate_limited"
	case ai.KindUnreachable:
		if svc.Hosted() {
			return "integration_ai.error.unreachable"
		}
	default:
		if svc.Hosted() {
			return "integration_ai.error.provider_failed"
		}
	}
	return ""
}
