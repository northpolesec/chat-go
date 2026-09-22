package linear

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/chattest"
	"github.com/northpolesec/chat-go/statememory"
	"github.com/shoenig/test/must"
)

const webhookSecret = "whsec"

// captureChat records ProcessMessage inputs and AbortTurn calls.
type captureChat struct {
	*chattest.MockChat
	mu      sync.Mutex
	last    chat.ProcessMessageInput
	aborted []string
	fail    error
	panics  int
}

func (c *captureChat) ProcessMessage(ctx context.Context, in chat.ProcessMessageInput) error {
	c.mu.Lock()
	c.last = in
	shouldPanic := c.panics > 0
	if shouldPanic {
		c.panics--
	}
	c.mu.Unlock()
	_ = c.MockChat.ProcessMessage(ctx, in) // count every dispatch, including the ones that fail
	if shouldPanic {
		panic("linear webhook test: processMessage")
	}
	return c.fail
}

func (c *captureChat) AbortTurn(_ context.Context, threadID string) error {
	c.mu.Lock()
	c.aborted = append(c.aborted, threadID)
	c.mu.Unlock()
	return nil
}

func (c *captureChat) lastInput() chat.ProcessMessageInput {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

func newWebhookAdapter(t *testing.T, m *lnMock) (*Adapter, *captureChat) {
	t.Helper()
	st := statememory.New()
	must.NoError(t, st.Connect(t.Context()))
	return newWebhookAdapterWithState(t, m, st)
}

func newWebhookAdapterWithState(t *testing.T, m *lnMock, st chat.StateAdapter) (*Adapter, *captureChat) {
	t.Helper()
	cfg := mockConfig(m)
	cfg.WebhookSecret = webhookSecret
	a, err := New(cfg)
	must.NoError(t, err)
	c := &captureChat{MockChat: chattest.NewMockChat(st)}
	must.NoError(t, a.Initialize(t.Context(), c))
	return a, c
}

type failingState struct {
	chat.StateAdapter
}

func (s failingState) SetIfNotExists(context.Context, string, json.RawMessage, time.Duration) (bool, error) {
	return false, errors.New("claim store down")
}

type webhookResp struct {
	status int
	body   string
}

func post(t *testing.T, a *Adapter, body string, headers map[string]string) webhookResp {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "https://example.com/webhooks/linear", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	a.HandleWebhook(rec, req)
	res := rec.Result()
	b, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	return webhookResp{status: res.StatusCode, body: string(b)}
}

func signed(t *testing.T, a *Adapter, p sessionEventPayload) webhookResp {
	t.Helper()
	body := mustJSON(t, p)
	return post(t, a, body, map[string]string{
		headerSignature: sig(webhookSecret, body),
		headerTimestamp: strconv.FormatInt(time.Now().UnixMilli(), 10),
	})
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	must.NoError(t, err)
	return string(b)
}

const activityCreatedJSON = `{"data":{"agentActivityCreate":{"success":true,"agentActivity":{"id":"act-new","sourceComment":{"id":"c-new"}}}}}`

func TestWebhookRejectsBadSignature(t *testing.T) {
	t.Parallel()
	a, c := newWebhookAdapter(t, newLnMock(t))
	body := mustJSON(t, createdEvent())
	r := post(t, a, body, map[string]string{headerSignature: sig("wrong", body), headerTimestamp: strconv.FormatInt(time.Now().UnixMilli(), 10)})
	must.Eq(t, http.StatusUnauthorized, r.status)
	must.False(t, c.Dispatched("processMessage"))
}

func TestWebhookRejectsStaleTimestamp(t *testing.T) {
	t.Parallel()
	a, c := newWebhookAdapter(t, newLnMock(t))
	p := createdEvent()
	p.WebhookTimestamp = float64(time.Now().Add(-10 * time.Minute).UnixMilli())
	body := mustJSON(t, p)
	r := post(t, a, body, map[string]string{headerSignature: sig(webhookSecret, body)})
	must.Eq(t, http.StatusUnauthorized, r.status)
	must.False(t, c.Dispatched("processMessage"))
}

func TestWebhookGarbageTimestampHeaderFallsBackToBody(t *testing.T) {
	t.Parallel()
	a, c := newWebhookAdapter(t, newLnMock(t))
	fresh := createdEvent()
	freshBody := mustJSON(t, fresh)
	freshResp := post(t, a, freshBody, map[string]string{
		headerSignature: sig(webhookSecret, freshBody),
		headerTimestamp: "not-a-number",
	})
	must.Eq(t, http.StatusOK, freshResp.status)
	must.True(t, c.Dispatched("processMessage"))

	a2, c2 := newWebhookAdapter(t, newLnMock(t))
	stale := createdEvent()
	stale.WebhookTimestamp = float64(time.Now().Add(-10 * time.Minute).UnixMilli())
	staleBody := mustJSON(t, stale)
	staleResp := post(t, a2, staleBody, map[string]string{
		headerSignature: sig(webhookSecret, staleBody),
		headerTimestamp: "not-a-number",
	})
	must.Eq(t, http.StatusUnauthorized, staleResp.status)
	must.False(t, c2.Dispatched("processMessage"))
}

func TestWebhookRejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	a, _ := newWebhookAdapter(t, newLnMock(t))
	body := `{not json`
	r := post(t, a, body, map[string]string{headerSignature: sig(webhookSecret, body), headerTimestamp: strconv.FormatInt(time.Now().UnixMilli(), 10)})
	must.Eq(t, http.StatusBadRequest, r.status)
}

