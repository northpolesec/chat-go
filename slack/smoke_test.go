// End-to-end smoke. Constructs slack.New, delivers a real HMAC-signed
// event_callback, asserts ProcessMessage dispatch, then streams a reply
// and posts a card the way a consumer handler would.
package slack

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/slack/api"
	"github.com/northpolesec/chat-go/statememory"
	"github.com/shoenig/test/must"
)

const (
	smokeSecret  = "chatbot-signing-secret"
	smokeToken   = "xoxb-chatbot-token"
	smokeChannel = "D08CHATBOT"
	smokeThread  = "1234567890.000000"
	smokeTS      = "1234567890.123456"
	smokeUser    = "U123"
	smokeText    = "Search the docs for the onboarding guide"
	// Trailing newlines make each chunk committable; 256+ bytes flush the
	// native streamer. The closer is held until Finish → stopStream.
	smokeChunk1 = "Packets move from the agent to the channel. Packets move from the agent to the channel. Packets move from the agent to the channel. Packets move from the agent to the channel. Packets move from the agent to the channel. Packets move from the agent to the channel.\n"
	smokeChunk2 = "The reply is ready when the stream finishes. The reply is ready when the stream finishes. The reply is ready when the stream finishes. The reply is ready when the stream finishes. The reply is ready when the stream finishes. The reply is ready when the stream finishes.\n"
	smokeChunk3 = "Here is a card."
)

func smokeSign(secret, body string, ts int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:"))
	mac.Write([]byte(strconv.FormatInt(ts, 10)))
	mac.Write([]byte{':'})
	mac.Write([]byte(body))
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func smokeWebhook(t *testing.T, secret, body string) *http.Request {
	t.Helper()
	ts := time.Now().Unix()
	req := httptest.NewRequest(http.MethodPost, "https://example.com/webhook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Slack-Request-Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("X-Slack-Signature", smokeSign(secret, body, ts))
	return req.WithContext(t.Context())
}

func smokeMethods(m *slackAPIMock) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.calls))
	for i, c := range m.calls {
		out[i] = c.Method
	}
	return out
}

type smokeChat struct {
	stubChat
	t     *testing.T
	state chat.StateAdapter

	mu         sync.Mutex
	in         chat.ProcessMessageInput
	dispatched bool
	streamErr  error
	postErr    error
}

func (c *smokeChat) State() chat.StateAdapter { return c.state }

func (c *smokeChat) ProcessMessage(ctx context.Context, in chat.ProcessMessageInput) error {
	c.mu.Lock()
	c.in = in
	c.dispatched = true
	c.mu.Unlock()

	streamer, ok := in.Adapter.(chat.Streamer)
	if !ok {
		c.t.Errorf("adapter does not implement chat.Streamer")
		return nil
	}
	_, err := streamer.Stream(ctx, in.ThreadID, mdStream(smokeChunk1, smokeChunk2, smokeChunk3), chat.StreamOptions{})
	c.mu.Lock()
	c.streamErr = err
	c.mu.Unlock()
	if err != nil {
		return err
	}
	_, err = in.Adapter.PostMessage(ctx, in.ThreadID, chat.Card{
		Type:     "card",
		Title:    "Project notes",
		Children: []any{chat.CardText("See the docs.")},
	})
	c.mu.Lock()
	c.postErr = err
	c.mu.Unlock()
	return err
}

