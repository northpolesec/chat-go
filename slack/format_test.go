package slack

import (
	"strings"
	"testing"
	"time"

	"github.com/shoenig/test/must"
)

func didPanic(fn func()) bool {
	panicked := false
	func() {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		fn()
	}()
	return panicked
}

func TestSlackFormatPrimitives(t *testing.T) {
	t.Parallel()

	t.Run("escapes Slack mrkdwn control characters", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "a &amp; &lt;b&gt;", escapeSlackText("a & <b>"))
	})

	t.Run("unescapes Slack mrkdwn control characters", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "a & <b>", unescapeSlackText("a &amp; &lt;b&gt;"))
	})

	t.Run("creates plain_text objects", func(t *testing.T) {
		t.Parallel()
		emoji := true
		must.Eq(t, slackPlainTextObject{
			Emoji: &emoji,
			Text:  "hello",
			Type:  "plain_text",
		}, createSlackPlainText("hello", slackTextOptions{Emoji: &emoji}))
	})

	t.Run("rejects invalid text object lengths", func(t *testing.T) {
		t.Parallel()
		must.True(t, didPanic(func() { createSlackPlainText("", slackTextOptions{}) }))
		must.True(t, didPanic(func() { createSlackMrkdwn(strings.Repeat("x", 3001), slackTextOptions{}) }))
	})

	t.Run("creates mrkdwn objects", func(t *testing.T) {
		t.Parallel()
		verbatim := true
		must.Eq(t, slackMrkdwnTextObject{
			Text:     "*hello*",
			Type:     "mrkdwn",
			Verbatim: &verbatim,
		}, createSlackMrkdwn("*hello*", slackTextOptions{Verbatim: &verbatim}))
	})

	t.Run("formats Slack user mentions", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "<@U123>", formatSlackUser("U123"))
	})

	t.Run("formats Slack channel mentions", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "<#C123>", formatSlackChannel("C123"))
	})

	t.Run("formats Slack user group mentions", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "<!subteam^S123>", formatSlackUserGroup("S123"))
	})

	t.Run("formats Slack special mentions", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "<!here>", formatSlackSpecialMention("here"))
	})

	t.Run("formats Slack links", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "<https://example.com?a=1&b=2>", formatSlackLink("https://example.com?a=1&b=2", ""))
		must.Eq(t, "<https://example.com|read &lt;this&gt;>", formatSlackLink("https://example.com", "read <this>"))
	})

	t.Run("rejects unsafe Slack link control characters", func(t *testing.T) {
		t.Parallel()
		must.True(t, didPanic(func() { formatSlackLink("https://example.com|bad", "") }))
	})

	t.Run("formats Slack dates", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "<!date^1710000000^{date_short}|Mar 9>", formatSlackDate(1_710_000_000, "{date_short}", "Mar 9", slackDateOptions{}))
		must.Eq(t, "<!date^1710000000^{time}^https://example.com|4pm>", formatSlackDate(
			time.Date(2024, time.March, 9, 16, 0, 0, 0, time.UTC),
			"{time}",
			"4pm",
			slackDateOptions{Link: "https://example.com"},
		))
	})

	t.Run("normalizes Slack mrkdwn to Markdown", func(t *testing.T) {
		t.Parallel()
		must.Eq(t,
			"Hey @jane in #general (C123), see [this](https://example.com) and **bold** ~~done~~",
			slackMrkdwnToMarkdown("Hey <@U123|jane> in <#C123|general>, see <https://example.com|this> and *bold* ~done~"),
		)
	})

	t.Run("normalizes Slack code fences for CommonMark parsing", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "```\nfirst line\nsecond line\n```", slackMrkdwnToMarkdown("```first line\nsecond line\n```"))
	})

	t.Run("puts Slack code fences on separate lines from surrounding text", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "before \n```\ncode\n```\n after", slackMrkdwnToMarkdown("before ```code``` after"))
	})

	t.Run("keeps an unpaired ``` as literal text", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "use ``` to fence code, **see**?", slackMrkdwnToMarkdown("use ``` to fence code, *see*?"))
	})

	t.Run("keeps a ``` inside an inline code span as literal text", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "`use ``` here`", slackMrkdwnToMarkdown("`use ``` here`"))
	})

	t.Run("keeps a ``` on a blockquote line as literal text", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "> a ```c``` b", slackMrkdwnToMarkdown("&gt; a ```c``` b"))
	})

	t.Run("keeps a ``` inside a link token as part of the label", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "[```code```](https://x.com)", slackMrkdwnToMarkdown("<https://x.com|```code```>"))
	})

	t.Run("does not rewrite emphasis inside fenced code", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "```\nint *a = *b;\n```", slackMrkdwnToMarkdown("```int *a = *b;```"))
		must.Eq(t, "```\nkeep ~x~ raw\n```", slackMrkdwnToMarkdown("```keep ~x~ raw```"))
	})

	t.Run("still resolves mention tokens inside fenced code", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "```\nping @jane\n```", slackMrkdwnToMarkdown("```ping <@U123|jane>```"))
	})

	t.Run("escapes trailing text that would become a block construct", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "```\nx\n```\n \\> note", slackMrkdwnToMarkdown("```x``` &gt; note"))
		must.Eq(t, "```\nx\n```\n \\# heading", slackMrkdwnToMarkdown("```x``` # heading"))
		must.Eq(t, "```\nx\n```\n \\- item", slackMrkdwnToMarkdown("```x``` - item"))
		must.Eq(t, "```\nx\n```\n 1\\. item", slackMrkdwnToMarkdown("```x``` 1. item"))
	})

	t.Run("collapses trailing indentation that would become indented code", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "see \n```\nx\n```\n result is 5", slackMrkdwnToMarkdown("see ```x```\tresult is 5"))
		must.Eq(t, "see \n```\nx\n```\n result is 5", slackMrkdwnToMarkdown("see ```x```     result is 5"))
	})

	t.Run("preserves the channel ID for labeled channel tokens", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Post in #general (C042BLND6R6)", slackMrkdwnToMarkdown("Post in <#C042BLND6R6|general>"))
		must.Eq(t, "Post in #C042BLND6R6", slackMrkdwnToMarkdown("Post in <#C042BLND6R6>"))
	})

	t.Run("normalizes bare Slack links to Markdown URLs", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "See https://example.com", slackMrkdwnToMarkdown("See <https://example.com>"))
	})

	t.Run("normalizes inverted Slack link tokens before Markdown conversion", func(t *testing.T) {
		t.Parallel()
		must.Eq(t,
			"See [docs](https://example.com) and [A](https://a.com)",
			slackMrkdwnToMarkdown("See <docs|https://example.com> and <https://a.com|A>"),
		)
	})

	t.Run("does not invert links whose display label is itself a URL", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "See [https://b.com](https://a.com)", slackMrkdwnToMarkdown("See <https://a.com|https://b.com>"))
	})

	t.Run("converts basic Markdown bold to Slack mrkdwn bold", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "The *domain* is example.com", markdownBoldToSlackMrkdwn("The **domain** is example.com"))
	})

	t.Run("links bare mention-like tokens without touching emails", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "(cc <@U123>, <@U456>)", linkBareSlackMentions("(cc @U123, @U456)"))
		must.Eq(t, "@george", linkBareSlackMentions("@george"))
		must.Eq(t, "user@example.com", linkBareSlackMentions("user@example.com"))
	})
}
