// End-to-end smoke: App config → signed issue_comment @chatbot →
// ProcessMessage → Stream posts once.
package github

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/chattest"
	"github.com/shoenig/test/must"
)

func TestAdapterSmoke(t *testing.T) {
	t.Parallel()

	m := newGHMock(t)
	m.json("POST /app/installations/99/access_tokens", 201, `{"token":"ghs_1","expires_at":"2099-01-01T00:00:00Z"}`)
	m.json("GET /app", 200, `{"slug":"chatbot"}`)
	m.json("GET /users/chatbot[bot]", 200, `{"id":555,"login":"chatbot[bot]","type":"Bot"}`)
	posted := m.jsonCapture("POST /repos/o/r/issues/5/comments", 201, commentJSON(999, "Hello world"))

	a, err := New(appConfig(t, m))
	must.NoError(t, err)
	handler := &captureChat{MockChat: chattest.NewMockChat(nil)}
	must.NoError(t, a.Initialize(t.Context(), handler))

	p := issuePRPayload()
	p.Comment.Body = "@chatbot please review"
	body := mustJSON(t, p)
	req := httptest.NewRequest(http.MethodPost, "https://example.com/webhook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", eventIssue)
	req.Header.Set("X-Hub-Signature-256", sig("whsec", body))
	rec := httptest.NewRecorder()
	a.HandleWebhook(rec, req)
	must.Eq(t, http.StatusOK, rec.Code)

	must.True(t, handler.Dispatched("processMessage"))
	must.NotNil(t, handler.last.Message)
	_, ok := handler.last.Message.Raw.(RawMessage)
	must.True(t, ok)
	must.NotNil(t, handler.last.Message.IsMention)
	must.True(t, *handler.last.Message.IsMention)
	must.False(t, handler.last.Message.Author.IsMe)

	_, err = a.Stream(t.Context(), handler.last.ThreadID, streamChunks(
		chat.MarkdownTextChunk{Text: "Hello "},
		chat.MarkdownTextChunk{Text: "world"},
	), chat.StreamOptions{})
	must.NoError(t, err)

	must.Eq(t, "Hello world", posted.jsonBody(t)["body"])
	const post = "POST /repos/o/r/issues/5/comments"
	n := 0
	calls := m.callsSnapshot()
	for _, c := range calls {
		if c == post {
			n++
		}
	}
	must.Eq(t, 1, n)
	must.Eq(t, post, calls[len(calls)-1])
}
