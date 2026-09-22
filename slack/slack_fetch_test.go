// Ported from packages/adapter-slack/src/index.test.ts @ 6adca36 (chat v4.40.0)
// (partial: Task 26 fetchMessages, fetchMessage, fetchThread, initialize).
package slack

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/slack/api"
	"github.com/shoenig/test/must"
)

func TestFetchMessages(t *testing.T) {
	t.Parallel()

	t.Run("fetches messages in forward direction using cursor pagination", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("conversations.replies", map[string]any{
			"messages": []any{
				map[string]any{"type": "message", "user": "U1", "text": "msg1", "ts": "1000.000", "channel": "C123"},
				map[string]any{"type": "message", "user": "U2", "text": "msg2", "ts": "1001.000", "channel": "C123"},
			},
			"response_metadata": map[string]any{"next_cursor": "cursor-abc"},
		})
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{"name": "user1", "real_name": "User One"},
		})
		adapter := apiMock.adapter(t, Config{BotUserID: "U_BOT"})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		result, err := adapter.FetchMessages(t.Context(), "slack:C123:1234567890.000000", chat.FetchOptions{
			Direction: chat.FetchForward,
			Limit:     10,
		})
		must.NoError(t, err)
		must.Eq(t, 2, len(result.Messages))
		must.Eq(t, "cursor-abc", result.NextCursor)
		call := apiMock.last("conversations.replies")
		must.Eq(t, "C123", call.Form.Get("channel"))
		must.Eq(t, "1234567890.000000", call.Form.Get("ts"))
		must.Eq(t, "10", call.Form.Get("limit"))
	})

	t.Run("fetches messages in backward direction (default)", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("conversations.replies", map[string]any{
			"messages": []any{
				map[string]any{"type": "message", "user": "U1", "text": "oldest", "ts": "1000.000", "channel": "C123"},
				map[string]any{"type": "message", "user": "U2", "text": "newest", "ts": "1001.000", "channel": "C123"},
			},
			"has_more": false,
		})
		apiMock.ok("users.info", map[string]any{"user": map[string]any{"name": "user1"}})
		adapter := apiMock.adapter(t, Config{BotUserID: "U_BOT"})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		result, err := adapter.FetchMessages(t.Context(), "slack:C123:1234567890.000000", chat.FetchOptions{})
		must.NoError(t, err)
		must.True(t, len(result.Messages) > 0)
		must.Eq(t, "oldest", result.Messages[0].Text)
		must.Eq(t, "newest", result.Messages[1].Text)
	})

	t.Run("passes cursor for backward pagination", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("conversations.replies", map[string]any{
			"messages": []any{
				map[string]any{"type": "message", "user": "U1", "text": "old", "ts": "900.000", "channel": "C123"},
			},
			"has_more": false,
		})
		apiMock.ok("users.info", map[string]any{"user": map[string]any{"name": "user1"}})
		adapter := apiMock.adapter(t, Config{BotUserID: "U_BOT"})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		_, err := adapter.FetchMessages(t.Context(), "slack:C123:1234567890.000000", chat.FetchOptions{Cursor: "1000.000"})
		must.NoError(t, err)
		call := apiMock.last("conversations.replies")
		must.Eq(t, "1000.000", call.Form.Get("latest"))
		must.Eq(t, "false", call.Form.Get("inclusive"))
	})
}

func TestFetchMessage(t *testing.T) {
	t.Parallel()

	t.Run("fetches a single message by ID", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("conversations.replies", map[string]any{
			"messages": []any{
				map[string]any{"type": "message", "user": "U1", "text": "Found it", "ts": "1234567890.123456", "channel": "C123"},
			},
		})
		apiMock.ok("users.info", map[string]any{"user": map[string]any{"name": "user1"}})
		adapter := apiMock.adapter(t, Config{BotUserID: "U_BOT"})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		msg, err := adapter.FetchMessage(t.Context(), "slack:C123:1234567890.000000", "1234567890.123456")
		must.NoError(t, err)
		must.True(t, msg != nil)
		must.Eq(t, "Found it", msg.Text)
	})

	t.Run("returns null when message not found", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("conversations.replies", map[string]any{
			"messages": []any{
				map[string]any{"type": "message", "user": "U1", "text": "Different msg", "ts": "9999999999.000000", "channel": "C123"},
			},
		})
		adapter := apiMock.adapter(t, Config{BotUserID: "U_BOT"})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		msg, err := adapter.FetchMessage(t.Context(), "slack:C123:1234567890.000000", "1234567890.123456")
		must.NoError(t, err)
		must.Nil(t, msg)
	})
}

