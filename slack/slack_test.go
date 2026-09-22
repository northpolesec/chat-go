// Ported from packages/adapter-slack/src/index.test.ts @ 6adca36 (chat v4.40.0)
// (partial: Task 25 describe blocks — construction, thread IDs, parseMessage).
package slack

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/northpolesec/chat-go/slack/api"
	"github.com/shoenig/test/must"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
)

func testCfg(extra Config) Config {
	if extra.SigningSecret == "" && extra.WebhookVerifier == nil {
		extra.SigningSecret = "test-secret"
	}
	if extra.Token == nil && extra.ClientID == "" {
		extra.Token = api.StaticToken("xoxb-test-token")
	}
	return extra
}

func mustNew(t *testing.T, cfg Config) *SlackAdapter {
	t.Helper()
	a, err := New(testCfg(cfg))
	must.NoError(t, err)
	return a
}

func TestCreateSlackAdapter(t *testing.T) {
	t.Parallel()

	t.Run("creates a SlackAdapter instance", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{})
		must.Eq(t, "slack", adapter.Name())
	})

	t.Run("sets default userName to 'bot'", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{})
		must.Eq(t, "bot", adapter.UserName())
	})

	t.Run("uses provided userName", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{UserName: "custombot"})
		must.Eq(t, "custombot", adapter.UserName())
	})

	t.Run("stores botUserId when provided", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{BotUserID: "U12345"})
		must.Eq(t, "U12345", adapter.BotUserID())
	})

	t.Run("rejects suggestedPrompts and suggestedPromptsResolver together", func(t *testing.T) {
		t.Parallel()
		_, err := New(testCfg(Config{
			SuggestedPrompts:         &chat.SuggestedPrompts{Prompts: []chat.SuggestedPrompt{{Title: "t", Message: "m"}}},
			SuggestedPromptsResolver: func(context.Context, SuggestedPromptsContext) (*chat.SuggestedPrompts, error) { return nil, nil },
		}))
		must.Error(t, err)
		var ve *shared.ValidationError
		must.True(t, errors.As(err, &ve))
		must.StrContains(t, err.Error(), suggestedPromptsBothSetMessage)
	})

	t.Run("rejects sessionTitle and sessionTitleResolver together", func(t *testing.T) {
		t.Parallel()
		_, err := New(testCfg(Config{
			SessionTitle:         boolPtr(true),
			SessionTitleResolver: func(context.Context, SessionTitleContext) (string, error) { return "", nil },
		}))
		must.Error(t, err)
		var ve *shared.ValidationError
		must.True(t, errors.As(err, &ve))
		must.StrContains(t, err.Error(), sessionTitleBothSetMessage)
	})
}

