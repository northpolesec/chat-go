// Ported from packages/adapter-slack/src/agent-context.test.ts @ 6adca36 (chat v4.40.0).
package slack

import (
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/shoenig/test/must"
)

func TestNormalizeAppContextEntities(t *testing.T) {
	t.Parallel()

	t.Run("returns [] for an empty context object", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, []chat.AppContextEntity{}, NormalizeAppContextEntities(&SlackAppContext{}))
	})

	t.Run("returns [] for a missing context", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, []chat.AppContextEntity{}, NormalizeAppContextEntities(nil))
	})

	t.Run("maps channel_id", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, []chat.AppContextEntity{
			{Kind: chat.AppContextChannel, ChannelID: "C123"},
		}, NormalizeAppContextEntities(&SlackAppContext{
			Entities: []SlackAppContextEntity{
				{Type: "slack#/types/channel_id", Value: "C123"},
			},
		}))
	})

	t.Run("maps canvas_id and list_id", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, []chat.AppContextEntity{
			{Kind: chat.AppContextCanvas, CanvasID: "F1"},
			{Kind: chat.AppContextList, ListID: "L1"},
		}, NormalizeAppContextEntities(&SlackAppContext{
			Entities: []SlackAppContextEntity{
				{Type: "slack#/types/canvas_id", Value: "F1"},
				{Type: "slack#/types/list_id", Value: "L1"},
			},
		}))
	})

	t.Run("maps message_context", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, []chat.AppContextEntity{
			{Kind: chat.AppContextMessage, MessageTs: "111.222", ChannelID: "C9"},
		}, NormalizeAppContextEntities(&SlackAppContext{
			Entities: []SlackAppContextEntity{
				{
					Type: "slack#/types/message_context",
					Value: map[string]any{
						"message_ts": "111.222",
						"channel_id": "C9",
					},
				},
			},
		}))
	})

	t.Run("maps unrecognized tokens to kind unknown", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, []chat.AppContextEntity{
			{Kind: chat.AppContextUnknown, Type: "slack#/types/future", Value: 42},
		}, NormalizeAppContextEntities(&SlackAppContext{
			Entities: []SlackAppContextEntity{
				{Type: "slack#/types/future", Value: 42},
			},
		}))
	})

	t.Run("maps a message_context with a malformed value to kind unknown instead of throwing", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, []chat.AppContextEntity{
			{Kind: chat.AppContextUnknown, Type: "slack#/types/message_context", Value: nil},
			{Kind: chat.AppContextUnknown, Type: "slack#/types/message_context", Value: "not-an-object"},
			{
				Kind:  chat.AppContextUnknown,
				Type:  "slack#/types/message_context",
				Value: map[string]any{"message_ts": 1},
			},
		}, NormalizeAppContextEntities(&SlackAppContext{
			Entities: []SlackAppContextEntity{
				{Type: "slack#/types/message_context", Value: nil},
				{Type: "slack#/types/message_context", Value: "not-an-object"},
				{Type: "slack#/types/message_context", Value: map[string]any{"message_ts": 1}},
			},
		}))
	})

	t.Run("preserves team_id/enterprise_id and relevance order", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, []chat.AppContextEntity{
			{Kind: chat.AppContextChannel, ChannelID: "C1", TeamID: "T1"},
			{Kind: chat.AppContextCanvas, CanvasID: "F1", EnterpriseID: "E1"},
		}, NormalizeAppContextEntities(&SlackAppContext{
			Entities: []SlackAppContextEntity{
				{Type: "slack#/types/channel_id", Value: "C1", TeamID: "T1"},
				{Type: "slack#/types/canvas_id", Value: "F1", EnterpriseID: "E1"},
			},
		}))
	})
}

func TestGetAppContext(t *testing.T) {
	t.Parallel()

	t.Run("reads and normalizes folded app_context from message.raw", func(t *testing.T) {
		t.Parallel()
		message := &chat.Message{
			Raw: map[string]any{
				"app_context": SlackAppContext{
					Entities: []SlackAppContextEntity{
						{Type: "slack#/types/channel_id", Value: "C1"},
					},
				},
			},
		}
		must.Eq(t, []chat.AppContextEntity{
			{Kind: chat.AppContextChannel, ChannelID: "C1"},
		}, GetAppContext(message))
	})

	t.Run("returns [] when the message has no folded app_context", func(t *testing.T) {
		t.Parallel()
		message := &chat.Message{Raw: map[string]any{}}
		must.Eq(t, []chat.AppContextEntity{}, GetAppContext(message))
	})

	t.Run("reads JSON-shaped app_context from message.raw", func(t *testing.T) {
		t.Parallel()
		message := &chat.Message{
			Raw: map[string]any{
				"app_context": map[string]any{
					"entities": []any{
						map[string]any{"type": "slack#/types/channel_id", "value": "C1", "team_id": "T1"},
					},
				},
			},
		}
		must.Eq(t, []chat.AppContextEntity{
			{Kind: chat.AppContextChannel, ChannelID: "C1", TeamID: "T1"},
		}, GetAppContext(message))
	})
}
