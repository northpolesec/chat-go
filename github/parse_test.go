package github

import (
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/shoenig/test/must"
)

var mentionWhitespaceRe = regexp.MustCompile(`@test-bot\s+hi there`)

func parseAdapter(t *testing.T, botUserID string) *Adapter {
	t.Helper()
	a, err := New(Config{
		Token:         StaticToken("x"),
		WebhookSecret: "s",
		UserName:      "test-bot",
		BotUserID:     botUserID,
	})
	must.NoError(t, err)
	return a
}

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

var (
	createdAt = ts("2024-01-01T00:00:00Z")
	editedAt  = ts("2024-01-02T00:00:00Z")
)

func acmeRepo() Repository {
	return Repository{
		ID:       1,
		Name:     "app",
		FullName: "acme/app",
		Owner:    User{ID: 10, Login: "acme", Type: "User"},
	}
}

func acmeRepoRef() Repository {
	return Repository{
		Name:     "app",
		FullName: "acme/app",
		Owner:    User{ID: 10, Login: "acme", Type: "User"},
	}
}

func mustRaw(t *testing.T, msg *chat.Message) RawMessage {
	t.Helper()
	rm, ok := msg.Raw.(RawMessage)
	must.True(t, ok)
	return rm
}

func mustEmptyAttachments(t *testing.T, msg *chat.Message) {
	t.Helper()
	must.NotNil(t, msg.Attachments)
	must.Eq(t, 0, len(msg.Attachments))
}

func mustMention(t *testing.T, msg *chat.Message, want bool) {
	t.Helper()
	must.NotNil(t, msg.IsMention)
	must.Eq(t, want, *msg.IsMention)
}