func TestConstructorEnvVarResolution(t *testing.T) {
	t.Run("should throw when signingSecret is missing and env var not set", func(t *testing.T) {
		t.Setenv(envSigningSecret, "")
		t.Setenv(envBotToken, "")
		_, err := New(Config{})
		must.Error(t, err)
		var ve *shared.ValidationError
		must.True(t, errors.As(err, &ve))
		must.StrContains(t, err.Error(), "signingSecret or webhookVerifier is required")
	})

	t.Run("should not throw when webhookVerifier is provided without signingSecret", func(t *testing.T) {
		t.Setenv("SLACK_UNUSED", "")
		adapter, err := New(Config{
			WebhookVerifier: func(http.Header, []byte) error { return nil },
		})
		must.NoError(t, err)
		must.Eq(t, "slack", adapter.Name())
	})

	t.Run("should resolve signingSecret from SLACK_SIGNING_SECRET env var", func(t *testing.T) {
		t.Setenv(envSigningSecret, "env-signing-secret")
		adapter, err := New(Config{})
		must.NoError(t, err)
		must.Eq(t, "slack", adapter.Name())
	})

	t.Run("should resolve botToken from SLACK_BOT_TOKEN in zero-config mode", func(t *testing.T) {
		t.Setenv(envSigningSecret, "env-signing-secret")
		t.Setenv(envBotToken, "xoxb-env-token")
		adapter, err := New(Config{})
		must.NoError(t, err)
		must.Eq(t, "slack", adapter.Name())
	})

	t.Run("keeps SLACK_BOT_TOKEN env fallback when only non-auth config is passed", func(t *testing.T) {
		t.Setenv(envSigningSecret, "env-signing-secret")
		t.Setenv(envBotToken, "xoxb-env-token")
		captured := &authCapture{ok: true}
		adapter, err := New(Config{HTTPClient: captured, AgentView: true})
		must.NoError(t, err)
		err = adapter.setSuggestedPrompts(t.Context(), "C1", "", []chat.SuggestedPrompt{{Title: "t", Message: "m"}}, "")
		must.NoError(t, err)
		must.Eq(t, "Bearer xoxb-env-token", captured.auth)
	})

	t.Run("disables SLACK_BOT_TOKEN env fallback when another auth field is passed", func(t *testing.T) {
		t.Setenv(envBotToken, "xoxb-env-token")
		adapter, err := New(Config{
			SigningSecret: "config-secret",
			ClientID:      "client-id",
			ClientSecret:  "client-secret",
		})
		must.NoError(t, err)
		err = adapter.setSuggestedPrompts(t.Context(), "C1", "", nil, "")
		must.Error(t, err)
	})

	t.Run("disables SLACK_BOT_TOKEN env fallback for a signingSecret-only config (multi-workspace)", func(t *testing.T) {
		t.Setenv(envBotToken, "xoxb-env-token")
		adapter, err := New(Config{SigningSecret: "config-secret"})
		must.NoError(t, err)
		err = adapter.setSuggestedPrompts(t.Context(), "C1", "", nil, "")
		must.Error(t, err)
	})

	t.Run("should default logger when not provided", func(t *testing.T) {
		t.Setenv(envSigningSecret, "env-signing-secret")
		adapter, err := New(Config{})
		must.NoError(t, err)
		must.Eq(t, "slack", adapter.Name())
	})

	t.Run("should prefer config values over env vars", func(t *testing.T) {
		t.Setenv(envSigningSecret, "env-secret")
		adapter, err := New(Config{SigningSecret: "config-secret"})
		must.NoError(t, err)
		must.Eq(t, "slack", adapter.Name())
	})

	t.Run("should resolve apiUrl from SLACK_API_URL env var", func(t *testing.T) {
		t.Setenv(envSigningSecret, "env-signing-secret")
		t.Setenv(envAPIURL, "https://slack-gov.com/api/")
		adapter, err := New(Config{})
		must.NoError(t, err)
		must.Eq(t, "https://slack-gov.com/api/", adapter.apiURL)
	})

	t.Run("should accept apiUrl config value", func(t *testing.T) {
		t.Setenv("SLACK_UNUSED", "")
		adapter := mustNew(t, Config{APIURL: "https://slack-gov.com/api/"})
		must.Eq(t, "https://slack-gov.com/api/", adapter.apiURL)
	})

	t.Run("resolves encryptionKey from env when signingSecret is set", func(t *testing.T) {
		t.Setenv(envEncryptionKey, "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
		adapter, err := New(Config{SigningSecret: "config-secret"})
		must.NoError(t, err)
		want := make([]byte, 32)
		for i := range want {
			want[i] = byte(i)
		}
		must.Eq(t, want, adapter.encryptionKey)
	})
}

// Thread-id encode/decode/isDM cases live in chattest.RunThreadIDContract
// (slack/contracts_test.go). Slack-local edge cases stay below.

func TestDecodeThreadIdEdgeCases(t *testing.T) {
	t.Parallel()
	adapter := mustNew(t, Config{})

	t.Run("decodes channel-only ID (no threadTs)", func(t *testing.T) {
		t.Parallel()
		got, err := adapter.decodeThreadID("slack:C12345")
		must.NoError(t, err)
		must.Eq(t, ThreadID{Channel: "C12345", ThreadTS: ""}, got)
	})

	t.Run("throws on invalid thread ID format", func(t *testing.T) {
		t.Parallel()
		for _, id := range []string{"invalid", "slack", "teams:C12345:123", "slack:A:B:C:D"} {
			_, err := adapter.decodeThreadID(id)
			must.Error(t, err)
			var ve *shared.ValidationError
			must.True(t, errors.As(err, &ve))
		}
	})
}

func TestIsDMEdgeCases(t *testing.T) {
	t.Parallel()
	adapter := mustNew(t, Config{})

	t.Run("returns false for private channels (G prefix)", func(t *testing.T) {
		t.Parallel()
		must.False(t, adapter.IsDM("slack:G12345:1234567890.123456"))
	})
}

func TestChannelIdFromThreadId(t *testing.T) {
	t.Parallel()
	adapter := mustNew(t, Config{})

	t.Run("extracts channel ID from thread ID", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "slack:C123", adapter.ChannelIDFromThreadID("slack:C123:1234567890.000000"))
	})

	t.Run("works with empty threadTs", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "slack:C456", adapter.ChannelIDFromThreadID("slack:C456:"))
	})
}

