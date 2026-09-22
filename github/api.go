package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/northpolesec/chat-go/internal/shared"
)

// maxResponseBytes bounds every API response read (the largest we read is
// 100 review comments with diff hunks).
const maxResponseBytes = 8 << 20

// client is the thin REST client. Header semantics for rate limits follow
// go-github's CheckResponse/parseRate (google/go-github github.go), the
// reference implementation; the library itself is not a dependency.
type client struct {
	http  HTTPDoer
	base  string // no trailing slash
	token TokenSource
}

func newClient(h HTTPDoer, apiURL string, tok TokenSource) *client {
	if h == nil {
		h = defaultHTTPClient
	}
	if apiURL == "" {
		apiURL = defaultAPIURL
	}
	return &client{http: h, base: strings.TrimRight(apiURL, "/"), token: tok}
}

// setStandardHeaders sets the headers every GitHub call carries. An empty
// bearer omits Authorization (public GET /users/{login}).
func setStandardHeaders(req *http.Request, bearer string) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "chat-go-github")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if req.Body != nil && req.Body != http.NoBody {
		req.Header.Set("Content-Type", "application/json")
	}
}

// do issues one request. in (nil = no body) is JSON-encoded with HTML
// escaping off; out (nil = discard) is JSON-decoded from a 2xx body.
// 204 and empty bodies leave out untouched.
func (c *client) do(ctx context.Context, method, path string, query url.Values, in, out any) error {
	var body io.Reader = http.NoBody
	if in != nil {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(in); err != nil {
			return err
		}
		body = &buf
	}
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	tok, err := c.token.Token(ctx)
	if err != nil {
		return err
	}
	setStandardHeaders(req, tok)
	resp, err := c.http.Do(req)
	if err != nil {
		return shared.NewNetworkError(adapterName, fmt.Sprintf("%s %s failed", method, path), err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return mapAPIError(resp, raw, method, path)
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return shared.NewNetworkError(adapterName, fmt.Sprintf("%s %s: response was not the expected JSON", method, path), err)
	}
	return nil
}

// APIError is a non-2xx GitHub response that maps to no shared error type.
type APIError struct {
	Status           int
	Message          string
	DocumentationURL string
	Method           string
	Path             string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("github: %s %s: HTTP %d", e.Method, e.Path, e.Status)
	}
	return fmt.Sprintf("github: %s %s: HTTP %d: %s", e.Method, e.Path, e.Status, e.Message)
}

// mapAPIError maps a non-2xx response: 401 → AuthenticationError; 403 with
// X-RateLimit-Remaining: 0, 403 with the secondary-limit message, or 429 →
// AdapterRateLimitError (retry-after from Retry-After seconds, else from
// X-RateLimit-Reset epoch); 404 → ResourceNotFoundError; 422 →
// ValidationError; anything else → *APIError.
func mapAPIError(resp *http.Response, body []byte, method, path string) error {
	var payload struct {
		Message          string `json:"message"`
		DocumentationURL string `json:"documentation_url"`
	}
	_ = json.Unmarshal(body, &payload)
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return shared.NewAuthenticationError(adapterName, payload.Message)
	case resp.StatusCode == http.StatusTooManyRequests,
		resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0",
		resp.StatusCode == http.StatusForbidden && strings.Contains(strings.ToLower(payload.Message), "secondary rate limit"),
		resp.StatusCode == http.StatusForbidden && strings.Contains(strings.ToLower(payload.Message), "abuse detection"):
		return shared.NewAdapterRateLimitError(adapterName, retryAfterSeconds(resp.Header, time.Now()))
	case resp.StatusCode == http.StatusNotFound:
		return shared.NewResourceNotFoundError(adapterName, "resource", path)
	case resp.StatusCode == http.StatusUnprocessableEntity:
		return shared.NewValidationError(adapterName, payload.Message)
	}
	return &APIError{Status: resp.StatusCode, Message: payload.Message, DocumentationURL: payload.DocumentationURL, Method: method, Path: path}
}

// retryAfterSeconds reads Retry-After (seconds) first, then
// X-RateLimit-Reset (unix seconds) relative to now; 0 when neither parses.
func retryAfterSeconds(h http.Header, now time.Time) int {
	if v, err := strconv.Atoi(h.Get("Retry-After")); err == nil && v > 0 {
		return v
	}
	if v, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		if d := time.Unix(v, 0).Sub(now); d > 0 {
			return int(d.Seconds()) + 1
		}
	}
	return 0
}
