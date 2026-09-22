// Linear adapter wire-up for chattest adapter contracts.
package linear

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/chattest"
	"github.com/shoenig/test/must"
)

const contractSecret = "whsec"

var errConnectFalsy = errors.New("rejected")

func TestAdapterContracts(t *testing.T) {
	t.Parallel()
	factory := func(t *testing.T) chat.Adapter { return newLinearContract(t) }
	t.Run("Vercel Connect webhook contract (linear)", func(t *testing.T) {
		t.Parallel()
		chattest.RunConnectContract(t, factory)
	})
	t.Run("thread id contract (linear)", func(t *testing.T) {
		t.Parallel()
		chattest.RunThreadIDContract(t, factory)
	})
	t.Run("self-message contract (linear)", func(t *testing.T) {
		t.Parallel()
		chattest.RunSelfMessageContract(t, factory)
	})
}

type linearContract struct {
	*Adapter
	api  *lnMock
	chat *chattest.MockChat
}

func newLinearContract(t *testing.T) *linearContract {
	t.Helper()
	m := newLnMock(t)
	cfg := mockConfig(m)
	cfg.WebhookSecret = contractSecret
	a, err := New(cfg)
	must.NoError(t, err)
	mock := chattest.NewMockChat(nil)
	must.NoError(t, a.Initialize(t.Context(), mock))
	return &linearContract{Adapter: a, api: m, chat: mock}
}

func (l *linearContract) NewWithVerifier(t *testing.T, v chattest.ConnectWebhookVerifier) chat.Adapter {
	t.Helper()
	return l.newConnect(t, v, false)
}

func (l *linearContract) NewWithSecretAndVerifier(t *testing.T, v chattest.ConnectWebhookVerifier) (chat.Adapter, bool) {
	t.Helper()
	return l.newConnect(t, v, true), true
}

func (l *linearContract) WebhookRequest(t *testing.T) *http.Request {
	return l.signedEvent(t, createdEvent())
}

func (l *linearContract) ThreadIDCases() []chattest.ThreadIDCase {
	return []chattest.ThreadIDCase{
		{Decoded: ThreadID{IssueID: "iss-1", SessionID: "sess-1"}, Encoded: "linear:iss-1:s:sess-1"},
	}
}

func (l *linearContract) OtherMessageRequest(t *testing.T) *http.Request {
	return l.signedEvent(t, promptedEvent())
}

func (l *linearContract) SelfMessageRequest(t *testing.T) *http.Request {
	p := promptedEvent()
	p.AgentActivity.User = &User{ID: testBotUserID, Name: "chatbot"}
	return l.signedEvent(t, p)
}

func (l *linearContract) Chat() chat.ChatInstance { return l.chat }

func (l *linearContract) newConnect(t *testing.T, v chattest.ConnectWebhookVerifier, withSecret bool) chat.Adapter {
	t.Helper()
	cfg := Config{
		Token: StaticToken("t"),
		WebhookVerifier: func(r *http.Request, body []byte) error {
			ok, err := v(r, string(body))
			if err != nil {
				return err
			}
			if chattest.IsFalsy(ok) {
				return errConnectFalsy
			}
			return nil
		},
		UserName: "chatbot", BotUserID: testBotUserID, APIURL: l.api.srv.URL, HTTPClient: l.api.srv.Client(),
	}
	if withSecret {
		cfg.WebhookSecret = "native-secret"
	}
	a, err := New(cfg)
	must.NoError(t, err)
	must.NoError(t, a.Initialize(t.Context(), chattest.NewMockChat(nil)))
	return a
}

func (l *linearContract) signedEvent(t *testing.T, p sessionEventPayload) *http.Request {
	t.Helper()
	body := mustJSON(t, p)
	req := httptest.NewRequest(http.MethodPost, "https://example.com/webhook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(headerSignature, sig(contractSecret, body))
	req.Header.Set(headerTimestamp, strconv.FormatInt(time.Now().UnixMilli(), 10))
	return req
}