func TestParseMessage(t *testing.T) {
	t.Parallel()
	adapter := mustNew(t, Config{BotUserID: "U_BOT"})

	t.Run("parses a basic message event", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "Hello world", Ts: "1234567890.123456",
		})
		must.NoError(t, err)
		must.Eq(t, "1234567890.123456", msg.ID)
		must.Eq(t, "Hello world", msg.Text)
		must.Eq(t, "U123", msg.Author.UserID)
		must.False(t, *msg.Author.IsBot)
		must.False(t, msg.Author.IsMe)
	})

	t.Run("parses a bot message", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", BotID: "B123", Channel: "C456",
			Text: "Bot message", Ts: "1234567890.123456", Subtype: "bot_message",
		})
		must.NoError(t, err)
		must.Eq(t, "B123", msg.Author.UserID)
		must.True(t, *msg.Author.IsBot)
	})

	t.Run("uses the bot user ID instead of the app bot ID", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", BotID: "B123",
			BotProfile: &SlackBotProfile{UserID: "U123"},
			Channel:    "C456", Text: "Bot message",
			Ts: "1234567890.123456", Subtype: "bot_message",
		})
		must.NoError(t, err)
		must.Eq(t, "U123", msg.Author.UserID)
		must.True(t, *msg.Author.IsBot)
	})

	t.Run("marks USLACK messages as system-authored", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "USLACK", Channel: "D456", ChannelType: "im",
			Text: "<@U123> archived the channel <#C123>", Ts: "1234567890.123456",
		})
		must.NoError(t, err)
		must.Eq(t, "USLACK", msg.Author.UserID)
		must.False(t, *msg.Author.IsBot)
		must.True(t, msg.Author.IsSystem)
		must.False(t, msg.Author.IsMe)
	})

	t.Run("detects messages from self", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U_BOT", Channel: "C456",
			Text: "Self message", Ts: "1234567890.123456",
		})
		must.NoError(t, err)
		must.True(t, msg.Author.IsMe)
	})

	t.Run("parses message with thread_ts", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "Thread reply", Ts: "1234567891.123456", ThreadTs: "1234567890.123456",
		})
		must.NoError(t, err)
		must.Eq(t, "slack:C456:1234567890.123456", msg.ThreadID)
	})

	t.Run("parses edited message", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "Edited message", Ts: "1234567890.123456",
			Edited: &SlackEdited{Ts: "1234567891.000000"},
		})
		must.NoError(t, err)
		must.True(t, msg.Metadata.Edited)
		must.True(t, msg.Metadata.EditedAt != nil)
	})

	t.Run("parses message with files", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "Message with file", Ts: "1234567890.123456",
			Files: []SlackFile{{
				ID: "F123", Mimetype: "image/png",
				URLPrivate: "https://files.slack.com/file.png",
				Name:       "image.png", Size: 12345, OriginalW: 800, OriginalH: 600,
			}},
		})
		must.NoError(t, err)
		must.Eq(t, 1, len(msg.Attachments))
		must.Eq(t, chat.AttachmentImage, msg.Attachments[0].Type)
		must.Eq(t, "image.png", msg.Attachments[0].Name)
		must.Eq(t, "image/png", msg.Attachments[0].MIMEType)
		must.Eq(t, 800, msg.Attachments[0].Width)
		must.Eq(t, 600, msg.Attachments[0].Height)
	})

	t.Run("downloads external message files without resolving the bot token", func(t *testing.T) {
		t.Parallel()
		tok := &countingToken{tok: "xoxb-test"}
		adapter := mustNew(t, Config{Token: tok, BotUserID: "U_BOT"})
		var gotURL string
		var gotAuth bool
		adapter.fileTransport = func(_ context.Context, u *url.URL, headers map[string]string) (*http.Response, error) {
			gotURL = u.String()
			_, gotAuth = headers["authorization"]
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{"application/octet-stream"}},
				Body:       io.NopCloser(strings.NewReader("file-bytes")),
			}, nil
		}
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "External file", Ts: "1234567890.123456",
			Files: []SlackFile{{
				ID: "F123", Mimetype: "application/vnd.slack-remote",
				URLPrivate: "https://docs.google.com/document/d/external",
			}},
		})
		must.NoError(t, err)
		must.True(t, msg.Attachments[0].FetchData != nil)
		_, err = msg.Attachments[0].FetchData()
		must.NoError(t, err)
		must.Eq(t, 0, tok.n)
		must.Eq(t, "https://docs.google.com/document/d/external", gotURL)
		must.False(t, gotAuth)
	})

	t.Run("handles different file types", func(t *testing.T) {
		t.Parallel()
		createEvent := func(mimetype string) SlackEvent {
			return SlackEvent{
				Type: "message", User: "U123", Channel: "C456",
				Ts:    "1234567890.123456",
				Files: []SlackFile{{ID: "F123", Mimetype: mimetype, URLPrivate: "https://example.com"}},
			}
		}
		imageMsg, err := adapter.ParseMessage(createEvent("image/jpeg"))
		must.NoError(t, err)
		must.Eq(t, chat.AttachmentImage, imageMsg.Attachments[0].Type)
		videoMsg, err := adapter.ParseMessage(createEvent("video/mp4"))
		must.NoError(t, err)
		must.Eq(t, chat.AttachmentVideo, videoMsg.Attachments[0].Type)
		audioMsg, err := adapter.ParseMessage(createEvent("audio/mpeg"))
		must.NoError(t, err)
		must.Eq(t, chat.AttachmentAudio, audioMsg.Attachments[0].Type)
		fileMsg, err := adapter.ParseMessage(createEvent("application/pdf"))
		must.NoError(t, err)
		must.Eq(t, chat.AttachmentFile, fileMsg.Attachments[0].Type)
	})

	t.Run("preserves pasted table attachments as message content", func(t *testing.T) {
		t.Parallel()
		event := SlackEvent{
			Type: "message", User: "U123", Username: "alice", Channel: "C456",
			Text: "Which devices support remote firmware upgrades?",
			Ts:   "1786120899.208429",
			Attachments: []SlackAttachment{{
				Fallback: "[no preview available]",
				Blocks: []map[string]any{
					{"type": "table", "rows": []any{
						[]any{
							map[string]any{"type": "rich_text", "elements": []any{
								map[string]any{"type": "rich_text_section", "elements": []any{
									map[string]any{"type": "text", "text": "Manufacturer", "style": map[string]any{"bold": true}},
								}},
							}},
							map[string]any{"type": "raw_text", "text": "Identifier Listed"},
							map[string]any{"type": "raw_text", "text": "Units"},
						},
						[]any{
							map[string]any{"type": "raw_text", "text": "Samsung"},
							map[string]any{"type": "raw_text", "text": "QB55C"},
							map[string]any{"type": "raw_number", "value": 3},
						},
					}},
				},
			}},
		}
		expected := "Which devices support remote firmware upgrades?\n\n" +
			"Manufacturer\tIdentifier Listed\tUnits\nSamsung\tQB55C\t3"
		syncMsg, err := adapter.ParseMessage(event)
		must.NoError(t, err)
		asyncMsg, err := adapter.parseSlackMessage(t.Context(), event, "slack:C456:1786120899.208429")
		must.NoError(t, err)
		for _, message := range []*chat.Message{syncMsg, asyncMsg} {
			must.Eq(t, expected, message.Text)
			must.Eq(t, 0, len(message.Attachments))
			table := nthChild(t, formattedNode(message), 1)
			must.Eq(t, "table", nodeType(table))
			must.Eq(t, "Manufacturer", cellText(t, table, 0, 0))
			must.Eq(t, "Identifier Listed", cellText(t, table, 0, 1))
			must.Eq(t, "Units", cellText(t, table, 0, 2))
			must.Eq(t, "Samsung", cellText(t, table, 1, 0))
			must.Eq(t, "QB55C", cellText(t, table, 1, 1))
			must.Eq(t, "3", cellText(t, table, 1, 2))
		}
	})

	t.Run("preserves table-only messages and ignores malformed table blocks", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Ts: "1786120899.208429",
			Blocks: []map[string]any{
				{"type": "table", "rows": []any{[]any{map[string]any{"type": "raw_text", "text": "Visible"}}}},
				{"type": "table", "rows": "invalid"},
			},
			Attachments: []SlackAttachment{{Blocks: []map[string]any{{"type": "table"}}}},
		})
		must.NoError(t, err)
		must.Eq(t, "Visible", msg.Text)
		must.Eq(t, 1, childCount(formattedNode(msg)))
		must.Eq(t, "table", nodeType(formattedNode(msg).FirstChild()))
	})

	t.Run("preserves inline rich text within table cells", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Ts: "1786120899.208429",
			Blocks: []map[string]any{{
				"type": "table",
				"rows": []any{[]any{map[string]any{
					"type": "rich_text",
					"elements": []any{map[string]any{
						"type": "rich_text_quote",
						"elements": []any{
							map[string]any{"type": "text", "text": "See "},
							map[string]any{"type": "link", "text": "details", "url": "https://example.com"},
						},
					}},
				}}},
			}},
		})
		must.NoError(t, err)
		must.Eq(t, "See details", msg.Text)
		table := formattedNode(msg).FirstChild()
		must.Eq(t, "table", nodeType(table))
		// Headerless: empty header row + data row
		must.Eq(t, 0, childCount(nthChild(t, nthChild(t, table, 0), 0)))
		dataCell := nthChild(t, nthChild(t, table, 1), 0)
		must.Eq(t, "See ", nodeText(dataCell.FirstChild()))
		link := dataCell.FirstChild().NextSibling()
		must.True(t, chat.IsLinkNode(link))
		must.Eq(t, "https://example.com", string(link.(*ast.Link).Destination))
		must.Eq(t, "details", nodeText(link.FirstChild()))
	})

	t.Run("preserves rich text metadata within table cells", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Ts: "1786120899.208429",
			Blocks: []map[string]any{{
				"type": "table",
				"rows": []any{[]any{map[string]any{
					"type": "rich_text",
					"elements": []any{map[string]any{
						"type": "rich_text_section",
						"elements": []any{
							map[string]any{"type": "channel", "channel_id": "C789"},
							map[string]any{"type": "text", "text": " "},
							map[string]any{"type": "usergroup", "usergroup_id": "S789"},
							map[string]any{"type": "text", "text": " "},
							map[string]any{"type": "date", "timestamp": 1_720_710_212, "format": "{date_num}", "fallback": "July 11"},
							map[string]any{"type": "text", "text": " "},
							map[string]any{"type": "color", "value": "#ff0000"},
						},
					}},
				}}},
			}},
		})
		must.NoError(t, err)
		must.Eq(t, "#C789 <!subteam^S789> July 11 #ff0000", msg.Text)
	})

	t.Run("formats date cells from the timestamp when no fallback is present", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Ts: "1786120899.208429",
			Blocks: []map[string]any{{
				"type": "table",
				"rows": []any{[]any{map[string]any{
					"type": "date", "timestamp": 1_720_710_212, "format": "{date_num}",
				}}},
			}},
		})
		must.NoError(t, err)
		must.Eq(t, "2024-07-11", msg.Text)
		must.False(t, strings.Contains(msg.Text, "{date_num}"))
	})

	t.Run("preserves empty and raw value cells so columns stay aligned", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Ts: "1786120899.208429",
			Blocks: []map[string]any{{
				"type": "table",
				"rows": []any{
					[]any{
						map[string]any{"type": "raw_text", "text": "Samsung"},
						map[string]any{"type": "raw_text", "text": ""},
						map[string]any{"type": "raw_number", "value": 3},
					},
					[]any{
						map[string]any{"type": "raw_text", "text": "LG"},
						map[string]any{"type": "raw_boolean", "value": true},
						map[string]any{"type": "raw_number", "value": "7"},
					},
				},
			}},
		})
		must.NoError(t, err)
		must.Eq(t, "Samsung\t\t3\nLG\ttrue\t7", msg.Text)
	})

	t.Run("parses data_table blocks with their header row intact", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Ts: "1786120899.208429",
			Blocks: []map[string]any{{
				"type": "data_table", "caption": "Devices",
				"rows": []any{
					[]any{
						map[string]any{"type": "raw_text", "text": "Manufacturer"},
						map[string]any{"type": "raw_text", "text": "Units"},
					},
					[]any{
						map[string]any{"type": "raw_text", "text": "Samsung"},
						map[string]any{"type": "raw_text", "text": "3"},
					},
				},
			}},
		})
		must.NoError(t, err)
		must.Eq(t, "Manufacturer\tUnits\nSamsung\t3", msg.Text)
		table := formattedNode(msg).FirstChild()
		must.Eq(t, "Manufacturer", cellText(t, table, 0, 0))
		must.Eq(t, "Units", cellText(t, table, 0, 1))
		must.Eq(t, "Samsung", cellText(t, table, 1, 0))
		must.Eq(t, "3", cellText(t, table, 1, 1))
	})

	t.Run("preserves alert attachment content as message content", func(t *testing.T) {
		t.Parallel()
		event := SlackEvent{
			Type: "message", User: "U123", Username: "sentry", Channel: "C456",
			Text: "New alert", Ts: "1786120899.208429",
			Attachments: []SlackAttachment{{
				Fallback: "[Sentry] TypeError in checkout",
				Title:    "TypeError: cannot read property 'id' of undefined",
				Text:     "Occurred 42 times in the last hour.",
				Fields: []SlackAttachmentField{
					{Title: "Project", Value: "storefront", Short: true},
					{Title: "Environment", Value: "production", Short: true},
				},
			}},
		}
		expected := "New alert\n\n" +
			"TypeError: cannot read property 'id' of undefined\n" +
			"Occurred 42 times in the last hour.\n" +
			"Project: storefront\n" +
			"Environment: production"
		syncMsg, err := adapter.ParseMessage(event)
		must.NoError(t, err)
		asyncMsg, err := adapter.parseSlackMessage(t.Context(), event, "slack:C456:1786120899.208429")
		must.NoError(t, err)
		for _, message := range []*chat.Message{syncMsg, asyncMsg} {
			must.Eq(t, expected, message.Text)
		}
	})

	t.Run("falls back to attachment fallback only when nothing else carries content", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "Deploy finished", Ts: "1786120899.208429",
			Attachments: []SlackAttachment{{Fallback: "build #421 succeeded"}},
		})
		must.NoError(t, err)
		must.Eq(t, "Deploy finished\n\nbuild #421 succeeded", msg.Text)
	})

	t.Run("ignores content in unfurl and app attachments", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "Check this out", Ts: "1786120899.208429",
			Attachments: []SlackAttachment{
				{IsMsgUnfurl: true, Title: "Foreign title", Text: "Foreign text"},
				{IsAppUnfurl: true, Fallback: "Foreign fallback"},
				{FromURL: "https://example.com", Title: "Preview"},
				{OriginalURL: "https://example.com/page", Fields: []SlackAttachmentField{{Title: "Key", Value: "Value"}}},
			},
		})
		must.NoError(t, err)
		must.Eq(t, "Check this out", msg.Text)
	})

	t.Run("keeps attachment formatting characters literal unless mrkdwn_in enables them", func(t *testing.T) {
		t.Parallel()
		attachment := SlackAttachment{
			Title: "Cleanup failed in <module>",
			Text:  "rm -rf /tmp/*cache* failed for _id_ values",
		}
		literal, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "Alert", Ts: "1786120899.208429",
			Attachments: []SlackAttachment{attachment},
		})
		must.NoError(t, err)
		must.Eq(t, "Alert\n\n"+
			"Cleanup failed in <module>\n"+
			"rm -rf /tmp/*cache* failed for _id_ values", literal.Text)

		mrkdwn, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "Alert", Ts: "1786120899.208429",
			Attachments: []SlackAttachment{{
				Title: attachment.Title, MrkdwnIn: []string{"text"}, Text: "deploy *failed* badly",
			}},
		})
		must.NoError(t, err)
		must.Eq(t, "Alert\n\nCleanup failed in <module>\n\ndeploy failed badly", mrkdwn.Text)
		must.True(t, hasStrong(formattedNode(mrkdwn)))
	})

	t.Run("keeps attachment content out of an unclosed code fence in the body", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "Deploy failed:\n```\nTypeError: boom",
			Ts:   "1786120899.208429",
			Attachments: []SlackAttachment{{
				Title:  "Deploy status",
				Fields: []SlackAttachmentField{{Title: "Environment", Value: "production"}},
			}},
		})
		must.NoError(t, err)
		must.Eq(t, []string{"paragraph", "code", "paragraph"}, childTypes(formattedNode(msg)))
		must.Eq(t, "Deploy failed:\n\nTypeError: boom\n\nDeploy status\nEnvironment: production", msg.Text)
	})

	t.Run("uses the fallback when attachment blocks carry nothing renderable", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "Heads up", Ts: "1786120899.208429",
			Attachments: []SlackAttachment{{
				Fallback: "Deploy failed on step 3",
				Blocks:   []map[string]any{{"type": "section", "text": "Deploy failed on step 3"}},
			}},
		})
		must.NoError(t, err)
		must.Eq(t, "Heads up\n\nDeploy failed on step 3", msg.Text)
	})

	t.Run("prefers attachment blocks over legacy fields, matching Slack rendering", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "Report", Ts: "1786120899.208429",
			Attachments: []SlackAttachment{{
				Fallback: "table fallback",
				Title:    "Legacy title Slack does not render",
				Blocks: []map[string]any{{
					"type": "table",
					"rows": []any{
						[]any{
							map[string]any{"type": "raw_text", "text": "Region"},
							map[string]any{"type": "raw_text", "text": "Status"},
						},
						[]any{
							map[string]any{"type": "raw_text", "text": "us-east"},
							map[string]any{"type": "raw_text", "text": "down"},
						},
					},
				}},
			}},
		})
		must.NoError(t, err)
		must.Eq(t, "Report\n\nRegion\tStatus\nus-east\tdown", msg.Text)
	})

	t.Run("links the attachment title to title_link and surfaces the URL", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "New issue", Ts: "1786120899.208429",
			Attachments: []SlackAttachment{{
				Title:     "TypeError in checkout",
				TitleLink: "https://sentry.example.com/issues/123",
			}},
		})
		must.NoError(t, err)
		must.Eq(t, "New issue\n\nTypeError in checkout", msg.Text)
		para := nthChild(t, formattedNode(msg), 1)
		must.Eq(t, "paragraph", nodeType(para))
		link := para.FirstChild()
		must.True(t, chat.IsLinkNode(link))
		must.Eq(t, "https://sentry.example.com/issues/123", string(link.(*ast.Link).Destination))
		must.Eq(t, "TypeError in checkout", nodeText(link.FirstChild()))
		var urls []string
		for _, l := range msg.Links {
			urls = append(urls, l.URL)
		}
		must.SliceContains(t, urls, "https://sentry.example.com/issues/123")
	})

	t.Run("keeps each attachment's tables adjacent to its text", func(t *testing.T) {
		t.Parallel()
		table := func(cell string) map[string]any {
			return map[string]any{"type": "table", "rows": []any{[]any{map[string]any{"type": "raw_text", "text": cell}}}}
		}
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "Two alerts", Ts: "1786120899.208429",
			Attachments: []SlackAttachment{
				{Title: "Alert A"},
				{Blocks: []map[string]any{table("table A")}},
				{Title: "Alert B"},
				{Blocks: []map[string]any{table("table B")}},
			},
		})
		must.NoError(t, err)
		must.Eq(t, "Two alerts\n\nAlert A\n\ntable A\n\nAlert B\n\ntable B", msg.Text)
	})

	t.Run("resolves mentions in attachment content with a single lookup per user", func(t *testing.T) {
		t.Parallel()
		info := &usersInfoDoer{}
		local := mustNew(t, Config{BotUserID: "U_BOT", HTTPClient: info})
		must.NoError(t, local.Initialize(t.Context(), stubChat{}))
		msg, err := local.parseSlackMessage(t.Context(), SlackEvent{
			Type: "message", User: "U123", Username: "pager", Channel: "C456",
			Text: "Incident", Ts: "1786120899.208429",
			Attachments: []SlackAttachment{{
				Fields: []SlackAttachmentField{
					{Title: "Primary", Value: "<@U777>"},
					{Title: "Secondary", Value: "<@U777>"},
				},
			}},
		}, "slack:C456:1786120899.208429")
		must.NoError(t, err)
		must.Eq(t, "Incident\n\nPrimary: @jane\nSecondary: @jane", msg.Text)
		must.Eq(t, 1, info.n)
	})

	t.Run("ignores tables in unfurl and app attachments", func(t *testing.T) {
		t.Parallel()
		tableBlock := map[string]any{"type": "table", "rows": []any{[]any{map[string]any{"type": "raw_text", "text": "Foreign"}}}}
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "Check this out", Ts: "1786120899.208429",
			Attachments: []SlackAttachment{
				{IsMsgUnfurl: true, Blocks: []map[string]any{tableBlock}},
				{IsAppUnfurl: true, Blocks: []map[string]any{tableBlock}},
				{FromURL: "https://example.com", Blocks: []map[string]any{tableBlock}},
				{OriginalURL: "https://example.com/page", Blocks: []map[string]any{tableBlock}},
			},
		})
		must.NoError(t, err)
		must.Eq(t, "Check this out", msg.Text)
		must.Eq(t, 1, childCount(formattedNode(msg)))
	})

	t.Run("keeps tables pasted above the message text above it", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "The table above shows Q1", Ts: "1786120899.208429",
			Blocks: []map[string]any{
				{"type": "table", "rows": []any{[]any{map[string]any{"type": "raw_text", "text": "Row"}}}},
				{"type": "rich_text", "elements": []any{
					map[string]any{"type": "rich_text_section", "elements": []any{
						map[string]any{"type": "text", "text": "The table above shows Q1"},
					}},
				}},
			},
		})
		must.NoError(t, err)
		must.Eq(t, "table", nodeType(formattedNode(msg).FirstChild()))
		must.Eq(t, "paragraph", nodeType(formattedNode(msg).FirstChild().NextSibling()))
		must.Eq(t, "Row\n\nThe table above shows Q1", msg.Text)
	})

	t.Run("does not leave raw bot mention tokens in table cells", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Ts: "1786120899.208429",
			Blocks: []map[string]any{{
				"type": "table",
				"rows": []any{[]any{
					map[string]any{"type": "raw_text", "text": "On call"},
					map[string]any{"type": "user", "user_id": "UBOT123"},
				}},
			}},
		})
		must.NoError(t, err)
		must.Eq(t, "On call\t@UBOT123", msg.Text)
		must.False(t, strings.Contains(msg.Text, "<@"))
	})
}

