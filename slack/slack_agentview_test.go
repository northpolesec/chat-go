// Ported from packages/adapter-slack/src/index.test.ts @ 6adca36 (chat v4.40.0)
// (partial: Task 29 describe blocks — agent_view app_home_opened,
// app_context_changed, setSuggestedPrompts thread_ts optional, agent_view DM
// threading, publishHomeView, configured suggestedPrompts / loadingMessages,
// setSuggestedPrompts, setAssistantStatus, setAssistantTitle).
package slack

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/shoenig/test/must"
)

func TestAgentViewAppHomeOpened(t *testing.T) {
	t.Parallel()

	t.Run("dispatches app_home_opened for a non-home tab when agentView is enabled", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{AgentView: true, BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		resp := postJSON(t, adapter, webhookSecret, eventJSON(map[string]any{
			"type": "app_home_opened", "user": "U1", "channel": "D1",
			"tab": "messages", "event_ts": "1.2",
		}))
		must.Eq(t, 200, resp.status)
		must.Eq(t, 1, len(mc.appHome))
		must.Eq(t, "D1", mc.appHome[0].ChannelID)
		must.Eq(t, "U1", mc.appHome[0].UserID)
		must.Eq(t, "messages", mc.appHome[0].Tab)
	})

	t.Run("ignores app_home_opened for a non-home tab by default (assistant_view)", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventJSON(map[string]any{
			"type": "app_home_opened", "user": "U1", "channel": "D1",
			"tab": "messages", "event_ts": "1.2",
		}))
		must.Eq(t, 0, len(mc.appHome))
	})

	t.Run("normalizes folded app_home_opened context into entities", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventJSON(map[string]any{
			"type": "app_home_opened", "user": "U1", "channel": "D1",
			"tab": "home", "event_ts": "1.2",
			"context": map[string]any{
				"entities": []any{map[string]any{"type": "slack#/types/channel_id", "value": "C9"}},
			},
		}))
		must.Eq(t, 1, len(mc.appHome))
		must.Eq(t, "D1", mc.appHome[0].ChannelID)
		must.Eq(t, "U1", mc.appHome[0].UserID)
		must.Eq(t, []chat.AppContextEntity{{Kind: chat.AppContextChannel, ChannelID: "C9"}}, mc.appHome[0].Entities)
	})
}

func TestAppContextChanged(t *testing.T) {
	t.Parallel()

	t.Run("routes app_context_changed with normalized entities", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		resp := postJSON(t, adapter, webhookSecret, eventJSON(map[string]any{
			"type": "app_context_changed", "channel": "D1", "user": "U1",
			"context": map[string]any{
				"entities": []any{map[string]any{"type": "slack#/types/channel_id", "value": "C9"}},
			},
			"event_ts": "1.2",
		}))
		must.Eq(t, 200, resp.status)
		must.Eq(t, 1, len(mc.appContext))
		must.Eq(t, "D1", mc.appContext[0].ChannelID)
		must.Eq(t, "U1", mc.appContext[0].UserID)
		must.Eq(t, []chat.AppContextEntity{{Kind: chat.AppContextChannel, ChannelID: "C9"}}, mc.appContext[0].Entities)
	})

	t.Run("passes empty entities for an empty context object", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, eventJSON(map[string]any{
			"type": "app_context_changed", "channel": "D1", "user": "U1",
			"context": map[string]any{}, "event_ts": "1.2",
		}))
		must.Eq(t, 1, len(mc.appContext))
		must.Eq(t, []chat.AppContextEntity{}, mc.appContext[0].Entities)
	})

	t.Run("returns 200 with empty entities when the payload has no context field", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		resp := postJSON(t, adapter, webhookSecret, eventJSON(map[string]any{
			"type": "app_context_changed", "channel": "D1", "user": "U1", "event_ts": "1.2",
		}))
		must.Eq(t, 200, resp.status)
		must.Eq(t, 1, len(mc.appContext))
		must.Eq(t, []chat.AppContextEntity{}, mc.appContext[0].Entities)
	})
}

