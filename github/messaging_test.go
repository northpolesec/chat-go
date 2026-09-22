package github

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/shoenig/test/must"
	"github.com/yuin/goldmark/ast"
)

func newMessagingAdapter(t *testing.T) (*Adapter, *ghMock) {
	t.Helper()
	m := newGHMock(t)
	a, err := New(patConfig(m))
	must.NoError(t, err)
	return a, m
}

func commentJSON(id int64, body string) string {
	return fmt.Sprintf(`{"id":%d,"body":%q,"user":{"id":777,"login":"test-bot","type":"Bot"},"created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z","html_url":"https://github.com/acme/app/pull/42#issuecomment-%d"}`, id, body, id)
}

func issueCommentsJSON(n int) string {
	var b strings.Builder
	b.WriteByte('[')
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		day := fmt.Sprintf("%02d", i+1)
		id := 100 + i
		fmt.Fprintf(&b, `{"id":%d,"body":"Comment %d","user":{"id":1,"login":"user1","type":"User"},"created_at":"2024-01-%sT00:00:00Z","updated_at":"2024-01-%sT00:00:00Z","html_url":"https://github.com/acme/app/pull/42#issuecomment-%d"}`, id, i, day, day, id)
	}
	b.WriteByte(']')
	return b.String()
}

func openPullsJSON(n int) string {
	var b strings.Builder
	b.WriteByte('[')
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		num := i + 1
		fmt.Fprintf(&b, `{"number":%d,"title":"PR %d","body":null,"state":"open","html_url":"https://github.com/acme/app/pull/%d","user":{"id":1,"login":"testuser","type":"User"},"created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`, num, num, num)
	}
	b.WriteByte(']')
	return b.String()
}

func mustPosted(t *testing.T, got *chat.RawMessage, wantID, wantThread, wantType string) RawMessage {
	t.Helper()
	must.NotNil(t, got)
	must.Eq(t, wantID, got.ID)
	must.Eq(t, wantThread, got.Channel)
	rm, ok := got.Raw.(RawMessage)
	must.True(t, ok)
	must.Eq(t, wantType, rm.Type)
	return rm
}

func streamChunks(chunks ...chat.StreamChunk) func(func(chat.StreamChunk, error) bool) {
	return func(yield func(chat.StreamChunk, error) bool) {
		for _, c := range chunks {
			if !yield(c, nil) {
				return
			}
		}
	}
}

func TestPostMessage(t *testing.T) {
	t.Parallel()

	t.Run("should post an issue comment for PR-level thread", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		req := m.jsonCapture("POST /repos/acme/app/issues/42/comments", 201, commentJSON(999, "Hello"))
		got, err := a.PostMessage(t.Context(), "github:acme/app:42", chat.PostableText("Hello world"))
		must.NoError(t, err)
		must.Eq(t, "Hello world", req.jsonBody(t)["body"])
		rm := mustPosted(t, got, "999", "github:acme/app:42", rawIssueComment)
		must.Eq(t, int64(999), rm.Comment.ID)
		must.Eq(t, 42, rm.Number)
		must.Eq(t, ThreadTypePR, rm.ThreadType)
		must.Eq(t, "app", rm.Repository.Name)
		must.Eq(t, "acme/app", rm.Repository.FullName)
		must.Eq(t, "acme", rm.Repository.Owner.Login)
		must.Eq(t, "777", a.BotUserID())
	})

	t.Run("should post a review comment reply for review comment thread", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		req := m.jsonCapture("POST /repos/acme/app/pulls/42/comments/200/replies", 201, commentJSON(1001, "LGTM"))
		got, err := a.PostMessage(t.Context(), "github:acme/app:42:rc:200", chat.PostableText("LGTM"))
		must.NoError(t, err)
		must.Eq(t, "LGTM", req.jsonBody(t)["body"])
		rm := mustPosted(t, got, "1001", "github:acme/app:42:rc:200", rawReviewComment)
		must.Eq(t, 42, rm.Number)
		must.Eq(t, "", rm.ThreadType)
	})

	t.Run("should post with AST message format", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		req := m.jsonCapture("POST /repos/acme/app/issues/42/comments", 201, commentJSON(888, "**bold**"))
		_, err := a.PostMessage(t.Context(), "github:acme/app:42", chat.PostableMarkdown{Markdown: "**bold**"})
		must.NoError(t, err)
		must.Eq(t, "**bold**", req.jsonBody(t)["body"])
	})

	t.Run("should render card messages to GitHub markdown", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		req := m.jsonCapture("POST /repos/acme/app/issues/42/comments", 201, commentJSON(555, "**Title**"))
		card := chat.Card{
			Type:  "card",
			Title: "Deploy Status",
			Children: []any{
				chat.CardTextElement{Type: "text", Content: "Deploy succeeded"},
			},
		}
		_, err := a.PostMessage(t.Context(), "github:acme/app:42", card)
		must.NoError(t, err)
		must.StrContains(t, req.jsonBody(t)["body"].(string), "Deploy Status")
	})

	t.Run("should convert emoji placeholders to unicode", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		req := m.jsonCapture("POST /repos/acme/app/issues/42/comments", 201, commentJSON(1, "Nice"))
		_, err := a.PostMessage(t.Context(), "github:acme/app:42", chat.PostableText("Nice {{emoji:thumbs_up}}"))
		must.NoError(t, err)
		must.Eq(t, "Nice 👍", req.jsonBody(t)["body"])
	})
}