func TestLinkExtraction(t *testing.T) {
	t.Parallel()
	adapter := mustNew(t, Config{BotUserID: "U_BOT"})

	t.Run("extracts links from rich_text blocks", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "Check <https://example.com|this> out", Ts: "1234567890.123456",
			Blocks: []map[string]any{{
				"type": "rich_text",
				"elements": []any{map[string]any{
					"type": "rich_text_section",
					"elements": []any{
						map[string]any{"type": "text", "text": "Check "},
						map[string]any{"type": "link", "url": "https://example.com", "text": "this"},
						map[string]any{"type": "text", "text": " out"},
					},
				}},
			}},
		})
		must.NoError(t, err)
		must.Eq(t, 1, len(msg.Links))
		must.Eq(t, "https://example.com", msg.Links[0].URL)
	})

	t.Run("extracts links from text when no blocks are present", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "Visit <https://vercel.com> and <https://example.com|Example>",
			Ts:   "1234567890.123456",
		})
		must.NoError(t, err)
		must.Eq(t, 2, len(msg.Links))
		must.Eq(t, "https://vercel.com", msg.Links[0].URL)
		must.Eq(t, "https://example.com", msg.Links[1].URL)
	})

	t.Run("deduplicates URLs", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "<https://example.com> and <https://example.com|again>",
			Ts:   "1234567890.123456",
			Blocks: []map[string]any{{
				"type": "rich_text",
				"elements": []any{map[string]any{
					"type": "rich_text_section",
					"elements": []any{
						map[string]any{"type": "link", "url": "https://example.com"},
						map[string]any{"type": "link", "url": "https://example.com"},
					},
				}},
			}},
		})
		must.NoError(t, err)
		must.Eq(t, 1, len(msg.Links))
	})

	t.Run("provides fetchMessage for Slack message links", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "<https://myteam.slack.com/archives/C789/p1234567890123456>",
			Ts:   "1234567890.123456",
		})
		must.NoError(t, err)
		must.Eq(t, 1, len(msg.Links))
		must.Eq(t, "https://myteam.slack.com/archives/C789/p1234567890123456", msg.Links[0].URL)
		must.True(t, msg.Links[0].FetchMessage != nil)
	})

	t.Run("returns empty links for messages without URLs", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "Just a plain message", Ts: "1234567890.123456",
		})
		must.NoError(t, err)
		must.Eq(t, []chat.LinkPreview{}, msg.Links)
	})

	t.Run("does not treat user mentions as links", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "<@U456> hello", Ts: "1234567890.123456",
		})
		must.NoError(t, err)
		must.Eq(t, []chat.LinkPreview{}, msg.Links)
	})
}