func TestSetSuggestedPromptsThreadTsOptional(t *testing.T) {
	t.Parallel()

	t.Run("omits thread_ts when not provided", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		adapter := api.adapter(t, Config{})
		must.NoError(t, adapter.setSuggestedPrompts(t.Context(), "C1", "", []chat.SuggestedPrompt{{Title: "t", Message: "m"}}, ""))
		call := api.last("assistant.threads.setSuggestedPrompts")
		must.Eq(t, "C1", call.JSON["channel_id"])
		_, has := call.JSON["thread_ts"]
		must.False(t, has)
	})

	t.Run("includes thread_ts when provided", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		adapter := api.adapter(t, Config{})
		must.NoError(t, adapter.setSuggestedPrompts(t.Context(), "C1", "111.222", []chat.SuggestedPrompt{{Title: "t", Message: "m"}}, ""))
		call := api.last("assistant.threads.setSuggestedPrompts")
		must.Eq(t, "111.222", call.JSON["thread_ts"])
	})
}

func TestAgentViewDMThreading(t *testing.T) {
	t.Parallel()

	dmBody := func() string {
		return eventJSON(map[string]any{
			"type": "message", "channel": "D1", "channel_type": "im",
			"user": "U1", "text": "hi", "ts": "1771.99", "event_ts": "1771.99",
		})
	}

	t.Run("threads a top-level agent_view DM message under its own ts", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{AgentView: true, BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, dmBody())
		mustWait(t, adapter)
		must.Eq(t, 1, len(mc.messages))
		must.Eq(t, "slack:D1:1771.99", mc.messages[0].ThreadID)
	})

	t.Run("automatically titles a top-level agent_view DM session", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		adapter := api.adapter(t, Config{AgentView: true, BotUserID: "U_BOT"})
		must.NoError(t, adapter.Initialize(t.Context(), newMockChat(t)))
		_ = postJSON(t, adapter, webhookSecret, dmBody())
		mustWait(t, adapter)
		call := api.last("agents.sessions.rename")
		must.Eq(t, "D1", call.Form.Get("channel_id"))
		must.Eq(t, "1771.99", call.Form.Get("thread_ts"))
		must.Eq(t, "hi", call.Form.Get("title"))
	})

	t.Run("skips automatic titles when sessionTitle is disabled", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		adapter := api.adapter(t, Config{AgentView: true, SessionTitle: boolPtr(false), BotUserID: "U_BOT"})
		must.NoError(t, adapter.Initialize(t.Context(), newMockChat(t)))
		_ = postJSON(t, adapter, webhookSecret, dmBody())
		mustWait(t, adapter)
		must.Eq(t, 0, api.count("agents.sessions.rename"))
	})

	t.Run("does not retitle an agent_view DM follow-up", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		adapter := api.adapter(t, Config{AgentView: true, BotUserID: "U_BOT"})
		must.NoError(t, adapter.Initialize(t.Context(), newMockChat(t)))
		_ = postJSON(t, adapter, webhookSecret, eventJSON(map[string]any{
			"type": "message", "channel": "D1", "channel_type": "im",
			"user": "U1", "text": "hi", "ts": "1771.99", "event_ts": "1771.99",
			"thread_ts": "1771.00",
		}))
		must.Eq(t, 0, api.count("agents.sessions.rename"))
	})

	t.Run("routes a top-level agent_view DM message to the conversation-scoped thread when it is subscribed (openDM flow)", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{AgentView: true, BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, mc.state.Subscribe(t.Context(), "slack:D1:"))
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, dmBody())
		mustWait(t, adapter)
		must.Eq(t, 1, len(mc.messages))
		must.Eq(t, "slack:D1:", mc.messages[0].ThreadID)
	})

	t.Run("keeps DM top-level messages conversation-scoped without agentView", func(t *testing.T) {
		t.Parallel()
		adapter := webhookAdapter(t, Config{BotUserID: "U_BOT"})
		mc := newMockChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		_ = postJSON(t, adapter, webhookSecret, dmBody())
		must.Eq(t, 1, len(mc.messages))
		must.Eq(t, "slack:D1:", mc.messages[0].ThreadID)
	})

	t.Run("routes custom labels and clearing separately on an agent_view DM thread", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		adapter := api.adapter(t, Config{AgentView: true})
		must.NoError(t, adapter.StartTyping(t.Context(), "slack:D1:1771.99", "Thinking...", chat.TypingOptions{InitiatorUserID: "U1"}))
		must.NoError(t, adapter.StartTyping(t.Context(), "slack:D1:1771.99", "", chat.TypingOptions{InitiatorUserID: "U1"}))
		must.NoError(t, adapter.StartTyping(t.Context(), "slack:D1:1771.99", "Reading xyz.md", chat.TypingOptions{InitiatorUserID: "U1"}))

		first := api.nth("assistant.threads.setStatus", 1)
		must.Eq(t, "D1", first.Form.Get("channel_id"))
		must.Eq(t, "1771.99", first.Form.Get("thread_ts"))
		must.Eq(t, "Thinking...", first.Form.Get("status"))
		must.Eq(t, `["Thinking..."]`, first.Form.Get("loading_messages"))

		// Go "" is JS unset → processing (Task 26). JS explicit "" → active
		// cannot be expressed on StartTyping's string status.
		session := api.nth("agents.sessions.setStatus", 1)
		must.Eq(t, "D1", session.Form.Get("channel_id"))
		must.Eq(t, "U1", session.Form.Get("initiator_user_id"))
		must.Eq(t, "1771.99", session.Form.Get("thread_ts"))
		must.Eq(t, "processing", session.Form.Get("status"))

		second := api.nth("assistant.threads.setStatus", 2)
		must.Eq(t, "D1", second.Form.Get("channel_id"))
		must.Eq(t, "1771.99", second.Form.Get("thread_ts"))
		must.Eq(t, "Reading xyz.md", second.Form.Get("status"))
		must.Eq(t, `["Reading xyz.md"]`, second.Form.Get("loading_messages"))
	})
}