func TestWebhookIgnoresOtherTypes(t *testing.T) {
	t.Parallel()
	a, c := newWebhookAdapter(t, newLnMock(t))
	p := createdEvent()
	p.Type = "Comment"
	must.Eq(t, http.StatusOK, signed(t, a, p).status)
	must.False(t, c.Dispatched("processMessage"))
}

func TestWebhookIgnoresOtherAppUser(t *testing.T) {
	t.Parallel()
	a, c := newWebhookAdapter(t, newLnMock(t))
	p := createdEvent()
	p.AgentSession.AppUserID = "someone-else"
	must.Eq(t, http.StatusOK, signed(t, a, p).status)
	must.False(t, c.Dispatched("processMessage"))
}

func TestWebhookIgnoresSessionWithoutIssue(t *testing.T) {
	t.Parallel()
	a, c := newWebhookAdapter(t, newLnMock(t))
	p := createdEvent()
	p.AgentSession.Issue, p.AgentSession.IssueID = nil, ""
	must.Eq(t, http.StatusOK, signed(t, a, p).status)
	must.False(t, c.Dispatched("processMessage"))
}

func TestWebhookCreatedDispatches(t *testing.T) {
	t.Parallel()
	a, c := newWebhookAdapter(t, newLnMock(t))
	must.Eq(t, http.StatusOK, signed(t, a, createdEvent()).status)
	in := c.lastInput()
	must.Eq(t, "linear:iss-1:s:sess-1", in.ThreadID)
	must.Eq(t, "@chatbot what is this?", in.Message.Text)
	must.Eq(t, "alice@example.com", in.Message.Author.Email)
	must.True(t, *in.Message.IsMention)
	must.Eq[chat.Adapter](t, a, in.Adapter)
}

func TestWebhookPromptedDispatchesOnSameThread(t *testing.T) {
	t.Parallel()
	a, c := newWebhookAdapter(t, newLnMock(t))
	must.Eq(t, http.StatusOK, signed(t, a, createdEvent()).status)
	must.Eq(t, http.StatusOK, signed(t, a, promptedEvent()).status)
	in := c.lastInput()
	must.Eq(t, "linear:iss-1:s:sess-1", in.ThreadID)
	must.Eq(t, "also check the logs", in.Message.Text)
}

func TestWebhookPromptedBySelfIsDropped(t *testing.T) {
	t.Parallel()
	a, c := newWebhookAdapter(t, newLnMock(t))
	p := promptedEvent()
	p.AgentActivity.User = &User{ID: testBotUserID, Name: "chatbot"}
	must.Eq(t, http.StatusOK, signed(t, a, p).status)
	must.False(t, c.Dispatched("processMessage"))
}

