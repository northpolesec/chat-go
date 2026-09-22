package github

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/northpolesec/chat-go/chattest"
	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/shoenig/test/must"
)

// ghMock serves the subset of the REST API the adapter calls. Handlers
// switch on method+path; unhandled paths 404 with a GitHub-shaped body.
type ghMock struct {
	srv   *httptest.Server
	mu    sync.Mutex
	calls []string // "METHOD /path"
	on    map[string]func(w http.ResponseWriter, r *http.Request)
}

func newGHMock(t *testing.T) *ghMock {
	t.Helper()
	m := &ghMock{on: map[string]func(http.ResponseWriter, *http.Request){}}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		m.mu.Lock()
		m.calls = append(m.calls, key)
		h, ok := m.on[key]
		m.mu.Unlock()
		if ok {
			h(w, r)
			return
		}
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *ghMock) json(key string, status int, body string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.on[key] = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// captured is the request body and query seen by a jsonCapture handler.
type captured struct {
	body  string
	query url.Values
}

func (m *ghMock) jsonCapture(key string, status int, body string) *captured {
	c := &captured{}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.on[key] = func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c.body = string(b)
		c.query = r.URL.Query()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
	return c
}

func (c *captured) jsonBody(t *testing.T) map[string]any {
	t.Helper()
	var v map[string]any
	must.NoError(t, json.Unmarshal([]byte(c.body), &v))
	return v
}

func (m *ghMock) callsSnapshot() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.calls...)
}

func patConfig(m *ghMock) Config {
	return Config{Token: StaticToken("ghp_t"), WebhookSecret: "whsec", UserName: "my-bot", APIURL: m.srv.URL, HTTPClient: m.srv.Client()}
}

func appConfig(t *testing.T, m *ghMock) Config {
	t.Helper()
	_, p := testKey(t)
	return Config{AppID: "12345", PrivateKey: p, InstallationID: 99, WebhookSecret: "whsec", UserName: "chatbot", APIURL: m.srv.URL, HTTPClient: m.srv.Client()}
}

func TestConstructor(t *testing.T) {
	t.Parallel()
	t.Run("uses default userName github-bot", func(t *testing.T) {
		t.Parallel()
		a, err := New(Config{Token: StaticToken("x"), WebhookSecret: "s", UserName: "github-bot"})
		must.NoError(t, err)
		must.Eq(t, "github-bot", a.UserName())
		must.Eq(t, "github", a.Name())
	})
	t.Run("throws when app fields are incomplete", func(t *testing.T) {
		t.Parallel()
		_, err := New(Config{AppID: "1", WebhookSecret: "s"})
		must.ErrorContains(t, err, "Authentication is required")
	})
	t.Run("accepts an explicit botUserId", func(t *testing.T) {
		t.Parallel()
		a, err := New(Config{Token: StaticToken("x"), WebhookSecret: "s", BotUserID: "777"})
		must.NoError(t, err)
		must.Eq(t, "777", a.BotUserID())
	})
	t.Run("should create adapter with PAT config", func(t *testing.T) {
		t.Parallel()
		a, err := New(Config{Token: StaticToken("ghp_abc"), WebhookSecret: "secret", UserName: "bot"})
		must.NoError(t, err)
		must.Eq(t, "github", a.Name())
		must.Eq(t, "bot", a.UserName())
	})
	t.Run("should set botUserId when provided in config", func(t *testing.T) {
		t.Parallel()
		a, err := New(Config{Token: StaticToken("ghp_abc"), WebhookSecret: "secret", UserName: "bot", BotUserID: "42"})
		must.NoError(t, err)
		must.Eq(t, "42", a.BotUserID())
	})
	t.Run("should return undefined botUserId when not provided", func(t *testing.T) {
		t.Parallel()
		a, err := New(Config{Token: StaticToken("x"), WebhookSecret: "s", UserName: "bot"})
		must.NoError(t, err)
		must.Eq(t, "", a.BotUserID())
	})
}

