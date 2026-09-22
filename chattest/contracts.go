// Adapter contract suites ported from packages/tests/src/{connect,thread-id,self-message}-contract.ts
// @ 6adca36 (chat v4.40.0); see PORTING.md.
package chattest

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/shoenig/test/must"
)

// AdapterFactory builds a fresh adapter for one contract case.
type AdapterFactory func(t *testing.T) chat.Adapter

// WebhookHandler is adapter.handleWebhook (not on core chat.Adapter).
type WebhookHandler interface {
	HandleWebhook(http.ResponseWriter, *http.Request)
}

// ConnectWebhookVerifier is the upstream ConnectWebhookVerifier.
// A truthy result accepts; a thrown error or a falsy result rejects (401).
type ConnectWebhookVerifier func(r *http.Request, body string) (any, error)

// ConnectBuilder rebuilds the adapter in Vercel Connect mode. Skip Connect
// cases when the factory's adapter does not implement this.
type ConnectBuilder interface {
	NewWithVerifier(t *testing.T, v ConnectWebhookVerifier) chat.Adapter
	// NewWithSecretAndVerifier is optional; ok=false skips the secret+verifier case.
	NewWithSecretAndVerifier(t *testing.T, v ConnectWebhookVerifier) (adapter chat.Adapter, ok bool)
	WebhookRequest(t *testing.T) *http.Request
}

// ThreadIDCase is a decoded thread descriptor plus an optional pinned encoding.
type ThreadIDCase struct {
	Decoded any
	Encoded string // empty = not pinned
}

// ThreadIDCaser supplies adapter-specific encode/decode fixtures.
type ThreadIDCaser interface {
	ThreadIDCases() []ThreadIDCase
}

// Named is adapter.name — the `{adapter}` in `{adapter}:...`.
type Named interface {
	Name() string
}

// DMChecker is adapter.isDM.
type DMChecker interface {
	IsDM(threadID string) bool
}

// DMFixtures are the isDM true/false thread ids.
type DMFixtures interface {
	DMThreadID() string
	NonDMThreadID() string
}

// SelfMessageRequests builds the other-user and self-authored inbound webhooks.
type SelfMessageRequests interface {
	OtherMessageRequest(t *testing.T) *http.Request
	SelfMessageRequest(t *testing.T) *http.Request
}

// DispatchWatcher observes process* calls on the mock chat the adapter
// initialized against. Implemented by MockChat.
type DispatchWatcher interface {
	Dispatched(handler string) bool
}

// ChatProvider exposes the mock chat used at Initialize.
type ChatProvider interface {
	Chat() chat.ChatInstance
}

// DispatchHandler names the process* hook a non-self message should reach.
// Defaults to "processMessage" when absent.
type DispatchHandler interface {
	DispatchHandler() string
}