func TestFetchThread(t *testing.T) {
	t.Parallel()
	t.Run("fetches thread info with channel details", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("conversations.info", map[string]any{
			"channel": map[string]any{"id": "C123", "name": "general"},
		})
		adapter := apiMock.adapter(t, Config{})
		info, err := adapter.FetchThread(t.Context(), "slack:C123:1234567890.000000")
		must.NoError(t, err)
		must.Eq(t, "slack:C123:1234567890.000000", info.ID)
		must.Eq(t, "C123", info.ChannelID)
		must.Eq(t, "general", info.ChannelName)
		must.Eq(t, "1234567890.000000", info.Metadata["threadTs"])
	})
}

func TestFetchThreadChannelCacheRace(t *testing.T) {
	t.Parallel()
	apiMock := newSlackAPIMock(t)
	apiMock.ok("conversations.info", map[string]any{
		"channel": map[string]any{"id": "C123", "name": "shared", "is_ext_shared": true},
	})
	adapter := apiMock.adapter(t, Config{BotUserID: "U_BOT"})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)

	const n = 32
	var (
		wg        sync.WaitGroup
		errMu     sync.Mutex
		fetchErrs []error
	)
	wg.Add(n * 2)
	for range n {
		go func() {
			defer wg.Done()
			_, err := adapter.FetchThread(ctx, "slack:C123:1234567890.000000")
			if err != nil {
				errMu.Lock()
				fetchErrs = append(fetchErrs, err)
				errMu.Unlock()
			}
		}()
		go func() {
			defer wg.Done()
			_ = adapter.GetChannelVisibility("slack:C123:1234567890.000000")
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("timed out waiting for concurrent FetchThread/GetChannelVisibility")
	}
	must.Eq(t, 0, len(fetchErrs))
	must.Eq(t, chat.ChannelExternal, adapter.GetChannelVisibility("slack:C123:1234567890.000000"))
}

func TestInitialize(t *testing.T) {
	t.Parallel()

	t.Run("fetches bot user ID on initialize with bot token", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("auth.test", map[string]any{
			"user_id": "U_INITIALIZED_BOT", "bot_id": "B_INITIALIZED_BOT", "user": "testbot",
		})
		adapter := apiMock.adapter(t, Config{})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		must.Eq(t, "U_INITIALIZED_BOT", adapter.BotUserID())
		must.Eq(t, "testbot", adapter.UserName())
	})

	t.Run("handles auth.test failure gracefully", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.fail("auth.test", "invalid_auth")
		adapter := apiMock.adapter(t, Config{})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
	})

	t.Run("skips auth.test in multi-workspace mode", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		adapter, err := New(Config{
			SigningSecret: "test-signing-secret",
			APIURL:        apiMock.server.URL + "/",
			HTTPClient:    apiMock.client(),
		})
		must.NoError(t, err)
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		must.Eq(t, 0, apiMock.count("auth.test"))
	})
}

func TestSubclassExtensibility(t *testing.T) {
	t.Parallel()
	adapter := mustNew(t, Config{})
	must.True(t, adapter.logger != nil)
	must.True(t, adapter.format != nil)
}

func TestInitializeSkipsAuthWhenBotUserSet(t *testing.T) {
	t.Parallel()
	// Go-added: upstream skips auth.test when _botUserId is already set.
	apiMock := newSlackAPIMock(t)
	adapter := apiMock.adapter(t, Config{BotUserID: "U_ALREADY", Token: api.StaticToken("xoxb-test-token")})
	sc, _ := newStateChat(t)
	must.NoError(t, adapter.Initialize(t.Context(), sc))
	must.Eq(t, 0, apiMock.count("auth.test"))
	must.Eq(t, "U_ALREADY", adapter.BotUserID())
}