func TestEdgeCases(t *testing.T) {
	t.Parallel()
	adapter := mustNew(t, Config{})

	t.Run("handles missing text in event", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456", Ts: "1234567890.123456",
		})
		must.NoError(t, err)
		must.Eq(t, "", msg.Text)
	})

	t.Run("handles missing user in event", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", Channel: "C456", Text: "Anonymous message", Ts: "1234567890.123456",
		})
		must.NoError(t, err)
		must.Eq(t, "unknown", msg.Author.UserID)
	})

	t.Run("handles missing ts in event", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456", Text: "No timestamp",
		})
		must.NoError(t, err)
		must.Eq(t, "", msg.ID)
	})

	t.Run("parses username from event when available", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Username: "testuser",
			Channel: "C456", Text: "Hello", Ts: "1234567890.123456",
		})
		must.NoError(t, err)
		must.Eq(t, "testuser", msg.Author.UserName)
	})
}

func TestDateParsing(t *testing.T) {
	t.Parallel()
	adapter := mustNew(t, Config{})

	t.Run("parses Slack timestamp to Date", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "Hello", Ts: "1609459200.000000",
		})
		must.NoError(t, err)
		must.Eq(t, time.UnixMilli(1609459200000).UTC(), msg.Metadata.DateSent.UTC())
	})

	t.Run("handles edited timestamp", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "Hello", Ts: "1609459200.000000",
			Edited: &SlackEdited{Ts: "1609459260.000000"},
		})
		must.NoError(t, err)
		must.True(t, msg.Metadata.EditedAt != nil)
		must.Eq(t, time.UnixMilli(1609459260000).UTC(), msg.Metadata.EditedAt.UTC())
	})
}

