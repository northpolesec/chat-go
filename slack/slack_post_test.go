// Ported from packages/adapter-slack/src/index.test.ts @ 6adca36 (chat v4.40.0)
// (partial: Task 26 describe blocks — post/edit/delete/ephemeral/reactions/
// typing/openDM/modals/error handling).
package slack

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/shoenig/test/must"
	"github.com/yuin/goldmark/ast"
)

func TestRenderFormatted(t *testing.T) {
	t.Parallel()
	adapter := mustNew(t, Config{})
	node := chat.Root([]ast.Node{chat.Paragraph([]ast.Node{chat.Strong([]ast.Node{chat.Text("bold")})})})
	got := strings.TrimSpace(adapter.RenderFormatted(node))
	must.Eq(t, "**bold**", got)
}

func TestPostMessage(t *testing.T) {
	t.Parallel()

	t.Run("posts a text message to a thread", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.ok("chat.postMessage", map[string]any{"ts": "1234567890.999999"})
		adapter := api.adapter(t, Config{BotUserID: "U_BOT"})
		result, err := adapter.PostMessage(t.Context(), "slack:C123:1234567890.000000", chat.PostableText("Hello from test"))
		must.NoError(t, err)
		must.Eq(t, "1234567890.999999", result.ID)
		must.Eq(t, "slack:C123:1234567890.000000", result.Channel)
		must.True(t, result.Raw != nil)
		call := api.last("chat.postMessage")
		must.Eq(t, "Bearer xoxb-test-token", call.Auth)
		must.Eq(t, "C123", call.Form.Get("channel"))
		must.Eq(t, "1234567890.000000", call.Form.Get("thread_ts"))
	})

	t.Run("posts to a channel with empty threadTs", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.ok("chat.postMessage", map[string]any{"ts": "1111111111.000000"})
		adapter := api.adapter(t, Config{BotUserID: "U_BOT"})
		result, err := adapter.PostMessage(t.Context(), "slack:C123:", chat.PostableText("Channel message"))
		must.NoError(t, err)
		must.Eq(t, "1111111111.000000", result.ID)
		call := api.last("chat.postMessage")
		must.Eq(t, "C123", call.Form.Get("channel"))
		must.Eq(t, "", call.Form.Get("thread_ts"))
	})

	t.Run("sets unfurl_links and unfurl_media to false", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.ok("chat.postMessage", map[string]any{"ts": "1234567890.999999"})
		adapter := api.adapter(t, Config{BotUserID: "U_BOT"})
		_, err := adapter.PostMessage(t.Context(), "slack:C123:1234567890.000000", chat.PostableText("test"))
		must.NoError(t, err)
		call := api.last("chat.postMessage")
		must.Eq(t, "false", call.Form.Get("unfurl_links"))
		must.Eq(t, "false", call.Form.Get("unfurl_media"))
	})

	t.Run("returns early for file-only post with empty markdown", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		fileIDs := []string{"F123"}
		n := 0
		api.on("files.getUploadURLExternal", func(w http.ResponseWriter, _ *http.Request) {
			id := fileIDs[n]
			if n < len(fileIDs)-1 {
				n++
			}
			writeJSON(w, 200, map[string]any{"ok": true, "upload_url": api.server.URL + "/file-upload", "file_id": id})
		})
		adapter := api.adapter(t, Config{})
		result, err := adapter.PostMessage(t.Context(), "slack:C123:1234567890.000000", chat.PostableMarkdown{
			Markdown: "",
			Files:    []chat.FileUpload{{Data: []byte("hello"), Filename: "test.txt"}},
		})
		must.NoError(t, err)
		must.True(t, strings.HasPrefix(result.ID, "file-"))
		raw, ok := result.Raw.(map[string]any)
		must.True(t, ok)
		must.Eq(t, []string{"F123"}, asStringSlice(raw["uploadedFileIds"]))
		must.Eq(t, 0, api.count("chat.postMessage"))
	})

	t.Run("adds uploaded file ids to text posts with files", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		ids := []string{"F123", "F456"}
		n := 0
		api.on("files.getUploadURLExternal", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, 200, map[string]any{
				"ok": true, "upload_url": api.server.URL + "/file-upload", "file_id": ids[n],
			})
			n++
		})
		api.ok("chat.postMessage", map[string]any{"ts": "1234567890.999999"})
		adapter := api.adapter(t, Config{})
		result, err := adapter.PostMessage(t.Context(), "slack:C123:1234567890.000000", chat.PostableMarkdown{
			Markdown: "uploaded files",
			Files: []chat.FileUpload{
				{Data: []byte("one"), Filename: "one.txt"},
				{Data: []byte("two"), Filename: "two.txt"},
			},
		})
		must.NoError(t, err)
		must.Eq(t, "1234567890.999999", result.ID)
		raw, ok := result.Raw.(map[string]any)
		must.True(t, ok)
		must.Eq(t, []string{"F123", "F456"}, asStringSlice(raw["uploadedFileIds"]))
	})
}