func TestParseMessage(t *testing.T) {
	t.Parallel()
	a := parseAdapter(t, "")

	t.Run("should parse an issue_comment raw message", func(t *testing.T) {
		t.Parallel()
		raw := RawMessage{
			Type: rawIssueComment,
			Comment: Comment{
				ID:        100,
				Body:      "Test comment",
				User:      User{ID: 1, Login: "testuser", Type: "User"},
				CreatedAt: createdAt,
				UpdatedAt: createdAt,
				HTMLURL:   "https://github.com/acme/app/pull/42#issuecomment-100",
			},
			Repository: acmeRepo(),
			Number:     42,
		}
		msg, err := a.ParseMessage(raw)
		must.NoError(t, err)
		must.Eq(t, "100", msg.ID)
		must.Eq(t, "github:acme/app:42", msg.ThreadID)
		must.Eq(t, "Test comment", msg.Text)
		must.Eq(t, "1", msg.Author.UserID)
		must.Eq(t, "testuser", msg.Author.UserName)
		must.Eq(t, "testuser", msg.Author.FullName)
		must.NotNil(t, msg.Author.IsBot)
		must.False(t, *msg.Author.IsBot)
		must.False(t, msg.Author.IsMe)
		must.Eq(t, createdAt, msg.Metadata.DateSent)
		must.False(t, msg.Metadata.Edited)
		must.Nil(t, msg.Metadata.EditedAt)
		mustMention(t, msg, false)
		mustEmptyAttachments(t, msg)
		got := mustRaw(t, msg)
		must.Eq(t, rawIssueComment, got.Type)
		must.Eq(t, raw.Comment, got.Comment)
		must.Eq(t, 42, got.Number)
		must.Eq(t, ThreadTypePR, got.ThreadType)
		must.Eq(t, acmeRepoRef(), got.Repository)
	})

	t.Run("should preserve whitespace in newline-separated issue comment mentions", func(t *testing.T) {
		t.Parallel()
		raw := RawMessage{
			Type: rawIssueComment,
			Comment: Comment{
				ID:        100,
				Body:      "@test-bot\nhi there",
				User:      User{ID: 1, Login: "testuser", Type: "User"},
				CreatedAt: createdAt,
				UpdatedAt: createdAt,
				HTMLURL:   "https://github.com/acme/app/pull/42#issuecomment-100",
			},
			Repository: acmeRepo(),
			Number:     42,
		}
		msg, err := a.ParseMessage(raw)
		must.NoError(t, err)
		must.RegexMatch(t, mentionWhitespaceRe, msg.Text)
		must.StrNotContains(t, msg.Text, "@test-bothi there")
		mustMention(t, msg, true)
	})

	t.Run("should parse an issue_comment raw message from an issue thread", func(t *testing.T) {
		t.Parallel()
		raw := RawMessage{
			Type: rawIssueComment,
			Comment: Comment{
				ID:        100,
				Body:      "Issue comment",
				User:      User{ID: 1, Login: "testuser", Type: "User"},
				CreatedAt: createdAt,
				UpdatedAt: createdAt,
				HTMLURL:   "https://github.com/acme/app/issues/10#issuecomment-100",
			},
			Repository: acmeRepo(),
			Number:     10,
			ThreadType: ThreadTypeIssue,
		}
		msg, err := a.ParseMessage(raw)
		must.NoError(t, err)
		must.Eq(t, "100", msg.ID)
		must.Eq(t, "github:acme/app:issue:10", msg.ThreadID)
		must.Eq(t, "Issue comment", msg.Text)
		got := mustRaw(t, msg)
		must.Eq(t, rawIssueComment, got.Type)
		must.Eq(t, ThreadTypeIssue, got.ThreadType)
	})

	t.Run("should default to PR thread format when threadType is omitted", func(t *testing.T) {
		t.Parallel()
		raw := RawMessage{
			Type: rawIssueComment,
			Comment: Comment{
				ID:        100,
				Body:      "Test comment",
				User:      User{ID: 1, Login: "testuser", Type: "User"},
				CreatedAt: createdAt,
				UpdatedAt: createdAt,
				HTMLURL:   "https://github.com/acme/app/pull/42#issuecomment-100",
			},
			Repository: acmeRepo(),
			Number:     42,
		}
		msg, err := a.ParseMessage(raw)
		must.NoError(t, err)
		must.Eq(t, "github:acme/app:42", msg.ThreadID)
	})

	t.Run("should parse a review_comment raw message (root comment)", func(t *testing.T) {
		t.Parallel()
		raw := RawMessage{
			Type: rawReviewComment,
			Comment: Comment{
				ID:        200,
				Body:      "Line comment",
				User:      User{ID: 2, Login: "reviewer", Type: "User"},
				CreatedAt: createdAt,
				UpdatedAt: createdAt,
				HTMLURL:   "https://github.com/acme/app/pull/42#discussion_r200",
				Path:      "src/index.ts",
				DiffHunk:  "@@",
				CommitID:  "abc",
			},
			Repository: acmeRepo(),
			Number:     42,
		}
		msg, err := a.ParseMessage(raw)
		must.NoError(t, err)
		must.Eq(t, "200", msg.ID)
		must.Eq(t, "github:acme/app:42:rc:200", msg.ThreadID)
	})

	t.Run("should preserve whitespace in newline-separated review comment mentions", func(t *testing.T) {
		t.Parallel()
		raw := RawMessage{
			Type: rawReviewComment,
			Comment: Comment{
				ID:        200,
				Body:      "@test-bot\nhi there",
				User:      User{ID: 2, Login: "reviewer", Type: "User"},
				CreatedAt: createdAt,
				UpdatedAt: createdAt,
				HTMLURL:   "https://github.com/acme/app/pull/42#discussion_r200",
				Path:      "src/index.ts",
				DiffHunk:  "@@",
				CommitID:  "abc",
			},
			Repository: acmeRepo(),
			Number:     42,
		}
		msg, err := a.ParseMessage(raw)
		must.NoError(t, err)
		must.RegexMatch(t, mentionWhitespaceRe, msg.Text)
		must.StrNotContains(t, msg.Text, "@test-bothi there")
		mustMention(t, msg, true)
	})

	t.Run("should parse a review_comment raw message (reply)", func(t *testing.T) {
		t.Parallel()
		raw := RawMessage{
			Type: rawReviewComment,
			Comment: Comment{
				ID:          300,
				Body:        "Reply",
				User:        User{ID: 2, Login: "reviewer", Type: "User"},
				CreatedAt:   createdAt,
				UpdatedAt:   createdAt,
				HTMLURL:     "https://github.com/acme/app/pull/42#discussion_r300",
				Path:        "src/index.ts",
				DiffHunk:    "@@",
				CommitID:    "abc",
				InReplyToID: 200,
			},
			Repository: acmeRepo(),
			Number:     42,
		}
		msg, err := a.ParseMessage(raw)
		must.NoError(t, err)
		must.Eq(t, "300", msg.ID)
		must.Eq(t, "github:acme/app:42:rc:200", msg.ThreadID)
	})

	t.Run("should mark edited messages", func(t *testing.T) {
		t.Parallel()
		raw := RawMessage{
			Type: rawIssueComment,
			Comment: Comment{
				ID:        100,
				Body:      "Edited",
				User:      User{ID: 1, Login: "testuser", Type: "User"},
				CreatedAt: createdAt,
				UpdatedAt: editedAt,
				HTMLURL:   "https://github.com/acme/app/pull/42#issuecomment-100",
			},
			Repository: acmeRepo(),
			Number:     42,
		}
		msg, err := a.ParseMessage(raw)
		must.NoError(t, err)
		must.True(t, msg.Metadata.Edited)
		must.NotNil(t, msg.Metadata.EditedAt)
		must.Eq(t, editedAt, *msg.Metadata.EditedAt)
	})

	t.Run("should not mark unedited messages as edited", func(t *testing.T) {
		t.Parallel()
		raw := RawMessage{
			Type: rawIssueComment,
			Comment: Comment{
				ID:        100,
				Body:      "Not edited",
				User:      User{ID: 1, Login: "testuser", Type: "User"},
				CreatedAt: createdAt,
				UpdatedAt: createdAt,
				HTMLURL:   "https://github.com/acme/app/pull/42#issuecomment-100",
			},
			Repository: acmeRepo(),
			Number:     42,
		}
		msg, err := a.ParseMessage(raw)
		must.NoError(t, err)
		must.False(t, msg.Metadata.Edited)
		must.Nil(t, msg.Metadata.EditedAt)
	})

	t.Run("should extract GFM plain text", func(t *testing.T) {
		t.Parallel()
		raw := RawMessage{
			Type: rawIssueComment,
			Comment: Comment{
				ID:        100,
				Body:      "**bold**",
				User:      User{ID: 1, Login: "testuser", Type: "User"},
				CreatedAt: createdAt,
				UpdatedAt: createdAt,
			},
			Repository: acmeRepo(),
			Number:     42,
		}
		msg, err := a.ParseMessage(raw)
		must.NoError(t, err)
		must.Eq(t, "bold", msg.Text)
	})

	t.Run("accepts *RawMessage", func(t *testing.T) {
		t.Parallel()
		raw := RawMessage{
			Type: rawIssueComment,
			Comment: Comment{
				ID:        100,
				Body:      "ptr",
				User:      User{ID: 1, Login: "testuser", Type: "User"},
				CreatedAt: createdAt,
				UpdatedAt: createdAt,
			},
			Repository: acmeRepo(),
			Number:     42,
		}
		msg, err := a.ParseMessage(&raw)
		must.NoError(t, err)
		must.Eq(t, "100", msg.ID)
		must.Eq(t, "ptr", msg.Text)
	})

	t.Run("accepts JSON-shaped map[string]any", func(t *testing.T) {
		t.Parallel()
		raw := RawMessage{
			Type: rawIssueComment,
			Comment: Comment{
				ID:        100,
				Body:      "from map",
				User:      User{ID: 1, Login: "testuser", Type: "User"},
				CreatedAt: createdAt,
				UpdatedAt: createdAt,
			},
			Repository: acmeRepo(),
			Number:     42,
		}
		b, err := json.Marshal(raw)
		must.NoError(t, err)
		var m map[string]any
		must.NoError(t, json.Unmarshal(b, &m))
		msg, err := a.ParseMessage(m)
		must.NoError(t, err)
		must.Eq(t, "100", msg.ID)
		must.Eq(t, "from map", msg.Text)
		must.Eq(t, "github:acme/app:42", msg.ThreadID)
	})

	t.Run("rejects other types with ValidationError", func(t *testing.T) {
		t.Parallel()
		_, err := a.ParseMessage("not a raw message")
		var ve *shared.ValidationError
		must.True(t, errors.As(err, &ve))
		must.ErrorContains(t, err, "ParseMessage expects a github.RawMessage")
	})

	t.Run("rejects unknown raw message type", func(t *testing.T) {
		t.Parallel()
		_, err := a.ParseMessage(RawMessage{Type: "gollum", Repository: acmeRepo(), Number: 1})
		var ve *shared.ValidationError
		must.True(t, errors.As(err, &ve))
		must.ErrorContains(t, err, `unknown raw message type "gollum"`)
	})
}