func TestConstructorEnvVarResolution(t *testing.T) {
	t.Run("throws when neither webhookSecret nor webhookVerifier is set", func(t *testing.T) {
		t.Setenv(envWebhookSecret, "")
		_, err := New(Config{Token: StaticToken("x")})
		var v *shared.ValidationError
		must.True(t, errors.As(err, &v))
		must.ErrorContains(t, err, "webhookSecret or webhookVerifier is required")
	})
	t.Run("should throw when webhookSecret is missing", func(t *testing.T) {
		t.Setenv(envWebhookSecret, "")
		_, err := New(Config{Token: StaticToken("ghp_test")})
		must.ErrorContains(t, err, "webhookSecret or webhookVerifier is required")
	})
	t.Run("should throw when no auth is provided", func(t *testing.T) {
		t.Setenv(envToken, "")
		t.Setenv(envAppID, "")
		t.Setenv(envPrivateKey, "")
		t.Setenv(envInstallationID, "")
		_, err := New(Config{WebhookSecret: "secret"})
		must.ErrorContains(t, err, "Authentication is required")
	})
	t.Run("should use default userName when not provided", func(t *testing.T) {
		t.Setenv(envBotUserName, "")
		a, err := New(Config{Token: StaticToken("ghp_test"), WebhookSecret: "secret"})
		must.NoError(t, err)
		must.Eq(t, "github-bot", a.UserName())
	})
	t.Run("should fall back to env vars for token", func(t *testing.T) {
		t.Setenv(envWebhookSecret, "env-secret")
		t.Setenv(envToken, "env-token")
		t.Setenv(envBotUserName, "env-bot")
		t.Setenv(envAppID, "")
		t.Setenv(envPrivateKey, "")
		a, err := New(Config{})
		must.NoError(t, err)
		must.Eq(t, "env-bot", a.UserName())
	})
	t.Run("should fall back to env vars for app credentials", func(t *testing.T) {
		_, p := testKey(t)
		t.Setenv(envWebhookSecret, "env-secret")
		t.Setenv(envToken, "")
		t.Setenv(envAppID, "env-app-id")
		t.Setenv(envPrivateKey, string(p))
		t.Setenv(envInstallationID, "789")
		a, err := New(Config{})
		must.NoError(t, err)
		must.Eq(t, int64(789), a.app.installationID)
	})
	t.Run("should not mix auth modes when explicit config has auth fields", func(t *testing.T) {
		t.Setenv(envToken, "env-token")
		t.Setenv(envWebhookSecret, "env-secret")
		_, err := New(Config{AppID: "123", WebhookSecret: "secret"})
		must.ErrorContains(t, err, "Authentication is required")
	})
	t.Run("auto-detects botUserId from the GITHUB_BOT_USER_ID env var", func(t *testing.T) {
		t.Setenv(envWebhookSecret, "env-secret")
		t.Setenv(envToken, "env-token")
		t.Setenv(envBotUserID, "4242")
		a, err := New(Config{})
		must.NoError(t, err)
		must.Eq(t, "4242", a.BotUserID())
	})
	t.Run("prefers an explicit botUserId over GITHUB_BOT_USER_ID", func(t *testing.T) {
		t.Setenv(envWebhookSecret, "env-secret")
		t.Setenv(envToken, "env-token")
		t.Setenv(envBotUserID, "4242")
		a, err := New(Config{BotUserID: "99"})
		must.NoError(t, err)
		must.Eq(t, "99", a.BotUserID())
	})
	t.Run("should resolve apiUrl from GITHUB_API_URL env var", func(t *testing.T) {
		t.Setenv(envWebhookSecret, "env-secret")
		t.Setenv(envToken, "env-token")
		t.Setenv(envAPIURL, "https://github.example.com/api/v3")
		a, err := New(Config{})
		must.NoError(t, err)
		must.Eq(t, "https://github.example.com/api/v3", a.api.base)
	})
}

func TestInitializeDetectsBotUserIDForPAT(t *testing.T) {
	t.Parallel()
	m := newGHMock(t)
	m.json("GET /user", 200, `{"id":424242,"login":"my-bot","type":"User"}`)
	a, err := New(patConfig(m))
	must.NoError(t, err)
	must.NoError(t, a.Initialize(t.Context(), chattest.NewMockChat(nil)))
	must.Eq(t, "424242", a.BotUserID())
}

func TestInitializeDetectsBotUserIDForApp(t *testing.T) {
	t.Parallel()
	_, p := testKey(t)
	m := newGHMock(t)
	m.json("POST /app/installations/99/access_tokens", 201, `{"token":"ghs_1","expires_at":"2099-01-01T00:00:00Z"}`)
	m.json("GET /app", 200, `{"slug":"chatbot"}`)
	m.json("GET /users/chatbot[bot]", 200, `{"id":555,"login":"chatbot[bot]","type":"Bot"}`)
	a, err := New(Config{AppID: "12345", PrivateKey: p, InstallationID: 99, WebhookSecret: "s", UserName: "chatbot", APIURL: m.srv.URL, HTTPClient: m.srv.Client()})
	must.NoError(t, err)
	must.NoError(t, a.Initialize(t.Context(), chattest.NewMockChat(nil)))
	must.Eq(t, "555", a.BotUserID())
	must.SliceContains(t, m.callsSnapshot(), "GET /app")
	must.SliceContains(t, m.callsSnapshot(), "GET /users/chatbot[bot]")
}

func TestInitializeSwallowsDetectionFailure(t *testing.T) {
	t.Parallel()
	m := newGHMock(t) // GET /user → 404
	a, err := New(patConfig(m))
	must.NoError(t, err)
	must.NoError(t, a.Initialize(t.Context(), chattest.NewMockChat(nil)))
	must.Eq(t, "", a.BotUserID())
}

