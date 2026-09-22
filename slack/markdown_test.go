package slack

import (
	"strings"
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/shoenig/test/must"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
)

func TestSlackFormatConverter(t *testing.T) {
	t.Parallel()
	converter := newSlackFormatConverter()

	t.Run("toMarkdown (mrkdwn -> markdown)", func(t *testing.T) {
		t.Parallel()

		t.Run("preserves code starting immediately after the opening fence", func(t *testing.T) {
			t.Parallel()
			node, src := toAstSrc(converter, "```first line\nsecond line\n```")
			child := node.FirstChild()
			must.True(t, chat.IsCodeNode(child))
			must.Eq(t, "", chat.CodeLanguage(child, src))
			must.Eq(t, "first line\nsecond line", chat.GetNodeValue(child, src))
			must.Eq(t, "first line\nsecond line", chat.ToPlainText(node, src))
		})

		t.Run("parses a code block pasted into a message", func(t *testing.T) {
			t.Parallel()
			// From sample-messages.md: a fence with a first line, mid-message.
			node, src := toAstSrc(converter, "Here you go:\n```first line\nsecond line```")
			must.True(t, chat.IsParagraphNode(node.FirstChild()))
			code := node.FirstChild().NextSibling()
			must.True(t, chat.IsCodeNode(code))
			must.Eq(t, "", chat.CodeLanguage(code, src))
			must.Eq(t, "first line\nsecond line", chat.GetNodeValue(code, src))
		})

		t.Run("does not swallow text after an unpaired fence", func(t *testing.T) {
			t.Parallel()
			node, src := toAstSrc(converter, "use ``` to fence code, *see*?")
			must.Eq(t, 1, node.ChildCount())
			must.True(t, chat.IsParagraphNode(node.FirstChild()))
			must.Eq(t, "use ``` to fence code, see?", chat.ToPlainText(node, src))
		})

		t.Run("keeps a quoted fence inside the blockquote", func(t *testing.T) {
			t.Parallel()
			node, src := toAstSrc(converter, "&gt; a ```c``` b")
			must.Eq(t, 1, node.ChildCount())
			must.True(t, chat.IsBlockquoteNode(node.FirstChild()))
			must.Eq(t, "a c b", chat.ToPlainText(node, src))
		})

		t.Run("keeps trailing text after a code block as a paragraph", func(t *testing.T) {
			t.Parallel()
			node, src := toAstSrc(converter, "```x``` &gt; note")
			must.True(t, chat.IsCodeNode(node.FirstChild()))
			must.Eq(t, "x", chat.GetNodeValue(node.FirstChild(), src))
			must.True(t, chat.IsParagraphNode(node.FirstChild().NextSibling()))
			must.Eq(t, "x\n\n> note", chat.ToPlainText(node, src))
		})

		t.Run("keeps code content verbatim inside the fence", func(t *testing.T) {
			t.Parallel()
			node, src := toAstSrc(converter, "```int *a = *b;```")
			must.True(t, chat.IsCodeNode(node.FirstChild()))
			must.Eq(t, "int *a = *b;", chat.GetNodeValue(node.FirstChild(), src))
		})

		t.Run("should convert bold", func(t *testing.T) {
			t.Parallel()
			must.StrContains(t, converter.ToMarkdown("Hello *world*!"), "**world**")
		})

		t.Run("should convert strikethrough", func(t *testing.T) {
			t.Parallel()
			must.StrContains(t, converter.ToMarkdown("Hello ~world~!"), "~~world~~")
		})

		t.Run("should convert links with text", func(t *testing.T) {
			t.Parallel()
			result := converter.ToMarkdown("Check <https://example.com|this>")
			must.StrContains(t, result, "[this](https://example.com)")
		})

		t.Run("should convert bare links", func(t *testing.T) {
			t.Parallel()
			result := converter.ToMarkdown("Visit <https://example.com>")
			must.StrContains(t, result, "https://example.com")
		})

		t.Run("should convert user mentions", func(t *testing.T) {
			t.Parallel()
			result := converter.ToMarkdown("Hey <@U123|john>!")
			must.StrContains(t, result, "@john")
		})

		t.Run("should convert channel mentions", func(t *testing.T) {
			t.Parallel()
			result := converter.ToMarkdown("Join <#C123|general>")
			must.StrContains(t, result, "#general")
		})

		t.Run("should convert bare channel ID mentions", func(t *testing.T) {
			t.Parallel()
			result := converter.ToMarkdown("Join <#C123>")
			must.StrContains(t, result, "#C123")
		})
	})

	t.Run("toSlackPayload", func(t *testing.T) {
		t.Parallel()

		t.Run("routes plain strings to text (preserves literal markdown chars)", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, slackTextPayload{Text: "Use *foo* literally"}, converter.ToSlackPayload("Use *foo* literally"))
		})

		t.Run("routes raw strings to text", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, slackTextPayload{Text: "*already mrkdwn*"}, converter.ToSlackPayload(chat.PostableRaw{Raw: "*already mrkdwn*"}))
		})

		t.Run("routes markdown to markdown_text", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, slackTextPayload{MarkdownText: "## Heading\n\n- a\n- b"}, converter.ToSlackPayload(chat.PostableMarkdown{Markdown: "## Heading\n\n- a\n- b"}))
		})

		t.Run("routes ast to markdown_text via stringifyMarkdown", func(t *testing.T) {
			t.Parallel()
			root := chat.Root([]ast.Node{
				chat.Paragraph([]ast.Node{
					chat.Strong([]ast.Node{chat.Text("bold")}),
				}),
			})
			result := converter.ToSlackPayload(chat.PostableAst{AST: root})
			must.Eq(t, "", result.Text)
			must.StrContains(t, result.MarkdownText, "**bold**")
		})

		t.Run("preserves tables when rendering ast to markdown_text", func(t *testing.T) {
			t.Parallel()
			result := converter.ToSlackPayload(chat.PostableAst{AST: constructedTableAST()})
			must.Eq(t, "", result.Text)
			must.StrContains(t, result.MarkdownText, "| A | B |")
			must.StrContains(t, result.MarkdownText, "| 1 | 2 |")
		})
	})

	t.Run("toResponseUrlText", func(t *testing.T) {
		t.Parallel()

		t.Run("renders markdown to Slack mrkdwn text", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "*Bold* and <https://example.com|link>", converter.ToResponseUrlText(chat.PostableMarkdown{
				Markdown: "**Bold** and [link](https://example.com)",
			}))
		})

		t.Run("renders markdown tables as ASCII code blocks", func(t *testing.T) {
			t.Parallel()
			must.StrContains(t, converter.ToResponseUrlText(chat.PostableMarkdown{
				Markdown: "| A | B |\n|---|---|\n| 1 | 2 |",
			}), "```\n")
		})
	})

	t.Run("mentions", func(t *testing.T) {
		t.Parallel()

		t.Run("does not double-wrap existing <@U123> mentions in plain strings", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, slackTextPayload{Text: "Hey <@U12345>. Please select"}, converter.ToSlackPayload("Hey <@U12345>. Please select"))
		})

		t.Run("does not double-wrap existing mentions in markdown", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, slackTextPayload{MarkdownText: "Hey <@U12345>. Please select"}, converter.ToSlackPayload(chat.PostableMarkdown{Markdown: "Hey <@U12345>. Please select"}))
		})

		t.Run("rewrites bare @mentions in plain strings", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, slackTextPayload{Text: "Hey <@george>. Please select"}, converter.ToSlackPayload("Hey @george. Please select"))
		})

		t.Run("rewrites bare @mentions in markdown", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, slackTextPayload{MarkdownText: "Hey <@george>. Please select"}, converter.ToSlackPayload(chat.PostableMarkdown{Markdown: "Hey @george. Please select"}))
		})

		t.Run("does not mangle email addresses in plain strings", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, slackTextPayload{Text: "Contact user@example.com for help"}, converter.ToSlackPayload("Contact user@example.com for help"))
		})

		t.Run("does not mangle mailto links", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, slackTextPayload{Text: "Email <mailto:user@example.com>"}, converter.ToSlackPayload("Email <mailto:user@example.com>"))
		})

		t.Run("converts mentions adjacent to non-word punctuation", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, slackTextPayload{Text: "(cc <@george>, <@anne>)"}, converter.ToSlackPayload("(cc @george, @anne)"))
		})

		t.Run("does not mangle @handles inside URL paths in plain strings", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, slackTextPayload{Text: "See https://hackmd.io/@jkyang/B1W69XA-fe"}, converter.ToSlackPayload("See https://hackmd.io/@jkyang/B1W69XA-fe"))
		})

		t.Run("does not mangle @handles inside URL paths in markdown", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, slackTextPayload{MarkdownText: "See https://mastodon.social/@user for updates"}, converter.ToSlackPayload(chat.PostableMarkdown{
				Markdown: "See https://mastodon.social/@user for updates",
			}))
		})

		t.Run("does not mangle @handles inside URL query strings", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, slackTextPayload{Text: "Profile https://example.com/p?user=@george"}, converter.ToSlackPayload("Profile https://example.com/p?user=@george"))
		})

		t.Run("does not mangle @handles inside URL fragments", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, slackTextPayload{Text: "Jump https://example.com/docs#@george"}, converter.ToSlackPayload("Jump https://example.com/docs#@george"))
		})

		t.Run("does not mangle @handles inside Slack links", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, slackTextPayload{Text: "See <https://example.com/p?user=@george|George's profile>"}, converter.ToSlackPayload(
				"See <https://example.com/p?user=@george|George's profile>",
			))
		})

		t.Run("does not mangle @handles inside Markdown links", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, slackTextPayload{MarkdownText: "See [profile](https://example.com/p?user=@george)"}, converter.ToSlackPayload(chat.PostableMarkdown{
				Markdown: "See [profile](https://example.com/p?user=@george)",
			}))
		})

		t.Run("does not link @handles inside inline code", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, slackTextPayload{Text: "Use `@vercel/postgres` and ping <@george>"}, converter.ToSlackPayload("Use `@vercel/postgres` and ping @george"))
		})

		t.Run("does not link @handles inside fenced code", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, slackTextPayload{MarkdownText: "Install:\n```\nnpm install @vercel/postgres\n```\ncc <@george>"}, converter.ToSlackPayload(chat.PostableMarkdown{
				Markdown: "Install:\n```\nnpm install @vercel/postgres\n```\ncc @george",
			}))
		})

		t.Run("does not mangle @handles inside schemeless host paths", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, slackTextPayload{Text: "See hackmd.io/@jkyang/abc"}, converter.ToSlackPayload("See hackmd.io/@jkyang/abc"))
		})

		t.Run("rewrites slash-separated mentions", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, slackTextPayload{Text: "cc <@george>/<@anne>"}, converter.ToSlackPayload("cc @george/@anne"))
		})

		t.Run("rewrites mentions after schemeless host punctuation", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, slackTextPayload{Text: "See example.com,<@george>"}, converter.ToSlackPayload("See example.com,@george"))
		})

		t.Run("still rewrites a real mention that follows a URL", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, slackTextPayload{Text: "See https://hackmd.io/@jkyang/abc cc <@george>"}, converter.ToSlackPayload("See https://hackmd.io/@jkyang/abc cc @george"))
		})

		t.Run("preserves schemeless URL handles in response URL markdown", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "See hackmd.io/@jkyang/abc cc <@george>", converter.ToResponseUrlText(chat.PostableMarkdown{
				Markdown: "See hackmd.io/@jkyang/abc cc @george",
			}))
		})

		t.Run("handles malformed angle text without rescanning it", func(t *testing.T) {
			t.Parallel()
			prefix := strings.Repeat("<", 20_000)
			must.Eq(t, slackTextPayload{Text: prefix + " https://example.com/@jkyang cc <@george>"}, converter.ToSlackPayload(
				prefix+" https://example.com/@jkyang cc @george",
			))
		})
	})

	t.Run("toPlainText", func(t *testing.T) {
		t.Parallel()

		t.Run("should remove bold markers", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "Hello world!", converter.ToPlainText("Hello *world*!"))
		})

		t.Run("should remove italic markers", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "Hello world!", converter.ToPlainText("Hello _world_!"))
		})

		t.Run("should extract link text", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "Check this", converter.ToPlainText("Check <https://example.com|this>"))
		})

		t.Run("should format user mentions", func(t *testing.T) {
			t.Parallel()
			must.StrContains(t, converter.ToPlainText("Hey <@U123>!"), "@U123")
		})

		t.Run("should handle complex messages", func(t *testing.T) {
			t.Parallel()
			input := "*Bold* and _italic_ with <https://x.com|link> and <@U123|user>"
			result := converter.ToPlainText(input)
			must.StrContains(t, result, "Bold")
			must.StrContains(t, result, "italic")
			must.StrContains(t, result, "link")
			must.StrContains(t, result, "user")
			must.False(t, strings.Contains(result, "*"))
			must.False(t, strings.Contains(result, "<"))
		})
	})
}

func toAstSrc(converter *slackFormatConverter, mrkdwn string) (ast.Node, []byte) {
	node := converter.ToAst(mrkdwn)
	return node, []byte(slackMrkdwnToMarkdown(mrkdwn))
}

func constructedTableAST() ast.Node {
	cell := func(value string) *east.TableCell {
		c := east.NewTableCell()
		c.AppendChild(c, chat.Text(value))
		return c
	}
	row := func(values ...string) *east.TableRow {
		r := east.NewTableRow(nil)
		for _, value := range values {
			r.AppendChild(r, cell(value))
		}
		return r
	}
	table := east.NewTable()
	table.AppendChild(table, east.NewTableHeader(row("A", "B")))
	table.AppendChild(table, row("1", "2"))
	return chat.Root([]ast.Node{table})
}