// RunConnectContract is connectWebhookContract.
func RunConnectContract(t *testing.T, factory AdapterFactory) {
	t.Run("constructs with a webhookVerifier and no native secret", func(t *testing.T) {
		t.Parallel()
		b := requireConnect(t, factory)
		a := b.NewWithVerifier(t, func(*http.Request, string) (any, error) { return true, nil })
		must.True(t, a != nil, clause("createAdapter with webhookVerifier and no native secret"))
	})

	t.Run("accepts the request (200) when the verifier passes", func(t *testing.T) {
		t.Parallel()
		b := requireConnect(t, factory)
		rec := newVerifierRecorder(func(*http.Request, string) (any, error) { return true, nil })
		status := postContractWebhook(t, b.NewWithVerifier(t, rec.fn), b.WebhookRequest(t))
		must.Eq(t, 200, status, clause("handleWebhook status when verifier passes"))
		must.True(t, rec.called(), clause("verifier is invoked"))
	})

	t.Run("invokes the verifier with the request and raw body", func(t *testing.T) {
		t.Parallel()
		b := requireConnect(t, factory)
		rec := newVerifierRecorder(func(*http.Request, string) (any, error) { return true, nil })
		_ = postContractWebhook(t, b.NewWithVerifier(t, rec.fn), b.WebhookRequest(t))
		req, body, ok := rec.last()
		must.True(t, ok, clause("verifier is invoked with (request, raw body)"))
		must.True(t, req != nil, clause("verifier receives a request"))
		must.True(t, body != "", clause("verifier receives a non-empty raw body"))
	})

	t.Run("rejects (401) when the verifier throws", func(t *testing.T) {
		t.Parallel()
		b := requireConnect(t, factory)
		status := postContractWebhook(t, b.NewWithVerifier(t, func(*http.Request, string) (any, error) {
			return nil, errVerifierInvalid
		}), b.WebhookRequest(t))
		must.Eq(t, 401, status, clause("handleWebhook status when verifier throws"))
	})

	t.Run("rejects (401) when the verifier returns falsy", func(t *testing.T) {
		t.Parallel()
		b := requireConnect(t, factory)
		status := postContractWebhook(t, b.NewWithVerifier(t, func(*http.Request, string) (any, error) {
			return false, nil
		}), b.WebhookRequest(t))
		must.Eq(t, 401, status, clause("handleWebhook status when verifier returns falsy"))
	})

	t.Run("uses the verifier in place of a configured native secret", func(t *testing.T) {
		t.Parallel()
		b := requireConnect(t, factory)
		rec := newVerifierRecorder(func(*http.Request, string) (any, error) { return true, nil })
		adapter, ok := b.NewWithSecretAndVerifier(t, rec.fn)
		if !ok {
			t.Skip("adapter does not implement createAdapterWithSecretAndVerifier")
		}
		status := postContractWebhook(t, adapter, b.WebhookRequest(t))
		must.Eq(t, 200, status, clause("handleWebhook status uses verifier over native secret"))
		must.True(t, rec.called(), clause("verifier is invoked in place of the native secret"))
	})
}

// RunThreadIDContract is threadIdContract.
func RunThreadIDContract(t *testing.T, factory AdapterFactory) {
	t.Run("prefixes encoded thread ids with the adapter name", func(t *testing.T) {
		t.Parallel()
		a, cases, name := requireThreadID(t, factory)
		for _, tc := range cases {
			got, err := a.EncodeThreadID(tc.Decoded)
			must.NoError(t, err, clause("encodeThreadId"))
			must.True(t, strings.HasPrefix(got, name+":"), clause("encoded thread id starts with "+name+":"))
		}
	})

	t.Run("round-trips decode(encode(x))", func(t *testing.T) {
		t.Parallel()
		a, cases, _ := requireThreadID(t, factory)
		for _, tc := range cases {
			encoded, err := a.EncodeThreadID(tc.Decoded)
			must.NoError(t, err, clause("encodeThreadId"))
			decoded, err := a.DecodeThreadID(encoded)
			must.NoError(t, err, clause("decodeThreadId"))
			must.Eq(t, tc.Decoded, decoded, clause("decode(encode(x)) equals x"))
		}
	})

	t.Run("matches pinned encoded strings", func(t *testing.T) {
		t.Parallel()
		a, cases, _ := requireThreadID(t, factory)
		for _, tc := range cases {
			if tc.Encoded == "" {
				continue
			}
			got, err := a.EncodeThreadID(tc.Decoded)
			must.NoError(t, err, clause("encodeThreadId"))
			must.Eq(t, tc.Encoded, got, clause("pinned encoded string"))
		}
	})

	t.Run("distinguishes DM from non-DM threads", func(t *testing.T) {
		t.Parallel()
		a := factory(t)
		checker, ok := a.(DMChecker)
		if !ok {
			t.Skip("adapter does not implement isDM")
		}
		fix, ok := a.(DMFixtures)
		if !ok {
			t.Skip("adapter does not implement isDM fixtures")
		}
		must.True(t, checker.IsDM(fix.DMThreadID()), clause("isDM is true for a DM thread id"))
		must.False(t, checker.IsDM(fix.NonDMThreadID()), clause("isDM is false for a non-DM thread id"))
	})
}