func TestInitializeSkipsFetchingUserIfBotUserIDAlreadySet(t *testing.T) {
	t.Parallel()
	m := newGHMock(t)
	m.json("GET /user", 200, `{"id":424242,"login":"my-bot","type":"User"}`)
	a, err := New(Config{Token: StaticToken("ghp_t"), WebhookSecret: "whsec", UserName: "my-bot", BotUserID: "42", APIURL: m.srv.URL, HTTPClient: m.srv.Client()})
	must.NoError(t, err)
	must.NoError(t, a.Initialize(t.Context(), chattest.NewMockChat(nil)))
	must.Eq(t, "42", a.BotUserID())
	must.Eq(t, 0, len(m.callsSnapshot()))
}

func TestChannelIDFromThreadID(t *testing.T) {
	t.Parallel()
	a, err := New(Config{Token: StaticToken("x"), WebhookSecret: "s"})
	must.NoError(t, err)
	t.Run("should derive channel ID from PR-level thread", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "github:vercel/chat", a.ChannelIDFromThreadID("github:vercel/chat:123"))
	})
	t.Run("should derive channel ID from review comment thread", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "github:vercel/chat", a.ChannelIDFromThreadID("github:vercel/chat:123:rc:456"))
	})
	must.Eq(t, "github:vercel/chat", a.ChannelIDFromThreadID("github:vercel/chat:issue:7"))
}

func TestEncodeThreadIDRejectsWrongType(t *testing.T) {
	t.Parallel()
	a, err := New(Config{Token: StaticToken("x"), WebhookSecret: "s"})
	must.NoError(t, err)
	_, err = a.EncodeThreadID("not a ThreadID")
	must.ErrorContains(t, err, "ThreadID")
}

func TestEncodeThreadIDRejectsIssueWithReviewComment(t *testing.T) {
	t.Parallel()
	a, err := New(Config{Token: StaticToken("x"), WebhookSecret: "s"})
	must.NoError(t, err)
	t.Run("should throw for issue thread with reviewCommentId", func(t *testing.T) {
		t.Parallel()
		_, err := a.EncodeThreadID(ThreadID{Owner: "acme", Repo: "app", Number: 10, Type: ThreadTypeIssue, ReviewCommentID: 999})
		must.ErrorContains(t, err, "Review comments are not supported on issue threads")
	})
}

func TestDecodeThreadIDMalformed(t *testing.T) {
	t.Parallel()
	a, err := New(Config{Token: StaticToken("x"), WebhookSecret: "s"})
	must.NoError(t, err)
	t.Run("should throw for invalid thread ID prefix", func(t *testing.T) {
		t.Parallel()
		_, err := a.DecodeThreadID("slack:C123:ts")
		must.ErrorContains(t, err, "Invalid GitHub thread ID")
	})
	t.Run("should throw for malformed thread ID", func(t *testing.T) {
		t.Parallel()
		_, err := a.DecodeThreadID("github:invalid")
		must.ErrorContains(t, err, "Invalid GitHub thread ID format")
	})
	got, err := a.DecodeThreadID("github:vercel/chat:123")
	must.NoError(t, err)
	id, ok := got.(ThreadID)
	must.True(t, ok)
	must.Eq(t, ThreadID{Owner: "vercel", Repo: "chat", Number: 123, Type: ThreadTypePR}, id)
}

func TestSetBotUserIDNeverOverwrites(t *testing.T) {
	t.Parallel()
	a, err := New(Config{Token: StaticToken("x"), WebhookSecret: "s", BotUserID: "777"})
	must.NoError(t, err)
	a.captureBotUserID(User{ID: 555, Login: "other"})
	must.Eq(t, "777", a.BotUserID())
	unset, err := New(Config{Token: StaticToken("x"), WebhookSecret: "s"})
	must.NoError(t, err)
	unset.captureBotUserID(User{ID: 555})
	must.Eq(t, "555", unset.BotUserID())
	unset.captureBotUserID(User{ID: 999})
	must.Eq(t, "555", unset.BotUserID())
}

func TestConfigLogValueRedactsSecrets(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Token:         StaticToken("ghp_secret"),
		PrivateKey:    []byte("SECRET KEY"),
		WebhookSecret: "whsec-secret",
		AppID:         "12345",
		UserName:      "bot",
	}
	s := cfg.LogValue().String()
	must.False(t, strings.Contains(s, "ghp_secret"))
	must.False(t, strings.Contains(s, "SECRET KEY"))
	must.False(t, strings.Contains(s, "whsec-secret"))
	must.True(t, strings.Contains(s, "REDACTED"))
	must.True(t, strings.Contains(s, "12345"))
}