func TestEditMessage(t *testing.T) {
	t.Parallel()

	t.Run("should edit an issue comment", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		req := m.jsonCapture("PATCH /repos/acme/app/issues/comments/100", 200, commentJSON(100, "Updated text"))
		got, err := a.EditMessage(t.Context(), "github:acme/app:42", "100", chat.PostableText("Updated text"))
		must.NoError(t, err)
		must.Eq(t, "Updated text", req.jsonBody(t)["body"])
		mustPosted(t, got, "100", "github:acme/app:42", rawIssueComment)
	})

	t.Run("should edit a review comment", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		req := m.jsonCapture("PATCH /repos/acme/app/pulls/comments/200", 200, commentJSON(200, "Updated review"))
		got, err := a.EditMessage(t.Context(), "github:acme/app:42:rc:200", "200", chat.PostableText("Updated review"))
		must.NoError(t, err)
		must.Eq(t, "Updated review", req.jsonBody(t)["body"])
		mustPosted(t, got, "200", "github:acme/app:42:rc:200", rawReviewComment)
	})

	t.Run("should render card messages when editing", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		req := m.jsonCapture("PATCH /repos/acme/app/issues/comments/100", 200, commentJSON(100, "**Updated Card**"))
		card := chat.Card{
			Type:  "card",
			Title: "Updated Card",
			Children: []any{
				chat.CardTextElement{Type: "text", Content: "New content"},
			},
		}
		_, err := a.EditMessage(t.Context(), "github:acme/app:42", "100", card)
		must.NoError(t, err)
		must.StrContains(t, req.jsonBody(t)["body"].(string), "Updated Card")
	})
}

func TestStream(t *testing.T) {
	t.Parallel()

	t.Run("should accumulate text chunks and post once to an issue comment thread", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		req := m.jsonCapture("POST /repos/acme/app/issues/42/comments", 201, commentJSON(500, "Hello World"))
		got, err := a.Stream(t.Context(), "github:acme/app:42", streamChunks(
			chat.MarkdownTextChunk{Text: "Hello"},
			chat.MarkdownTextChunk{Text: " "},
			chat.MarkdownTextChunk{Text: "World"},
		), chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, "Hello World", req.jsonBody(t)["body"])
		mustPosted(t, got, "500", "github:acme/app:42", rawIssueComment)
		must.Eq(t, []string{"POST /repos/acme/app/issues/42/comments"}, m.callsSnapshot())
	})

	t.Run("should accumulate text chunks and post once to a review comment thread", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		req := m.jsonCapture("POST /repos/acme/app/pulls/42/comments/200/replies", 201, commentJSON(501, "Looks good"))
		got, err := a.Stream(t.Context(), "github:acme/app:42:rc:200", streamChunks(
			chat.MarkdownTextChunk{Text: "Looks"},
			chat.MarkdownTextChunk{Text: " "},
			chat.MarkdownTextChunk{Text: "good"},
		), chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, "Looks good", req.jsonBody(t)["body"])
		mustPosted(t, got, "501", "github:acme/app:42:rc:200", rawReviewComment)
		must.Eq(t, []string{"POST /repos/acme/app/pulls/42/comments/200/replies"}, m.callsSnapshot())
	})

	t.Run("should handle StreamChunk objects alongside strings", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		req := m.jsonCapture("POST /repos/acme/app/issues/42/comments", 201, commentJSON(502, "Hello World"))
		got, err := a.Stream(t.Context(), "github:acme/app:42", streamChunks(
			chat.MarkdownTextChunk{Text: "Hello"},
			chat.MarkdownTextChunk{Text: " World"},
			chat.TaskUpdateChunk{ID: "1", Status: chat.TaskComplete},
		), chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, "Hello World", req.jsonBody(t)["body"])
		mustPosted(t, got, "502", "github:acme/app:42", rawIssueComment)
	})

	t.Run("should post empty markdown when stream yields no text", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		m.json("POST /repos/acme/app/issues/42/comments", 201, commentJSON(503, ""))
		got, err := a.Stream(t.Context(), "github:acme/app:42", streamChunks(), chat.StreamOptions{})
		must.NoError(t, err)
		must.Nil(t, got)
		must.Eq(t, 0, len(m.callsSnapshot()))
	})

	t.Run("posts nothing on iterator error", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		m.json("POST /repos/acme/app/issues/42/comments", 201, commentJSON(1, "Hello"))
		boom := errors.New("boom")
		got, err := a.Stream(t.Context(), "github:acme/app:42", func(yield func(chat.StreamChunk, error) bool) {
			if !yield(chat.MarkdownTextChunk{Text: "Hello"}, nil) {
				return
			}
			yield(chat.MarkdownTextChunk{}, boom)
		}, chat.StreamOptions{})
		must.ErrorIs(t, err, boom)
		must.Nil(t, got)
		must.Eq(t, 0, len(m.callsSnapshot()))
	})

	t.Run("posts nothing on canceled ctx", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		m.json("POST /repos/acme/app/issues/42/comments", 201, commentJSON(1, "hi"))
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		got, err := a.Stream(ctx, "github:acme/app:42", streamChunks(chat.MarkdownTextChunk{Text: "hi"}), chat.StreamOptions{})
		must.ErrorIs(t, err, context.Canceled)
		must.Nil(t, got)
		must.Eq(t, 0, len(m.callsSnapshot()))
	})
}

