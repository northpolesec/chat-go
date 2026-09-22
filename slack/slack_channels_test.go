// Ported from packages/adapter-slack/src/index.test.ts @ 6adca36 (chat v4.40.0)
// (partial: Task 31 — fetchChannelInfo, fetchChannelMessages,
// postChannelMessage, listThreads, scheduleMessage with empty threadTs).
package slack

import (
	"errors"
	"testing"
	"time"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/shoenig/test/must"
)

func TestFetchChannelInfo(t *testing.T) {
	t.Parallel()

	t.Run("fetches channel info", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("conversations.info", map[string]any{
			"channel": map[string]any{
				"id": "C123", "name": "general",
				"is_im": false, "is_mpim": false, "num_members": 42,
				"purpose": map[string]any{"value": "General discussion"},
				"topic":   map[string]any{"value": "Anything goes"},
			},
		})
		adapter := apiMock.adapter(t, Config{})
		info, err := adapter.FetchChannelInfo(t.Context(), "slack:C123")
		must.NoError(t, err)
		must.Eq(t, "slack:C123", info.ID)
		must.Eq(t, "#general", info.Name)
		must.False(t, info.IsDM)
		must.Eq(t, 42, info.MemberCount)
		must.Eq(t, "General discussion", info.Metadata["purpose"])
		must.Eq(t, "Anything goes", info.Metadata["topic"])
	})

	t.Run("detects DM channels", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("conversations.info", map[string]any{
			"channel": map[string]any{"id": "D123", "is_im": true},
		})
		adapter := apiMock.adapter(t, Config{})
		info, err := adapter.FetchChannelInfo(t.Context(), "slack:D123")
		must.NoError(t, err)
		must.True(t, info.IsDM)
	})

	t.Run("throws on invalid channel ID", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{})
		_, err := adapter.FetchChannelInfo(t.Context(), "invalid")
		must.Error(t, err)
		var ve *shared.ValidationError
		must.True(t, errors.As(err, &ve))
		must.StrContains(t, err.Error(), "Invalid Slack channel ID: invalid")
	})
}

func TestFetchChannelMessages(t *testing.T) {
	t.Parallel()

	t.Run("fetches channel messages backward (default)", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("conversations.history", map[string]any{
			"messages": []any{
				map[string]any{"type": "message", "user": "U1", "text": "newest", "ts": "1002.000"},
				map[string]any{"type": "message", "user": "U2", "text": "older", "ts": "1001.000"},
			},
			"has_more": true,
		})
		apiMock.ok("users.info", map[string]any{"user": map[string]any{"name": "user1"}})
		adapter := apiMock.adapter(t, Config{BotUserID: "U_BOT"})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		result, err := adapter.FetchChannelMessages(t.Context(), "slack:C123", chat.FetchOptions{})
		must.NoError(t, err)
		must.Eq(t, 2, len(result.Messages))
		must.True(t, result.NextCursor != "")
	})

	t.Run("fetches channel messages forward", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("conversations.history", map[string]any{
			"messages": []any{
				map[string]any{"type": "message", "user": "U1", "text": "oldest", "ts": "1000.000"},
				map[string]any{"type": "message", "user": "U2", "text": "newer", "ts": "1001.000"},
			},
			"has_more": false,
		})
		apiMock.ok("users.info", map[string]any{"user": map[string]any{"name": "user1"}})
		adapter := apiMock.adapter(t, Config{BotUserID: "U_BOT"})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		result, err := adapter.FetchChannelMessages(t.Context(), "slack:C123", chat.FetchOptions{
			Direction: chat.FetchForward,
		})
		must.NoError(t, err)
		must.Eq(t, 2, len(result.Messages))
	})

	t.Run("throws on invalid channel ID", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{})
		_, err := adapter.FetchChannelMessages(t.Context(), "invalid", chat.FetchOptions{})
		must.Error(t, err)
		var ve *shared.ValidationError
		must.True(t, errors.As(err, &ve))
	})
}

func TestPostChannelMessage(t *testing.T) {
	t.Parallel()

	t.Run("posts to channel without thread context", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("chat.postMessage", map[string]any{"ts": "2222222222.000000"})
		adapter := apiMock.adapter(t, Config{})
		result, err := adapter.PostChannelMessage(t.Context(), "slack:C123", chat.PostableText("Top-level message"))
		must.NoError(t, err)
		must.Eq(t, "2222222222.000000", result.ID)
		must.Eq(t, "slack:C123:2222222222.000000", result.Channel)
		call := apiMock.last("chat.postMessage")
		must.Eq(t, "C123", call.Form.Get("channel"))
		_, hasThread := call.Form["thread_ts"]
		must.False(t, hasThread)
	})

	t.Run("throws on invalid channel ID", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{})
		_, err := adapter.PostChannelMessage(t.Context(), "invalid", chat.PostableText("test"))
		must.Error(t, err)
		var ve *shared.ValidationError
		must.True(t, errors.As(err, &ve))
	})
}

func TestListThreads(t *testing.T) {
	t.Parallel()

	t.Run("lists threads with replies in a channel", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("conversations.history", map[string]any{
			"messages": []any{
				map[string]any{
					"type": "message", "user": "U1", "text": "Thread parent",
					"ts": "1000.000", "reply_count": 5, "latest_reply": "1005.000",
				},
				map[string]any{
					"type": "message", "user": "U2", "text": "No replies", "ts": "999.000",
				},
				map[string]any{
					"type": "message", "user": "U3", "text": "Another thread",
					"ts": "998.000", "reply_count": 2, "latest_reply": "1003.000",
				},
			},
			"response_metadata": map[string]any{},
		})
		apiMock.ok("users.info", map[string]any{"user": map[string]any{"name": "user1"}})
		adapter := apiMock.adapter(t, Config{BotUserID: "U_BOT"})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		result, err := adapter.ListThreads(t.Context(), "slack:C123", chat.FetchOptions{})
		must.NoError(t, err)
		must.Eq(t, 2, len(result.Threads))
		must.Eq(t, 5, result.Threads[0].ReplyCount)
		must.False(t, result.Threads[0].LastReplyAt.IsZero())
	})

	t.Run("throws on invalid channel ID", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{})
		_, err := adapter.ListThreads(t.Context(), "invalid", chat.FetchOptions{})
		must.Error(t, err)
		var ve *shared.ValidationError
		must.True(t, errors.As(err, &ve))
	})
}

func TestScheduleMessageWithEmptyThreadTs(t *testing.T) {
	t.Parallel()
	apiMock := newSlackAPIMock(t)
	apiMock.ok("chat.scheduleMessage", map[string]any{
		"scheduled_message_id": "Q123",
		"post_at":              time.Now().Unix() + 3600,
	})
	adapter := apiMock.adapter(t, Config{})
	_, err := adapter.ScheduleMessage(t.Context(), "slack:C123:", chat.PostableText("Scheduled msg"), time.Now().Add(time.Hour))
	must.NoError(t, err)
	call := apiMock.last("chat.scheduleMessage")
	must.Eq(t, "C123", call.Form.Get("channel"))
	_, hasThread := call.Form["thread_ts"]
	must.False(t, hasThread)
}