func TestPostEphemeral(t *testing.T) {
	t.Parallel()

	t.Run("posts an ephemeral message to a user", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.ok("chat.postEphemeral", map[string]any{"message_ts": "1234567890.888888"})
		adapter := api.adapter(t, Config{})
		result, err := adapter.PostEphemeral(t.Context(), "slack:C123:1234567890.000000", "U_USER_1", chat.PostableText("Ephemeral text"))
		must.NoError(t, err)
		must.Eq(t, "1234567890.888888", result.ID)
		must.Eq(t, "slack:C123:1234567890.000000", result.Channel)
		call := api.last("chat.postEphemeral")
		must.Eq(t, "Bearer xoxb-test-token", call.Auth)
		must.Eq(t, "C123", call.Form.Get("channel"))
		must.Eq(t, "1234567890.000000", call.Form.Get("thread_ts"))
		must.Eq(t, "U_USER_1", call.Form.Get("user"))
	})

	t.Run("normalizes empty threadTs to undefined", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.ok("chat.postEphemeral", map[string]any{"message_ts": "1234567890.888888"})
		adapter := api.adapter(t, Config{})
		_, err := adapter.PostEphemeral(t.Context(), "slack:C123:", "U_USER_1", chat.PostableText("Ephemeral text"))
		must.NoError(t, err)
		call := api.last("chat.postEphemeral")
		must.Eq(t, "C123", call.Form.Get("channel"))
		must.Eq(t, "", call.Form.Get("thread_ts"))
		must.Eq(t, "U_USER_1", call.Form.Get("user"))
	})

	t.Run("handles empty message_ts in response", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.ok("chat.postEphemeral", map[string]any{})
		adapter := api.adapter(t, Config{})
		result, err := adapter.PostEphemeral(t.Context(), "slack:C123:1234567890.000000", "U_USER_1", chat.PostableText("test"))
		must.NoError(t, err)
		must.Eq(t, "", result.ID)
	})
}

func TestEditMessage(t *testing.T) {
	t.Parallel()
	t.Run("edits a regular text message", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.ok("chat.update", map[string]any{"ts": "1234567890.123456"})
		adapter := api.adapter(t, Config{})
		result, err := adapter.EditMessage(t.Context(), "slack:C123:1234567890.000000", "1234567890.123456", chat.PostableText("Updated message"))
		must.NoError(t, err)
		must.Eq(t, "1234567890.123456", result.ID)
		must.Eq(t, "slack:C123:1234567890.000000", result.Channel)
		call := api.last("chat.update")
		must.Eq(t, "Bearer xoxb-test-token", call.Auth)
		must.Eq(t, "C123", call.Form.Get("channel"))
		must.Eq(t, "1234567890.123456", call.Form.Get("ts"))
	})
}