func TestFormattedTextExtraction(t *testing.T) {
	t.Parallel()
	adapter := mustNew(t, Config{})

	t.Run("extracts plain text from mrkdwn", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "*bold* and _italic_", Ts: "1234567890.123456",
		})
		must.NoError(t, err)
		must.Eq(t, "bold and italic", msg.Text)
	})

	t.Run("extracts text from links", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "Check <https://example.com|this link>", Ts: "1234567890.123456",
		})
		must.NoError(t, err)
		must.True(t, strings.Contains(msg.Text, "this link"))
	})

	t.Run("extracts text from user mentions", func(t *testing.T) {
		t.Parallel()
		msg, err := adapter.ParseMessage(SlackEvent{
			Type: "message", User: "U123", Channel: "C456",
			Text: "Hey <@U456|john>!", Ts: "1234567890.123456",
		})
		must.NoError(t, err)
		must.True(t, strings.Contains(msg.Text, "@john"))
	})
}

func TestIsMessageFromSelf(t *testing.T) {
	t.Parallel()

	t.Run("matches by bot user ID", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{})
		adapter.botUserID = "U_BOT_123"
		must.True(t, adapter.isMessageFromSelf(t.Context(), SlackEvent{User: "U_BOT_123"}))
	})

	t.Run("matches by bot profile user ID", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{})
		adapter.botUserID = "U_BOT_123"
		must.True(t, adapter.isMessageFromSelf(t.Context(), SlackEvent{
			BotID: "B_BOT_456", BotProfile: &SlackBotProfile{UserID: "U_BOT_123"},
		}))
	})

	t.Run("matches request bot user ID by bot profile", func(t *testing.T) {
		t.Parallel()
		adapter, err := New(Config{SigningSecret: "s"})
		must.NoError(t, err)
		var result bool
		ctx := withRequestContext(t.Context(), &requestContext{token: "xoxb-test", botUserID: "U_BOT_123"})
		result = adapter.isMessageFromSelf(ctx, SlackEvent{
			BotID: "B_BOT_456", BotProfile: &SlackBotProfile{UserID: "U_BOT_123"},
		})
		must.True(t, result)
	})

	t.Run("matches by bot ID", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{})
		adapter.botID = "B_BOT_456"
		must.True(t, adapter.isMessageFromSelf(t.Context(), SlackEvent{BotID: "B_BOT_456"}))
	})

	t.Run("returns false for non-bot messages", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{})
		adapter.botUserID = "U_BOT_123"
		adapter.botID = "B_BOT_456"
		must.False(t, adapter.isMessageFromSelf(t.Context(), SlackEvent{User: "U_OTHER"}))
	})
}