func TestDeleteMessage(t *testing.T) {
	t.Parallel()

	t.Run("should delete an issue comment", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		m.json("DELETE /repos/acme/app/issues/comments/100", 204, "")
		must.NoError(t, a.DeleteMessage(t.Context(), "github:acme/app:42", "100"))
		must.Eq(t, []string{"DELETE /repos/acme/app/issues/comments/100"}, m.callsSnapshot())
	})

	t.Run("should delete a review comment", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		m.json("DELETE /repos/acme/app/pulls/comments/300", 204, "")
		must.NoError(t, a.DeleteMessage(t.Context(), "github:acme/app:42:rc:200", "300"))
		must.Eq(t, []string{"DELETE /repos/acme/app/pulls/comments/300"}, m.callsSnapshot())
	})
}

func TestAddReaction(t *testing.T) {
	t.Parallel()

	t.Run("should add reaction to an issue comment", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		req := m.jsonCapture("POST /repos/acme/app/issues/comments/100/reactions", 201, `{"id":1}`)
		must.NoError(t, a.AddReaction(t.Context(), "github:acme/app:42", "100", chat.EmojiValue{Name: "thumbs_up"}))
		must.Eq(t, "+1", req.jsonBody(t)["content"])
	})

	t.Run("should add reaction to a review comment", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		req := m.jsonCapture("POST /repos/acme/app/pulls/comments/200/reactions", 201, `{"id":1}`)
		must.NoError(t, a.AddReaction(t.Context(), "github:acme/app:42:rc:200", "200", chat.EmojiValue{Name: "heart"}))
		must.Eq(t, "heart", req.jsonBody(t)["content"])
	})

	t.Run("should handle EmojiValue objects", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		req := m.jsonCapture("POST /repos/acme/app/issues/comments/100/reactions", 201, `{"id":1}`)
		must.NoError(t, a.AddReaction(t.Context(), "github:acme/app:42", "100", chat.EmojiValue{Name: "rocket"}))
		must.Eq(t, "rocket", req.jsonBody(t)["content"])
	})
}

