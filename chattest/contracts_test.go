package chattest_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/chattest"
)

// Fake adapters self-test the runners the way packages/tests/src/*.test.ts
// run each contract against a compliant stub.

func TestConnectContractFake(t *testing.T) {
	t.Parallel()
	chattest.RunConnectContract(t, func(t *testing.T) chat.Adapter {
		return &fakeConnect{}
	})
}

func TestThreadIDContractFake(t *testing.T) {
	t.Parallel()
	chattest.RunThreadIDContract(t, func(t *testing.T) chat.Adapter {
		return fakeThread{}
	})
}

func TestSelfMessageContractFake(t *testing.T) {
	t.Parallel()
	chattest.RunSelfMessageContract(t, func(t *testing.T) chat.Adapter {
		return newFakeSelf()
	})
}

type fakeDecoded struct {
	Channel string
	Thread  string
}

type fakeThread struct{ stubAdapter }

func (fakeThread) Name() string { return "fake" }

func (fakeThread) EncodeThreadID(decoded any) (string, error) {
	d := decoded.(fakeDecoded)
	return "fake:" + d.Channel + ":" + d.Thread, nil
}

func (fakeThread) DecodeThreadID(id string) (any, error) {
	parts := strings.Split(id, ":")
	return fakeDecoded{Channel: parts[1], Thread: parts[2]}, nil
}

func (fakeThread) ThreadIDCases() []chattest.ThreadIDCase {
	return []chattest.ThreadIDCase{
		{Decoded: fakeDecoded{Channel: "C1", Thread: "T1"}, Encoded: "fake:C1:T1"},
		{Decoded: fakeDecoded{Channel: "C2", Thread: "T2"}},
	}
}

func (fakeThread) IsDM(id string) bool   { return strings.Contains(id, ":D") }
func (fakeThread) DMThreadID() string    { return "fake:D1:T1" }
func (fakeThread) NonDMThreadID() string { return "fake:C1:T1" }

type fakeConnect struct{ stubAdapter }

func (f *fakeConnect) NewWithVerifier(_ *testing.T, v chattest.ConnectWebhookVerifier) chat.Adapter {
	return &fakeConnectHandler{verifier: v}
}

func (f *fakeConnect) NewWithSecretAndVerifier(t *testing.T, v chattest.ConnectWebhookVerifier) (chat.Adapter, bool) {
	return f.NewWithVerifier(t, v), true
}

func (f *fakeConnect) WebhookRequest(*testing.T) *http.Request {
	return httptest.NewRequest(http.MethodPost, "https://example.com/api/webhooks/fake", strings.NewReader("{}"))
}

type fakeConnectHandler struct {
	stubAdapter
	verifier chattest.ConnectWebhookVerifier
}

func (f *fakeConnectHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	result, err := f.verifier(r, string(body))
	if err != nil || chattest.IsFalsy(result) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	w.WriteHeader(http.StatusOK)
}

const fakeBotID = "bot"

type fakeSelf struct {
	stubAdapter
	chat *chattest.MockChat
}

func newFakeSelf() *fakeSelf {
	return &fakeSelf{chat: chattest.NewMockChat(nil)}
}

func (f *fakeSelf) Chat() chat.ChatInstance { return f.chat }

func (f *fakeSelf) OtherMessageRequest(*testing.T) *http.Request {
	return fakeSelfRequest("alice")
}

func (f *fakeSelf) SelfMessageRequest(*testing.T) *http.Request {
	return fakeSelfRequest(fakeBotID)
}

func (f *fakeSelf) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		AuthorID string `json:"authorId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	if payload.AuthorID != fakeBotID {
		_ = f.chat.ProcessMessage(r.Context(), chat.ProcessMessageInput{})
	}
	w.WriteHeader(http.StatusOK)
}

func fakeSelfRequest(authorID string) *http.Request {
	body, _ := json.Marshal(map[string]string{"authorId": authorID})
	return httptest.NewRequest(http.MethodPost, "https://example.com/api/webhooks/fake", strings.NewReader(string(body)))
}

type stubAdapter struct{}

func (stubAdapter) Initialize(context.Context, chat.ChatInstance) error { return nil }
func (stubAdapter) EncodeThreadID(any) (string, error)                  { return "", nil }
func (stubAdapter) DecodeThreadID(string) (any, error)                  { return nil, nil }
func (stubAdapter) ChannelIDFromThreadID(string) string                 { return "" }
func (stubAdapter) BotUserID() string                                   { return "" }
func (stubAdapter) ParseMessage(any) (*chat.Message, error)             { return nil, nil }
func (stubAdapter) PostMessage(context.Context, string, chat.AdapterPostableMessage) (*chat.RawMessage, error) {
	return &chat.RawMessage{ID: "msg-1"}, nil
}
func (stubAdapter) EditMessage(context.Context, string, string, chat.AdapterPostableMessage) (*chat.RawMessage, error) {
	return &chat.RawMessage{ID: "msg-1"}, nil
}
func (stubAdapter) DeleteMessage(context.Context, string, string) error { return nil }
func (stubAdapter) AddReaction(context.Context, string, string, chat.EmojiValue) error {
	return nil
}
func (stubAdapter) RemoveReaction(context.Context, string, string, chat.EmojiValue) error {
	return nil
}
func (stubAdapter) FetchMessages(context.Context, string, chat.FetchOptions) (chat.FetchResult, error) {
	return chat.FetchResult{}, nil
}
func (stubAdapter) StartTyping(context.Context, string, string, chat.TypingOptions) error {
	return nil
}
