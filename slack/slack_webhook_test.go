// Ported from packages/adapter-slack/src/index.test.ts @ 6adca36 (chat v4.40.0)
// (partial: Task 28 describe blocks — handleWebhook signature/verifier/URL
// verification/event_callback/interactive/JSON/slash/assistant, DM handling,
// message subtypes, event dedupe, link unfurls, authorizations[], plus
// deferred user_change and feedback-button onAction). Socket dedupe it is
// DROPPED. process* factories flatten to *Input. waitUntil → waitPending.
package slack

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/slack/api"
	"github.com/shoenig/test/must"
)

const webhookSecret = "test-signing-secret"

var _ chat.ChatInstance = (*mockChat)(nil)

type mockChat struct {
	mu              sync.Mutex
	state           chat.StateAdapter
	messages        []chat.ProcessMessageInput
	updates         []chat.ProcessMessageInput
	deletes         [][2]string
	reactions       []chat.ProcessReactionInput
	slashes         []chat.ProcessSlashCommandInput
	actions         []chat.ProcessActionInput
	modalSubmits    []chat.ProcessModalInput
	modalCloses     []chat.ProcessModalInput
	options         []chat.ProcessOptionsLoadInput
	joined          []chat.ProcessMemberJoinedInput
	assistantStart  []chat.ProcessAssistantThreadInput
	assistantChange []chat.ProcessAssistantThreadInput
	appHome         []processAppHomeInput
	appContext      []processAppContextInput
	sessionStopped  []processSessionStoppedInput
	titleChanged    []processTitleChangedInput
	aborts          []string

	modalSubmitRet any
	optionsRet     any
	optionsBlock   <-chan struct{}
}

func (m *mockChat) State() chat.StateAdapter { return m.state }

func (m *mockChat) ProcessMessage(_ context.Context, in chat.ProcessMessageInput) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, in)
	return nil
}

func (m *mockChat) ProcessMessageUpdated(_ context.Context, in chat.ProcessMessageInput) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updates = append(m.updates, in)
	return nil
}

func (m *mockChat) ProcessMessageDeleted(_ context.Context, threadID, messageID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletes = append(m.deletes, [2]string{threadID, messageID})
	return nil
}

func (m *mockChat) ProcessReaction(_ context.Context, in chat.ProcessReactionInput) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reactions = append(m.reactions, in)
	return nil
}

func (m *mockChat) ProcessSlashCommand(_ context.Context, in chat.ProcessSlashCommandInput) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.slashes = append(m.slashes, in)
	return nil
}

func (m *mockChat) ProcessAction(_ context.Context, in chat.ProcessActionInput) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.actions = append(m.actions, in)
	return nil
}

func (m *mockChat) ProcessModalSubmit(_ context.Context, in chat.ProcessModalInput) (any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.modalSubmits = append(m.modalSubmits, in)
	return m.modalSubmitRet, nil
}

func (m *mockChat) ProcessModalClose(_ context.Context, in chat.ProcessModalInput) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.modalCloses = append(m.modalCloses, in)
	return nil
}

func (m *mockChat) ProcessOptionsLoad(ctx context.Context, in chat.ProcessOptionsLoadInput) (any, error) {
	m.mu.Lock()
	m.options = append(m.options, in)
	block := m.optionsBlock
	ret := m.optionsRet
	m.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
		}
	}
	return ret, nil
}

func (m *mockChat) ProcessMemberJoinedChannel(_ context.Context, in chat.ProcessMemberJoinedInput) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.joined = append(m.joined, in)
	return nil
}

func (m *mockChat) ProcessAssistantThreadStarted(_ context.Context, in chat.ProcessAssistantThreadInput) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.assistantStart = append(m.assistantStart, in)
	return nil
}

func (m *mockChat) ProcessAssistantContextChanged(_ context.Context, in chat.ProcessAssistantThreadInput) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.assistantChange = append(m.assistantChange, in)
	return nil
}

func (m *mockChat) ProcessAppHomeOpened(_ context.Context, in processAppHomeInput) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appHome = append(m.appHome, in)
	return nil
}

func (m *mockChat) ProcessAppContextChanged(_ context.Context, in processAppContextInput) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appContext = append(m.appContext, in)
	return nil
}

func (m *mockChat) ProcessAgentSessionStopped(_ context.Context, in processSessionStoppedInput) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionStopped = append(m.sessionStopped, in)
	return nil
}

func (m *mockChat) ProcessAgentSessionTitleChanged(_ context.Context, in processTitleChangedInput) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.titleChanged = append(m.titleChanged, in)
	return nil
}

func (m *mockChat) AbortTurn(_ context.Context, threadID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.aborts = append(m.aborts, threadID)
	return nil
}

func newMockChat(t *testing.T) *mockChat {
	t.Helper()
	_, st := newStateChat(t)
	return &mockChat{state: st}
}

