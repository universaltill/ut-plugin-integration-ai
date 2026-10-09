package ai

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/universaltill/universal-till/sdk/plugin"
)

// postJSON sends one JSON POST through the till's http_request
// (plugin.HTTP) and returns the status and body. The host caps the body at
// 256 KiB (core read at most 1 MiB); a truncated JSON body fails to parse,
// so it is never mistaken for a whole answer.
func postJSON(rawURL string, headers map[string]string, payload any) (int, []byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return 0, nil, fmt.Errorf("invalid endpoint URL")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	h := map[string]string{"Content-Type": "application/json"}
	for k, v := range headers {
		h[k] = v
	}
	resp, err := plugin.HTTP(plugin.HTTPRequest{Method: "POST", URL: rawURL, Headers: h, Body: body})
	if err != nil {
		return 0, nil, err
	}
	return resp.Status, resp.Body, nil
}

// statusText is a short "<code> <reason>" for error messages, like
// net/http's resp.Status (net/http itself stays out of the wasm module).
func statusText(code int) string {
	reasons := map[int]string{
		400: "Bad Request", 401: "Unauthorized", 403: "Forbidden", 404: "Not Found",
		408: "Request Timeout", 413: "Payload Too Large", 429: "Too Many Requests",
		500: "Internal Server Error", 502: "Bad Gateway", 503: "Service Unavailable", 504: "Gateway Timeout",
	}
	if r, ok := reasons[code]; ok {
		return fmt.Sprintf("%d %s", code, r)
	}
	return fmt.Sprintf("%d", code)
}