func TestRemoveReaction(t *testing.T) {
	t.Parallel()

	t.Run("should remove bot reaction from an issue comment", func(t *testing.T) {
		t.Parallel()
		m := newGHMock(t)
		a, err := New(Config{Token: StaticToken("ghp_t"), WebhookSecret: "whsec", UserName: "test-bot", BotUserID: "777", APIURL: m.srv.URL, HTTPClient: m.srv.Client()})
		must.NoError(t, err)
		m.json("GET /repos/acme/app/issues/comments/100/reactions", 200, `[{"id":50,"content":"+1","user":{"id":777}},{"id":51,"content":"+1","user":{"id":999}}]`)
		m.json("DELETE /repos/acme/app/issues/comments/100/reactions/50", 204, "")
		must.NoError(t, a.RemoveReaction(t.Context(), "github:acme/app:42", "100", chat.EmojiValue{Name: "thumbs_up"}))
		must.Eq(t, []string{
			"GET /repos/acme/app/issues/comments/100/reactions",
			"DELETE /repos/acme/app/issues/comments/100/reactions/50",
		}, m.callsSnapshot())
	})

	t.Run("should remove bot reaction from a review comment", func(t *testing.T) {
		t.Parallel()
		m := newGHMock(t)
		a, err := New(Config{Token: StaticToken("ghp_t"), WebhookSecret: "whsec", UserName: "test-bot", BotUserID: "777", APIURL: m.srv.URL, HTTPClient: m.srv.Client()})
		must.NoError(t, err)
		m.json("GET /repos/acme/app/pulls/comments/200/reactions", 200, `[{"id":60,"content":"heart","user":{"id":777}}]`)
		m.json("DELETE /repos/acme/app/pulls/comments/200/reactions/60", 204, "")
		must.NoError(t, a.RemoveReaction(t.Context(), "github:acme/app:42:rc:200", "200", chat.EmojiValue{Name: "heart"}))
		must.Eq(t, []string{
			"GET /repos/acme/app/pulls/comments/200/reactions",
			"DELETE /repos/acme/app/pulls/comments/200/reactions/60",
		}, m.callsSnapshot())
	})

	t.Run("should do nothing when no matching reaction found", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		m.json("GET /user", 200, `{"id":777,"login":"my-bot","type":"User"}`)
		m.json("GET /repos/acme/app/issues/comments/100/reactions", 200, `[]`)
		must.NoError(t, a.RemoveReaction(t.Context(), "github:acme/app:42", "100", chat.EmojiValue{Name: "thumbs_up"}))
		for _, call := range m.callsSnapshot() {
			must.False(t, len(call) > 6 && call[:6] == "DELETE")
		}
	})

	t.Run("should lazily detect botUserId when not set", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		m.json("GET /user", 200, `{"id":42,"login":"test-bot[bot]"}`)
		m.json("GET /repos/acme/app/issues/comments/100/reactions", 200, `[{"id":70,"content":"eyes","user":{"id":42}},{"id":71,"content":"eyes","user":{"id":999}}]`)
		m.json("DELETE /repos/acme/app/issues/comments/100/reactions/70", 204, "")
		must.NoError(t, a.RemoveReaction(t.Context(), "github:acme/app:42", "100", chat.EmojiValue{Name: "eyes"}))
		must.Eq(t, "42", a.BotUserID())
		must.Eq(t, []string{
			"GET /user",
			"GET /repos/acme/app/issues/comments/100/reactions",
			"DELETE /repos/acme/app/issues/comments/100/reactions/70",
		}, m.callsSnapshot())
	})
}

func TestEmojiToGitHubReaction(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"thumbs_up", "+1"},
		{"+1", "+1"},
		{"thumbs_down", "-1"},
		{"-1", "-1"},
		{"laugh", "laugh"},
		{"smile", "laugh"},
		{"confused", "confused"},
		{"thinking", "confused"},
		{"heart", "heart"},
		{"love_eyes", "heart"},
		{"hooray", "hooray"},
		{"party", "hooray"},
		{"confetti", "hooray"},
		{"rocket", "rocket"},
		{"eyes", "eyes"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("should map %q to %q", tc.in, tc.want), func(t *testing.T) {
			t.Parallel()
			must.Eq(t, tc.want, emojiToReaction(chat.EmojiValue{Name: tc.in}))
		})
	}
	t.Run("should default to +1 for unknown emoji", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "+1", emojiToReaction(chat.EmojiValue{Name: "unknown_emoji"}))
	})
}

