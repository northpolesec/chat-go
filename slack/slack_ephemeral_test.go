// Ported from packages/adapter-slack/src/index.test.ts @ 6adca36 (chat v4.40.0)
// (partial: Task 26 ephemeral ID encode/decode and response_url edit/delete).
package slack

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/shoenig/test/must"
)

func TestEphemeralMessageIDEncoding(t *testing.T) {
	t.Parallel()

	t.Run("encodes and decodes ephemeral message IDs for editMessage", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{BotUserID: "U_BOT"})
		id, err := adapter.encodeEphemeralMessageID("1234567890.123456", "https://hooks.slack.com/actions/T123/456/respond", "U_EPH_USER")
		must.NoError(t, err)
		must.True(t, strings.Contains(id, "ephemeral:"))
		got := adapter.decodeEphemeralMessageID(id)
		must.True(t, got != nil)
		must.Eq(t, "1234567890.123456", got.MessageTS)
		must.Eq(t, "https://hooks.slack.com/actions/T123/456/respond", got.ResponseURL)
		must.Eq(t, "U_EPH_USER", got.UserID)
	})

	t.Run("deleteMessage handles ephemeral messageId", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("chat.delete", nil)
		adapter := apiMock.adapter(t, Config{})
		must.NoError(t, adapter.DeleteMessage(t.Context(), "slack:C123:1234567890.000000", "1234567890.123456"))
		must.Eq(t, 1, apiMock.count("chat.delete"))
	})
}

func TestDecodeEphemeralMessageIdEdgeCases(t *testing.T) {
	t.Parallel()
	adapter := mustNew(t, Config{})

	t.Run("returns null for non-ephemeral message ID", func(t *testing.T) {
		t.Parallel()
		must.Nil(t, adapter.decodeEphemeralMessageID("1234567890.123456"))
	})

	t.Run("returns null for ephemeral ID with only 1 part after prefix", func(t *testing.T) {
		t.Parallel()
		must.Nil(t, adapter.decodeEphemeralMessageID("ephemeral:"))
	})

	t.Run("decodes a properly encoded ephemeral ID", func(t *testing.T) {
		t.Parallel()
		data, err := json.Marshal(map[string]string{
			"responseUrl": "https://hooks.slack.com/respond", "userId": "U123",
		})
		must.NoError(t, err)
		encoded := "ephemeral:1234567890.123456:" + base64.StdEncoding.EncodeToString(data)
		got := adapter.decodeEphemeralMessageID(encoded)
		must.True(t, got != nil)
		must.Eq(t, ephemeralID{
			MessageTS: "1234567890.123456", ResponseURL: "https://hooks.slack.com/respond", UserID: "U123",
		}, *got)
	})

	t.Run("rejects the legacy non-JSON responseUrl format", func(t *testing.T) {
		t.Parallel()
		encoded := "ephemeral:1234567890.123456:" + base64.StdEncoding.EncodeToString([]byte("https://hooks.slack.com/respond"))
		must.Nil(t, adapter.decodeEphemeralMessageID(encoded))
	})

	t.Run("rejects untrusted response_url", func(t *testing.T) {
		t.Parallel()
		for _, responseURL := range []string{
			"http://hooks.slack.com/respond",
			"https://hooks.slack.com.attacker.example/respond",
			"https://user@hooks.slack.com/respond",
			"https://hooks.slack.com:444/respond",
			"https://attacker.example/respond",
		} {
			data, err := json.Marshal(map[string]string{"responseUrl": responseURL, "userId": "U123"})
			must.NoError(t, err)
			encoded := "ephemeral:1234567890.123456:" + base64.StdEncoding.EncodeToString(data)
			must.Nil(t, adapter.decodeEphemeralMessageID(encoded))
		}
	})

	t.Run("returns null for invalid base64", func(t *testing.T) {
		t.Parallel()
		must.Nil(t, adapter.decodeEphemeralMessageID("ephemeral:1234:!!!invalid-base64!!!"))
	})
}

func TestEditMessageViaResponseURL(t *testing.T) {
	t.Parallel()

	t.Run("sends replace to response_url for ephemeral message", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		adapter := apiMock.adapter(t, Config{})
		data, err := json.Marshal(map[string]string{
			"responseUrl": "https://hooks.slack.com/respond", "userId": "U123",
		})
		must.NoError(t, err)
		ephID := "ephemeral:1234567890.123456:" + base64.StdEncoding.EncodeToString(data)
		_, err = adapter.EditMessage(t.Context(), "slack:C123:1234567890.000000", ephID, chat.PostableMarkdown{
			Markdown: "**Updated** [text](https://example.com)\n\n| A | B |\n|---|---|\n| 1 | 2 |",
		})
		must.NoError(t, err)
		must.Eq(t, 1, len(apiMock.hooks))
		body := apiMock.hooks[0].JSON
		must.Eq(t, true, body["replace_original"])
		text, _ := body["text"].(string)
		must.StrContains(t, text, "*Updated* <https://example.com|text>")
		must.StrContains(t, text, "```")
		_, hasMD := body["markdown_text"]
		must.False(t, hasMD)
	})

	t.Run("rejects an untrusted encoded response_url before fetching", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		adapter := apiMock.adapter(t, Config{})
		data, err := json.Marshal(map[string]string{
			"responseUrl": "https://attacker.example/respond", "userId": "U123",
		})
		must.NoError(t, err)
		ephID := "ephemeral:1234567890.123456:" + base64.StdEncoding.EncodeToString(data)
		_, err = adapter.EditMessage(t.Context(), "slack:C123:1234567890.000000", ephID, chat.PostableText("private message"))
		must.Error(t, err)
		var ve *shared.ValidationError
		must.True(t, errors.As(err, &ve))
		must.StrContains(t, err.Error(), "Invalid Slack ephemeral message ID")
		must.Eq(t, 0, len(apiMock.hooks))
	})

	t.Run("defends the response_url fetch sink against untrusted callers", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{})
		_, err := adapter.sendToResponseURL(t.Context(), "https://hooks.slack.com.attacker.example/respond", "delete", nil, "")
		must.Error(t, err)
		must.StrContains(t, err.Error(), "untrusted Slack response_url")
	})
}

func TestDeleteMessageViaResponseURL(t *testing.T) {
	t.Parallel()
	t.Run("sends delete_original to response_url for ephemeral message", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		adapter := apiMock.adapter(t, Config{})
		data, err := json.Marshal(map[string]string{
			"responseUrl": "https://hooks.slack.com/respond", "userId": "U123",
		})
		must.NoError(t, err)
		ephID := "ephemeral:1234567890.123456:" + base64.StdEncoding.EncodeToString(data)
		must.NoError(t, adapter.DeleteMessage(t.Context(), "slack:C123:1234567890.000000", ephID))
		must.Eq(t, 1, len(apiMock.hooks))
		must.Eq(t, true, apiMock.hooks[0].JSON["delete_original"])
	})
}