func TestDeleteMessage(t *testing.T) {
	t.Parallel()
	t.Run("deletes a message by ID", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.ok("chat.delete", nil)
		adapter := api.adapter(t, Config{})
		must.NoError(t, adapter.DeleteMessage(t.Context(), "slack:C123:1234567890.000000", "1234567890.123456"))
		call := api.last("chat.delete")
		must.Eq(t, "Bearer xoxb-test-token", call.Auth)
		must.Eq(t, "C123", call.Form.Get("channel"))
		must.Eq(t, "1234567890.123456", call.Form.Get("ts"))
	})
}

func TestAddReaction(t *testing.T) {
	t.Parallel()

	t.Run("adds a reaction to a message", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.ok("reactions.add", nil)
		adapter := api.adapter(t, Config{})
		must.NoError(t, adapter.AddReaction(t.Context(), "slack:C123:1234567890.000000", "1234567890.123456", chat.GetEmoji("thumbsup")))
		call := api.last("reactions.add")
		must.Eq(t, "Bearer xoxb-test-token", call.Auth)
		must.Eq(t, "C123", call.Form.Get("channel"))
		must.Eq(t, "1234567890.123456", call.Form.Get("timestamp"))
		must.True(t, call.Form.Get("name") != "")
	})

	t.Run("strips colons from emoji names", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.ok("reactions.add", nil)
		adapter := api.adapter(t, Config{})
		must.NoError(t, adapter.AddReaction(t.Context(), "slack:C123:1234567890.000000", "1234567890.123456", chat.GetEmoji(":thumbsup:")))
		call := api.last("reactions.add")
		must.False(t, strings.Contains(call.Form.Get("name"), ":"))
	})
}

func TestRemoveReaction(t *testing.T) {
	t.Parallel()
	t.Run("removes a reaction from a message", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.ok("reactions.remove", nil)
		adapter := api.adapter(t, Config{})
		must.NoError(t, adapter.RemoveReaction(t.Context(), "slack:C123:1234567890.000000", "1234567890.123456", chat.GetEmoji("thumbsup")))
		call := api.last("reactions.remove")
		must.Eq(t, "Bearer xoxb-test-token", call.Auth)
		must.Eq(t, "C123", call.Form.Get("channel"))
		must.Eq(t, "1234567890.123456", call.Form.Get("timestamp"))
		must.True(t, call.Form.Get("name") != "")
	})
}

func TestOpenModal(t *testing.T) {
	t.Parallel()

	t.Run("opens a modal with trigger ID", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.ok("views.open", map[string]any{"view": map[string]any{"id": "V_MODAL_1"}})
		adapter := api.adapter(t, Config{})
		modal := chat.NewModal("test_modal", "Test Modal")
		viewID, err := adapter.OpenModal(t.Context(), "trigger-123", &modal)
		must.NoError(t, err)
		must.Eq(t, "V_MODAL_1", viewID)
		call := api.last("views.open")
		must.Eq(t, "Bearer xoxb-test-token", call.Auth)
		must.Eq(t, "trigger-123", call.Form.Get("trigger_id"))
		var view map[string]any
		must.NoError(t, json.Unmarshal([]byte(call.Form.Get("view")), &view))
		must.Eq(t, "modal", view["type"])
		must.Eq(t, "test_modal", view["callback_id"])
	})

	t.Run("passes contextId as encoded private_metadata", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.ok("views.open", map[string]any{"view": map[string]any{"id": "V_CTX_1"}})
		adapter := api.adapter(t, Config{})
		modal := chat.NewModal("modal_with_ctx", "Context Modal")
		_, err := adapter.openModal(t.Context(), "trigger-ctx", &modal, "context-id-42")
		must.NoError(t, err)
		var view map[string]any
		must.NoError(t, json.Unmarshal([]byte(api.last("views.open").Form.Get("view")), &view))
		var parsed map[string]any
		must.NoError(t, json.Unmarshal([]byte(view["private_metadata"].(string)), &parsed))
		must.Eq(t, "context-id-42", parsed["c"])
	})

	t.Run("encodes modal privateMetadata with contextId", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.ok("views.open", map[string]any{"view": map[string]any{"id": "V_PM_1"}})
		adapter := api.adapter(t, Config{})
		modal := chat.NewModal("pm_modal", "PM Modal")
		modal.PrivateMetadata = "user-data-xyz"
		_, err := adapter.openModal(t.Context(), "trigger-pm", &modal, "ctx-99")
		must.NoError(t, err)
		var view map[string]any
		must.NoError(t, json.Unmarshal([]byte(api.last("views.open").Form.Get("view")), &view))
		var parsed map[string]any
		must.NoError(t, json.Unmarshal([]byte(view["private_metadata"].(string)), &parsed))
		must.Eq(t, "ctx-99", parsed["c"])
		must.Eq(t, "user-data-xyz", parsed["m"])
	})
}

