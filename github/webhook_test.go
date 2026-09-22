// Ported from packages/adapter-github/src/index.test.ts describe("handleWebhook")
// :557-920 and describe("self-message detection") :921-1038. Multi-tenant and
// raw-payload logging its are not ported (single-tenant; logs never dump the body).
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/chattest"
	"github.com/shoenig/test/must"
)

const webhookSecret = "test-secret"

type captureChat struct {
	*chattest.MockChat
	last chat.ProcessMessageInput
}

func (c *captureChat) ProcessMessage(ctx context.Context, in chat.ProcessMessageInput) error {
	c.last = in
	return c.MockChat.ProcessMessage(ctx, in)
}

type webhookResp struct {
	status int
	body   string
}

func newWebhookAdapter(t *testing.T) (*Adapter, *chattest.MockChat) {
	t.Helper()
	a, err := New(Config{Token: StaticToken("x"), WebhookSecret: webhookSecret, UserName: "test-bot", BotUserID: "777"})
	must.NoError(t, err)
	mock := chattest.NewMockChat(nil)
	must.NoError(t, a.Initialize(t.Context(), mock))
	return a, mock
}

func newCaptureAdapter(t *testing.T) (*Adapter, *captureChat) {
	t.Helper()
	a, err := New(Config{Token: StaticToken("x"), WebhookSecret: webhookSecret, UserName: "test-bot", BotUserID: "777"})
	must.NoError(t, err)
	c := &captureChat{MockChat: chattest.NewMockChat(nil)}
	must.NoError(t, a.Initialize(t.Context(), c))
	return a, c
}

func callWebhook(t *testing.T, a *Adapter, event, body, signature string) webhookResp {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "https://example.com/api/webhooks/github", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", event)
	if signature != "" {
		req.Header.Set("X-Hub-Signature-256", signature)
	}
	rec := httptest.NewRecorder()
	a.HandleWebhook(rec, req)
	resp := rec.Result()
	b, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	must.NoError(t, err)
	return webhookResp{status: resp.StatusCode, body: string(b)}
}

func signedWebhook(t *testing.T, a *Adapter, event, body string) webhookResp {
	t.Helper()
	return callWebhook(t, a, event, body, sig(webhookSecret, body))
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	must.NoError(t, err)
	return string(b)
}

func issuePRPayload() issueCommentPayload {
	return issueCommentPayload{
		Action:  "created",
		Comment: Comment{ID: 100, Body: "hello", User: User{ID: 1, Login: "alice", Type: "User"}},
		Issue: Issue{
			Number: 5,
			PullRequest: &struct {
				URL string `json:"url"`
			}{URL: "https://api.github.com/repos/o/r/pulls/5"},
		},
		Repository: Repository{Name: "r", Owner: User{Login: "o"}},
		Sender:     User{ID: 1, Login: "alice", Type: "User"},
	}
}

func issuePayload() issueCommentPayload {
	p := issuePRPayload()
	p.Issue.PullRequest = nil
	return p
}

func reviewPayload() reviewCommentPayload {
	return reviewCommentPayload{
		Action:      "created",
		Comment:     Comment{ID: 200, Body: "review", User: User{ID: 2, Login: "bob", Type: "User"}},
		PullRequest: Issue{Number: 5},
		Repository:  Repository{Name: "r", Owner: User{Login: "o"}},
		Sender:      User{ID: 2, Login: "bob", Type: "User"},
	}
}

