package linear

import (
	"errors"
	"testing"

	"github.com/northpolesec/chat-go/chattest"
	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/shoenig/test/must"
)

func TestNewRequiresWebhookSecretOrVerifier(t *testing.T) {
	t.Parallel()
	_, err := New(Config{Token: StaticToken("x")})
	var ve *shared.ValidationError
	must.True(t, errors.As(err, &ve))
}

func TestNewRequiresAuth(t *testing.T) {
	t.Parallel()
	_, err := New(Config{WebhookSecret: "s", ClientID: "only-id"})
	var ve *shared.ValidationError
	must.True(t, errors.As(err, &ve))
}

func TestNewDefaults(t *testing.T) {
	t.Parallel()
	a, err := New(Config{Token: StaticToken("x"), WebhookSecret: "s"})
	must.NoError(t, err)
	must.Eq(t, "linear", a.Name())
	must.Eq(t, "linear-bot", a.UserName())
	must.Eq(t, "", a.BotUserID())
}

func TestInitializeDetectsBotUserID(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	m.on("LinearAdapterViewer", 200, viewerJSON)
	cfg := mockConfig(m)
	cfg.BotUserID = ""
	a, err := New(cfg)
	must.NoError(t, err)
	must.NoError(t, a.Initialize(t.Context(), chattest.NewMockChat(nil)))
	must.Eq(t, "app-user-1", a.BotUserID())
}

func TestInitializeFailsWhenViewerFails(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	m.on("LinearAdapterViewer", 401, `{"errors":[{"message":"no"}]}`)
	cfg := mockConfig(m)
	cfg.BotUserID = ""
	a, err := New(cfg)
	must.NoError(t, err)
	must.Error(t, a.Initialize(t.Context(), chattest.NewMockChat(nil)))
}

func TestInitializeSkipsViewerWhenBotUserIDSet(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	a, err := New(mockConfig(m))
	must.NoError(t, err)
	must.NoError(t, a.Initialize(t.Context(), chattest.NewMockChat(nil)))
	must.Eq(t, 0, countOf(m.calls(), "LinearAdapterViewer"))
}

func TestChannelIDFromThreadID(t *testing.T) {
	t.Parallel()
	a, err := New(Config{Token: StaticToken("x"), WebhookSecret: "s"})
	must.NoError(t, err)
	must.Eq(t, "linear:iss-1", a.ChannelIDFromThreadID("linear:iss-1:s:sess-1"))
	must.Eq(t, "", a.ChannelIDFromThreadID("linear:iss-1:c:c-1"))
	enc, err := a.EncodeThreadID(ThreadID{IssueID: "iss-1", SessionID: "sess-1"})
	must.NoError(t, err)
	must.Eq(t, "linear:iss-1:s:sess-1", enc)
	_, err = a.EncodeThreadID("nope")
	must.Error(t, err)
}