func TestConfigLogValueRedactsSecrets(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Token:         api.StaticToken("xoxb-secret"),
		SigningSecret: "sig",
		ClientSecret:  "cs",
		EncryptionKey: "ek",
		UserName:      "bot",
	}
	v := cfg.LogValue()
	s := v.String()
	must.False(t, strings.Contains(s, "xoxb-secret"))
	must.True(t, strings.Contains(s, "REDACTED"))
}

type countingToken struct {
	n   int
	tok string
}

func (c *countingToken) Token(context.Context) (string, error) {
	c.n++
	return c.tok, nil
}

type authCapture struct {
	auth string
	ok   bool
}

func (a *authCapture) Do(req *http.Request) (*http.Response, error) {
	a.auth = req.Header.Get("Authorization")
	body := `{"ok":true}`
	if !a.ok {
		body = `{"ok":false,"error":"not_authed"}`
	}
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

type usersInfoDoer struct{ n int }

func (u *usersInfoDoer) Do(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Path, "users.info") {
		u.n++
	}
	body, _ := json.Marshal(map[string]any{
		"ok": true,
		"user": map[string]any{
			"name":    "jane",
			"profile": map[string]any{"display_name": "jane"},
		},
	})
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(string(body))),
	}, nil
}

type stubChat struct{}

