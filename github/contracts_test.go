// GitHubAdapter wire-up for chattest adapter contracts (Task 2.6).
package github

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/chattest"
	"github.com/shoenig/test/must"
)

const (
	contractSecret    = "whsec"
	contractNativeSec = "native-secret"
	contractBotUserID = "777"
)

func TestAdapterContracts(t *testing.T) {
	t.Parallel()
	factory := func(t *testing.T) chat.Adapter {
		return newGithubContract(t)
	}
	t.Run("Vercel Connect webhook contract (github)", func(t *testing.T) {
		t.Parallel()
		chattest.RunConnectContract(t, factory)
	})
	t.Run("thread id contract (github)", func(t *testing.T) {
		t.Parallel()
		chattest.RunThreadIDContract(t, factory)
	})
	t.Run("self-message contract (github)", func(t *testing.T) {
		t.Parallel()
		chattest.RunSelfMessageContract(t, factory)
	})
}

type githubContract struct {
	*Adapter
	api  *ghMock
	chat *chattest.MockChat
}

func newGithubContract(t *testing.T) *githubContract {
	t.Helper()
	m := newGHMock(t)
	cfg := patConfig(m)
	cfg.BotUserID = contractBotUserID
	a, err := New(cfg)
	must.NoError(t, err)
	mock := chattest.NewMockChat(nil)
	must.NoError(t, a.Initialize(t.Context(), mock))
	return &githubContract{Adapter: a, api: m, chat: mock}
}

func (g *githubContract) NewWithVerifier(t *testing.T, v chattest.ConnectWebhookVerifier) chat.Adapter {
	t.Helper()
	return g.newConnect(t, v, false)
}

func (g *githubContract) NewWithSecretAndVerifier(t *testing.T, v chattest.ConnectWebhookVerifier) (chat.Adapter, bool) {
	t.Helper()
	return g.newConnect(t, v, true), true
}

func (g *githubContract) WebhookRequest(t *testing.T) *http.Request {
	return g.signedIssueComment(t, 1)
}

func (g *githubContract) ThreadIDCases() []chattest.ThreadIDCase {
	return []chattest.ThreadIDCase{
		{Decoded: ThreadID{Owner: "vercel", Repo: "chat", Number: 123, Type: ThreadTypePR}, Encoded: "github:vercel/chat:123"},
		{Decoded: ThreadID{Owner: "vercel", Repo: "chat", Number: 123, ReviewCommentID: 456, Type: ThreadTypePR}, Encoded: "github:vercel/chat:123:rc:456"},
		{Decoded: ThreadID{Owner: "vercel", Repo: "chat", Number: 7, Type: ThreadTypeIssue}, Encoded: "github:vercel/chat:issue:7"},
	}
}

func (g *githubContract) OtherMessageRequest(t *testing.T) *http.Request {
	return g.signedIssueComment(t, 1)
}

func (g *githubContract) SelfMessageRequest(t *testing.T) *http.Request {
	return g.signedIssueComment(t, 777)
}

func (g *githubContract) Chat() chat.ChatInstance { return g.chat }

func (g *githubContract) newConnect(t *testing.T, v chattest.ConnectWebhookVerifier, withSecret bool) chat.Adapter {
	t.Helper()
	cfg := Config{
		Token: StaticToken("ghp_t"),
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
		UserName:   "my-bot",
		BotUserID:  contractBotUserID,
		APIURL:     g.api.srv.URL,
		HTTPClient: g.api.srv.Client(),
	}
	if withSecret {
		// WebhookRequest is signed with contractSecret; a different native
		// secret here proves the verifier, not HMAC, decides (New drops the
		// secret when a verifier is set).
		cfg.WebhookSecret = contractNativeSec
	}
	a, err := New(cfg)
	must.NoError(t, err)
	must.NoError(t, a.Initialize(t.Context(), chattest.NewMockChat(nil)))
	return a
}

func (g *githubContract) signedIssueComment(t *testing.T, senderID int64) *http.Request {
	t.Helper()
	p := issuePRPayload()
	p.Sender = User{ID: senderID, Login: "alice", Type: "User"}
	body := mustJSON(t, p)
	req := httptest.NewRequest(http.MethodPost, "https://example.com/webhook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", eventIssue)
	req.Header.Set("X-Hub-Signature-256", sig(contractSecret, body))
	return req
}

var errConnectFalsy = errors.New("rejected")