func TestWebhookPromptedNonPromptContentIsDropped(t *testing.T) {
	t.Parallel()
	a, c := newWebhookAdapter(t, newLnMock(t))
	p := promptedEvent()
	p.AgentActivity.Content.Type = "thought"
	must.Eq(t, http.StatusOK, signed(t, a, p).status)
	must.False(t, c.Dispatched("processMessage"))
}

func TestWebhookReplayedDeliveryRunsOnce(t *testing.T) {
	t.Parallel()
	a, c := newWebhookAdapter(t, newLnMock(t))
	must.Eq(t, http.StatusOK, signed(t, a, createdEvent()).status)
	must.Eq(t, http.StatusOK, signed(t, a, createdEvent()).status)
	must.Eq(t, 1, c.Calls("processMessage"))
}

func TestWebhookDispatchErrorReleasesClaimAnd500s(t *testing.T) {
	t.Parallel()
	a, c := newWebhookAdapter(t, newLnMock(t))
	c.fail = context.DeadlineExceeded
	must.Eq(t, http.StatusInternalServerError, signed(t, a, createdEvent()).status)
	c.fail = nil
	must.Eq(t, http.StatusOK, signed(t, a, createdEvent()).status)
	must.Eq(t, 2, c.Calls("processMessage"))
}

func TestWebhookStopAbortsAndPostsStopped(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	posted := m.onCapture("LinearAdapterActivityCreate", 200, activityCreatedJSON)
	a, c := newWebhookAdapter(t, m)
	p := promptedEvent()
	p.AgentActivity.Signal = signalStop
	must.Eq(t, http.StatusOK, signed(t, a, p).status)
	must.False(t, c.Dispatched("processMessage"))
	c.mu.Lock()
	must.Eq(t, []string{"linear:iss-1:s:sess-1"}, c.aborted)
	c.mu.Unlock()
	must.Eq(t, 1, posted.count())
	input, _ := posted.vars(t, 0)["input"].(map[string]any)
	must.Eq(t, "sess-1", input["agentSessionId"])
	content, _ := input["content"].(map[string]any)
	must.Eq(t, "response", content["type"])
	must.Eq(t, "Stopped.", content["body"])
}

func TestWebhookStopPostFailureReleasesClaim(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	m.on("LinearAdapterActivityCreate", 502, `down`)
	a, _ := newWebhookAdapter(t, m)
	p := promptedEvent()
	p.AgentActivity.Signal = signalStop
	must.Eq(t, http.StatusInternalServerError, signed(t, a, p).status)
	m.on("LinearAdapterActivityCreate", 200, activityCreatedJSON)
	must.Eq(t, http.StatusOK, signed(t, a, p).status)
}

func TestWebhookDispatchPanicReleasesClaim(t *testing.T) {
	t.Parallel()
	a, c := newWebhookAdapter(t, newLnMock(t))
	c.panics = 1
	body := mustJSON(t, createdEvent())
	headers := map[string]string{
		headerSignature: sig(webhookSecret, body),
		headerTimestamp: strconv.FormatInt(time.Now().UnixMilli(), 10),
	}
	func() {
		defer func() { must.True(t, recover() != nil) }()
		post(t, a, body, headers)
		t.Fatal("HandleWebhook should panic")
	}()
	must.Eq(t, http.StatusOK, post(t, a, body, headers).status)
	must.Eq(t, 2, c.Calls("processMessage"))
}

func TestWebhookClaimErrorStillDispatches(t *testing.T) {
	t.Parallel()
	st := statememory.New()
	must.NoError(t, st.Connect(t.Context()))
	a, c := newWebhookAdapterWithState(t, newLnMock(t), failingState{StateAdapter: st})
	must.Eq(t, http.StatusOK, signed(t, a, createdEvent()).status)
	must.True(t, c.Dispatched("processMessage"))
}

func TestWebhookNilStateStillDispatches(t *testing.T) {
	t.Parallel()
	a, c := newWebhookAdapterWithState(t, newLnMock(t), nil)
	must.Eq(t, http.StatusOK, signed(t, a, createdEvent()).status)
	must.True(t, c.Dispatched("processMessage"))
}