func TestFetchMessages(t *testing.T) {
	t.Parallel()

	t.Run("should fetch issue comments for PR-level thread", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		req := m.jsonCapture("GET /repos/acme/app/issues/42/comments", 200, `[
			{"id":100,"body":"First","user":{"id":1,"login":"user1","type":"User"},"created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z","html_url":"https://github.com/acme/app/pull/42#issuecomment-100"},
			{"id":101,"body":"Second","user":{"id":2,"login":"user2","type":"User"},"created_at":"2024-01-02T00:00:00Z","updated_at":"2024-01-02T00:00:00Z","html_url":"https://github.com/acme/app/pull/42#issuecomment-101"}
		]`)
		got, err := a.FetchMessages(t.Context(), "github:acme/app:42", chat.FetchOptions{})
		must.NoError(t, err)
		must.Eq(t, "100", req.query.Get("per_page"))
		must.Eq(t, 2, len(got.Messages))
		must.Eq(t, "100", got.Messages[0].ID)
		must.Eq(t, "101", got.Messages[1].ID)
		must.Eq(t, "", got.NextCursor)
	})

	t.Run("should fetch review comments filtered by thread", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		req := m.jsonCapture("GET /repos/acme/app/pulls/42/comments", 200, `[
			{"id":200,"body":"Root comment","user":{"id":1,"login":"user1","type":"User"},"created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z","html_url":"https://github.com/acme/app/pull/42#discussion_r200","path":"src/index.ts","diff_hunk":"@@","commit_id":"abc"},
			{"id":201,"body":"Reply","in_reply_to_id":200,"user":{"id":2,"login":"user2","type":"User"},"created_at":"2024-01-02T00:00:00Z","updated_at":"2024-01-02T00:00:00Z","html_url":"https://github.com/acme/app/pull/42#discussion_r201","path":"src/index.ts","diff_hunk":"@@","commit_id":"abc"},
			{"id":300,"body":"Different thread","user":{"id":3,"login":"user3","type":"User"},"created_at":"2024-01-03T00:00:00Z","updated_at":"2024-01-03T00:00:00Z","html_url":"https://github.com/acme/app/pull/42#discussion_r300","path":"src/other.ts","diff_hunk":"@@","commit_id":"def"}
		]`)
		got, err := a.FetchMessages(t.Context(), "github:acme/app:42:rc:200", chat.FetchOptions{})
		must.NoError(t, err)
		must.Eq(t, "100", req.query.Get("per_page"))
		must.Eq(t, 2, len(got.Messages))
		must.Eq(t, "200", got.Messages[0].ID)
		must.Eq(t, "201", got.Messages[1].ID)
	})

	t.Run("should respect limit option", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		req := m.jsonCapture("GET /repos/acme/app/issues/42/comments", 200, issueCommentsJSON(10))
		got, err := a.FetchMessages(t.Context(), "github:acme/app:42", chat.FetchOptions{Limit: 3})
		must.NoError(t, err)
		must.Eq(t, "3", req.query.Get("per_page"))
		must.Eq(t, 3, len(got.Messages))
		must.Eq(t, "107", got.Messages[0].ID)
		must.Eq(t, "109", got.Messages[2].ID)
	})

	t.Run("should respect forward direction with limit", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		m.json("GET /repos/acme/app/issues/42/comments", 200, issueCommentsJSON(10))
		got, err := a.FetchMessages(t.Context(), "github:acme/app:42", chat.FetchOptions{Limit: 3, Direction: chat.FetchForward})
		must.NoError(t, err)
		must.Eq(t, 3, len(got.Messages))
		must.Eq(t, "100", got.Messages[0].ID)
		must.Eq(t, "102", got.Messages[2].ID)
	})
}

func TestFetchThread(t *testing.T) {
	t.Parallel()

	t.Run("should fetch PR metadata for thread info", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		m.json("GET /repos/acme/app/pulls/42", 200, `{"title":"Add new feature","state":"open","number":42}`)
		got, err := a.FetchThread(t.Context(), "github:acme/app:42")
		must.NoError(t, err)
		must.Eq(t, "github:acme/app:42", got.ID)
		must.Eq(t, "acme/app", got.ChannelID)
		must.Eq(t, "app #42", got.ChannelName)
		must.Eq(t, map[string]any{
			"owner":    "acme",
			"repo":     "app",
			"prNumber": 42,
			"prTitle":  "Add new feature",
			"prState":  "open",
		}, got.Metadata)
	})

	t.Run("should include reviewCommentId in metadata for review thread", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		m.json("GET /repos/acme/app/pulls/42", 200, `{"title":"Fix bug","state":"open","number":42}`)
		got, err := a.FetchThread(t.Context(), "github:acme/app:42:rc:200")
		must.NoError(t, err)
		must.Eq(t, 200, got.Metadata["reviewCommentId"])
	})

	t.Run("should fetch issue metadata for issue thread", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		m.json("GET /repos/acme/app/issues/10", 200, `{"title":"Bug report","state":"open","number":10}`)
		got, err := a.FetchThread(t.Context(), "github:acme/app:issue:10")
		must.NoError(t, err)
		must.Eq(t, []string{"GET /repos/acme/app/issues/10"}, m.callsSnapshot())
		must.Eq(t, "github:acme/app:issue:10", got.ID)
		must.Eq(t, "acme/app", got.ChannelID)
		must.Eq(t, "app #10", got.ChannelName)
		must.Eq(t, map[string]any{
			"owner":       "acme",
			"repo":        "app",
			"issueNumber": 10,
			"issueTitle":  "Bug report",
			"issueState":  "open",
			"type":        "issue",
		}, got.Metadata)
	})
}