func TestHandleWebhook(t *testing.T) {
	t.Parallel()

	t.Run("should return 401 for missing signature", func(t *testing.T) {
		t.Parallel()
		a, mock := newWebhookAdapter(t)
		got := callWebhook(t, a, "ping", `{"zen":"test"}`, "")
		must.Eq(t, http.StatusUnauthorized, got.status)
		must.StrContains(t, got.body, "Invalid signature")
		must.False(t, mock.Dispatched("processMessage"))
	})

	t.Run("should return 401 for invalid signature", func(t *testing.T) {
		t.Parallel()
		a, mock := newWebhookAdapter(t)
		got := callWebhook(t, a, "ping", `{"zen":"test"}`, "sha256=invalid")
		must.Eq(t, http.StatusUnauthorized, got.status)
		must.StrContains(t, got.body, "Invalid signature")
		must.False(t, mock.Dispatched("processMessage"))
	})

	t.Run("should return 200 pong for ping event", func(t *testing.T) {
		t.Parallel()
		a, mock := newWebhookAdapter(t)
		got := signedWebhook(t, a, "ping", `{"zen":"test"}`)
		must.Eq(t, http.StatusOK, got.status)
		must.Eq(t, "pong", got.body)
		must.False(t, mock.Dispatched("processMessage"))
	})

	t.Run("should return 400 for invalid JSON", func(t *testing.T) {
		t.Parallel()
		a, mock := newWebhookAdapter(t)
		got := signedWebhook(t, a, "issue_comment", "not-json{{{")
		must.Eq(t, http.StatusBadRequest, got.status)
		must.StrContains(t, got.body, "Invalid JSON. Make sure webhook Content-Type is set to application/json")
		must.False(t, mock.Dispatched("processMessage"))
	})

	t.Run("should process issue_comment on a PR", func(t *testing.T) {
		t.Parallel()
		a, mock := newCaptureAdapter(t)
		got := signedWebhook(t, a, "issue_comment", mustJSON(t, issuePRPayload()))
		must.Eq(t, http.StatusOK, got.status)
		must.Eq(t, "ok", got.body)
		must.True(t, mock.Dispatched("processMessage"))
		must.Eq(t, "github:o/r:5", mock.last.ThreadID)
		must.Eq(t, "100", mock.last.Message.ID)
		must.True(t, mock.last.Adapter == a)
	})

	t.Run("should process issue_comment on a plain issue", func(t *testing.T) {
		t.Parallel()
		a, mock := newCaptureAdapter(t)
		got := signedWebhook(t, a, "issue_comment", mustJSON(t, issuePayload()))
		must.Eq(t, http.StatusOK, got.status)
		must.Eq(t, "ok", got.body)
		must.True(t, mock.Dispatched("processMessage"))
		must.Eq(t, "github:o/r:issue:5", mock.last.ThreadID)
		must.Eq(t, "100", mock.last.Message.ID)
		must.True(t, mock.last.Adapter == a)
	})

	t.Run("should ignore issue_comment with action other than created", func(t *testing.T) {
		t.Parallel()
		a, mock := newWebhookAdapter(t)
		p := issuePRPayload()
		p.Action = "edited"
		got := signedWebhook(t, a, "issue_comment", mustJSON(t, p))
		must.Eq(t, http.StatusOK, got.status)
		must.Eq(t, "ok", got.body)
		must.False(t, mock.Dispatched("processMessage"))
	})

	t.Run("should process pull_request_review_comment event", func(t *testing.T) {
		t.Parallel()
		a, mock := newCaptureAdapter(t)
		got := signedWebhook(t, a, "pull_request_review_comment", mustJSON(t, reviewPayload()))
		must.Eq(t, http.StatusOK, got.status)
		must.Eq(t, "ok", got.body)
		must.True(t, mock.Dispatched("processMessage"))
		must.Eq(t, "github:o/r:5:rc:200", mock.last.ThreadID)
		must.Eq(t, "200", mock.last.Message.ID)
		must.True(t, mock.last.Adapter == a)
	})

	t.Run("should use in_reply_to_id as root for review comment replies", func(t *testing.T) {
		t.Parallel()
		a, mock := newCaptureAdapter(t)
		p := reviewPayload()
		p.Comment.ID = 300
		p.Comment.InReplyToID = 200
		got := signedWebhook(t, a, "pull_request_review_comment", mustJSON(t, p))
		must.Eq(t, http.StatusOK, got.status)
		must.True(t, mock.Dispatched("processMessage"))
		must.Eq(t, "github:o/r:5:rc:200", mock.last.ThreadID)
		must.Eq(t, "300", mock.last.Message.ID)
		must.True(t, mock.last.Adapter == a)
	})

	t.Run("should ignore review_comment with action other than created", func(t *testing.T) {
		t.Parallel()
		a, mock := newWebhookAdapter(t)
		p := reviewPayload()
		p.Action = "edited"
		got := signedWebhook(t, a, "pull_request_review_comment", mustJSON(t, p))
		must.Eq(t, http.StatusOK, got.status)
		must.Eq(t, "ok", got.body)
		must.False(t, mock.Dispatched("processMessage"))
	})

	t.Run("should return ok for unrecognized event types", func(t *testing.T) {
		t.Parallel()
		a, mock := newWebhookAdapter(t)
		got := signedWebhook(t, a, "check_run", `{"action":"completed"}`)
		must.Eq(t, http.StatusOK, got.status)
		must.Eq(t, "ok", got.body)
		must.False(t, mock.Dispatched("processMessage"))
	})

	t.Run("should warn and ignore issue_comment when chat not initialized", func(t *testing.T) {
		t.Parallel()
		a, err := New(Config{Token: StaticToken("x"), WebhookSecret: webhookSecret, BotUserID: "777"})
		must.NoError(t, err)
		got := signedWebhook(t, a, "issue_comment", mustJSON(t, issuePRPayload()))
		must.Eq(t, http.StatusOK, got.status)
		must.Eq(t, "ok", got.body)
	})

	t.Run("should warn and ignore review_comment when chat not initialized", func(t *testing.T) {
		t.Parallel()
		a, err := New(Config{Token: StaticToken("x"), WebhookSecret: webhookSecret, BotUserID: "777"})
		must.NoError(t, err)
		got := signedWebhook(t, a, "pull_request_review_comment", mustJSON(t, reviewPayload()))
		must.Eq(t, http.StatusOK, got.status)
		must.Eq(t, "ok", got.body)
	})

	t.Run("accepts a custom webhookVerifier and ignores the secret", func(t *testing.T) {
		t.Parallel()
		called := false
		a, err := New(Config{
			Token:         StaticToken("x"),
			WebhookSecret: "should-be-ignored",
			WebhookVerifier: func(*http.Request, []byte) error {
				called = true
				return nil
			},
			BotUserID: "777",
		})
		must.NoError(t, err)
		must.NoError(t, a.Initialize(t.Context(), chattest.NewMockChat(nil)))
		got := callWebhook(t, a, "ping", `{"zen":"test"}`, sig("other-secret", `{"zen":"test"}`))
		must.Eq(t, http.StatusOK, got.status)
		must.Eq(t, "pong", got.body)
		must.True(t, called)
	})

	t.Run("returns 401 when webhookVerifier rejects", func(t *testing.T) {
		t.Parallel()
		a, err := New(Config{
			Token:         StaticToken("x"),
			WebhookSecret: "s",
			WebhookVerifier: func(*http.Request, []byte) error {
				return errors.New("nope")
			},
			BotUserID: "777",
		})
		must.NoError(t, err)
		body := `{"zen":"test"}`
		got := callWebhook(t, a, "ping", body, sig("s", body))
		must.Eq(t, http.StatusUnauthorized, got.status)
		must.StrContains(t, got.body, "Invalid signature")
	})

	t.Run("returns 413 when the body exceeds 25 MiB", func(t *testing.T) {
		t.Parallel()
		a, mock := newWebhookAdapter(t)
		body := bytes.Repeat([]byte("x"), 25<<20+1)
		req := httptest.NewRequest(http.MethodPost, "https://example.com/api/webhooks/github", bytes.NewReader(body))
		req.Header.Set("X-GitHub-Event", "ping")
		rec := httptest.NewRecorder()
		a.HandleWebhook(rec, req)
		resp := rec.Result()
		b, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		must.NoError(t, err)
		must.Eq(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
		must.StrContains(t, string(b), "Payload too large")
		must.False(t, mock.Dispatched("processMessage"))
	})
}

func TestHandleWebhookSelfMessageDetection(t *testing.T) {
	t.Parallel()

	t.Run("should ignore messages from the bot itself", func(t *testing.T) {
		t.Parallel()
		a, mock := newWebhookAdapter(t)
		p := issuePRPayload()
		p.Sender = User{ID: 777, Login: "test-bot", Type: "Bot"}
		got := signedWebhook(t, a, "issue_comment", mustJSON(t, p))
		must.Eq(t, http.StatusOK, got.status)
		must.Eq(t, "ok", got.body)
		must.False(t, mock.Dispatched("processMessage"))
	})

	t.Run("should ignore messages from the bot itself (review comment)", func(t *testing.T) {
		t.Parallel()
		a, mock := newWebhookAdapter(t)
		p := reviewPayload()
		p.Sender = User{ID: 777, Login: "test-bot", Type: "Bot"}
		got := signedWebhook(t, a, "pull_request_review_comment", mustJSON(t, p))
		must.Eq(t, http.StatusOK, got.status)
		must.Eq(t, "ok", got.body)
		must.False(t, mock.Dispatched("processMessage"))
	})
}