func TestUpdateModal(t *testing.T) {
	t.Parallel()
	t.Run("updates an existing modal view", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.ok("views.update", map[string]any{"view": map[string]any{"id": "V_UPDATED_1"}})
		adapter := api.adapter(t, Config{})
		modal := chat.NewModal("updated_modal", "Updated")
		viewID, err := adapter.updateModal(t.Context(), "V_ORIGINAL_1", &modal)
		must.NoError(t, err)
		must.Eq(t, "V_UPDATED_1", viewID)
		call := api.last("views.update")
		must.Eq(t, "Bearer xoxb-test-token", call.Auth)
		must.Eq(t, "V_ORIGINAL_1", call.Form.Get("view_id"))
		var view map[string]any
		must.NoError(t, json.Unmarshal([]byte(call.Form.Get("view")), &view))
		must.Eq(t, "modal", view["type"])
		must.Eq(t, "updated_modal", view["callback_id"])
	})
}

func TestStartTyping(t *testing.T) {
	t.Parallel()

	t.Run("uses native processing without custom text in agent view", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.ok("agents.sessions.setStatus", nil)
		adapter := api.adapter(t, Config{AgentView: true, LoadingMessages: []string{"Configured..."}})
		must.NoError(t, adapter.StartTyping(t.Context(), "slack:D123:1.2", "", chat.TypingOptions{InitiatorUserID: "U123"}))
		must.Eq(t, 0, api.count("assistant.threads.setStatus"))
		call := api.last("agents.sessions.setStatus")
		must.Eq(t, "Bearer xoxb-test-token", call.Auth)
		must.Eq(t, "D123", call.Form.Get("channel_id"))
		must.Eq(t, "1.2", call.Form.Get("thread_ts"))
		must.Eq(t, "processing", call.Form.Get("status"))
		must.Eq(t, "U123", call.Form.Get("initiator_user_id"))
	})

	t.Run("does not fall back to a legacy label when native status fails", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.fail("agents.sessions.setStatus", "internal_error")
		adapter := api.adapter(t, Config{AgentView: true})
		must.NoError(t, adapter.StartTyping(t.Context(), "slack:D123:1.2", "", chat.TypingOptions{}))
		must.Eq(t, 0, api.count("assistant.threads.setStatus"))
	})

	t.Run("calls assistant.threads.setStatus with default status", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.ok("assistant.threads.setStatus", nil)
		adapter := api.adapter(t, Config{})
		must.NoError(t, adapter.StartTyping(t.Context(), "slack:C123:1234567890.000000", "", chat.TypingOptions{}))
		call := api.last("assistant.threads.setStatus")
		must.Eq(t, "Bearer xoxb-test-token", call.Auth)
		must.Eq(t, "C123", call.Form.Get("channel_id"))
		must.Eq(t, "1234567890.000000", call.Form.Get("thread_ts"))
		must.Eq(t, "Typing...", call.Form.Get("status"))
		must.True(t, strings.Contains(call.Form.Get("loading_messages"), "Typing..."))
	})

	t.Run("uses custom status when provided", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.ok("assistant.threads.setStatus", nil)
		adapter := api.adapter(t, Config{})
		must.NoError(t, adapter.StartTyping(t.Context(), "slack:C123:1234567890.000000", "Searching documents...", chat.TypingOptions{}))
		call := api.last("assistant.threads.setStatus")
		must.Eq(t, "Searching documents...", call.Form.Get("status"))
		must.True(t, strings.Contains(call.Form.Get("loading_messages"), "Searching documents..."))
	})

	t.Run("skips when no threadTs present", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		adapter := api.adapter(t, Config{})
		must.NoError(t, adapter.StartTyping(t.Context(), "slack:C123:", "", chat.TypingOptions{}))
		must.Eq(t, 0, api.count("assistant.threads.setStatus"))
	})

	t.Run("does not throw on API error (logs warning instead)", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.fail("assistant.threads.setStatus", "internal_error")
		adapter := api.adapter(t, Config{})
		must.NoError(t, adapter.StartTyping(t.Context(), "slack:C123:1234567890.000000", "", chat.TypingOptions{}))
	})
}