func TestListThreads(t *testing.T) {
	t.Parallel()

	t.Run("should list open PRs as threads", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		req := m.jsonCapture("GET /repos/acme/app/pulls", 200, `[
			{"number":42,"title":"Add feature","body":"Description","state":"open","html_url":"https://github.com/acme/app/pull/42","user":{"id":1,"login":"testuser","type":"User"},"created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-02T00:00:00Z"},
			{"number":43,"title":"Fix bug","body":null,"state":"open","html_url":"https://github.com/acme/app/pull/43","user":{"id":2,"login":"otheruser","type":"User"},"created_at":"2024-01-03T00:00:00Z","updated_at":"2024-01-04T00:00:00Z"}
		]`)
		got, err := a.ListThreads(t.Context(), "github:acme/app", chat.FetchOptions{})
		must.NoError(t, err)
		must.Eq(t, "open", req.query.Get("state"))
		must.Eq(t, "updated", req.query.Get("sort"))
		must.Eq(t, "desc", req.query.Get("direction"))
		must.Eq(t, "30", req.query.Get("per_page"))
		must.Eq(t, "1", req.query.Get("page"))
		must.Eq(t, 2, len(got.Threads))
		must.Eq(t, "github:acme/app:42", got.Threads[0].ID)
		must.Eq(t, "Add feature", got.Threads[0].RootMessage.Text)
		must.Eq(t, "github:acme/app:43", got.Threads[1].ID)
	})

	t.Run("should handle cursor-based pagination", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		req := m.jsonCapture("GET /repos/acme/app/pulls", 200, `[]`)
		_, err := a.ListThreads(t.Context(), "github:acme/app", chat.FetchOptions{Cursor: "3"})
		must.NoError(t, err)
		must.Eq(t, "3", req.query.Get("page"))
	})

	t.Run("should provide nextCursor when results fill the limit", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		m.json("GET /repos/acme/app/pulls", 200, openPullsJSON(5))
		got, err := a.ListThreads(t.Context(), "github:acme/app", chat.FetchOptions{Limit: 5})
		must.NoError(t, err)
		must.Eq(t, "2", got.NextCursor)
	})

	t.Run("should not provide nextCursor when results are fewer than limit", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		m.json("GET /repos/acme/app/pulls", 200, `[{"number":1,"title":"Only PR","body":null,"state":"open","html_url":"https://github.com/acme/app/pull/1","user":{"id":1,"login":"testuser","type":"User"},"created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}]`)
		got, err := a.ListThreads(t.Context(), "github:acme/app", chat.FetchOptions{Limit: 30})
		must.NoError(t, err)
		must.Eq(t, "", got.NextCursor)
	})

	t.Run("should throw for invalid channel ID", func(t *testing.T) {
		t.Parallel()
		a, _ := newMessagingAdapter(t)
		_, err := a.ListThreads(t.Context(), "github:invalid", chat.FetchOptions{})
		must.ErrorContains(t, err, "Invalid GitHub channel ID")
	})

	t.Run("should use PR title as fallback body when body is null", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		m.json("GET /repos/acme/app/pulls", 200, `[{"number":1,"title":"No body PR","body":null,"state":"open","html_url":"https://github.com/acme/app/pull/1","user":{"id":1,"login":"testuser","type":"User"},"created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}]`)
		got, err := a.ListThreads(t.Context(), "github:acme/app", chat.FetchOptions{})
		must.NoError(t, err)
		rm, ok := got.Threads[0].RootMessage.Raw.(RawMessage)
		must.True(t, ok)
		must.Eq(t, "No body PR", rm.Comment.Body)
	})
}

func TestFetchChannelInfo(t *testing.T) {
	t.Parallel()

	t.Run("should return repo metadata as channel info", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		m.json("GET /repos/acme/app", 200, `{"full_name":"acme/app","description":"An app","visibility":"public","default_branch":"main","open_issues_count":5}`)
		got, err := a.FetchChannelInfo(t.Context(), "github:acme/app")
		must.NoError(t, err)
		must.Eq(t, "github:acme/app", got.ID)
		must.Eq(t, "acme/app", got.Name)
		must.False(t, got.IsDM)
		must.Eq(t, map[string]any{
			"owner":           "acme",
			"repo":            "app",
			"description":     "An app",
			"visibility":      "public",
			"defaultBranch":   "main",
			"openIssuesCount": 5,
		}, got.Metadata)
	})

	t.Run("should throw for invalid channel ID", func(t *testing.T) {
		t.Parallel()
		a, _ := newMessagingAdapter(t)
		_, err := a.FetchChannelInfo(t.Context(), "github:noslash")
		must.ErrorContains(t, err, "Invalid GitHub channel ID")
	})
}