func TestPublishHomeView(t *testing.T) {
	t.Parallel()

	t.Run("publishes a home tab view", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		adapter := api.adapter(t, Config{})
		view := map[string]any{
			"type":   "home",
			"blocks": []any{map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": "Hello"}}},
		}
		must.NoError(t, adapter.PublishHomeView(t.Context(), "U_USER_1", view))
		call := api.last("views.publish")
		must.Eq(t, "U_USER_1", call.JSON["user_id"])
		must.Eq(t, "Bearer xoxb-test-token", call.Auth)
	})
}

func TestConfiguredSuggestedPrompts(t *testing.T) {
	t.Parallel()

	assistantStarted := eventJSONTeam(map[string]any{
		"type": "assistant_thread_started", "event_ts": "1234567890.000000",
		"assistant_thread": map[string]any{
			"user_id": "U_USER", "channel_id": "D_ASSISTANT", "thread_ts": "1234567890.111111",
			"context": map[string]any{"channel_id": "C_CONTEXT", "team_id": "T123"},
		},
	})
	homeOpened := func(tab string, context any) string {
		event := map[string]any{
			"type": "app_home_opened", "user": "U_USER", "channel": "D1",
			"tab": tab, "event_ts": "1.2",
		}
		if context != nil {
			event["context"] = context
		}
		return eventJSONTeam(event)
	}
	setup := func(t *testing.T, extra Config) (*SlackAdapter, *slackAPIMock) {
		t.Helper()
		api := newSlackAPIMock(t)
		extra.BotUserID = "U_BOT"
		adapter := api.adapter(t, extra)
		must.NoError(t, adapter.Initialize(t.Context(), newMockChat(t)))
		return adapter, api
	}

	t.Run("applies static prompts on assistant_thread_started (legacy assistant_view)", func(t *testing.T) {
		t.Parallel()
		adapter, api := setup(t, Config{SuggestedPrompts: &chat.SuggestedPrompts{
			Title:   "Welcome!",
			Prompts: []chat.SuggestedPrompt{{Title: "Ideas", Message: "Generate ideas"}},
		}})
		resp := postJSON(t, adapter, webhookSecret, assistantStarted)
		must.Eq(t, 200, resp.status)
		call := api.last("assistant.threads.setSuggestedPrompts")
		must.Eq(t, "D_ASSISTANT", call.JSON["channel_id"])
		must.Eq(t, "1234567890.111111", call.JSON["thread_ts"])
		must.Eq(t, "Welcome!", call.JSON["title"])
		must.Eq(t, []any{map[string]any{"title": "Ideas", "message": "Generate ideas"}}, jsonAnySlice(call.JSON["prompts"]))
	})

	t.Run("applies prompts on Messages-tab app_home_opened under agentView, without thread_ts", func(t *testing.T) {
		t.Parallel()
		adapter, api := setup(t, Config{
			AgentView: true,
			SuggestedPrompts: &chat.SuggestedPrompts{
				Prompts: []chat.SuggestedPrompt{{Title: "Summarize", Message: "Summarize this channel"}},
			},
		})
		_ = postJSON(t, adapter, webhookSecret, homeOpened("messages", nil))
		must.Eq(t, 1, api.count("assistant.threads.setSuggestedPrompts"))
		call := api.last("assistant.threads.setSuggestedPrompts")
		must.Eq(t, "D1", call.JSON["channel_id"])
		_, has := call.JSON["thread_ts"]
		must.False(t, has)
	})

	t.Run("does not apply prompts on a Home-tab open under agentView", func(t *testing.T) {
		t.Parallel()
		adapter, api := setup(t, Config{
			AgentView: true,
			SuggestedPrompts: &chat.SuggestedPrompts{
				Prompts: []chat.SuggestedPrompt{{Title: "Summarize", Message: "Summarize this channel"}},
			},
		})
		_ = postJSON(t, adapter, webhookSecret, homeOpened("home", nil))
		must.Eq(t, 0, api.count("assistant.threads.setSuggestedPrompts"))
	})

	t.Run("does nothing when suggestedPrompts is not configured", func(t *testing.T) {
		t.Parallel()
		adapter, api := setup(t, Config{})
		_ = postJSON(t, adapter, webhookSecret, assistantStarted)
		must.Eq(t, 0, api.count("assistant.threads.setSuggestedPrompts"))
	})

	t.Run("invokes a dynamic resolver with thread context", func(t *testing.T) {
		t.Parallel()
		var got SuggestedPromptsContext
		adapter, api := setup(t, Config{
			SuggestedPromptsResolver: func(_ context.Context, in SuggestedPromptsContext) (*chat.SuggestedPrompts, error) {
				got = in
				return &chat.SuggestedPrompts{Prompts: []chat.SuggestedPrompt{{Title: "Dynamic", Message: "Resolved per thread"}}}, nil
			},
		})
		_ = postJSON(t, adapter, webhookSecret, assistantStarted)
		must.Eq(t, "D_ASSISTANT", got.ChannelID)
		must.Eq(t, "1234567890.111111", got.ThreadTS)
		must.Eq(t, "U_USER", got.UserID)
		must.Eq(t, "T123", got.TeamID)
		call := api.last("assistant.threads.setSuggestedPrompts")
		must.Eq(t, []any{map[string]any{"title": "Dynamic", "message": "Resolved per thread"}}, jsonAnySlice(call.JSON["prompts"]))
	})

	t.Run("passes team and active-view entities to the resolver under agentView", func(t *testing.T) {
		t.Parallel()
		var got SuggestedPromptsContext
		adapter, _ := setup(t, Config{
			AgentView: true,
			SuggestedPromptsResolver: func(_ context.Context, in SuggestedPromptsContext) (*chat.SuggestedPrompts, error) {
				got = in
				return nil, nil
			},
		})
		_ = postJSON(t, adapter, webhookSecret, homeOpened("messages", map[string]any{
			"entities": []any{map[string]any{"type": "slack#/types/channel_id", "value": "C42", "team_id": "T123"}},
		}))
		must.Eq(t, "D1", got.ChannelID)
		must.Eq(t, "T123", got.TeamID)
		must.Eq(t, 1, len(got.Entities))
		must.Eq(t, chat.AppContextChannel, got.Entities[0].Kind)
		must.Eq(t, "C42", got.Entities[0].ChannelID)
	})

	t.Run("skips setting prompts when the resolver returns null", func(t *testing.T) {
		t.Parallel()
		adapter, api := setup(t, Config{
			SuggestedPromptsResolver: func(context.Context, SuggestedPromptsContext) (*chat.SuggestedPrompts, error) {
				return nil, nil
			},
		})
		_ = postJSON(t, adapter, webhookSecret, assistantStarted)
		must.Eq(t, 0, api.count("assistant.threads.setSuggestedPrompts"))
	})

	t.Run("truncates to Slack's 4-prompt limit with a warning", func(t *testing.T) {
		t.Parallel()
		prompts := make([]chat.SuggestedPrompt, 6)
		for i := range prompts {
			prompts[i] = chat.SuggestedPrompt{Title: "P" + strconv.Itoa(i), Message: "M" + strconv.Itoa(i)}
		}
		adapter, api := setup(t, Config{SuggestedPrompts: &chat.SuggestedPrompts{Prompts: prompts}})
		_ = postJSON(t, adapter, webhookSecret, assistantStarted)
		call := api.last("assistant.threads.setSuggestedPrompts")
		got := jsonAnySlice(call.JSON["prompts"])
		must.Eq(t, 4, len(got))
		must.Eq(t, map[string]any{"title": "P3", "message": "M3"}, got[3].(map[string]any))
	})

	t.Run("logs and keeps the webhook green when the resolver throws", func(t *testing.T) {
		t.Parallel()
		adapter, api := setup(t, Config{
			SuggestedPromptsResolver: func(context.Context, SuggestedPromptsContext) (*chat.SuggestedPrompts, error) {
				return nil, errors.New("resolver blew up")
			},
		})
		resp := postJSON(t, adapter, webhookSecret, assistantStarted)
		must.Eq(t, 200, resp.status)
		must.Eq(t, 0, api.count("assistant.threads.setSuggestedPrompts"))
	})

	t.Run("logs and keeps the webhook green when the API call fails", func(t *testing.T) {
		t.Parallel()
		adapter, api := setup(t, Config{SuggestedPrompts: &chat.SuggestedPrompts{
			Prompts: []chat.SuggestedPrompt{{Title: "Ideas", Message: "Generate ideas"}},
		}})
		api.fail("assistant.threads.setSuggestedPrompts", "internal_error")
		resp := postJSON(t, adapter, webhookSecret, assistantStarted)
		must.Eq(t, 200, resp.status)
		must.Eq(t, 1, api.count("assistant.threads.setSuggestedPrompts"))
	})
}