func TestAdapterSmoke(t *testing.T) {
	t.Parallel()

	apiMock := newSlackAPIMock(t)
	apiMock.nativeOK()
	apiMock.ok("auth.test", map[string]any{
		"user_id": "U_BOT", "bot_id": "B_BOT", "user": "chatbot",
	})
	apiMock.ok("users.info", map[string]any{
		"user": map[string]any{
			"id": smokeUser, "name": "alice", "real_name": "Alice Example",
			"profile": map[string]any{"display_name": "Alice", "real_name": "Alice Example"},
		},
	})
	apiMock.ok("chat.postMessage", map[string]any{"ts": "1234567890.999999"})

	adapter, err := New(Config{
		Token:         api.StaticToken(smokeToken),
		SigningSecret: smokeSecret,
		APIURL:        apiMock.server.URL + "/",
		HTTPClient:    apiMock.client(),
		AgentView:     true,
		FeedbackButtons: &FeedbackButtonsOptions{
			ActionID: "message_feedback",
		},
		SuggestedPrompts: &chat.SuggestedPrompts{
			Title: "What can I help with?",
			Prompts: []chat.SuggestedPrompt{
				{Title: "Search the docs", Message: "Search the docs for the onboarding guide"},
				{Title: "Find a page", Message: "Where does the handbook live?"},
				{Title: "Summarize a thread", Message: "Summarize this Slack thread: <paste permalink>"},
			},
		},
	})
	must.NoError(t, err)

	st := statememory.New()
	must.NoError(t, st.Connect(t.Context()))
	t.Cleanup(func() { _ = st.Disconnect(t.Context()) })
	handler := &smokeChat{t: t, state: st}
	must.NoError(t, adapter.Initialize(t.Context(), handler))

	inner := map[string]any{
		"type": "message", "channel": smokeChannel, "channel_type": "im",
		"user": smokeUser, "text": smokeText, "ts": smokeTS, "thread_ts": smokeThread,
	}
	body, err := json.Marshal(map[string]any{
		"type": "event_callback", "event_id": "EvSMOKE", "event": inner,
	})
	must.NoError(t, err)

	rec := httptest.NewRecorder()
	adapter.HandleWebhook(rec, smokeWebhook(t, smokeSecret, string(body)))
	must.Eq(t, http.StatusOK, rec.Code)

	waitCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	must.NoError(t, adapter.waitPending(waitCtx))

	handler.mu.Lock()
	must.True(t, handler.dispatched)
	must.NoError(t, handler.streamErr)
	must.NoError(t, handler.postErr)
	got := handler.in
	handler.mu.Unlock()

	t.Run("ProcessMessage dispatch", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "slack:"+smokeChannel+":"+smokeThread, got.ThreadID)
		must.True(t, got.Message != nil)
		must.Eq(t, smokeTS, got.Message.ID)
		must.Eq(t, smokeText, got.Message.Text)
		must.Eq(t, smokeUser, got.Message.Author.UserID)
		raw, ok := got.Message.Raw.(SlackEvent)
		must.True(t, ok)
		must.Eq(t, "message", raw.Type)
		must.Eq(t, smokeChannel, raw.Channel)
		must.Eq(t, smokeUser, raw.User)
		must.Eq(t, smokeText, raw.Text)
		must.Eq(t, smokeTS, raw.Ts)
		must.Eq(t, smokeThread, raw.ThreadTs)
	})

	t.Run("native stream markdown_text", func(t *testing.T) {
		t.Parallel()
		start := apiMock.nth(methodStartStream, 1)
		must.Eq(t, "Bearer "+smokeToken, start.Auth)
		must.Eq(t, smokeChannel, jsonStr(start, "channel"))
		must.Eq(t, smokeThread, jsonStr(start, "thread_ts"))
		must.Eq(t, smokeChunk1, streamText(start))

		appendCall := apiMock.nth(methodAppendStream, 1)
		must.Eq(t, "Bearer "+smokeToken, appendCall.Auth)
		must.Eq(t, smokeChunk2, streamText(appendCall))

		stop := apiMock.last(methodStopStream)
		must.Eq(t, "Bearer "+smokeToken, stop.Auth)
		must.Eq(t, smokeChunk3, streamText(stop))
		must.Eq(t, "active", jsonStr(stop, "session_status"))
		blocks, _ := stop.JSON["blocks"].([]any)
		must.True(t, len(blocks) >= 1)
		block0, _ := blocks[0].(map[string]any)
		must.Eq(t, "context_actions", block0["type"])
	})

	t.Run("card postMessage blocks", func(t *testing.T) {
		t.Parallel()
		call := apiMock.last("chat.postMessage")
		must.Eq(t, "Bearer "+smokeToken, call.Auth)
		must.Eq(t, smokeChannel, call.Form.Get("channel"))
		must.Eq(t, smokeThread, call.Form.Get("thread_ts"))
		var gotBlocks []any
		must.NoError(t, json.Unmarshal([]byte(call.Form.Get("blocks")), &gotBlocks))
		want := CardToBlockKit(chat.Card{
			Type:     "card",
			Title:    "Project notes",
			Children: []any{chat.CardText("See the docs.")},
		})
		wantRaw, err := json.Marshal(want)
		must.NoError(t, err)
		var wantBlocks []any
		must.NoError(t, json.Unmarshal(wantRaw, &wantBlocks))
		must.Eq(t, wantBlocks, gotBlocks)
	})

	t.Run("exact API call sequence", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, []string{
			"auth.test",
			"users.info",
			methodStartStream,
			methodAppendStream,
			methodStopStream,
			"chat.postMessage",
		}, smokeMethods(apiMock))
	})
}
