package slack

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/slack/api"
	"github.com/northpolesec/chat-go/statememory"
	"github.com/shoenig/test/must"
)

type recordedCall struct {
	Method string
	Path   string
	Auth   string
	Form   url.Values
	JSON   map[string]any
	Body   string
}

type slackAPIMock struct {
	t        *testing.T
	mu       sync.Mutex
	calls    []recordedCall
	routes   map[string]http.HandlerFunc
	hooks    []recordedCall
	server   *httptest.Server
	hooksURL string
}

func newSlackAPIMock(t *testing.T) *slackAPIMock {
	t.Helper()
	m := &slackAPIMock{t: t, routes: map[string]http.HandlerFunc{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/", m.serve)
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/hooks/") || r.URL.Path == "/respond" {
			m.recordHook(r)
			w.WriteHeader(200)
			_, _ = w.Write([]byte("ok"))
			return
		}
		if r.URL.Path == "/file-upload" {
			w.WriteHeader(200)
			return
		}
		m.serve(w, r)
	}))
	t.Cleanup(m.server.Close)
	m.hooksURL = m.server.URL + "/respond"
	m.on("files.getUploadURLExternal", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]any{
			"ok": true, "upload_url": m.server.URL + "/file-upload", "file_id": "F123",
		})
	})
	m.on("files.completeUploadExternal", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	return m
}

func (m *slackAPIMock) on(method string, h http.HandlerFunc) { m.routes[method] = h }

func (m *slackAPIMock) ok(method string, body map[string]any) {
	if body == nil {
		body = map[string]any{}
	}
	body["ok"] = true
	m.on(method, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, body) })
}

func (m *slackAPIMock) fail(method, code string) {
	m.on(method, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": false, "error": code})
	})
}

func (m *slackAPIMock) reject(method string) {
	m.on(method, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
}

func (m *slackAPIMock) serve(w http.ResponseWriter, r *http.Request) {
	method := strings.TrimPrefix(r.URL.Path, "/")
	m.record(r, method)
	if h, ok := m.routes[method]; ok {
		h(w, r)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (m *slackAPIMock) record(r *http.Request, method string) {
	body, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	form, _ := url.ParseQuery(string(body))
	var payload map[string]any
	if strings.Contains(r.Header.Get("Content-Type"), "json") {
		_ = json.Unmarshal(body, &payload)
	}
	m.mu.Lock()
	m.calls = append(m.calls, recordedCall{
		Method: method,
		Path:   r.URL.Path,
		Auth:   r.Header.Get("Authorization"),
		Form:   form,
		JSON:   payload,
		Body:   string(body),
	})
	m.mu.Unlock()
	r.Body = io.NopCloser(strings.NewReader(string(body)))
}

func (m *slackAPIMock) recordHook(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	var payload map[string]any
	_ = json.Unmarshal(body, &payload)
	m.mu.Lock()
	m.hooks = append(m.hooks, recordedCall{
		Method: "response_url",
		Path:   r.URL.Path,
		Auth:   r.Header.Get("Authorization"),
		JSON:   payload,
		Body:   string(body),
	})
	m.mu.Unlock()
}

func (m *slackAPIMock) last(method string) recordedCall {
	m.t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range slices.Backward(m.calls) {
		if c.Method == method {
			return c
		}
	}
	m.t.Fatalf("no call to %s", method)
	return recordedCall{}
}

func (m *slackAPIMock) count(method string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, c := range m.calls {
		if c.Method == method {
			n++
		}
	}
	return n
}

func (m *slackAPIMock) all(method string) []recordedCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []recordedCall
	for _, c := range m.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

func (m *slackAPIMock) nth(method string, n int) recordedCall {
	m.t.Helper()
	calls := m.all(method)
	if n < 1 || n > len(calls) {
		m.t.Fatalf("nth(%s, %d): have %d calls", method, n, len(calls))
	}
	return calls[n-1]
}

func (m *slackAPIMock) adapter(t *testing.T, extra Config) *SlackAdapter {
	t.Helper()
	extra.APIURL = m.server.URL + "/"
	extra.HTTPClient = m.client()
	if extra.SigningSecret == "" && extra.WebhookVerifier == nil {
		extra.SigningSecret = "test-signing-secret"
	}
	if extra.Token == nil && extra.ClientID == "" {
		extra.Token = api.StaticToken("xoxb-test-token")
	}
	a, err := New(extra)
	must.NoError(t, err)
	return a
}

func (m *slackAPIMock) client() *http.Client {
	base := m.server.Client()
	return &http.Client{Transport: rewriteHooks{base: base.Transport, hooks: m.server.URL}}
}

type rewriteHooks struct {
	base  http.RoundTripper
	hooks string
}

func (r rewriteHooks) RoundTrip(req *http.Request) (*http.Response, error) {
	host := strings.ToLower(req.URL.Hostname())
	if host == "hooks.slack.com" || host == "hooks.slack-gov.com" {
		u, _ := url.Parse(r.hooks + "/respond")
		clone := req.Clone(req.Context())
		clone.URL = u
		clone.Host = u.Host
		return r.base.RoundTrip(clone)
	}
	if r.base != nil {
		return r.base.RoundTrip(req)
	}
	return http.DefaultTransport.RoundTrip(req)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

type stateChat struct {
	stubChat
	state chat.StateAdapter
}

func (s stateChat) State() chat.StateAdapter { return s.state }

func newStateChat(t *testing.T) (stateChat, *statememory.State) {
	t.Helper()
	st := statememory.New()
	must.NoError(t, st.Connect(t.Context()))
	t.Cleanup(func() { _ = st.Disconnect(t.Context()) })
	return stateChat{state: st}, st
}

func mustAppend(t *testing.T, st chat.StateAdapter, key, val string) {
	t.Helper()
	raw, err := json.Marshal(val)
	must.NoError(t, err)
	must.NoError(t, st.AppendToList(t.Context(), key, raw, 50, 0))
}

func mustList(t *testing.T, st chat.StateAdapter, key string) []string {
	t.Helper()
	raw, err := st.GetList(t.Context(), key)
	must.NoError(t, err)
	return uniqueStrings(raw)
}
