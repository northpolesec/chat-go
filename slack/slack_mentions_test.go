// Ported from packages/adapter-slack/src/index.test.ts @ 6adca36 (chat v4.40.0)
// (partial: Task 26 resolveInlineMentions, reverse user lookup, getUser,
// W-prefixed enterprise user IDs). Webhook-dispatch cases call parseSlackMessage
// / resolveInlineMentions directly (handleWebhook is Task 28).
package slack

import (
	"net/http"
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/slack/api"
	"github.com/shoenig/test/must"
)

func TestResolveInlineMentions(t *testing.T) {
	t.Parallel()

	t.Run("resolves user mentions in incoming messages via webhook", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{
				"name": "johndoe", "real_name": "John Doe",
				"profile": map[string]any{"display_name": "John", "real_name": "John Doe"},
			},
		})
		adapter := apiMock.adapter(t, Config{BotUserID: "U_BOT"})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		msg, err := adapter.parseSlackMessage(t.Context(), SlackEvent{
			Type: "message", User: "U_SENDER", Channel: "C456",
			Text: "Hey <@UOTHER123> check this out", Ts: "1234567890.555555",
		}, "slack:C456:1234567890.555555")
		must.NoError(t, err)
		must.StrContains(t, msg.Text, "@John")
	})

	t.Run("resolves the bot's own mention and flags it in incoming webhooks", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{
				"name": "user", "profile": map[string]any{"display_name": "Test User", "real_name": "Test User"},
			},
		})
		adapter := apiMock.adapter(t, Config{BotUserID: "U_BOT"})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		msg, err := adapter.parseSlackMessage(t.Context(), SlackEvent{
			Type: "app_mention", User: "U_SENDER", Channel: "C456",
			Text: "<@U_BOT> help me", Ts: "1234567890.666666",
		}, "slack:C456:1234567890.666666")
		must.NoError(t, err)
		must.Eq(t, "@Test User help me", msg.Text)
		must.True(t, msg.IsMention != nil && *msg.IsMention)
	})

	t.Run("flags the bot's own mention in rich text table cells", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.on("users.info", func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			user := r.Form.Get("user")
			name := "Sender"
			if user == "U_BOT" {
				name = "Test Bot"
			}
			writeJSON(w, 200, map[string]any{
				"ok": true,
				"user": map[string]any{
					"name": user, "profile": map[string]any{"display_name": name},
				},
			})
		})
		adapter := apiMock.adapter(t, Config{BotUserID: "U_BOT"})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		msg, err := adapter.parseSlackMessage(t.Context(), SlackEvent{
			Type: "message", User: "U_SENDER", Channel: "C456",
			Ts: "1234567890.676767",
			Blocks: []map[string]any{{
				"type": "table",
				"rows": []any{[]any{map[string]any{
					"type": "rich_text",
					"elements": []any{map[string]any{
						"type": "rich_text_section",
						"elements": []any{
							map[string]any{"type": "user", "user_id": "U_BOT"},
						},
					}},
				}}},
			}},
		}, "slack:C456:1234567890.676767")
		must.NoError(t, err)
		must.Eq(t, "@Test Bot", msg.Text)
		must.True(t, msg.IsMention != nil && *msg.IsMention)
	})

	t.Run("falls back to the bot's user ID when users.info fails for its own mention", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.on("users.info", func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			if r.Form.Get("user") == "U_BOT" {
				writeJSON(w, 200, map[string]any{"ok": false, "error": "rate_limited"})
				return
			}
			writeJSON(w, 200, map[string]any{
				"ok":   true,
				"user": map[string]any{"name": "user", "profile": map[string]any{"display_name": "Test User"}},
			})
		})
		adapter := apiMock.adapter(t, Config{BotUserID: "U_BOT"})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		msg, err := adapter.parseSlackMessage(t.Context(), SlackEvent{
			Type: "app_mention", User: "U_SENDER", Channel: "C456",
			Text: "<@U_BOT> help me", Ts: "1234567890.777777",
		}, "slack:C456:1234567890.777777")
		must.NoError(t, err)
		must.Eq(t, "@U_BOT help me", msg.Text)
		must.True(t, msg.IsMention != nil && *msg.IsMention)
	})

	t.Run("resolves request-scoped self mention in multi-workspace mode", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{
				"name": "workspacebot", "real_name": "Workspace Bot",
				"profile": map[string]any{"display_name": "Workspace Bot", "real_name": "Workspace Bot"},
			},
		})
		adapter, err := New(Config{
			SigningSecret: "test-signing-secret",
			APIURL:        apiMock.server.URL + "/",
			HTTPClient:    apiMock.client(),
		})
		must.NoError(t, err)
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		ctx := withRequestContext(t.Context(), &requestContext{token: "xoxb-multi-token", botUserID: "U_BOT_MULTI"})
		result := adapter.resolveInlineMentions(ctx, "<@U_BOT_MULTI> help me")
		must.Eq(t, "<@U_BOT_MULTI|Workspace Bot> help me", result)
	})

	t.Run("resolves bare channel mentions in incoming messages", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{"name": "sender", "profile": map[string]any{"display_name": "Sender"}},
		})
		apiMock.ok("conversations.info", map[string]any{"channel": map[string]any{"name": "general"}})
		adapter := apiMock.adapter(t, Config{BotUserID: "U_BOT"})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		msg, err := adapter.parseSlackMessage(t.Context(), SlackEvent{
			Type: "message", User: "U_SENDER", Channel: "C456",
			Text: "Check out <#C789>", Ts: "1234567890.777777",
		}, "slack:C456:1234567890.777777")
		must.NoError(t, err)
		must.StrContains(t, msg.Text, "#general")
	})

	t.Run("leaves channel mentions with existing labels unchanged", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{"name": "sender", "profile": map[string]any{"display_name": "Sender"}},
		})
		adapter := apiMock.adapter(t, Config{BotUserID: "U_BOT"})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		msg, err := adapter.parseSlackMessage(t.Context(), SlackEvent{
			Type: "message", User: "U_SENDER", Channel: "C456",
			Text: "Check out <#C789|existing-name>", Ts: "1234567890.888888",
		}, "slack:C456:1234567890.888888")
		must.NoError(t, err)
		must.StrContains(t, msg.Text, "#existing-name")
		must.Eq(t, 0, apiMock.count("conversations.info"))
	})

	t.Run("falls back to channel ID when conversations.info fails", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{"name": "sender", "profile": map[string]any{"display_name": "Sender"}},
		})
		apiMock.fail("conversations.info", "channel_not_found")
		adapter := apiMock.adapter(t, Config{BotUserID: "U_BOT"})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		msg, err := adapter.parseSlackMessage(t.Context(), SlackEvent{
			Type: "message", User: "U_SENDER", Channel: "C456",
			Text: "Check out <#CUNKNOWN>", Ts: "1234567890.999999",
		}, "slack:C456:1234567890.999999")
		must.NoError(t, err)
		must.StrContains(t, msg.Text, "#CUNKNOWN")
	})
}