func signBody(secret, body string, ts int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:"))
	mac.Write([]byte(strconv.FormatInt(ts, 10)))
	mac.Write([]byte{':'})
	mac.Write([]byte(body))
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func signedWebhook(t *testing.T, secret, contentType, body string, extra http.Header, tsOffset int64) *http.Request {
	t.Helper()
	ts := time.Now().Unix() + tsOffset
	req := httptest.NewRequest(http.MethodPost, "https://example.com/webhook", strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Slack-Request-Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("X-Slack-Signature", signBody(secret, body, ts))
	for k, vs := range extra {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	return req
}

type webhookResp struct {
	status int
	header http.Header
	body   string
}

func postJSON(t *testing.T, a *SlackAdapter, secret, body string) webhookResp {
	t.Helper()
	return postWebhook(t, a, signedWebhook(t, secret, "application/json", body, nil, 0))
}

func postForm(t *testing.T, a *SlackAdapter, secret, body string) webhookResp {
	t.Helper()
	return postWebhook(t, a, signedWebhook(t, secret, "application/x-www-form-urlencoded", body, nil, 0))
}

func postWebhook(t *testing.T, a *SlackAdapter, req *http.Request) webhookResp {
	t.Helper()
	rec := httptest.NewRecorder()
	a.HandleWebhook(rec, req)
	resp := rec.Result()
	b, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	must.NoError(t, err)
	return webhookResp{status: resp.StatusCode, header: resp.Header, body: string(b)}
}

func mustWait(t *testing.T, a *SlackAdapter) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	must.NoError(t, a.waitPending(ctx))
}

func eventJSON(event map[string]any) string {
	payload := map[string]any{"type": "event_callback", "event": event}
	b, _ := json.Marshal(payload)
	return string(b)
}

func eventJSONTeam(event map[string]any) string {
	payload := map[string]any{"type": "event_callback", "team_id": "T123", "event": event}
	b, _ := json.Marshal(payload)
	return string(b)
}

func formPayload(t *testing.T, payload map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	must.NoError(t, err)
	return "payload=" + url.QueryEscape(string(raw))
}

func webhookAdapter(t *testing.T, extra Config) *SlackAdapter {
	t.Helper()
	if extra.SigningSecret == "" && extra.WebhookVerifier == nil {
		extra.SigningSecret = webhookSecret
	}
	if extra.Token == nil && extra.ClientID == "" {
		extra.Token = api.StaticToken("xoxb-test-token")
	}
	return mustNew(t, extra)
}

func TestHandleWebhookSignatureVerification(t *testing.T) {
	t.Parallel()
	adapter := webhookAdapter(t, Config{})

	t.Run("rejects requests without timestamp header", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "https://example.com/webhook", strings.NewReader(`{"type":"url_verification"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Slack-Signature", "v0=invalid")
		resp := postWebhook(t, adapter, req)
		must.Eq(t, 401, resp.status)
	})

	t.Run("rejects requests without signature header", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "https://example.com/webhook", strings.NewReader(`{"type":"url_verification"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Slack-Request-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))
		resp := postWebhook(t, adapter, req)
		must.Eq(t, 401, resp.status)
	})

	t.Run("rejects requests with invalid signature", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "https://example.com/webhook", strings.NewReader(`{"type":"url_verification"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Slack-Request-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))
		req.Header.Set("X-Slack-Signature", "v0=invalid")
		resp := postWebhook(t, adapter, req)
		must.Eq(t, 401, resp.status)
	})

	t.Run("rejects requests with old timestamp (>5 min)", func(t *testing.T) {
		t.Parallel()
		body := `{"type":"url_verification"}`
		resp := postWebhook(t, adapter, signedWebhook(t, webhookSecret, "application/json", body, nil, -400))
		must.Eq(t, 401, resp.status)
	})

	t.Run("accepts requests with valid signature", func(t *testing.T) {
		t.Parallel()
		body := `{"type":"url_verification","challenge":"test-challenge"}`
		resp := postJSON(t, adapter, webhookSecret, body)
		must.Eq(t, 200, resp.status)
	})

	t.Run("returns 413 when the body exceeds 25 MiB", func(t *testing.T) {
		t.Parallel()
		body := bytes.Repeat([]byte("x"), 25<<20+1)
		req := httptest.NewRequest(http.MethodPost, "https://example.com/webhook", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp := postWebhook(t, adapter, req)
		must.Eq(t, http.StatusRequestEntityTooLarge, resp.status)
		must.StrContains(t, resp.body, "Payload too large")
	})
}

func TestHandleWebhookWebhookVerifier(t *testing.T) {
	t.Setenv(envSigningSecret, "env-signing-secret")
	calls := 0
	adapter, err := New(Config{
		Token: api.StaticToken("xoxb-test-token"),
		WebhookVerifier: func(http.Header, []byte) error {
			calls++
			return nil
		},
	})
	must.NoError(t, err)
	body := `{"type":"url_verification","challenge":"verifier-challenge"}`
	req := httptest.NewRequest(http.MethodPost, "https://example.com/webhook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := postWebhook(t, adapter, req)
	must.Eq(t, 200, resp.status)
	must.Eq(t, 1, calls)
}

func TestHandleWebhookURLVerification(t *testing.T) {
	t.Parallel()
	adapter := webhookAdapter(t, Config{})
	body := `{"type":"url_verification","challenge":"test-challenge-123"}`
	resp := postJSON(t, adapter, webhookSecret, body)
	must.Eq(t, 200, resp.status)
	var got map[string]string
	must.NoError(t, json.Unmarshal([]byte(resp.body), &got))
	must.Eq(t, map[string]string{"challenge": "test-challenge-123"}, got)
}

func TestHandleWebhookEventCallback(t *testing.T) {
	t.Parallel()

	t.Run("handles message events", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{})
		body := eventJSON(map[string]any{
			"type": "message", "user": "U123", "channel": "C456",
			"text": "Hello world", "ts": "1234567890.123456",
		})
		resp := postJSON(t, adapter, webhookSecret, body)
		must.Eq(t, 200, resp.status)
		must.Eq(t, "ok", resp.body)
	})

	t.Run("handles app_mention events", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{})
		body := eventJSON(map[string]any{
			"type": "app_mention", "user": "U123", "channel": "C456",
			"text": "<@U_BOT> hello", "ts": "1234567890.123456",
		})
		resp := postJSON(t, adapter, webhookSecret, body)
		must.Eq(t, 200, resp.status)
	})

	t.Run("handles reaction_added events", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("conversations.replies", map[string]any{
			"messages": []any{map[string]any{"ts": "1234567890.123456"}},
		})
		adapter := apiMock.adapter(t, Config{})
		body := eventJSON(map[string]any{
			"type": "reaction_added", "user": "U123", "reaction": "thumbsup",
			"item": map[string]any{"type": "message", "channel": "C456", "ts": "1234567890.123456"},
		})
		resp := postJSON(t, adapter, webhookSecret, body)
		must.Eq(t, 200, resp.status)
		mustWait(t, adapter)
	})

	t.Run("handles reaction_removed events", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("conversations.replies", map[string]any{
			"messages": []any{map[string]any{"ts": "1234567890.123456"}},
		})
		adapter := apiMock.adapter(t, Config{})
		body := eventJSON(map[string]any{
			"type": "reaction_removed", "user": "U123", "reaction": "thumbsup",
			"item": map[string]any{"type": "message", "channel": "C456", "ts": "1234567890.123456"},
		})
		resp := postJSON(t, adapter, webhookSecret, body)
		must.Eq(t, 200, resp.status)
		mustWait(t, adapter)
	})

	t.Run("resolves parent thread_ts for reactions on threaded replies", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("conversations.replies", map[string]any{
			"messages": []any{map[string]any{"ts": "1234567890.123456", "thread_ts": "1111111111.000000"}},
		})
		adapter := apiMock.adapter(t, Config{})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		body := eventJSON(map[string]any{
			"type": "reaction_added", "user": "U123", "reaction": "thumbsup",
			"item": map[string]any{"type": "message", "channel": "C456", "ts": "1234567890.123456"},
		})
		_ = postJSON(t, adapter, webhookSecret, body)
		mustWait(t, adapter)
		must.Eq(t, 1, len(mc.reactions))
		must.Eq(t, "slack:C456:1111111111.000000", mc.reactions[0].ThreadID)
		must.Eq(t, "1234567890.123456", mc.reactions[0].MessageID)
	})

	t.Run("resolves reaction user display name", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("conversations.replies", map[string]any{
			"messages": []any{map[string]any{"ts": "1234567890.123456"}},
		})
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{
				"id": "U123", "name": "alice", "real_name": "Alice Example",
				"profile": map[string]any{"display_name": "Alice", "real_name": "Alice Example"},
			},
		})
		adapter := apiMock.adapter(t, Config{})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		body := eventJSON(map[string]any{
			"type": "reaction_added", "user": "U123", "reaction": "thumbsup",
			"item": map[string]any{"type": "message", "channel": "C456", "ts": "1234567890.123456"},
		})
		_ = postJSON(t, adapter, webhookSecret, body)
		mustWait(t, adapter)
		must.True(t, apiMock.count("users.info") >= 1)
		must.Eq(t, 1, len(mc.reactions))
		must.Eq(t, "U123", mc.reactions[0].User.UserID)
		must.Eq(t, "Alice", mc.reactions[0].User.UserName)
		must.Eq(t, "Alice Example", mc.reactions[0].User.FullName)
	})
}

func TestHandleWebhookInteractivePayloads(t *testing.T) {
	t.Parallel()

	t.Run("handles block_actions payload", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{})
		resp := postForm(t, adapter, webhookSecret, formPayload(t, map[string]any{
			"type": "block_actions",
			"user": map[string]any{"id": "U123", "username": "testuser", "name": "Test User"},
			"container": map[string]any{
				"type": "message", "message_ts": "1234567890.123456", "channel_id": "C456",
			},
			"channel": map[string]any{"id": "C456", "name": "general"},
			"message": map[string]any{"ts": "1234567890.123456", "thread_ts": "1234567890.000000"},
			"actions": []any{map[string]any{"type": "button", "action_id": "approve_btn", "value": "approved"}},
		}))
		must.Eq(t, 200, resp.status)
	})

	t.Run("returns 400 for missing payload", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{})
		resp := postForm(t, adapter, webhookSecret, "foo=bar")
		must.Eq(t, 400, resp.status)
	})

	t.Run("returns 400 for invalid payload JSON", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{})
		resp := postForm(t, adapter, webhookSecret, "payload=invalid-json")
		must.Eq(t, 400, resp.status)
	})

	t.Run("handles view_submission payload", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{})
		resp := postForm(t, adapter, webhookSecret, formPayload(t, map[string]any{
			"type": "view_submission", "trigger_id": "trigger123",
			"user": map[string]any{"id": "U123", "username": "testuser", "name": "Test User"},
			"view": map[string]any{
				"id": "V123", "callback_id": "feedback_form", "private_metadata": "thread-context",
				"state": map[string]any{"values": map[string]any{
					"message_block":  map[string]any{"message_input": map[string]any{"value": "Great feedback!"}},
					"category_block": map[string]any{"category_select": map[string]any{"selected_option": map[string]any{"value": "feature"}}},
				}},
			},
		}))
		must.Eq(t, 200, resp.status)
	})

	t.Run("flattens datepicker and number_input state into submitted values", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{})
		mc := newMockChat(t)
		mc.modalSubmitRet = map[string]any{"action": "close"}
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		resp := postForm(t, adapter, webhookSecret, formPayload(t, map[string]any{
			"type": "view_submission", "trigger_id": "trigger123",
			"user": map[string]any{"id": "U123", "username": "testuser", "name": "Test User"},
			"view": map[string]any{
				"id": "V123", "callback_id": "renewal_form",
				"state": map[string]any{"values": map[string]any{
					"renewal_date": map[string]any{"renewal_date": map[string]any{"selected_date": "2026-08-01"}},
					"quantity":     map[string]any{"quantity": map[string]any{"value": "3"}},
				}},
			},
		}))
		must.Eq(t, 200, resp.status)
		must.Eq(t, 1, len(mc.modalSubmits))
		must.Eq(t, "renewal_form", mc.modalSubmits[0].CallbackID)
		must.Eq(t, map[string]string{"renewal_date": "2026-08-01", "quantity": "3"}, mc.modalSubmits[0].Values)
	})

	t.Run("responds with response_action: clear when handler returns clear", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		mc.modalSubmitRet = map[string]any{"action": "clear"}
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		resp := postForm(t, adapter, webhookSecret, formPayload(t, map[string]any{
			"type": "view_submission", "trigger_id": "trigger123",
			"user": map[string]any{"id": "U123", "username": "testuser", "name": "Test User"},
			"view": map[string]any{"id": "V123", "callback_id": "feedback_form", "state": map[string]any{"values": map[string]any{}}},
		}))
		must.Eq(t, 200, resp.status)
		var got map[string]any
		must.NoError(t, json.Unmarshal([]byte(resp.body), &got))
		must.Eq(t, "clear", got["response_action"])
	})

	t.Run("handles view_closed payload", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{})
		resp := postForm(t, adapter, webhookSecret, formPayload(t, map[string]any{
			"type": "view_closed",
			"user": map[string]any{"id": "U123", "username": "testuser", "name": "Test User"},
			"view": map[string]any{"id": "V123", "callback_id": "feedback_form", "private_metadata": "thread-context"},
		}))
		must.Eq(t, 200, resp.status)
	})

	t.Run("handles block_suggestion payloads and returns options JSON", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{})
		mc := newMockChat(t)
		mc.optionsRet = []chat.SelectOptionElement{{Label: "Maria Garcia", Value: "person_123"}}
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		resp := postForm(t, adapter, webhookSecret, formPayload(t, map[string]any{
			"type": "block_suggestion", "team": map[string]any{"id": "T123"},
			"user":      map[string]any{"id": "U123", "username": "testuser", "name": "Test User"},
			"action_id": "person_select", "block_id": "person_block", "value": "mar",
		}))
		must.Eq(t, 200, resp.status)
		must.StrContains(t, resp.header.Get("Content-Type"), "application/json")
		must.Eq(t, 1, len(mc.options))
		must.Eq(t, "person_select", mc.options[0].ActionID)
		must.Eq(t, "mar", mc.options[0].Query)
		must.Eq(t, "U123", mc.options[0].User.UserID)
		var got map[string]any
		must.NoError(t, json.Unmarshal([]byte(resp.body), &got))
		must.Eq(t, map[string]any{
			"options": []any{map[string]any{
				"text":  map[string]any{"type": "plain_text", "text": "Maria Garcia"},
				"value": "person_123",
			}},
		}, got)
	})

	t.Run("handles block_suggestion with option_groups response", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{})
		mc := newMockChat(t)
		mc.optionsRet = []optionsLoadGroup{
			{Label: "Recent", Options: []chat.SelectOptionElement{{Label: "Alice", Value: "u1"}}},
			{Label: "All", Options: []chat.SelectOptionElement{{Label: "Bob", Value: "u2"}}},
		}
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		resp := postForm(t, adapter, webhookSecret, formPayload(t, map[string]any{
			"type": "block_suggestion", "team": map[string]any{"id": "T123"},
			"user":      map[string]any{"id": "U123", "username": "testuser", "name": "Test User"},
			"action_id": "user_select", "block_id": "user_block", "value": "",
		}))
		must.Eq(t, 200, resp.status)
		var got map[string]any
		must.NoError(t, json.Unmarshal([]byte(resp.body), &got))
		must.Eq(t, map[string]any{
			"option_groups": []any{
				map[string]any{
					"label":   map[string]any{"type": "plain_text", "text": "Recent"},
					"options": []any{map[string]any{"text": map[string]any{"type": "plain_text", "text": "Alice"}, "value": "u1"}},
				},
				map[string]any{
					"label":   map[string]any{"type": "plain_text", "text": "All"},
					"options": []any{map[string]any{"text": map[string]any{"type": "plain_text", "text": "Bob"}, "value": "u2"}},
				},
			},
		}, got)
	})

	t.Run("returns empty options when block_suggestion handler exceeds 2.5s budget", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{
			WebhookVerifier: func(http.Header, []byte) error { return nil },
		})
		adapter.optionsLoadTimeout = time.Millisecond
		mc := newMockChat(t)
		block := make(chan struct{})
		mc.optionsBlock = block
		mc.optionsRet = []chat.SelectOptionElement{{Label: "Too late", Value: "late"}}
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		t.Cleanup(func() {
			close(block)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			must.NoError(t, adapter.waitPending(ctx))
		})
		resp := postForm(t, adapter, webhookSecret, formPayload(t, map[string]any{
			"type": "block_suggestion", "team": map[string]any{"id": "T123"},
			"user":      map[string]any{"id": "U123", "username": "testuser", "name": "Test User"},
			"action_id": "person_select", "block_id": "person_block", "value": "mar",
		}))
		must.Eq(t, 200, resp.status)
		var got map[string]any
		must.NoError(t, json.Unmarshal([]byte(resp.body), &got))
		must.Eq(t, map[string]any{"options": []any{}}, got)
	})

	t.Run("includes trigger_id in block_actions event", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		resp := postForm(t, adapter, webhookSecret, formPayload(t, map[string]any{
			"type": "block_actions", "trigger_id": "trigger456",
			"user":      map[string]any{"id": "U123", "username": "testuser", "name": "Test User"},
			"container": map[string]any{"type": "message", "message_ts": "1234567890.123456", "channel_id": "C456"},
			"channel":   map[string]any{"id": "C456", "name": "general"},
			"message":   map[string]any{"ts": "1234567890.123456"},
			"actions":   []any{map[string]any{"type": "button", "action_id": "open_modal", "value": "modal-data"}},
		}))
		must.Eq(t, 200, resp.status)
		must.Eq(t, 1, len(mc.actions))
		must.Eq(t, "trigger456", mc.actions[0].TriggerID)
	})
}

func TestHandleWebhookJSONParsing(t *testing.T) {
	t.Parallel()
	adapter := webhookAdapter(t, Config{})
	resp := postJSON(t, adapter, webhookSecret, "not valid json")
	must.Eq(t, 400, resp.status)
}

func TestHandleWebhookSlashCommands(t *testing.T) {
	t.Parallel()

	t.Run("detects slash command payload (form-urlencoded with command field)", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{"name": "user", "profile": map[string]any{"display_name": "User"}},
		})
		adapter := apiMock.adapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		body := url.Values{
			"command": {"/help"}, "text": {"topic search"}, "user_id": {"U123456"},
			"channel_id": {"C789ABC"}, "trigger_id": {"trigger-123"}, "team_id": {"T_TEAM_1"},
		}.Encode()
		resp := postForm(t, adapter, webhookSecret, body)
		must.Eq(t, 200, resp.status)
		must.Eq(t, 1, len(mc.slashes))
	})

	t.Run("passes command, text, user, and triggerId to processSlashCommand", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{"name": "user", "profile": map[string]any{"display_name": "User"}},
		})
		adapter := apiMock.adapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		body := url.Values{
			"command": {"/status"}, "text": {"verbose"}, "user_id": {"U_USER_1"},
			"channel_id": {"C_CHANNEL_1"}, "trigger_id": {"trigger-456"}, "team_id": {"T_TEAM_1"},
		}.Encode()
		_ = postForm(t, adapter, webhookSecret, body)
		must.Eq(t, 1, len(mc.slashes))
		ev := mc.slashes[0]
		must.Eq(t, "/status", ev.Command)
		must.Eq(t, "verbose", ev.Text)
		must.Eq(t, "U_USER_1", ev.User.UserID)
		must.Eq(t, "trigger-456", ev.TriggerID)
		must.True(t, ev.Adapter == adapter)
		must.Eq(t, "slack:C_CHANNEL_1", ev.ChannelID)
	})

	t.Run("does not treat interactive payload as slash command", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		resp := postForm(t, adapter, webhookSecret, formPayload(t, map[string]any{
			"type": "block_actions", "user": map[string]any{"id": "U123", "username": "user"},
			"actions":   []any{map[string]any{"action_id": "test"}},
			"container": map[string]any{"message_ts": "123", "channel_id": "C456"},
			"channel":   map[string]any{"id": "C456", "name": "general"},
			"message":   map[string]any{"ts": "123"},
		}))
		must.Eq(t, 200, resp.status)
	})

	t.Run("returns 200 immediately for slash commands", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{"name": "user", "profile": map[string]any{"display_name": "User"}},
		})
		adapter := apiMock.adapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		body := url.Values{
			"command": {"/feedback"}, "text": {""}, "user_id": {"U123"},
			"channel_id": {"C456"}, "team_id": {"T_TEAM_1"},
		}.Encode()
		resp := postForm(t, adapter, webhookSecret, body)
		must.Eq(t, 200, resp.status)
		must.Eq(t, "", resp.body)
	})

	t.Run("handles slash command in multi-workspace mode", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{"name": "user", "profile": map[string]any{"display_name": "User"}},
		})
		adapter := apiMock.adapter(t, Config{Token: nil, BotUserID: "U_BOT", ClientID: "x"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		must.NoError(t, adapter.SetInstallation(t.Context(), "T_SLASH_TEAM", Installation{
			BotToken: "xoxb-slash-token", BotUserID: "U_SLASH_BOT",
		}))
		body := url.Values{
			"command": {"/help"}, "text": {""}, "user_id": {"U123"},
			"channel_id": {"C456"}, "team_id": {"T_SLASH_TEAM"},
		}.Encode()
		resp := postForm(t, adapter, webhookSecret, body)
		must.Eq(t, 200, resp.status)
		must.Eq(t, 1, len(mc.slashes))
	})

	t.Run("includes raw payload in event", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{"name": "user", "profile": map[string]any{"display_name": "User"}},
		})
		adapter := apiMock.adapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		body := url.Values{
			"command": {"/deploy"}, "text": {"production"}, "user_id": {"U_DEPLOY"},
			"user_name": {"deployer"}, "channel_id": {"C_DEPLOY"}, "channel_name": {"ops"},
			"team_id": {"T_TEAM"}, "response_url": {"https://hooks.slack.com/commands/xxx"},
		}.Encode()
		_ = postForm(t, adapter, webhookSecret, body)
		must.Eq(t, 1, len(mc.slashes))
		raw, ok := mc.slashes[0].Raw.(map[string]string)
		must.True(t, ok)
		must.Eq(t, "/deploy", raw["command"])
		must.Eq(t, "production", raw["text"])
		must.Eq(t, "C_DEPLOY", raw["channel_id"])
		must.Eq(t, "https://hooks.slack.com/commands/xxx", raw["response_url"])
	})
}

func TestHandleWebhookAssistantEvents(t *testing.T) {
	t.Parallel()

	t.Run("handles assistant_thread_started event", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		body := eventJSONTeam(map[string]any{
			"type": "assistant_thread_started", "event_ts": "1234567890.000000",
			"assistant_thread": map[string]any{
				"user_id": "U_USER", "channel_id": "C_ASSISTANT", "thread_ts": "1234567890.111111",
				"context": map[string]any{"channel_id": "C_CONTEXT", "team_id": "T123"},
			},
		})
		resp := postJSON(t, adapter, webhookSecret, body)
		must.Eq(t, 200, resp.status)
		must.Eq(t, 1, len(mc.assistantStart))
		must.Eq(t, "slack:C_ASSISTANT:1234567890.111111", mc.assistantStart[0].ThreadID)
		must.Eq(t, "U_USER", mc.assistantStart[0].UserID)
		must.Eq(t, "C_ASSISTANT", mc.assistantStart[0].ChannelID)
		must.True(t, mc.assistantStart[0].Adapter == adapter)
	})

	t.Run("handles assistant_thread_context_changed event", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		body := eventJSONTeam(map[string]any{
			"type": "assistant_thread_context_changed", "event_ts": "1234567891.000000",
			"assistant_thread": map[string]any{
				"user_id": "U_USER", "channel_id": "C_ASSISTANT", "thread_ts": "1234567890.111111",
				"context": map[string]any{"channel_id": "C_NEW_CONTEXT", "team_id": "T123"},
			},
		})
		resp := postJSON(t, adapter, webhookSecret, body)
		must.Eq(t, 200, resp.status)
		must.Eq(t, 1, len(mc.assistantChange))
		must.Eq(t, "slack:C_ASSISTANT:1234567890.111111", mc.assistantChange[0].ThreadID)
		must.Eq(t, "U_USER", mc.assistantChange[0].UserID)
		must.Eq(t, "C_NEW_CONTEXT", mc.assistantChange[0].Context.ChannelID)
	})

	t.Run("aborts and activates an agent session when the user stops it", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		adapter := apiMock.adapter(t, Config{AgentView: true, BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		body := eventJSONTeam(map[string]any{
			"type": "agent_session_stopped", "user": "U_USER",
			"channel": "D_AGENT", "thread_ts": "1234567890.111111",
			"streaming_message_ts": []any{"1234567891.222222", "1234567891.333333"},
			"event_ts":             "1234567892.333333",
		})
		resp := postJSON(t, adapter, webhookSecret, body)
		mustWait(t, adapter)
		must.Eq(t, 200, resp.status)
		must.Eq(t, []string{"slack:D_AGENT:1234567890.111111"}, mc.aborts)
		must.Eq(t, 1, apiMock.count("agents.sessions.setStatus"))
		call := apiMock.last("agents.sessions.setStatus")
		must.Eq(t, "D_AGENT", call.Form.Get("channel_id"))
		must.Eq(t, "active", call.Form.Get("status"))
		must.Eq(t, "1234567890.111111", call.Form.Get("thread_ts"))
		must.Eq(t, 1, len(mc.sessionStopped))
		must.Eq(t, "slack:D_AGENT:1234567890.111111", mc.sessionStopped[0].ThreadID)
		must.Eq(t, "U_USER", mc.sessionStopped[0].UserID)
	})

	t.Run("dispatches agent session title changes", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{AgentView: true, BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		body := eventJSONTeam(map[string]any{
			"type": "agent_session_title_changed", "user": "U_USER",
			"channel": "D_AGENT", "thread_ts": "1234567890.111111",
			"title": "New title", "event_ts": "1234567892.333333", "team_id": "T123",
		})
		resp := postJSON(t, adapter, webhookSecret, body)
		must.Eq(t, 200, resp.status)
		must.Eq(t, 1, len(mc.titleChanged))
		must.Eq(t, "", mc.titleChanged[0].PreviousTitle)
		must.Eq(t, "slack:D_AGENT:1234567890.111111", mc.titleChanged[0].ThreadID)
		must.Eq(t, "New title", mc.titleChanged[0].Title)
	})

	t.Run("handles app_home_opened event", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		body := eventJSONTeam(map[string]any{
			"type": "app_home_opened", "user": "U_HOME_USER",
			"channel": "D_HOME_CHAN", "tab": "home", "event_ts": "1234567892.000000",
		})
		resp := postJSON(t, adapter, webhookSecret, body)
		must.Eq(t, 200, resp.status)
		must.Eq(t, 1, len(mc.appHome))
		must.Eq(t, "U_HOME_USER", mc.appHome[0].UserID)
		must.Eq(t, "D_HOME_CHAN", mc.appHome[0].ChannelID)
		must.True(t, mc.appHome[0].Adapter == adapter)
	})

	t.Run("handles member_joined_channel event", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		body := eventJSONTeam(map[string]any{
			"type": "member_joined_channel", "user": "U_JOINED_USER",
			"channel": "C_TARGET_CHAN", "inviter": "U_INVITER", "event_ts": "1234567893.000000",
		})
		resp := postJSON(t, adapter, webhookSecret, body)
		must.Eq(t, 200, resp.status)
		must.Eq(t, 1, len(mc.joined))
		must.Eq(t, "U_JOINED_USER", mc.joined[0].UserID)
		must.Eq(t, "slack:C_TARGET_CHAN:", mc.joined[0].ChannelID)
		must.Eq(t, "U_INVITER", mc.joined[0].InviterID)
		must.True(t, mc.joined[0].Adapter == adapter)
	})
}

func TestDMMessageHandling(t *testing.T) {
	t.Parallel()

	t.Run("top-level DM messages use empty threadTs (matches openDM subscriptions)", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventJSONTeam(map[string]any{
			"type": "message", "user": "U_USER", "channel": "D_DM_CHAN",
			"channel_type": "im", "text": "hello from DM", "ts": "1234567890.111111",
		}))
		must.Eq(t, 1, len(mc.messages))
		must.Eq(t, "slack:D_DM_CHAN:", mc.messages[0].ThreadID)
	})

	t.Run("DM thread replies use parent thread_ts", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventJSONTeam(map[string]any{
			"type": "message", "user": "U_USER", "channel": "D_DM_CHAN",
			"channel_type": "im", "text": "reply in DM thread",
			"ts": "1234567890.222222", "thread_ts": "1234567890.111111",
		}))
		must.Eq(t, 1, len(mc.messages))
		must.Eq(t, "slack:D_DM_CHAN:1234567890.111111", mc.messages[0].ThreadID)
	})

	t.Run("DM messages do NOT have isMention set (routed via onDirectMessage)", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{"name": "user", "profile": map[string]any{"display_name": "User"}},
		})
		adapter := apiMock.adapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventJSONTeam(map[string]any{
			"type": "message", "user": "U_USER", "channel": "D_DM_CHAN",
			"channel_type": "im", "text": "hello from DM", "ts": "1234567890.333333",
		}))
		must.Eq(t, 1, len(mc.messages))
		must.Nil(t, mc.messages[0].Message.IsMention)
	})

	t.Run("USLACK system notifications in DMs are dispatched with isSystem set", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{"name": "slackbot", "profile": map[string]any{"display_name": "Slackbot"}},
		})
		adapter := apiMock.adapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventJSONTeam(map[string]any{
			"type": "message", "user": "USLACK", "channel": "D_DM_CHAN",
			"channel_type": "im", "text": "<@U_USER> archived the channel <#C_CHANNEL>",
			"ts": "1234567890.555555",
		}))
		must.Eq(t, 1, len(mc.messages))
		must.Eq(t, "USLACK", mc.messages[0].Message.Author.UserID)
		must.False(t, *mc.messages[0].Message.Author.IsBot)
		must.True(t, mc.messages[0].Message.Author.IsSystem)
		must.False(t, mc.messages[0].Message.Author.IsMe)
	})

	t.Run("channel messages do NOT have isMention auto-set", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{"name": "user", "profile": map[string]any{"display_name": "User"}},
		})
		adapter := apiMock.adapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventJSONTeam(map[string]any{
			"type": "message", "user": "U_USER", "channel": "C_CHANNEL",
			"text": "hello from channel", "ts": "1234567890.444444",
		}))
		must.Eq(t, 1, len(mc.messages))
		must.Nil(t, mc.messages[0].Message.IsMention)
	})
}

func TestMessageSubtypeHandling(t *testing.T) {
	t.Parallel()

	t.Run("allows file_share messages through to processMessage", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventJSONTeam(map[string]any{
			"type": "message", "subtype": "file_share", "user": "U_USER", "channel": "C_CHAN",
			"text": "Check this file", "ts": "1234567890.111111", "thread_ts": "1234567890.000000",
			"files": []any{map[string]any{
				"id": "F123", "mimetype": "image/png", "url_private": "https://files.slack.com/file.png",
				"name": "screenshot.png", "size": 12345,
			}},
		}))
		must.Eq(t, 1, len(mc.messages))
		must.Eq(t, "slack:C_CHAN:1234567890.000000", mc.messages[0].ThreadID)
	})

	t.Run("allows thread_broadcast messages through to processMessage", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventJSONTeam(map[string]any{
			"type": "message", "subtype": "thread_broadcast", "user": "U_USER", "channel": "C_CHAN",
			"text": "Also posted to channel", "ts": "1234567890.222222", "thread_ts": "1234567890.000000",
		}))
		must.Eq(t, 1, len(mc.messages))
		must.Eq(t, "slack:C_CHAN:1234567890.000000", mc.messages[0].ThreadID)
	})

	for _, tc := range []struct {
		name   string
		cfg    Config
		expect string
	}{
		{"a flat DM", Config{BotUserID: "U_BOT"}, "slack:D_DM:"},
		{"a threaded agent_view DM", Config{BotUserID: "U_BOT", AgentView: true, SessionTitle: boolPtr(false)}, "slack:D_DM:1111.0001"},
	} {
		t.Run("routes the message, its edit, and its delete to one thread id in "+tc.name, func(t *testing.T) {
			t.Parallel()
			adapter := webhookAdapter(t, tc.cfg)
			mc := newMockChat(t)
			must.NoError(t, adapter.Initialize(t.Context(), mc))
			dm := map[string]any{
				"type": "message", "user": "U_USER", "channel": "D_DM",
				"channel_type": "im", "text": "hello", "ts": "1111.0001",
			}
			send := func(event map[string]any) {
				payload := map[string]any{"type": "event_callback", "team_id": "T123", "event": event}
				raw, _ := json.Marshal(payload)
				_ = postJSON(t, adapter, webhookSecret, string(raw))
				mustWait(t, adapter)
			}
			send(dm)
			send(map[string]any{
				"type": "message", "subtype": "message_changed", "channel": "D_DM",
				"channel_type": "im", "ts": "1111.0002",
				"message":          map[string]any{"type": "message", "user": "U_USER", "channel": "D_DM", "text": "edited", "ts": "1111.0001", "edited": map[string]any{"ts": "1111.0002"}},
				"previous_message": dm,
			})
			send(map[string]any{
				"type": "message", "subtype": "message_deleted", "channel": "D_DM",
				"channel_type": "im", "ts": "1111.0003", "deleted_ts": "1111.0001",
				"previous_message": dm,
			})
			must.Eq(t, 1, len(mc.messages))
			must.Eq(t, tc.expect, mc.messages[0].ThreadID)
			must.Eq(t, 1, len(mc.updates))
			must.Eq(t, tc.expect, mc.updates[0].ThreadID)
			must.Eq(t, 1, len(mc.deletes))
			must.Eq(t, tc.expect, mc.deletes[0][0])
		})
	}

	t.Run("dispatches message_changed subtypes as message updates", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventJSONTeam(map[string]any{
			"type": "message", "subtype": "message_changed", "channel": "C_CHAN", "ts": "1234567891.111111",
			"message": map[string]any{
				"type": "message", "user": "U_USER", "channel": "C_CHAN",
				"text": "edited text", "ts": "1234567890.111111", "edited": map[string]any{"ts": "1234567891.111111"},
			},
		}))
		must.Eq(t, 0, len(mc.messages))
		must.Eq(t, 1, len(mc.updates))
		must.Eq(t, "slack:C_CHAN:1234567890.111111", mc.updates[0].ThreadID)
	})

	t.Run("forwards the pre-edit message so handlers can diff the change", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		before := map[string]any{
			"type": "message", "user": "U_USER", "username": "user",
			"channel": "C_CHAN", "text": "before", "ts": "1234567890.111111",
		}
		_ = postJSON(t, adapter, webhookSecret, eventJSONTeam(map[string]any{
			"type": "message", "subtype": "message_changed", "channel": "C_CHAN", "ts": "1234567891.111111",
			"message": map[string]any{
				"type": "message", "user": "U_USER", "username": "user",
				"channel": "C_CHAN", "text": "after", "ts": "1234567890.111111",
				"edited": map[string]any{"ts": "1234567891.111111"},
			},
			"previous_message": before,
		}))
		must.Eq(t, 1, len(mc.updates))
		must.True(t, mc.updates[0].PreviousMessage != nil)
		must.Eq(t, "before", mc.updates[0].PreviousMessage.Text)
	})

	t.Run("ignores a message_changed where nothing actually changed", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		unchanged := map[string]any{
			"type": "message", "user": "U_USER", "channel": "C_CHAN",
			"text": "same text", "ts": "1234567890.111111",
		}
		_ = postJSON(t, adapter, webhookSecret, eventJSONTeam(map[string]any{
			"type": "message", "subtype": "message_changed", "channel": "C_CHAN", "ts": "1234567891.111111",
			"message": unchanged, "previous_message": unchanged,
		}))
		must.Eq(t, 0, len(mc.updates))
		must.Eq(t, 0, len(mc.messages))
	})

	t.Run("leaves previousMessage undefined when Slack omits it", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventJSONTeam(map[string]any{
			"type": "message", "subtype": "message_changed", "channel": "C_CHAN", "ts": "1234567891.111111",
			"message": map[string]any{
				"type": "message", "user": "U_USER", "channel": "C_CHAN",
				"text": "after", "ts": "1234567890.111111", "edited": map[string]any{"ts": "1234567891.111111"},
			},
		}))
		must.Eq(t, 1, len(mc.updates))
		must.Nil(t, mc.updates[0].PreviousMessage)
	})

	t.Run("dispatches hidden message_changed edits as message updates", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventJSONTeam(map[string]any{
			"type": "message", "subtype": "message_changed", "hidden": true,
			"channel": "D_DM", "channel_type": "im", "ts": "1779425554.000100", "event_ts": "1779425554.000100",
			"message": map[string]any{
				"type": "message", "user": "U_USER",
				"text": "What do you see in this attachment? Test",
				"ts":   "1779271807.493869", "thread_ts": "1779271794.544339",
				"edited": map[string]any{"user": "U_USER", "ts": "1779425554.000000"},
			},
			"previous_message": map[string]any{
				"type": "message", "user": "U_USER",
				"text": "What do you see in this attachment?",
				"ts":   "1779271807.493869", "thread_ts": "1779271794.544339",
			},
		}))
		must.Eq(t, 0, len(mc.messages))
		must.Eq(t, 1, len(mc.updates))
		must.Eq(t, "slack:D_DM:1779271794.544339", mc.updates[0].ThreadID)
	})

	t.Run("ignores hidden message_changed thread metadata updates after deletes", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventJSONTeam(map[string]any{
			"type": "message", "subtype": "message_changed", "hidden": true,
			"channel": "D_DM", "channel_type": "im", "ts": "1779425682.000300", "event_ts": "1779425682.000300",
			"message": map[string]any{
				"type": "message", "subtype": "assistant_app_thread", "user": "U_BOT",
				"text": "New Assistant Thread", "ts": "1778127887.294739", "thread_ts": "1778127887.294739",
				"edited":      map[string]any{"user": "U_BOT", "ts": "1778128187.000000"},
				"reply_count": 41, "latest_reply": "1779271265.010909",
			},
			"previous_message": map[string]any{
				"type": "message", "subtype": "assistant_app_thread", "user": "U_BOT",
				"text": "New Assistant Thread", "ts": "1778127887.294739", "thread_ts": "1778127887.294739",
				"edited":      map[string]any{"user": "U_BOT", "ts": "1778128187.000000"},
				"reply_count": 41, "latest_reply": "1779271265.010909",
			},
		}))
		must.Eq(t, 0, len(mc.messages))
		must.Eq(t, 0, len(mc.updates))
	})

	t.Run("dispatches message_deleted subtypes as message deletes", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventJSONTeam(map[string]any{
			"type": "message", "subtype": "message_deleted", "channel": "C_CHAN",
			"deleted_ts": "1234567890.111111", "event_ts": "1234567891.111111",
			"previous_message": map[string]any{
				"type": "message", "user": "U_USER", "channel": "C_CHAN",
				"text": "deleted text", "ts": "1234567890.111111",
			},
		}))
		must.Eq(t, 0, len(mc.messages))
		must.Eq(t, 1, len(mc.deletes))
		must.Eq(t, "slack:C_CHAN:1234567890.111111", mc.deletes[0][0])
		must.Eq(t, "1234567890.111111", mc.deletes[0][1])
	})

	t.Run("ignores message_changed tombstone subtypes", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventJSONTeam(map[string]any{
			"type": "message", "subtype": "message_changed", "channel": "C_CHAN",
			"channel_type": "channel", "hidden": true, "ts": "1779426065.000200", "event_ts": "1779426065.000200",
			"message": map[string]any{
				"type": "message", "subtype": "tombstone", "user": "USLACKBOT",
				"text": "This message was deleted.", "hidden": true,
				"ts": "1778050260.824689", "thread_ts": "1778050260.824689",
			},
			"previous_message": map[string]any{
				"type": "message", "user": "U_USER", "channel": "C_CHAN",
				"text": "<@U_BOT> deleted message", "ts": "1778050260.824689", "thread_ts": "1778050260.824689",
			},
		}))
		must.Eq(t, 0, len(mc.messages))
		must.Eq(t, 0, len(mc.updates))
		must.Eq(t, 0, len(mc.deletes))
	})

	t.Run("ignores channel_join subtypes", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventJSONTeam(map[string]any{
			"type": "message", "subtype": "channel_join", "user": "U_USER",
			"channel": "C_CHAN", "ts": "1234567890.111111",
		}))
		must.Eq(t, 0, len(mc.messages))
	})
}

func TestLinkUnfurlEnrichment(t *testing.T) {
	t.Parallel()

	t.Run("should enrich links with metadata from attachments", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventJSON(map[string]any{
			"type": "message", "channel": "C123", "ts": "1234567890.123456",
			"text": "Check out ", "user": "U_USER",
			"blocks": []any{map[string]any{
				"type": "rich_text", "elements": []any{map[string]any{
					"type": "rich_text_section", "elements": []any{
						map[string]any{"type": "text", "text": "Check out "},
						map[string]any{"type": "link", "url": "https://example.com/article"},
					},
				}},
			}},
			"attachments": []any{map[string]any{
				"from_url": "https://example.com/article", "title": "Example Article",
				"text":      "An interesting article about testing",
				"image_url": "https://example.com/og-image.png", "service_name": "Example",
			}},
		}))
		must.Eq(t, 1, len(mc.messages))
		links := mc.messages[0].Message.Links
		must.Eq(t, 1, len(links))
		must.Eq(t, "https://example.com/article", links[0].URL)
		must.Eq(t, "Example Article", links[0].Title)
		must.Eq(t, "An interesting article about testing", links[0].Description)
		must.Eq(t, "https://example.com/og-image.png", links[0].ImageURL)
		must.Eq(t, "Example", links[0].SiteName)
	})

	t.Run("should extract links from attachments even without blocks", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventJSON(map[string]any{
			"type": "message", "channel": "C123", "ts": "1234567890.123456",
			"text": "https://github.com/vercel/chat", "user": "U_USER",
			"attachments": []any{map[string]any{
				"from_url": "https://github.com/vercel/chat", "title": "vercel/chat",
				"text": "Chat SDK for building bots", "service_name": "GitHub",
				"service_icon": "https://github.githubassets.com/favicon.ico",
			}},
		}))
		must.Eq(t, 1, len(mc.messages[0].Message.Links))
		must.Eq(t, "vercel/chat", mc.messages[0].Message.Links[0].Title)
		must.Eq(t, "GitHub", mc.messages[0].Message.Links[0].SiteName)
	})

	t.Run("should return bare links when no attachments present", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventJSON(map[string]any{
			"type": "message", "channel": "C123", "ts": "1234567890.123456",
			"text": "Check out ", "user": "U_USER",
			"blocks": []any{map[string]any{
				"type": "rich_text", "elements": []any{map[string]any{
					"type":     "rich_text_section",
					"elements": []any{map[string]any{"type": "link", "url": "https://example.com"}},
				}},
			}},
		}))
		must.Eq(t, 1, len(mc.messages[0].Message.Links))
		must.Eq(t, "https://example.com", mc.messages[0].Message.Links[0].URL)
		must.Eq(t, "", mc.messages[0].Message.Links[0].Title)
	})

	t.Run("parses bracketed links from text and bounds their length", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		overLong := "https://example.com/" + strings.Repeat("a", 4000)
		_ = postJSON(t, adapter, webhookSecret, eventJSON(map[string]any{
			"type": "message", "channel": "C123", "ts": "1234567890.123456",
			"text": "ok <https://example.com/x> and <" + overLong + ">", "user": "U_USER",
		}))
		var urls []string
		for _, l := range mc.messages[0].Message.Links {
			urls = append(urls, l.URL)
		}
		must.True(t, slices.Contains(urls, "https://example.com/x"))
		must.False(t, slices.Contains(urls, overLong))
	})

	t.Run("should match unfurl metadata with trailing slash differences", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventJSON(map[string]any{
			"type": "message", "channel": "C123", "ts": "1234567890.123456",
			"text": " ", "user": "U_USER",
			"blocks": []any{map[string]any{
				"type": "rich_text", "elements": []any{map[string]any{
					"type":     "rich_text_section",
					"elements": []any{map[string]any{"type": "link", "url": "https://example.com"}},
				}},
			}},
			"attachments": []any{map[string]any{
				"from_url": "https://example.com/", "title": "Example", "text": "Welcome",
			}},
		}))
		var title string
		for _, l := range mc.messages[0].Message.Links {
			if l.URL == "https://example.com" {
				title = l.Title
			}
		}
		must.Eq(t, "Example", title)
	})

	t.Run("should not re-dispatch message_changed as a new message", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventJSON(map[string]any{
			"type": "message", "subtype": "message_changed", "hidden": true,
			"channel": "C123", "ts": "1234567891.000000",
			"message": map[string]any{
				"type": "message", "user": "U_USER", "text": "https://example.com",
				"ts": "1234567890.123456",
				"attachments": []any{map[string]any{
					"from_url": "https://example.com", "title": "Example Site", "text": "Welcome to Example",
				}},
			},
		}))
		must.Eq(t, 0, len(mc.messages))
		must.Eq(t, 0, len(mc.updates))
	})

	t.Run("should ignore hidden message_changed without unfurl attachments", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventJSON(map[string]any{
			"type": "message", "subtype": "message_changed", "hidden": true,
			"channel": "C123", "ts": "1234567891.000000",
			"message": map[string]any{
				"type": "message", "user": "U_USER", "text": "edited text",
				"ts": "1234567890.123456", "edited": map[string]any{"user": "U_USER", "ts": "1234567891.000000"},
			},
		}))
		must.Eq(t, 0, len(mc.messages))
		must.Eq(t, 0, len(mc.updates))
	})
}

func TestEventDeliveryDeduplication(t *testing.T) {
	t.Parallel()

	eventBody := func(eventID string) string {
		payload := map[string]any{
			"type": "event_callback", "team_id": "T123", "event_id": eventID,
			"event": map[string]any{
				"type": "message", "user": "U_USER", "channel": "C123",
				"text": "hello", "ts": "1234567890.123456",
			},
		}
		b, _ := json.Marshal(payload)
		return string(b)
	}

	t.Run("drops a retried delivery of an already-dispatched event", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventBody("Ev1"))
		must.Eq(t, 1, len(mc.messages))
		extra := make(http.Header)
		extra.Set("X-Slack-Retry-Num", "1")
		resp := postWebhook(t, adapter, signedWebhook(t, webhookSecret, "application/json", eventBody("Ev1"), extra, 0))
		must.Eq(t, 200, resp.status)
		must.Eq(t, 1, len(mc.messages))
	})

	t.Run("processes a retry when the original delivery was never dispatched", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		extra := make(http.Header)
		extra.Set("X-Slack-Retry-Num", "2")
		resp := postWebhook(t, adapter, signedWebhook(t, webhookSecret, "application/json", eventBody("Ev_missed"), extra, 0))
		must.Eq(t, 200, resp.status)
		must.Eq(t, 1, len(mc.messages))
	})

	t.Run("does not consult state on first deliveries", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{})
		mc := newMockChat(t)
		spy := &getSpy{StateAdapter: mc.state}
		mc.state = spy
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventBody("Ev2"))
		must.Eq(t, 1, len(mc.messages))
		for _, k := range spy.gets {
			must.False(t, k == "slack:event-delivered:Ev2")
		}
	})
}

type getSpy struct {
	chat.StateAdapter
	mu   sync.Mutex
	gets []string
}

func (s *getSpy) Get(ctx context.Context, key string) (json.RawMessage, error) {
	s.mu.Lock()
	s.gets = append(s.gets, key)
	s.mu.Unlock()
	return s.StateAdapter.Get(ctx, key)
}

func TestEventRoutingViaAuthorizations(t *testing.T) {
	t.Parallel()

	t.Run("prefers authorizations[0] over top-level fields for org installs", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{Token: nil, ClientID: "x"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		must.NoError(t, adapter.SetInstallation(t.Context(), "E_ORG_1", Installation{
			BotToken: "xoxb-org", IsEnterpriseInstall: true,
		}))
		rc, status := adapter.resolveEventRequestContext(t.Context(), map[string]any{
			"type": "event_callback", "team_id": "T_GRID_1",
			"event": map[string]any{"type": "message", "user": "U_USER", "channel": "C123", "text": "hi", "ts": "1234567890.123456"},
			"authorizations": []any{map[string]any{
				"enterprise_id": "E_ORG_1", "team_id": nil, "is_enterprise_install": true,
			}},
		})
		must.Eq(t, "", status)
		must.Eq(t, "E_ORG_1", rc.installationID)
		must.True(t, rc.isEnterpriseInstall)
		must.Eq(t, "xoxb-org", rc.token)
		must.Eq(t, "T_GRID_1", rc.teamID)
	})

	t.Run("uses the authorization's team over a Slack Connect top-level team", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{Token: nil, ClientID: "x"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		must.NoError(t, adapter.SetInstallation(t.Context(), "T_RECIPIENT", Installation{BotToken: "xoxb-recipient"}))
		rc, status := adapter.resolveEventRequestContext(t.Context(), map[string]any{
			"type": "event_callback", "team_id": "T_OTHER_ORG", "enterprise_id": "E_OTHER_ORG",
			"event": map[string]any{"type": "message", "user": "U_USER", "channel": "C123", "text": "hi", "ts": "1234567890.123456"},
			"authorizations": []any{map[string]any{
				"enterprise_id": nil, "team_id": "T_RECIPIENT", "is_enterprise_install": false,
			}},
		})
		must.Eq(t, "", status)
		must.Eq(t, "T_RECIPIENT", rc.installationID)
		must.False(t, rc.isEnterpriseInstall)
		must.Eq(t, "xoxb-recipient", rc.token)
	})

	t.Run("falls back to top-level fields when authorizations is absent", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{Token: nil, ClientID: "x"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		must.NoError(t, adapter.SetInstallation(t.Context(), "E_ORG_1", Installation{
			BotToken: "xoxb-org", IsEnterpriseInstall: true,
		}))
		rc, status := adapter.resolveEventRequestContext(t.Context(), map[string]any{
			"type": "event_callback", "team_id": "T_GRID_1", "enterprise_id": "E_ORG_1",
			"is_enterprise_install": true,
			"event":                 map[string]any{"type": "message", "user": "U_USER", "channel": "C123", "text": "hi", "ts": "1234567890.123456"},
		})
		must.Eq(t, "", status)
		must.Eq(t, "E_ORG_1", rc.installationID)
		must.True(t, rc.isEnterpriseInstall)
		must.Eq(t, "xoxb-org", rc.token)
	})
}

func TestReverseUserLookupUserChangeEvent(t *testing.T) {
	t.Parallel()
	adapter := webhookAdapter(t, Config{})
	mc := newMockChat(t)
	must.NoError(t, adapter.Initialize(t.Context(), mc))
	must.NoError(t, chat.StateSet(t.Context(), mc.state, "slack:user:U_DOM_123",
		userInfo{DisplayName: "dominik", RealName: "Dominik G"}, 8*24*time.Hour))
	resp := postJSON(t, adapter, webhookSecret, eventJSON(map[string]any{
		"type": "user_change", "event_ts": "1234567890.123456",
		"user": map[string]any{
			"id": "U_DOM_123", "name": "dominik", "real_name": "Dominik New",
			"profile": map[string]any{"display_name": "dom_new", "real_name": "Dominik New"},
		},
	}))
	must.Eq(t, 200, resp.status)
	mustWait(t, adapter)
	raw, err := mc.state.Get(t.Context(), "slack:user:U_DOM_123")
	must.NoError(t, err)
	must.Eq(t, 0, len(raw))
}

func TestFeedbackButtonsRoutesClicksThroughOnAction(t *testing.T) {
	t.Parallel()
	adapter := webhookAdapter(t, Config{})
	mc := newMockChat(t)
	must.NoError(t, adapter.Initialize(t.Context(), mc))
	resp := postForm(t, adapter, webhookSecret, formPayload(t, map[string]any{
		"type":       "block_actions",
		"user":       map[string]any{"id": "U1", "username": "user"},
		"trigger_id": "trigger-1",
		"channel":    map[string]any{"id": "D123", "name": "dm"},
		"container":  map[string]any{"type": "message", "message_ts": "1234567890.111111", "channel_id": "D123"},
		"message":    map[string]any{"ts": "1234567890.111111", "thread_ts": "1234567890.000000"},
		"actions": []any{map[string]any{
			"type": "feedback_buttons", "action_id": "message_feedback",
			"value": "positive", "action_ts": "1234567891.000000",
		}},
	}))
	must.Eq(t, 200, resp.status)
	must.Eq(t, 1, len(mc.actions))
	must.Eq(t, "message_feedback", mc.actions[0].ActionID)
	must.Eq(t, "positive", mc.actions[0].Value)
	must.Eq(t, "slack:D123:1234567890.000000", mc.actions[0].ThreadID)
}

func TestConcurrentWebhookRequestContextIsolation(t *testing.T) {
	t.Parallel()
	apiMock := newSlackAPIMock(t)
	apiMock.ok("conversations.replies", map[string]any{"messages": []any{}})
	apiMock.ok("users.info", map[string]any{
		"user": map[string]any{"name": "user", "profile": map[string]any{"display_name": "User"}},
	})
	adapter := apiMock.adapter(t, Config{Token: nil, ClientID: "x"})
	mc := newMockChat(t)
	must.NoError(t, adapter.Initialize(t.Context(), mc))
	must.NoError(t, adapter.SetInstallation(t.Context(), "T_A", Installation{
		BotToken: "xoxb-team-a", BotUserID: "U_BOT_A",
	}))
	must.NoError(t, adapter.SetInstallation(t.Context(), "T_B", Installation{
		BotToken: "xoxb-team-b", BotUserID: "U_BOT_B",
	}))

	bodyA, err := json.Marshal(map[string]any{
		"type": "event_callback", "team_id": "T_A",
		"event": map[string]any{
			"type": "reaction_added", "user": "U123", "reaction": "thumbsup",
			"item": map[string]any{"type": "message", "channel": "C_A", "ts": "1234567890.123456"},
		},
	})
	must.NoError(t, err)
	bodyB, err := json.Marshal(map[string]any{
		"type": "event_callback", "team_id": "T_B",
		"event": map[string]any{
			"type": "reaction_added", "user": "U456", "reaction": "heart",
			"item": map[string]any{"type": "message", "channel": "C_B", "ts": "1234567890.654321"},
		},
	})
	must.NoError(t, err)
	reqA := signedWebhook(t, webhookSecret, "application/json", string(bodyA), nil, 0)
	reqB := signedWebhook(t, webhookSecret, "application/json", string(bodyB), nil, 0)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		adapter.HandleWebhook(httptest.NewRecorder(), reqA)
	}()
	go func() {
		defer wg.Done()
		adapter.HandleWebhook(httptest.NewRecorder(), reqB)
	}()
	delivered := make(chan struct{})
	go func() { wg.Wait(); close(delivered) }()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	select {
	case <-delivered:
	case <-ctx.Done():
		t.Fatal("concurrent webhook deliveries timed out")
	}
	must.NoError(t, adapter.waitPending(ctx))

	got := map[string]string{}
	for _, c := range apiMock.all("conversations.replies") {
		got[c.Form.Get("channel")] = c.Auth
	}
	must.Eq(t, "Bearer xoxb-team-a", got["C_A"])
	must.Eq(t, "Bearer xoxb-team-b", got["C_B"])
}