func TestConfiguredLoadingMessages(t *testing.T) {
	t.Parallel()

	t.Run("startTyping uses configured loading messages by default", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		adapter := api.adapter(t, Config{LoadingMessages: []string{"Thinking...", "Digging..."}})
		must.NoError(t, adapter.StartTyping(t.Context(), "slack:D1:1.2", "", chat.TypingOptions{}))
		call := api.last("assistant.threads.setStatus")
		must.Eq(t, "Thinking...", call.Form.Get("status"))
		must.Eq(t, `["Thinking...","Digging..."]`, call.Form.Get("loading_messages"))
	})

	t.Run("startTyping prefers an explicit status over configured messages", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		adapter := api.adapter(t, Config{LoadingMessages: []string{"Thinking..."}})
		must.NoError(t, adapter.StartTyping(t.Context(), "slack:D1:1.2", "Searching docs...", chat.TypingOptions{}))
		call := api.last("assistant.threads.setStatus")
		must.Eq(t, "Searching docs...", call.Form.Get("status"))
		must.Eq(t, `["Searching docs..."]`, call.Form.Get("loading_messages"))
	})

	t.Run("setAssistantStatus falls back to configured loading messages", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		adapter := api.adapter(t, Config{LoadingMessages: []string{"Thinking..."}})
		must.NoError(t, adapter.setAssistantStatus(t.Context(), "D1", "1.2", "working", nil))
		call := api.last("assistant.threads.setStatus")
		must.Eq(t, "working", call.Form.Get("status"))
		must.Eq(t, `["Thinking..."]`, call.Form.Get("loading_messages"))
	})

	t.Run("setAssistantStatus explicit loadingMessages win over config", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		adapter := api.adapter(t, Config{LoadingMessages: []string{"Thinking..."}})
		must.NoError(t, adapter.setAssistantStatus(t.Context(), "D1", "1.2", "working", []string{"Custom..."}))
		call := api.last("assistant.threads.setStatus")
		must.Eq(t, `["Custom..."]`, call.Form.Get("loading_messages"))
	})

	t.Run("startTyping keeps the Typing... default without config", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		adapter := api.adapter(t, Config{})
		must.NoError(t, adapter.StartTyping(t.Context(), "slack:D1:1.2", "", chat.TypingOptions{}))
		call := api.last("assistant.threads.setStatus")
		must.Eq(t, "Typing...", call.Form.Get("status"))
		must.Eq(t, `["Typing..."]`, call.Form.Get("loading_messages"))
	})
}

