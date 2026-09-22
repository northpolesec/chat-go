package linear

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/shoenig/test/must"
)

// lnMock serves POST /oauth/token and POST /graphql, routing GraphQL by the
// operationName the client sends. Unregistered operations get a GraphQL
// error body so a test that forgets a stub fails loudly.
type recordedReq struct {
	method string
	path   string
	auth   string
	body   map[string]any
}

type lnMock struct {
	srv      *httptest.Server
	mu       sync.Mutex
	ops      []string
	mints    int
	handlers map[string]func(w http.ResponseWriter, body []byte)
	requests []recordedReq
}

type captured struct {
	mu     sync.Mutex
	bodies []map[string]any
}

func (c *captured) vars(t *testing.T, i int) map[string]any {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	must.Less(t, len(c.bodies), i)
	v, _ := c.bodies[i]["variables"].(map[string]any)
	return v
}

func (c *captured) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.bodies)
}

func newLnMock(t *testing.T) *lnMock {
	t.Helper()
	m := &lnMock{handlers: map[string]func(http.ResponseWriter, []byte){}}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/oauth/token" {
			m.mu.Lock()
			m.mints++
			m.mu.Unlock()
			_, _ = w.Write([]byte(`{"access_token":"tok-1","expires_in":2591999}`))
			return
		}
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		var req struct {
			OperationName string `json:"operationName"`
		}
		_ = json.Unmarshal(body, &req)
		m.mu.Lock()
		m.ops = append(m.ops, req.OperationName)
		m.requests = append(m.requests, recordedReq{
			method: r.Method,
			path:   r.URL.Path,
			auth:   r.Header.Get("Authorization"),
			body:   parsed,
		})
		h, ok := m.handlers[req.OperationName]
		m.mu.Unlock()
		if !ok {
			_, _ = w.Write([]byte(`{"errors":[{"message":"no stub for ` + req.OperationName + `"}]}`))
			return
		}
		h(w, body)
	}))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *lnMock) on(op string, status int, body string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[op] = func(w http.ResponseWriter, _ []byte) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func (m *lnMock) onCapture(op string, status int, body string) *captured {
	c := &captured{}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[op] = func(w http.ResponseWriter, raw []byte) {
		var v map[string]any
		_ = json.Unmarshal(raw, &v)
		c.mu.Lock()
		c.bodies = append(c.bodies, v)
		c.mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
	return c
}

// onSeq answers the op with each body in turn, then the last one forever.
func (m *lnMock) onSeq(op string, responses ...struct {
	status int
	body   string
}) {
	var i int
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[op] = func(w http.ResponseWriter, _ []byte) {
		m.mu.Lock()
		r := responses[min(i, len(responses)-1)]
		i++
		m.mu.Unlock()
		w.WriteHeader(r.status)
		_, _ = w.Write([]byte(r.body))
	}
}

func (m *lnMock) recorded() []recordedReq {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]recordedReq(nil), m.requests...)
}

func (m *lnMock) calls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.ops...)
}

func (m *lnMock) tokenMints() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mints
}

func countOf(calls []string, op string) int {
	n := 0
	for _, c := range calls {
		if c == op {
			n++
		}
	}
	return n
}

const testBotUserID = "app-user-1"

func mockConfig(m *lnMock) Config {
	return Config{Token: StaticToken("tok-static"), WebhookSecret: "whsec", UserName: "chatbot", BotUserID: testBotUserID, APIURL: m.srv.URL, HTTPClient: m.srv.Client()}
}

const viewerJSON = `{"data":{"viewer":{"id":"app-user-1","displayName":"chatbot"}}}`