func TestStartTyping(t *testing.T) {
	t.Parallel()
	t.Run("should be a no-op", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		must.NoError(t, a.StartTyping(t.Context(), "github:acme/app:42", "", chat.TypingOptions{}))
		must.NoError(t, a.StartTyping(t.Context(), "github:acme/app:42", "thinking...", chat.TypingOptions{}))
		must.Eq(t, 0, len(m.callsSnapshot()))
	})
}

func TestRenderFormatted(t *testing.T) {
	t.Parallel()

	t.Run("should render simple markdown", func(t *testing.T) {
		t.Parallel()
		a, _ := newMessagingAdapter(t)
		node := chat.Root([]ast.Node{
			chat.Paragraph([]ast.Node{chat.Text("Hello world")}),
		})
		must.Eq(t, "Hello world", a.RenderFormatted(node))
	})

	t.Run("should render bold text", func(t *testing.T) {
		t.Parallel()
		a, _ := newMessagingAdapter(t)
		node := chat.Root([]ast.Node{
			chat.Paragraph([]ast.Node{chat.Strong([]ast.Node{chat.Text("bold")})}),
		})
		must.Eq(t, "**bold**", a.RenderFormatted(node))
	})
}

func TestGetUser(t *testing.T) {
	t.Parallel()

	t.Run("should return user info from GitHub API", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		m.json("GET /user/12345", 200, `{"id":12345,"login":"alice","name":"Alice Smith","email":"alice@example.com","avatar_url":"https://avatars.githubusercontent.com/u/12345","type":"User"}`)
		got, err := a.GetUser(t.Context(), "12345")
		must.NoError(t, err)
		must.NotNil(t, got)
		must.Eq(t, "Alice Smith", got.FullName)
		must.Eq(t, "alice", got.UserName)
		must.Eq(t, "alice@example.com", got.Email)
		must.Eq(t, "https://avatars.githubusercontent.com/u/12345", got.AvatarURL)
		must.False(t, got.IsBot)
	})

	t.Run("should return null on error", func(t *testing.T) {
		t.Parallel()
		a, _ := newMessagingAdapter(t)
		got, err := a.GetUser(t.Context(), "999999")
		must.NoError(t, err)
		must.Nil(t, got)
	})

	t.Run("should call GitHub API with correct endpoint and params", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		m.json("GET /user/12345", 200, `{"id":12345,"login":"alice","name":"Alice Smith","email":null,"avatar_url":"https://avatars.githubusercontent.com/u/12345","type":"User"}`)
		_, err := a.GetUser(t.Context(), "12345")
		must.NoError(t, err)
		must.Eq(t, []string{"GET /user/12345"}, m.callsSnapshot())
	})

	t.Run("should return isBot true for Bot type users", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		m.json("GET /user/99999", 200, `{"id":99999,"login":"dependabot[bot]","name":"Dependabot","email":null,"avatar_url":"https://avatars.githubusercontent.com/u/99999","type":"Bot"}`)
		got, err := a.GetUser(t.Context(), "99999")
		must.NoError(t, err)
		must.NotNil(t, got)
		must.True(t, got.IsBot)
	})

	t.Run("should fall back to login when name is null", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		m.json("GET /user/55555", 200, `{"id":55555,"login":"noname-user","name":null,"email":null,"avatar_url":"https://avatars.githubusercontent.com/u/55555","type":"User"}`)
		got, err := a.GetUser(t.Context(), "55555")
		must.NoError(t, err)
		must.NotNil(t, got)
		must.Eq(t, "noname-user", got.FullName)
	})

	t.Run("should include userId in the response", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		m.json("GET /user/12345", 200, `{"id":12345,"login":"alice","name":"Alice Smith","email":"alice@example.com","avatar_url":"https://avatars.githubusercontent.com/u/12345","type":"User"}`)
		got, err := a.GetUser(t.Context(), "12345")
		must.NoError(t, err)
		must.NotNil(t, got)
		must.Eq(t, "12345", got.UserID)
	})
}

