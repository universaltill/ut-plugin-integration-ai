package ai

import (
	"net/url"
	"strings"
	"time"
)

// compatProvider is provider = openai_compatible (ut-docs#2835): the shop's
// OWN server speaking OpenAI's Chat Completions API — LM Studio, vLLM,
// llama.cpp's server, or Ollama's /v1. It reuses openaiProvider's wire code
// against the endpoint setting, with the key optional (no Authorization
// header without one). Egress is the till's net:@setting:endpoint +
// http:lan, so the key only ever goes to that endpoint (ADR-0126 §7).
//
// Like ollamaProvider it sends no reference images: the small local models
// this targets lose accuracy when dozens of images share one request. It
// deliberately does not embed *openaiProvider, so it does not pick up
// openaiProvider's usesRefs marker.
type compatProvider struct {
	inner *openaiProvider
}

func newCompatProvider(endpoint, apiKey, visionModel, askModel string) *compatProvider {
	return &compatProvider{inner: &openaiProvider{
		apiKey:      apiKey,
		visionModel: visionModel,
		askModel:    askModel,
		baseURL:     compatBaseURL(endpoint),
		name:        "openai_compatible",
	}}
}

// compatBaseURL is the endpoint with a trailing slash dropped and, when it
// has no path at all, /v1 appended — where LM Studio, vLLM, llama.cpp and
// Ollama all serve the OpenAI API — so the default endpoint works as is.
func compatBaseURL(endpoint string) string {
	base := strings.TrimSpace(endpoint)
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return strings.TrimRight(base, "/")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	if u.Path == "" {
		u.Path = "/v1"
	}
	return u.String()
}

func (p *compatProvider) identify(photo []byte, photoMediaType string, items []CatalogItem, _ []RefImage) (*IdentifyResult, error) {
	return p.inner.identify(photo, photoMediaType, items, nil)
}

func (p *compatProvider) ask(system, question string, tools []AskTool, deadline time.Time) (string, error) {
	return p.inner.ask(system, question, tools, deadline)
}