func TestSetSuggestedPrompts(t *testing.T) {
	t.Parallel()

	t.Run("sets suggested prompts for assistant thread", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		adapter := api.adapter(t, Config{})
		must.NoError(t, adapter.setSuggestedPrompts(t.Context(), "C123", "1234567890.000000", []chat.SuggestedPrompt{{Title: "Help", Message: "How can I help?"}}, ""))
		call := api.last("assistant.threads.setSuggestedPrompts")
		must.Eq(t, "C123", call.JSON["channel_id"])
		must.Eq(t, "1234567890.000000", call.JSON["thread_ts"])
		must.Eq(t, []any{map[string]any{"title": "Help", "message": "How can I help?"}}, jsonAnySlice(call.JSON["prompts"]))
		must.Eq(t, "Bearer xoxb-test-token", call.Auth)
	})

	t.Run("passes optional title", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		adapter := api.adapter(t, Config{})
		must.NoError(t, adapter.setSuggestedPrompts(t.Context(), "C123", "1234567890.000000", []chat.SuggestedPrompt{{Title: "Prompt", Message: "Try this"}}, "Pick a prompt"))
		call := api.last("assistant.threads.setSuggestedPrompts")
		must.Eq(t, "Pick a prompt", call.JSON["title"])
	})

	t.Run("always sends the prompts key when empty", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		adapter := api.adapter(t, Config{})
		must.NoError(t, adapter.setSuggestedPrompts(t.Context(), "C1", "", nil, ""))
		call := api.last("assistant.threads.setSuggestedPrompts")
		_, has := call.JSON["prompts"]
		must.True(t, has)
		must.Eq(t, []any{}, jsonAnySlice(call.JSON["prompts"]))
	})
}