func TestOpenDM(t *testing.T) {
	t.Parallel()

	t.Run("opens a DM conversation and returns thread ID", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.ok("conversations.open", map[string]any{"channel": map[string]any{"id": "D_DM_CHANNEL"}})
		adapter := api.adapter(t, Config{})
		threadID, err := adapter.OpenDM(t.Context(), "U_TARGET_USER")
		must.NoError(t, err)
		must.Eq(t, "slack:D_DM_CHANNEL:", threadID)
		call := api.last("conversations.open")
		must.Eq(t, "Bearer xoxb-test-token", call.Auth)
		must.Eq(t, "U_TARGET_USER", call.Form.Get("users"))
	})

	t.Run("throws when no channel returned", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.ok("conversations.open", map[string]any{"channel": map[string]any{}})
		adapter := api.adapter(t, Config{})
		_, err := adapter.OpenDM(t.Context(), "U_BAD_USER")
		must.Error(t, err)
		must.StrContains(t, err.Error(), "Failed to open DM")
	})
}

func TestErrorHandling(t *testing.T) {
	t.Parallel()

	t.Run("throws AdapterRateLimitError on rate limit", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.fail("chat.postMessage", "ratelimited")
		adapter := api.adapter(t, Config{})
		_, err := adapter.PostMessage(t.Context(), "slack:C123:1234567890.000000", chat.PostableText("test"))
		var rl *shared.AdapterRateLimitError
		must.True(t, errors.As(err, &rl))
	})

	t.Run("re-throws non-rate-limit errors", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.fail("chat.postMessage", "channel_not_found")
		adapter := api.adapter(t, Config{})
		_, err := adapter.PostMessage(t.Context(), "slack:C123:1234567890.000000", chat.PostableText("test"))
		must.Error(t, err)
		must.StrContains(t, err.Error(), "channel_not_found")
	})

	t.Run("rate limit error in addReaction", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.fail("reactions.add", "ratelimited")
		adapter := api.adapter(t, Config{})
		err := adapter.AddReaction(t.Context(), "slack:C123:1234567890.000000", "1234.000", chat.GetEmoji("thumbsup"))
		var rl *shared.AdapterRateLimitError
		must.True(t, errors.As(err, &rl))
	})

	t.Run("rate limit error in deleteMessage", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.fail("chat.delete", "ratelimited")
		adapter := api.adapter(t, Config{})
		err := adapter.DeleteMessage(t.Context(), "slack:C123:1234567890.000000", "1234.000")
		var rl *shared.AdapterRateLimitError
		must.True(t, errors.As(err, &rl))
	})

	t.Run("rate limit error in editMessage", func(t *testing.T) {
		t.Parallel()
		api := newSlackAPIMock(t)
		api.fail("chat.update", "ratelimited")
		adapter := api.adapter(t, Config{})
		_, err := adapter.EditMessage(t.Context(), "slack:C123:1234567890.000000", "1234.000", chat.PostableText("update"))
		var rl *shared.AdapterRateLimitError
		must.True(t, errors.As(err, &rl))
	})
}

func asStringSlice(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			s, _ := item.(string)
			out = append(out, s)
		}
		return out
	default:
		return nil
	}
}
