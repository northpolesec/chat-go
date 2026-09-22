// End-to-end smoke: config → signed created webhook →
// ProcessMessage → StartTyping → Stream → the exact activity sequence.
package linear

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/chattest"
	"github.com/northpolesec/chat-go/statememory"
	"github.com/shoenig/test/must"
)

func TestAdapterSmoke(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	m.on(opViewer, 200, viewerJSON)
	posted := m.onCapture(opActivityCreate, 200, activityCreatedJSON)

	a, err := New(Config{ClientID: "cid", ClientSecret: "sec", WebhookSecret: webhookSecret, UserName: "chatbot", APIURL: m.srv.URL, HTTPClient: m.srv.Client()})
	must.NoError(t, err)
	st := statememory.New()
	must.NoError(t, st.Connect(t.Context()))
	handler := &captureChat{MockChat: chattest.NewMockChat(st)}
	must.NoError(t, a.Initialize(t.Context(), handler))
	must.Eq(t, testBotUserID, a.BotUserID())
	must.Eq(t, 1, m.tokenMints())

	body := mustJSON(t, createdEvent())
	req := httptest.NewRequest(http.MethodPost, "https://example.com/webhooks/linear", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(headerSignature, sig(webhookSecret, body))
	req.Header.Set(headerTimestamp, strconv.FormatInt(time.Now().UnixMilli(), 10))
	rec := httptest.NewRecorder()
	a.HandleWebhook(rec, req)
	must.Eq(t, http.StatusOK, rec.Code)

	in := handler.lastInput()
	_, ok := in.Message.Raw.(RawMessage)
	must.True(t, ok)
	must.Eq(t, "linear:iss-1:s:sess-1", in.ThreadID)

	must.NoError(t, a.StartTyping(t.Context(), in.ThreadID, "", chat.TypingOptions{}))
	res, err := a.Stream(t.Context(), in.ThreadID, streamChunks(
		chat.TaskUpdateChunk{ID: "t1", Title: "Using linear graphql", Status: chat.TaskInProgress},
		chat.TaskUpdateChunk{ID: "t1", Title: "Using linear graphql", Status: chat.TaskComplete},
		chat.MarkdownTextChunk{Text: "INF-7 is a widget.\n"},
	), chat.StreamOptions{})
	must.NoError(t, err)
	must.Eq(t, "c-new", res.ID)
	must.Eq(t, []string{
		"thought:Thinking…(eph)",
		"action:Using linear graphql(eph)",
		"action:Using linear graphql",
		"response:INF-7 is a widget.",
	}, activitySeq(t, posted))
	must.Eq(t, 1, m.tokenMints()) // one token for the whole turn
}