func TestFetchSubject(t *testing.T) {
	t.Parallel()

	t.Run("should return issue data from issue_comment raw", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		m.json("GET /repos/vercel/chat/issues/42", 200, `{"number":42,"title":"Add feature","body":"Feature description","state":"open","html_url":"https://github.com/vercel/chat/issues/42","user":{"id":123,"login":"dancer"},"assignees":[{"id":456,"login":"alice"}],"labels":[{"name":"enhancement"}]}`)
		got, err := a.FetchSubject(t.Context(), RawMessage{
			Type:       rawIssueComment,
			Comment:    Comment{ID: 1, Body: "test"},
			Repository: Repository{Name: "chat", FullName: "vercel/chat", Owner: User{Login: "vercel"}},
			Number:     42,
			ThreadType: ThreadTypeIssue,
		})
		must.NoError(t, err)
		must.NotNil(t, got)
		must.Eq(t, "issue", got.Type)
		must.Eq(t, "42", got.ID)
		must.Eq(t, "Add feature", got.Title)
		must.Eq(t, "open", got.Status)
		must.Eq(t, []string{"enhancement"}, got.Labels)
		must.Eq(t, "Feature description", got.Description)
		must.Eq(t, "https://github.com/vercel/chat/issues/42", got.URL)
		must.NotNil(t, got.Author)
		must.Eq(t, "123", got.Author.ID)
		must.Eq(t, "dancer", got.Author.Name)
		must.NotNil(t, got.Assignee)
		must.Eq(t, "456", got.Assignee.ID)
		must.Eq(t, "alice", got.Assignee.Name)
	})

	t.Run("should return pull_request data for PR comments", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		m.json("GET /repos/vercel/chat/pulls/10", 200, `{"number":10,"title":"Fix bug","body":"Bug fix","state":"open","html_url":"https://github.com/vercel/chat/pull/10","user":{"id":123,"login":"dancer"},"assignees":[],"labels":[{"name":"fix"}]}`)
		got, err := a.FetchSubject(t.Context(), RawMessage{
			Type:       rawIssueComment,
			Comment:    Comment{ID: 1, Body: "test"},
			Repository: Repository{Name: "chat", FullName: "vercel/chat", Owner: User{Login: "vercel"}},
			Number:     10,
		})
		must.NoError(t, err)
		must.NotNil(t, got)
		must.Eq(t, "pull_request", got.Type)
		must.Eq(t, "10", got.ID)
		must.Eq(t, "Fix bug", got.Title)
	})

	t.Run("should return pull_request data for review_comment type", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		m.json("GET /repos/vercel/chat/pulls/15", 200, `{"number":15,"title":"Refactor auth","body":"Refactoring","state":"open","html_url":"https://github.com/vercel/chat/pull/15","user":{"id":789,"login":"bob"},"assignees":[],"labels":[]}`)
		got, err := a.FetchSubject(t.Context(), RawMessage{
			Type:       rawReviewComment,
			Comment:    Comment{ID: 1, Body: "nit"},
			Repository: Repository{Name: "chat", FullName: "vercel/chat", Owner: User{Login: "vercel"}},
			Number:     15,
		})
		must.NoError(t, err)
		must.NotNil(t, got)
		must.Eq(t, "pull_request", got.Type)
		must.Eq(t, "15", got.ID)
		must.Eq(t, []string{"GET /repos/vercel/chat/pulls/15"}, m.callsSnapshot())
	})

	t.Run("should return null on API error", func(t *testing.T) {
		t.Parallel()
		a, _ := newMessagingAdapter(t)
		got, err := a.FetchSubject(t.Context(), RawMessage{
			Type:       rawIssueComment,
			Comment:    Comment{ID: 1, Body: "test"},
			Repository: Repository{Name: "chat", FullName: "vercel/chat", Owner: User{Login: "vercel"}},
			Number:     99,
			ThreadType: ThreadTypeIssue,
		})
		must.NoError(t, err)
		must.Nil(t, got)
	})
}

func TestProbe(t *testing.T) {
	t.Parallel()

	t.Run("returns slug and repo count in App mode", func(t *testing.T) {
		t.Parallel()
		m := newGHMock(t)
		m.json("POST /app/installations/99/access_tokens", 201, `{"token":"ghs_1","expires_at":"2099-01-01T00:00:00Z"}`)
		m.json("GET /app", 200, `{"slug":"chatbot"}`)
		req := m.jsonCapture("GET /installation/repositories", 200, `{"total_count":12}`)
		a, err := New(appConfig(t, m))
		must.NoError(t, err)
		slug, repos, err := a.Probe(t.Context())
		must.NoError(t, err)
		must.Eq(t, "chatbot", slug)
		must.Eq(t, 12, repos)
		must.Eq(t, "1", req.query.Get("per_page"))
	})

	t.Run("returns ValidationError in PAT mode without HTTP", func(t *testing.T) {
		t.Parallel()
		a, m := newMessagingAdapter(t)
		slug, repos, err := a.Probe(t.Context())
		must.Eq(t, "", slug)
		must.Eq(t, 0, repos)
		var v *shared.ValidationError
		must.True(t, errors.As(err, &v))
		must.ErrorContains(t, err, "Probe requires GitHub App credentials")
		must.Eq(t, 0, len(m.callsSnapshot()))
	})
}