func (stubChat) State() chat.StateAdapter { return nil }
func (stubChat) ProcessMessage(context.Context, chat.ProcessMessageInput) error {
	return nil
}
func (stubChat) ProcessMessageUpdated(context.Context, chat.ProcessMessageInput) error {
	return nil
}
func (stubChat) ProcessMessageDeleted(context.Context, string, string) error { return nil }
func (stubChat) ProcessReaction(context.Context, chat.ProcessReactionInput) error {
	return nil
}
func (stubChat) ProcessSlashCommand(context.Context, chat.ProcessSlashCommandInput) error {
	return nil
}
func (stubChat) ProcessAction(context.Context, chat.ProcessActionInput) error { return nil }
func (stubChat) ProcessModalSubmit(context.Context, chat.ProcessModalInput) (any, error) {
	return nil, nil
}
func (stubChat) ProcessModalClose(context.Context, chat.ProcessModalInput) error { return nil }
func (stubChat) ProcessOptionsLoad(context.Context, chat.ProcessOptionsLoadInput) (any, error) {
	return nil, nil
}
func (stubChat) ProcessMemberJoinedChannel(context.Context, chat.ProcessMemberJoinedInput) error {
	return nil
}
func (stubChat) ProcessAssistantThreadStarted(context.Context, chat.ProcessAssistantThreadInput) error {
	return nil
}
func (stubChat) ProcessAssistantContextChanged(context.Context, chat.ProcessAssistantThreadInput) error {
	return nil
}

func formattedNode(m *chat.Message) ast.Node {
	n, _ := m.Formatted.(ast.Node)
	return n
}

func childCount(n ast.Node) int {
	if n == nil {
		return 0
	}
	return n.ChildCount()
}

func childTypes(n ast.Node) []string {
	var out []string
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		out = append(out, nodeType(c))
	}
	return out
}

func nodeType(n ast.Node) string {
	if n == nil {
		return ""
	}
	switch n.Kind() {
	case ast.KindParagraph:
		return "paragraph"
	case ast.KindFencedCodeBlock, ast.KindCodeBlock:
		return "code"
	case east.KindTable:
		return "table"
	case east.KindTableRow, east.KindTableHeader:
		return "tableRow"
	case east.KindTableCell:
		return "tableCell"
	case ast.KindLink:
		return "link"
	case ast.KindEmphasis:
		if chat.IsStrongNode(n) {
			return "strong"
		}
		return "emphasis"
	default:
		return n.Kind().String()
	}
}

func nthChild(t *testing.T, n ast.Node, i int) ast.Node {
	t.Helper()
	c := n.FirstChild()
	for j := 0; j < i && c != nil; j++ {
		c = c.NextSibling()
	}
	must.True(t, c != nil)
	return c
}

func cellText(t *testing.T, table ast.Node, row, col int) string {
	t.Helper()
	r := nthChild(t, table, row)
	c := nthChild(t, r, col)
	return chat.ToPlainText(c, nil)
}

func nodeText(n ast.Node) string {
	if n == nil {
		return ""
	}
	if s, ok := n.(*ast.String); ok {
		return string(s.Value)
	}
	if t, ok := n.(*ast.Text); ok {
		return string(t.Value(nil))
	}
	return chat.ToPlainText(n, nil)
}

func hasStrong(n ast.Node) bool {
	found := false
	_ = ast.Walk(n, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && chat.IsStrongNode(node) {
			found = true
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	return found
}
