package shared

import (
	"testing"

	"github.com/shoenig/test/must"
)

func toToken(text string) string {
	return ReplaceBareMentions(text, func(_, name string) string {
		return "<@" + name + ">"
	})
}

func TestReplaceBareMentions(t *testing.T) {
	t.Parallel()

	t.Run("bare mentions", func(t *testing.T) {
		t.Parallel()

		t.Run("converts a mention at the start of the string", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "<@alice> hi", toToken("@alice hi"))
		})

		t.Run("converts a mention after whitespace", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "hey <@alice>", toToken("hey @alice"))
		})

		t.Run("converts a mention that follows a period", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "read the docs.<@everyone> please", toToken("read the docs.@everyone please"))
		})

		t.Run("converts multiple mentions in one string", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "ping <@one> and <@two>", toToken("ping @one and @two"))
		})

		t.Run("leaves a lone @ with no following word untouched", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "price @ $5", toToken("price @ $5"))
		})
	})

	t.Run("emails and handles", func(t *testing.T) {
		t.Parallel()

		t.Run("does not turn an email address into a mention", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "Contact me at user@example.com", toToken("Contact me at user@example.com"))
		})

		t.Run("leaves word@word handles intact", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "ping support@vercel.com now", toToken("ping support@vercel.com now"))
		})
	})

	t.Run("urls", func(t *testing.T) {
		t.Parallel()

		t.Run("does not mangle an @handle inside an https url", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "see https://github.com/@vercel here", toToken("see https://github.com/@vercel here"))
		})

		t.Run("does not mangle an @handle inside an http url", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "http://example.com/@team", toToken("http://example.com/@team"))
		})

		t.Run("does not mangle an @handle inside a schemeless host path", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "twitter.com/@jack", toToken("twitter.com/@jack"))
		})
	})

	t.Run("code", func(t *testing.T) {
		t.Parallel()

		t.Run("leaves a mention inside an inline code span untouched", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "run `ping @here` now", toToken("run `ping @here` now"))
		})

		t.Run("leaves a mention inside a fenced code block untouched", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "```\nping @here\n```", toToken("```\nping @here\n```"))
		})

		t.Run("still converts a mention outside the code span", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "`code` then <@alice>", toToken("`code` then @alice"))
		})
	})

	t.Run("existing tokens", func(t *testing.T) {
		t.Parallel()

		t.Run("does not double-wrap an already-formatted mention", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "ping <@123> now", toToken("ping <@123> now"))
		})

		t.Run("leaves other angle-bracket tokens untouched", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "see <at>bob</at>", toToken("see <at>bob</at>"))
		})
	})

	t.Run("replacer contract", func(t *testing.T) {
		t.Parallel()

		t.Run("passes the full mention and the bare name", func(t *testing.T) {
			t.Parallel()
			type seen struct {
				Mention string
				Name    string
			}
			var got []seen
			ReplaceBareMentions("hey @alice", func(mention, name string) string {
				got = append(got, seen{Mention: mention, Name: name})
				return mention
			})
			must.Eq(t, []seen{{Mention: "@alice", Name: "alice"}}, got)
		})

		t.Run("uses the replacer's return value verbatim", func(t *testing.T) {
			t.Parallel()
			result := ReplaceBareMentions("hey @alice", func(_, name string) string {
				return "<at>" + name + "</at>"
			})
			must.Eq(t, "hey <at>alice</at>", result)
		})
	})
}