// RunSelfMessageContract is selfMessageContract.
func RunSelfMessageContract(t *testing.T, factory AdapterFactory) {
	t.Run("dispatches processMessage for messages from other users", func(t *testing.T) {
		t.Parallel()
		a, reqs, watch := requireSelfMessage(t, factory)
		handler := selfHandler(a)
		status := postContractWebhook(t, a, reqs.OtherMessageRequest(t))
		must.Eq(t, 200, status, clause("handleWebhook status for a non-self message"))
		must.True(t, watch.Dispatched(handler), clause("chat dispatched "+handler))
	})

	t.Run("ignores the bot's own messages", func(t *testing.T) {
		t.Parallel()
		a, reqs, watch := requireSelfMessage(t, factory)
		handler := selfHandler(a)
		status := postContractWebhook(t, a, reqs.SelfMessageRequest(t))
		must.Eq(t, 200, status, clause("handleWebhook status for a self message"))
		must.False(t, watch.Dispatched(handler), clause("chat did not dispatch "+handler+" for a self message"))
	})
}

func selfHandler(a chat.Adapter) string {
	if h, ok := a.(DispatchHandler); ok && h.DispatchHandler() != "" {
		return h.DispatchHandler()
	}
	return "processMessage"
}

func requireConnect(t *testing.T, factory AdapterFactory) ConnectBuilder {
	t.Helper()
	a := factory(t)
	b, ok := a.(ConnectBuilder)
	if !ok {
		t.Skip("adapter does not implement ConnectBuilder (Vercel Connect webhook verification)")
	}
	return b
}

func requireThreadID(t *testing.T, factory AdapterFactory) (chat.Adapter, []ThreadIDCase, string) {
	t.Helper()
	a := factory(t)
	caser, ok := a.(ThreadIDCaser)
	if !ok {
		t.Skip("adapter does not implement ThreadIDCaser")
	}
	named, ok := a.(Named)
	if !ok {
		t.Skip("adapter does not implement Named")
	}
	return a, caser.ThreadIDCases(), named.Name()
}

func requireSelfMessage(t *testing.T, factory AdapterFactory) (chat.Adapter, SelfMessageRequests, DispatchWatcher) {
	t.Helper()
	a := factory(t)
	reqs, ok := a.(SelfMessageRequests)
	if !ok {
		t.Skip("adapter does not implement SelfMessageRequests (adapter-level self-message filter)")
	}
	_, ok = a.(WebhookHandler)
	if !ok {
		t.Skip("adapter does not implement WebhookHandler")
	}
	watch, ok := dispatchWatch(a)
	if !ok {
		t.Skip("adapter does not expose a DispatchWatcher (mock ChatInstance)")
	}
	return a, reqs, watch
}

func dispatchWatch(a chat.Adapter) (DispatchWatcher, bool) {
	if w, ok := a.(DispatchWatcher); ok {
		return w, true
	}
	p, ok := a.(ChatProvider)
	if !ok {
		return nil, false
	}
	w, ok := p.Chat().(DispatchWatcher)
	return w, ok
}

func postContractWebhook(t *testing.T, a chat.Adapter, req *http.Request) int {
	t.Helper()
	h, ok := a.(WebhookHandler)
	must.True(t, ok, clause("adapter implements HandleWebhook"))
	rec := httptest.NewRecorder()
	h.HandleWebhook(rec, req)
	return rec.Result().StatusCode
}

type verifierRecorder struct {
	mu    sync.Mutex
	calls []struct {
		req  *http.Request
		body string
	}
	inner ConnectWebhookVerifier
}

func newVerifierRecorder(inner ConnectWebhookVerifier) *verifierRecorder {
	return &verifierRecorder{inner: inner}
}

func (v *verifierRecorder) fn(r *http.Request, body string) (any, error) {
	v.mu.Lock()
	v.calls = append(v.calls, struct {
		req  *http.Request
		body string
	}{req: r, body: body})
	v.mu.Unlock()
	if v.inner != nil {
		return v.inner(r, body)
	}
	return true, nil
}

func (v *verifierRecorder) called() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.calls) > 0
}

func (v *verifierRecorder) last() (*http.Request, string, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.calls) == 0 {
		return nil, "", false
	}
	c := v.calls[len(v.calls)-1]
	return c.req, c.body, true
}

var errVerifierInvalid = errors.New("invalid token")

// IsFalsy is the JS falsy stand-in the Connect verifier / fake adapter share:
// nil, false, and "".
func IsFalsy(v any) bool {
	if v == nil {
		return true
	}
	switch x := v.(type) {
	case bool:
		return !x
	case string:
		return x == ""
	default:
		return false
	}
}
