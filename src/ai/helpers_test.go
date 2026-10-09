//go:build !wasip1

package ai

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/universaltill/universal-till/sdk/plugin"
)

// useServer installs a FakeHost whose http_request hook plays the model
// server: the SDK's real wire path (buffer ABI, base64 bodies) runs exactly
// as on a till, nothing leaves the test process. It stands in for core's
// httptest.NewServer.
func useServer(t *testing.T, handler func(t *testing.T, req plugin.HTTPRequest) plugin.HTTPResponse) *plugin.FakeHost {
	t.Helper()
	h := plugin.NewFakeHost()
	h.HTTP = func(req plugin.HTTPRequest) (plugin.HTTPResponse, error) {
		return handler(t, req), nil
	}
	plugin.UseFakeHost(t, h)
	return h
}

// useDownServer is an unreachable endpoint: the host answers -3.
func useDownServer(t *testing.T) {
	t.Helper()
	h := plugin.NewFakeHost()
	h.HTTP = func(plugin.HTTPRequest) (plugin.HTTPResponse, error) {
		return plugin.HTTPResponse{}, errors.New("connection refused")
	}
	plugin.UseFakeHost(t, h)
}

func jsonResponse(status int, v any) plugin.HTTPResponse {
	b, _ := json.Marshal(v)
	return plugin.HTTPResponse{Status: status, Headers: map[string]string{"Content-Type": "application/json"}, Body: b}
}

func rawResponse(status int, body string) plugin.HTTPResponse {
	return plugin.HTTPResponse{Status: status, Body: []byte(body)}
}

func decodeBody(t *testing.T, req plugin.HTTPRequest) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(req.Body, &m); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	return m
}