func TestSetAssistantStatus(t *testing.T) {
	t.Parallel()

	t.Run("sets assistant thread status", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		adapter := api.adapter(t, Config{})
		must.NoError(t, adapter.setAssistantStatus(t.Context(), "C123", "1234567890.000000", "Thinking...", nil))
		call := api.last("assistant.threads.setStatus")
		must.Eq(t, "C123", call.Form.Get("channel_id"))
		must.Eq(t, "1234567890.000000", call.Form.Get("thread_ts"))
		must.Eq(t, "Thinking...", call.Form.Get("status"))
		must.Eq(t, "Bearer xoxb-test-token", call.Auth)
	})

	t.Run("passes loading messages when provided", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		adapter := api.adapter(t, Config{})
		must.NoError(t, adapter.setAssistantStatus(t.Context(), "C123", "1234567890.000000", "Working...", []string{"Step 1", "Step 2"}))
		call := api.last("assistant.threads.setStatus")
		must.Eq(t, `["Step 1","Step 2"]`, call.Form.Get("loading_messages"))
	})

	t.Run("passes agent loading messages", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name       string
			configured []string
			supplied   []string
			expected   string
		}{
			{"undefined/undefined", nil, nil, `["Working..."]`},
			{"configured/undefined", []string{"Configured..."}, nil, `["Configured..."]`},
			{"configured/supplied", []string{"Configured..."}, []string{"Searching...", "Reading..."}, `["Searching...","Reading..."]`},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				api := newSlackAPIMock(t)
				adapter := api.adapter(t, Config{AgentView: true, LoadingMessages: tc.configured})
				must.NoError(t, adapter.setAssistantStatus(t.Context(), "D123", "1.2", "Working...", tc.supplied))
				must.Eq(t, 1, api.count("assistant.threads.setStatus"))
				must.Eq(t, 0, api.count("agents.sessions.setStatus"))
				call := api.last("assistant.threads.setStatus")
				must.Eq(t, "D123", call.Form.Get("channel_id"))
				must.Eq(t, "1.2", call.Form.Get("thread_ts"))
				must.Eq(t, "Working...", call.Form.Get("status"))
				must.Eq(t, tc.expected, call.Form.Get("loading_messages"))
				must.Eq(t, "Bearer xoxb-test-token", call.Auth)
			})
		}
	})

	t.Run("surfaces custom status text via the legacy API and clears via the sessions lifecycle", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		adapter := api.adapter(t, Config{AgentView: true})
		must.NoError(t, adapter.setAssistantStatus(t.Context(), "D123", "1.2", "Working...", nil))
		must.NoError(t, adapter.setAssistantStatus(t.Context(), "D123", "1.2", "", nil))
		status := api.last("assistant.threads.setStatus")
		must.Eq(t, "D123", status.Form.Get("channel_id"))
		must.Eq(t, "1.2", status.Form.Get("thread_ts"))
		must.Eq(t, "Working...", status.Form.Get("status"))
		session := api.last("agents.sessions.setStatus")
		must.Eq(t, "D123", session.Form.Get("channel_id"))
		must.Eq(t, "1.2", session.Form.Get("thread_ts"))
		must.Eq(t, "active", session.Form.Get("status"))
	})
}

func jsonAnySlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func TestSetAssistantTitle(t *testing.T) {
	t.Parallel()

	t.Run("sets title for assistant thread", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		adapter := api.adapter(t, Config{})
		must.NoError(t, adapter.setAssistantTitle(t.Context(), "C123", "1234567890.000000", "My Thread Title"))
		call := api.last("assistant.threads.setTitle")
		must.Eq(t, "C123", call.Form.Get("channel_id"))
		must.Eq(t, "1234567890.000000", call.Form.Get("thread_ts"))
		must.Eq(t, "My Thread Title", call.Form.Get("title"))
		must.Eq(t, "Bearer xoxb-test-token", call.Auth)
	})

	t.Run("renames an agent session under agentView", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		adapter := api.adapter(t, Config{AgentView: true})
		must.NoError(t, adapter.setAssistantTitle(t.Context(), "D123", "1.2", "Agent title"))
		call := api.last("agents.sessions.rename")
		must.Eq(t, "D123", call.Form.Get("channel_id"))
		must.Eq(t, "1.2", call.Form.Get("thread_ts"))
		must.Eq(t, "Agent title", call.Form.Get("title"))
	})
}