func TestParseAuthorViaParseMessage(t *testing.T) {
	t.Parallel()

	t.Run("should identify bot users", func(t *testing.T) {
		t.Parallel()
		a := parseAdapter(t, "")
		raw := RawMessage{
			Type: rawIssueComment,
			Comment: Comment{
				ID:        100,
				Body:      "Automated comment",
				User:      User{ID: 50, Login: "dependabot[bot]", Type: "Bot"},
				CreatedAt: createdAt,
				UpdatedAt: createdAt,
				HTMLURL:   "https://github.com/acme/app/pull/42#issuecomment-100",
			},
			Repository: acmeRepo(),
			Number:     42,
		}
		msg, err := a.ParseMessage(raw)
		must.NoError(t, err)
		must.NotNil(t, msg.Author.IsBot)
		must.True(t, *msg.Author.IsBot)
		must.Eq(t, "dependabot[bot]", msg.Author.UserName)
		must.Eq(t, "50", msg.Author.UserID)
		must.Eq(t, "dependabot[bot]", msg.Author.FullName)
	})

	t.Run("should detect isMe when botUserId matches", func(t *testing.T) {
		t.Parallel()
		a := parseAdapter(t, "50")
		raw := RawMessage{
			Type: rawIssueComment,
			Comment: Comment{
				ID:        100,
				Body:      "My comment",
				User:      User{ID: 50, Login: "test-bot", Type: "Bot"},
				CreatedAt: createdAt,
				UpdatedAt: createdAt,
				HTMLURL:   "https://github.com/acme/app/pull/42#issuecomment-100",
			},
			Repository: acmeRepo(),
			Number:     42,
		}
		msg, err := a.ParseMessage(raw)
		must.NoError(t, err)
		must.True(t, msg.Author.IsMe)
	})

	t.Run("should set isMe to false when user is not the bot", func(t *testing.T) {
		t.Parallel()
		a := parseAdapter(t, "")
		raw := RawMessage{
			Type: rawIssueComment,
			Comment: Comment{
				ID:        100,
				Body:      "Someone else",
				User:      User{ID: 999, Login: "someone", Type: "User"},
				CreatedAt: createdAt,
				UpdatedAt: createdAt,
				HTMLURL:   "https://github.com/acme/app/pull/42#issuecomment-100",
			},
			Repository: acmeRepo(),
			Number:     42,
		}
		msg, err := a.ParseMessage(raw)
		must.NoError(t, err)
		must.False(t, msg.Author.IsMe)
	})

	t.Run("should set isMe to false when user id is 0", func(t *testing.T) {
		t.Parallel()
		a := parseAdapter(t, "0")
		raw := RawMessage{
			Type: rawIssueComment,
			Comment: Comment{
				ID:        100,
				Body:      "zero id",
				User:      User{ID: 0, Login: "ghost", Type: "User"},
				CreatedAt: createdAt,
				UpdatedAt: createdAt,
			},
			Repository: acmeRepo(),
			Number:     42,
		}
		msg, err := a.ParseMessage(raw)
		must.NoError(t, err)
		must.False(t, msg.Author.IsMe)
	})
}
