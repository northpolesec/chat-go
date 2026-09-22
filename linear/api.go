package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/northpolesec/chat-go/internal/shared"
)

const (
	codeAuthentication = "AUTHENTICATION_ERROR"
	codeRateLimited    = "RATELIMITED"
	headerReset        = "X-RateLimit-Requests-Reset" // unix epoch milliseconds
)

// client is the thin GraphQL client: one endpoint, one method.
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

type gqlError struct {
	Message    string `json:"message"`
	Extensions struct {
		Code string `json:"code"`
	} `json:"extensions"`
}

type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []gqlError      `json:"errors"`
}

func (r gqlResponse) hasCode(code string) bool {
	for _, e := range r.Errors {
		if e.Extensions.Code == code {
			return true
		}
	}
	return false
}

// APIError is a non-2xx response that maps to no shared error type.
type APIError struct {
	Status int
	Op     string
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("linear: %s: HTTP %d: %s", e.Op, e.Status, e.Body)
}

// do posts one operation and decodes data into out (nil = discard). An
// authentication failure (HTTP 401 or an AUTHENTICATION_ERROR code)
// invalidates the token source when it can be and retries once.
func (c *client) do(ctx context.Context, op, query string, vars map[string]any, out any) error {
	for attempt := 0; ; attempt++ {
		err, authFail := c.once(ctx, op, query, vars, out)
		if !authFail || attempt == 1 {
			return err
		}
		if inv, ok := c.token.(invalidator); ok {
			inv.Invalidate()
		}
	}
}

// once returns the mapped error and whether it was an authentication failure.
func (c *client) once(ctx context.Context, op, query string, vars map[string]any, out any) (error, bool) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(map[string]any{"operationName": op, "query": query, "variables": vars}); err != nil {
		return err, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/graphql", &buf)
	if err != nil {
		return err, false
	}
	tok, err := c.token.Token(ctx)
	if err != nil {
		return err, false
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "chat-go-linear")
	resp, err := c.http.Do(req)
	if err != nil {
		return shared.NewNetworkError(adapterName, op+" failed", err), false
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return err, false
	}
	var gr gqlResponse
	parseErr := json.Unmarshal(raw, &gr)
	switch {
	case resp.StatusCode == http.StatusUnauthorized || gr.hasCode(codeAuthentication):
		return shared.NewAuthenticationError(adapterName, op+": "+joinMessages(gr.Errors)), true
	case gr.hasCode(codeRateLimited):
		return shared.NewAdapterRateLimitError(adapterName, retryAfterSeconds(resp.Header, time.Now())), false
	case resp.StatusCode/100 != 2:
		return &APIError{Status: resp.StatusCode, Op: op, Body: strings.TrimSpace(string(raw))}, false
	case parseErr != nil:
		return shared.NewNetworkError(adapterName, op+": response was not the expected JSON", parseErr), false
	case len(gr.Errors) > 0:
		return shared.NewValidationError(adapterName, op+": "+joinMessages(gr.Errors)), false
	}
	if out == nil || len(gr.Data) == 0 {
		return nil, false
	}
	if err := json.Unmarshal(gr.Data, out); err != nil {
		return shared.NewNetworkError(adapterName, op+": response data was not the expected JSON", err), false
	}
	return nil, false
}

func joinMessages(errs []gqlError) string {
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		parts = append(parts, e.Message)
	}
	return strings.Join(parts, "; ")
}

// retryAfterSeconds converts Linear's millisecond reset epoch to whole
// seconds from now, floored at 1 when the header is present; 0 when absent.
func retryAfterSeconds(h http.Header, now time.Time) int {
	ms, err := strconv.ParseInt(h.Get(headerReset), 10, 64)
	if err != nil {
		return 0
	}
	d := time.UnixMilli(ms).Sub(now)
	if d <= 0 {
		return 1
	}
	return int(d.Seconds()) + 1
}