func TestReverseUserLookup(t *testing.T) {
	t.Parallel()

	t.Run("reverse index storage in lookupUser / stores reverse index when looking up a user", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{
				"name": "dominik", "real_name": "Dominik G",
				"profile": map[string]any{"display_name": "dominik", "real_name": "Dominik G"},
			},
		})
		adapter := apiMock.adapter(t, Config{})
		sc, st := newStateChat(t)
		adapter.chat = sc
		must.True(t, adapter.lookupUser(t.Context(), "U_DOM_123") != nil)
		must.SliceContains(t, mustList(t, st, "slack:user-by-name:dominik"), "U_DOM_123")
	})

	t.Run("resolveOutgoingMentions / resolves unambiguous @mention to <@USER_ID>", func(t *testing.T) {
		t.Parallel()
		adapter, st := adapterWithState(t)
		mustAppend(t, st, "slack:user-by-name:dominik", "U_DOM_123")
		got, err := adapter.resolveOutgoingMentions(t.Context(), "Hey @dominik, check this out", "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, "Hey <@U_DOM_123>, check this out", got)
	})

	t.Run("resolveOutgoingMentions / handles case insensitivity", func(t *testing.T) {
		t.Parallel()
		adapter, st := adapterWithState(t)
		mustAppend(t, st, "slack:user-by-name:dominik", "U_DOM_123")
		got, err := adapter.resolveOutgoingMentions(t.Context(), "Hey @Dominik!", "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, "Hey <@U_DOM_123>!", got)
	})

	t.Run("resolveOutgoingMentions / does not resolve @handles inside URLs", func(t *testing.T) {
		t.Parallel()
		adapter, st := adapterWithState(t)
		mustAppend(t, st, "slack:user-by-name:jkyang", "U_URL_123")
		mustAppend(t, st, "slack:user-by-name:dominik", "U_DOM_123")
		mustAppend(t, st, "slack:user-by-name:example", "U_EMAIL_123")
		got, err := adapter.resolveOutgoingMentions(t.Context(),
			"See https://hackmd.io/@jkyang/abc, https://example.com/p?user=@jkyang, https://example.com/docs#@jkyang, hackmd.io/@jkyang/abc, <https://example.com/@jkyang|profile>, and user@example.com cc @dominik",
			"slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, "See https://hackmd.io/@jkyang/abc, https://example.com/p?user=@jkyang, https://example.com/docs#@jkyang, hackmd.io/@jkyang/abc, <https://example.com/@jkyang|profile>, and user@example.com cc <@U_DOM_123>", got)
	})

	t.Run("resolveOutgoingMentions / deduplicates user IDs from reverse index", func(t *testing.T) {
		t.Parallel()
		adapter, st := adapterWithState(t)
		mustAppend(t, st, "slack:user-by-name:dominik", "U_DOM_123")
		mustAppend(t, st, "slack:user-by-name:dominik", "U_DOM_123")
		got, err := adapter.resolveOutgoingMentions(t.Context(), "Hey @dominik", "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, "Hey <@U_DOM_123>", got)
	})

	t.Run("resolveOutgoingMentions / leaves mention as plain text when no match found", func(t *testing.T) {
		t.Parallel()
		adapter, _ := adapterWithState(t)
		got, err := adapter.resolveOutgoingMentions(t.Context(), "Hey @unknown_user", "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, "Hey @unknown_user", got)
	})

	t.Run("resolveOutgoingMentions / skips already-resolved <@USER_ID> mentions", func(t *testing.T) {
		t.Parallel()
		adapter, st := adapterWithState(t)
		mustAppend(t, st, "slack:user-by-name:dominik", "U_DOM_123")
		got, err := adapter.resolveOutgoingMentions(t.Context(), "Hey <@U_DOM_123> and @dominik", "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, "Hey <@U_DOM_123> and <@U_DOM_123>", got)
	})

	t.Run("resolveOutgoingMentions / disambiguates using thread participants", func(t *testing.T) {
		t.Parallel()
		adapter, st := adapterWithState(t)
		threadID := "slack:C123:1234567890.123456"
		mustAppend(t, st, "slack:user-by-name:alex", "U_ALEX_1")
		mustAppend(t, st, "slack:user-by-name:alex", "U_ALEX_2")
		mustAppend(t, st, "slack:thread-participants:"+threadID, "U_ALEX_2")
		got, err := adapter.resolveOutgoingMentions(t.Context(), "Hey @alex", threadID)
		must.NoError(t, err)
		must.Eq(t, "Hey <@U_ALEX_2>", got)
	})

	t.Run("resolveOutgoingMentions / leaves ambiguous mentions as plain text when thread participants don't help", func(t *testing.T) {
		t.Parallel()
		adapter, st := adapterWithState(t)
		threadID := "slack:C123:1234567890.123456"
		mustAppend(t, st, "slack:user-by-name:alex", "U_ALEX_1")
		mustAppend(t, st, "slack:user-by-name:alex", "U_ALEX_2")
		mustAppend(t, st, "slack:thread-participants:"+threadID, "U_ALEX_1")
		mustAppend(t, st, "slack:thread-participants:"+threadID, "U_ALEX_2")
		got, err := adapter.resolveOutgoingMentions(t.Context(), "Hey @alex", threadID)
		must.NoError(t, err)
		must.Eq(t, "Hey @alex", got)
	})

	t.Run("resolveOutgoingMentions / resolves multiple different mentions in one message", func(t *testing.T) {
		t.Parallel()
		adapter, st := adapterWithState(t)
		mustAppend(t, st, "slack:user-by-name:dominik", "U_DOM_123")
		mustAppend(t, st, "slack:user-by-name:malte", "U_MAL_456")
		got, err := adapter.resolveOutgoingMentions(t.Context(), "@dominik and @malte please review", "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, "<@U_DOM_123> and <@U_MAL_456> please review", got)
	})

	t.Run("resolveOutgoingMentions / does nothing when chat is not initialized", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{})
		got, err := adapter.resolveOutgoingMentions(t.Context(), "Hey @dominik", "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, "Hey @dominik", got)
	})

	t.Run("resolveOutgoingMentions / skips mentions inside inline code (backticks)", func(t *testing.T) {
		t.Parallel()
		adapter, st := adapterWithState(t)
		mustAppend(t, st, "slack:user-by-name:vercel", "U_VER_123")
		got, err := adapter.resolveOutgoingMentions(t.Context(), "Use `@vercel/postgres` for the database", "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, "Use `@vercel/postgres` for the database", got)
	})

	t.Run("resolveOutgoingMentions / skips mentions inside code blocks (triple backticks)", func(t *testing.T) {
		t.Parallel()
		adapter, st := adapterWithState(t)
		mustAppend(t, st, "slack:user-by-name:vercel", "U_VER_123")
		got, err := adapter.resolveOutgoingMentions(t.Context(), "Install:\n```\nnpm install @vercel/postgres\n```", "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, "Install:\n```\nnpm install @vercel/postgres\n```", got)
	})

	t.Run("resolveOutgoingMentions / resolves mentions outside code but skips those inside", func(t *testing.T) {
		t.Parallel()
		adapter, st := adapterWithState(t)
		mustAppend(t, st, "slack:user-by-name:dominik", "U_DOM_123")
		mustAppend(t, st, "slack:user-by-name:vercel", "U_VER_123")
		got, err := adapter.resolveOutgoingMentions(t.Context(), "Hey @dominik, use `@vercel/postgres` for this", "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, "Hey <@U_DOM_123>, use `@vercel/postgres` for this", got)
	})

	t.Run("resolveOutgoingMentions / handles multiple inline code spans with mentions", func(t *testing.T) {
		t.Parallel()
		adapter, st := adapterWithState(t)
		mustAppend(t, st, "slack:user-by-name:neondatabase", "U_NEON_123")
		mustAppend(t, st, "slack:user-by-name:vercel", "U_VER_123")
		got, err := adapter.resolveOutgoingMentions(t.Context(), "Use `@neondatabase/serverless` or `@vercel/postgres`", "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, "Use `@neondatabase/serverless` or `@vercel/postgres`", got)
	})

	t.Run("resolveOutgoingMentions / resolves the same name outside code while skipping it inside code", func(t *testing.T) {
		t.Parallel()
		adapter, st := adapterWithState(t)
		mustAppend(t, st, "slack:user-by-name:vercel", "U_VER_123")
		got, err := adapter.resolveOutgoingMentions(t.Context(), "Ping @vercel, but don't link `@vercel/postgres`", "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, "Ping <@U_VER_123>, but don't link `@vercel/postgres`", got)
	})

	t.Run("resolveOutgoingMentions / resolves a mention immediately following an inline code span", func(t *testing.T) {
		t.Parallel()
		adapter, st := adapterWithState(t)
		mustAppend(t, st, "slack:user-by-name:dominik", "U_DOM_123")
		got, err := adapter.resolveOutgoingMentions(t.Context(), "Run `npm i` then ping @dominik", "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, "Run `npm i` then ping <@U_DOM_123>", got)
	})

	t.Run("resolveOutgoingMentions / resolves a mention immediately preceding an inline code span", func(t *testing.T) {
		t.Parallel()
		adapter, st := adapterWithState(t)
		mustAppend(t, st, "slack:user-by-name:dominik", "U_DOM_123")
		got, err := adapter.resolveOutgoingMentions(t.Context(), "@dominik try `npm i`", "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, "<@U_DOM_123> try `npm i`", got)
	})

	t.Run("resolveOutgoingMentions / resolves mentions surrounding a multiline fenced code block", func(t *testing.T) {
		t.Parallel()
		adapter, st := adapterWithState(t)
		mustAppend(t, st, "slack:user-by-name:dominik", "U_DOM_123")
		mustAppend(t, st, "slack:user-by-name:george", "U_GEO_123")
		mustAppend(t, st, "slack:user-by-name:vercel", "U_VER_123")
		got, err := adapter.resolveOutgoingMentions(t.Context(),
			"Hey @dominik:\n```bash\nnpm install @vercel/postgres\n```\ncc @george",
			"slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, "Hey <@U_DOM_123>:\n```bash\nnpm install @vercel/postgres\n```\ncc <@U_GEO_123>", got)
	})

	t.Run("resolveOutgoingMentions / does not skip a mention after an unbalanced single backtick", func(t *testing.T) {
		t.Parallel()
		adapter, st := adapterWithState(t)
		mustAppend(t, st, "slack:user-by-name:dominik", "U_DOM_123")
		got, err := adapter.resolveOutgoingMentions(t.Context(), "Cost is `5 and @dominik should know", "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, "Cost is `5 and <@U_DOM_123> should know", got)
	})

	t.Run("resolveOutgoingMentions / skips a mention inside inline code at the start of the text", func(t *testing.T) {
		t.Parallel()
		adapter, st := adapterWithState(t)
		mustAppend(t, st, "slack:user-by-name:vercel", "U_VER_123")
		got, err := adapter.resolveOutgoingMentions(t.Context(), "`@vercel/postgres` is the package", "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, "`@vercel/postgres` is the package", got)
	})

	t.Run("resolveMessageMentions / resolves mentions in string messages", func(t *testing.T) {
		t.Parallel()
		adapter, st := adapterWithState(t)
		mustAppend(t, st, "slack:user-by-name:dominik", "U_DOM_123")
		got, err := adapter.resolveMessageMentions(t.Context(), chat.PostableText("Hey @dominik"), "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, chat.PostableText("Hey <@U_DOM_123>"), got.(chat.PostableText))
	})

	t.Run("resolveMessageMentions / resolves mentions in raw messages", func(t *testing.T) {
		t.Parallel()
		adapter, st := adapterWithState(t)
		mustAppend(t, st, "slack:user-by-name:dominik", "U_DOM_123")
		got, err := adapter.resolveMessageMentions(t.Context(), chat.PostableRaw{Raw: "Hey @dominik"}, "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, chat.PostableRaw{Raw: "Hey <@U_DOM_123>"}, got.(chat.PostableRaw))
	})

	t.Run("resolveMessageMentions / resolves mentions in markdown messages", func(t *testing.T) {
		t.Parallel()
		adapter, st := adapterWithState(t)
		mustAppend(t, st, "slack:user-by-name:dominik", "U_DOM_123")
		got, err := adapter.resolveMessageMentions(t.Context(), chat.PostableMarkdown{Markdown: "Hey @dominik"}, "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, chat.PostableMarkdown{Markdown: "Hey <@U_DOM_123>"}, got.(chat.PostableMarkdown))
	})

	t.Run("resolveMessageMentions / passes through AST messages unchanged", func(t *testing.T) {
		t.Parallel()
		adapter, _ := adapterWithState(t)
		astMsg := chat.PostableAst{AST: map[string]any{"type": "root", "children": []any{}}}
		got, err := adapter.resolveMessageMentions(t.Context(), astMsg, "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, astMsg, got.(chat.PostableAst))
	})

	t.Run("thread participant tracking / tracks thread participants on incoming messages", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{
				"name": "sender", "real_name": "Sender One",
				"profile": map[string]any{"display_name": "sender", "real_name": "Sender One"},
			},
		})
		adapter := apiMock.adapter(t, Config{})
		sc, st := newStateChat(t)
		adapter.chat = sc
		threadID := "slack:C123:1234567890.123456"
		_, err := adapter.parseSlackMessage(t.Context(), SlackEvent{
			Type: "message", User: "U_SENDER_1", Text: "Hello",
			Ts: "1234567890.123456", Channel: "C123", ThreadTs: "1234567890.123456",
		}, threadID)
		must.NoError(t, err)
		must.SliceContains(t, mustList(t, st, "slack:thread-participants:"+threadID), "U_SENDER_1")
	})

	t.Run("system-authored messages / marks messages from USLACK as isSystem", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{
				"name": "slackbot", "real_name": "Slackbot",
				"profile": map[string]any{"display_name": "Slackbot", "real_name": "Slackbot"},
			},
		})
		adapter := apiMock.adapter(t, Config{})
		sc, _ := newStateChat(t)
		adapter.chat = sc
		msg, err := adapter.parseSlackMessage(t.Context(), SlackEvent{
			Type: "message", User: "USLACK", ChannelType: "im",
			Text: "<@U123> archived the channel <#C123>", Ts: "1234567890.123456", Channel: "D123",
		}, "slack:D123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, "USLACK", msg.Author.UserID)
		must.False(t, *msg.Author.IsBot)
		must.True(t, msg.Author.IsSystem)
		must.False(t, msg.Author.IsMe)
	})

	t.Run("system-authored messages / marks human-authored messages as not isSystem", func(t *testing.T) {
		t.Parallel()
		adapter, _ := adapterWithState(t)
		msg, err := adapter.parseSlackMessage(t.Context(), SlackEvent{
			Type: "message", User: "U_HUMAN_1", Username: "human",
			Text: "Hello", Ts: "1234567890.123456", Channel: "C123",
		}, "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, "U_HUMAN_1", msg.Author.UserID)
		must.False(t, *msg.Author.IsBot)
		must.False(t, msg.Author.IsSystem)
		must.False(t, msg.Author.IsMe)
	})

	t.Run("incoming author email / hydrates author email from users.info", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{
				"name": "alice", "real_name": "Alice Example",
				"profile": map[string]any{"display_name": "Alice", "real_name": "Alice Example", "email": "alice@example.com"},
			},
		})
		adapter := apiMock.adapter(t, Config{})
		sc, _ := newStateChat(t)
		adapter.chat = sc
		msg, err := adapter.parseSlackMessage(t.Context(), SlackEvent{
			Type: "message", User: "U_HUMAN_1", Text: "Hello", Ts: "1234567890.123456", Channel: "C123",
		}, "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, "alice@example.com", msg.Author.Email)
	})

	t.Run("incoming author email / leaves email undefined when the profile has none", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{
				"name": "alice", "real_name": "Alice Example",
				"profile": map[string]any{"display_name": "Alice", "real_name": "Alice Example"},
			},
		})
		adapter := apiMock.adapter(t, Config{})
		sc, _ := newStateChat(t)
		adapter.chat = sc
		msg, err := adapter.parseSlackMessage(t.Context(), SlackEvent{
			Type: "message", User: "U_HUMAN_1", Text: "Hello", Ts: "1234567890.123456", Channel: "C123",
		}, "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, "", msg.Author.Email)
	})

	t.Run("incoming author email / leaves email undefined when the user lookup is skipped", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		adapter := apiMock.adapter(t, Config{})
		sc, _ := newStateChat(t)
		adapter.chat = sc
		msg, err := adapter.parseSlackMessage(t.Context(), SlackEvent{
			Type: "message", Username: "webhook-bot", Text: "Hello", Ts: "1234567890.123456", Channel: "C123",
		}, "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, "", msg.Author.Email)
		must.Eq(t, 0, apiMock.count("users.info"))
	})

	t.Run("incoming author email / serves email from the user cache without a second users.info call", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{
				"name": "alice", "real_name": "Alice Example",
				"profile": map[string]any{
					"display_name": "Alice", "real_name": "Alice Example", "email": "alice@example.com",
				},
			},
		})
		adapter := apiMock.adapter(t, Config{})
		sc, _ := newStateChat(t)
		adapter.chat = sc
		ev := SlackEvent{Type: "message", User: "U_HUMAN_1", Text: "Hello", Ts: "1234567890.123456", Channel: "C123"}
		_, err := adapter.parseSlackMessage(t.Context(), ev, "slack:C123:1234567890.123456")
		must.NoError(t, err)
		msg, err := adapter.parseSlackMessage(t.Context(), ev, "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.Eq(t, "alice@example.com", msg.Author.Email)
		must.Eq(t, 1, apiMock.count("users.info"))
	})
}

func TestGetUser(t *testing.T) {
	t.Parallel()

	t.Run("should return user info with email and avatar", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{
				"is_bot": false, "name": "alice", "real_name": "Alice Smith",
				"tz": "America/Denver",
				"profile": map[string]any{
					"display_name": "Alice", "real_name": "Alice Smith",
					"email": "alice@example.com", "image_192": "https://example.com/alice.png",
				},
			},
		})
		adapter := apiMock.adapter(t, Config{})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		user := adapter.GetUser(t.Context(), "U123456")
		must.True(t, user != nil)
		must.Eq(t, "Alice Smith", user.FullName)
		must.Eq(t, "Alice", user.UserName)
		must.Eq(t, "alice@example.com", user.Email)
		must.Eq(t, "https://example.com/alice.png", user.AvatarURL)
		must.False(t, user.IsBot)
		must.Eq(t, "U123456", user.UserID)
		must.Eq(t, "America/Denver", user.Tz)
	})

	t.Run("should return null when API fails", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.reject("users.info")
		adapter := apiMock.adapter(t, Config{})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		must.Nil(t, adapter.GetUser(t.Context(), "U_UNKNOWN"))
	})

	t.Run("should return null when user not found", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.fail("users.info", "user_not_found")
		adapter := apiMock.adapter(t, Config{})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		must.Nil(t, adapter.GetUser(t.Context(), "U_NOTFOUND"))
	})

	t.Run("should return isBot true for bot users", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{
				"is_bot": true, "name": "mybot", "real_name": "My Bot",
				"profile": map[string]any{"display_name": "Bot", "real_name": "My Bot", "image_192": "https://example.com/bot.png"},
			},
		})
		adapter := apiMock.adapter(t, Config{})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		user := adapter.GetUser(t.Context(), "UBOT123")
		must.True(t, user != nil)
		must.True(t, user.IsBot)
	})

	t.Run("should fall through to real_name when display_name is empty", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{
				"is_bot": false, "name": "alice", "real_name": "Alice Smith",
				"profile": map[string]any{
					"display_name": "", "real_name": "Alice Smith",
					"email": "alice@example.com", "image_192": "https://example.com/alice.png",
				},
			},
		})
		adapter := apiMock.adapter(t, Config{})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		user := adapter.GetUser(t.Context(), "U_PARTIAL")
		must.True(t, user != nil)
		must.Eq(t, "Alice Smith", user.FullName)
		must.Eq(t, "Alice Smith", user.UserName)
	})

	t.Run("should call users.info with correct user ID", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{
				"is_bot": false, "name": "alice", "real_name": "Alice",
				"profile": map[string]any{"display_name": "Alice", "real_name": "Alice"},
			},
		})
		adapter := apiMock.adapter(t, Config{})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		_ = adapter.GetUser(t.Context(), "U_VERIFY")
		must.Eq(t, "U_VERIFY", apiMock.last("users.info").Form.Get("user"))
	})

	t.Run("should return cached user without hitting API", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		adapter := apiMock.adapter(t, Config{})
		sc, st := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		must.NoError(t, chat.StateSet(t.Context(), st, "slack:user:U_CACHED", userInfo{
			AvatarURL: "https://example.com/cached.png", DisplayName: "Cached User",
			Email: "cached@example.com", IsBot: false, RealName: "Cached User Full",
		}, 0))
		user := adapter.GetUser(t.Context(), "U_CACHED")
		must.True(t, user != nil)
		must.Eq(t, "Cached User Full", user.FullName)
		must.Eq(t, "Cached User", user.UserName)
		must.Eq(t, "cached@example.com", user.Email)
		must.Eq(t, 0, apiMock.count("users.info"))
	})
}

func TestWPrefixedEnterpriseUserIDs(t *testing.T) {
	t.Parallel()

	t.Run("treats bare @W… mentions as raw user IDs, not display names", func(t *testing.T) {
		t.Parallel()
		adapter, st := adapterWithState(t)
		got, err := adapter.resolveOutgoingMentions(t.Context(), "Hey @W012345AB, ping", "slack:C1:1.1")
		must.NoError(t, err)
		must.Eq(t, "Hey @W012345AB, ping", got)
		raw, err := st.GetList(t.Context(), "slack:user-by-name:w012345ab")
		must.NoError(t, err)
		must.Eq(t, 0, len(raw))
	})

	t.Run("resolves incoming <@W…> mentions like U-prefixed ones", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", map[string]any{
			"user": map[string]any{
				"name": "wanda", "real_name": "Wanda Grid",
				"profile": map[string]any{"display_name": "Wanda", "real_name": "Wanda Grid"},
			},
		})
		adapter := apiMock.adapter(t, Config{})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		msg, err := adapter.parseSlackMessage(t.Context(), SlackEvent{
			Type: "message", User: "W_SENDER_1", Username: "sender",
			Text: "hello <@W012345AB>", Ts: "1234567890.123456", Channel: "C123",
		}, "slack:C123:1234567890.123456")
		must.NoError(t, err)
		must.StrContains(t, msg.Text, "Wanda")
	})
}

func adapterWithState(t *testing.T) (*SlackAdapter, chat.StateAdapter) {
	t.Helper()
	adapter := mustNew(t, Config{Token: api.StaticToken("xoxb-test-token")})
	sc, st := newStateChat(t)
	adapter.chat = sc
	return adapter, st
}
