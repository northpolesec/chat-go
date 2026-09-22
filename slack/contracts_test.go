// SlackAdapter wire-up for chattest adapter contracts (Task 32).
// Connect and thread-id run against *SlackAdapter. Self-message skips:
// Slack leaves isMe filtering to Chat (upstream Slack does not call
// selfMessageContract).
package slack

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/chattest"
	"github.com/northpolesec/chat-go/slack/api"
	"github.com/northpolesec/chat-go/statememory"
	"github.com/shoenig/test/must"
)

func TestAdapterContracts(t *testing.T) {
	t.Parallel()
	factory := func(t *testing.T) chat.Adapter {
		return newSlackContract(t)
	}
	t.Run("Vercel Connect webhook contract (slack)", func(t *testing.T) {
		t.Parallel()
		chattest.RunConnectContract(t, factory)
	})
	t.Run("thread id contract (slack)", func(t *testing.T) {
		t.Parallel()
		chattest.RunThreadIDContract(t, factory)
	})
	t.Run("self-message contract (slack)", func(t *testing.T) {
		t.Parallel()
		chattest.RunSelfMessageContract(t, factory)
	})
}

type slackContract struct {
	*SlackAdapter
	api *slackAPIMock
}

func newSlackContract(t *testing.T) *slackContract {
	t.Helper()
	apiMock := newSlackAPIMock(t)
	st := statememory.New()
	must.NoError(t, st.Connect(t.Context()))
	t.Cleanup(func() { _ = st.Disconnect(t.Context()) })
	a := apiMock.adapter(t, Config{BotUserID: "UBOT"})
	must.NoError(t, a.Initialize(t.Context(), chattest.NewMockChat(st)))
	return &slackContract{SlackAdapter: a, api: apiMock}
}

func (s *slackContract) NewWithVerifier(t *testing.T, v chattest.ConnectWebhookVerifier) chat.Adapter {
	t.Helper()
	return s.newConnect(t, v, false)
}

func (s *slackContract) NewWithSecretAndVerifier(t *testing.T, v chattest.ConnectWebhookVerifier) (chat.Adapter, bool) {
	t.Helper()
	return s.newConnect(t, v, true), true
}

func (s *slackContract) WebhookRequest(*testing.T) *http.Request {
	body := `{"type":"url_verification","challenge":"c"}`
	req := httptest.NewRequest(http.MethodPost, "https://example.com/webhook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func (s *slackContract) ThreadIDCases() []chattest.ThreadIDCase {
	return []chattest.ThreadIDCase{
		{Decoded: ThreadID{Channel: "C12345", ThreadTS: "1234567890.123456"}, Encoded: "slack:C12345:1234567890.123456"},
		{Decoded: ThreadID{Channel: "C12345", ThreadTS: ""}, Encoded: "slack:C12345:"},
	}
}

func (s *slackContract) DMThreadID() string    { return "slack:D12345:1234567890.123456" }
func (s *slackContract) NonDMThreadID() string { return "slack:C12345:1234567890.123456" }

func (s *slackContract) newConnect(t *testing.T, v chattest.ConnectWebhookVerifier, withSecret bool) chat.Adapter {
	t.Helper()
	cfg := Config{
		Token:           api.StaticToken("xoxb-test-token"),
		BotUserID:       "UBOT",
		WebhookVerifier: adaptConnectVerifier(v),
		APIURL:          s.api.server.URL + "/",
		HTTPClient:      s.api.client(),
	}
	if withSecret {
		cfg.SigningSecret = "test-signing-secret"
	}
	a, err := New(cfg)
	must.NoError(t, err)
	st := statememory.New()
	must.NoError(t, st.Connect(t.Context()))
	t.Cleanup(func() { _ = st.Disconnect(t.Context()) })
	must.NoError(t, a.Initialize(t.Context(), chattest.NewMockChat(st)))
	return a
}

func adaptConnectVerifier(v chattest.ConnectWebhookVerifier) func(http.Header, []byte) error {
	return func(h http.Header, body []byte) error {
		req := httptest.NewRequest(http.MethodPost, "https://example.com/webhook", bytes.NewReader(body))
		req.Header = h.Clone()
		result, err := v(req, string(body))
		if err != nil {
			return err
		}
		if chattest.IsFalsy(result) {
			return errConnectFalsy
		}
		return nil
	}
}

var errConnectFalsy = errors.New("verifier rejected")
