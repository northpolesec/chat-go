// Ported from packages/adapter-github/src/markdown.test.ts @ 6adca36 (chat v4.40.0).
// Go divergence: PostableMarkdown is returned verbatim (FromAst is lossy).
package shared

import (
	"regexp"
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/shoenig/test/must"
	"github.com/yuin/goldmark/ast"
)

var testBotMentionWithWhitespace = regexp.MustCompile(`@test-bot\s+hi there`)

func TestGitHubFormatConverter(t *testing.T) {
	t.Parallel()
	c := NewMarkdownFormatConverter()

	t.Run("toAst", func(t *testing.T) {
		t.Parallel()

		t.Run("should parse plain text", func(t *testing.T) {
			t.Parallel()
			node := c.ToAst("Hello world")
			must.Eq(t, ast.KindDocument, node.Kind())
			must.Eq(t, 1, node.ChildCount())
		})

		t.Run("should parse bold text", func(t *testing.T) {
			t.Parallel()
			node := c.ToAst("**bold text**")
			must.Eq(t, ast.KindDocument, node.Kind())
			must.True(t, chat.IsParagraphNode(node.FirstChild()))
		})

		t.Run("should parse @mentions", func(t *testing.T) {
			t.Parallel()
			_ = c.ToAst("Hey @username, check this out")
			text := c.ExtractPlainText("Hey @username, check this out")
			must.StrContains(t, text, "@username")
		})

		t.Run("should parse code blocks", func(t *testing.T) {
			t.Parallel()
			node := c.ToAst("```javascript\nconsole.log('hello');\n```")
			must.Eq(t, ast.KindDocument, node.Kind())
		})

		t.Run("should parse links", func(t *testing.T) {
			t.Parallel()
			node := c.ToAst("[link text](https://example.com)")
			must.Eq(t, ast.KindDocument, node.Kind())
		})

		t.Run("should parse strikethrough", func(t *testing.T) {
			t.Parallel()
			node := c.ToAst("~~deleted~~")
			must.Eq(t, ast.KindDocument, node.Kind())
		})
	})

	t.Run("fromAst", func(t *testing.T) {
		t.Parallel()

		t.Run("should render plain text", func(t *testing.T) {
			t.Parallel()
			node := chat.Root([]ast.Node{
				chat.Paragraph([]ast.Node{chat.Text("Hello world")}),
			})
			must.Eq(t, "Hello world", c.FromAst(node, nil))
		})

		t.Run("should render bold text", func(t *testing.T) {
			t.Parallel()
			node := chat.Root([]ast.Node{
				chat.Paragraph([]ast.Node{chat.Strong([]ast.Node{chat.Text("bold")})}),
			})
			must.Eq(t, "**bold**", c.FromAst(node, nil))
		})

		t.Run("should render italic text", func(t *testing.T) {
			t.Parallel()
			node := chat.Root([]ast.Node{
				chat.Paragraph([]ast.Node{chat.Emphasis([]ast.Node{chat.Text("italic")})}),
			})
			must.Eq(t, "*italic*", c.FromAst(node, nil))
		})
	})

	t.Run("extractPlainText", func(t *testing.T) {
		t.Parallel()

		t.Run("should extract text from markdown", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "bold and italic", c.ExtractPlainText("**bold** and _italic_"))
		})

		t.Run("should preserve @mentions", func(t *testing.T) {
			t.Parallel()
			result := c.ExtractPlainText("Hey @user, **thanks**!")
			must.StrContains(t, result, "@user")
			must.StrContains(t, result, "thanks")
		})

		t.Run("should preserve whitespace after newline-separated @mentions", func(t *testing.T) {
			t.Parallel()
			must.RegexMatch(t, testBotMentionWithWhitespace, c.ExtractPlainText("@test-bot\nhi there"))
		})

		t.Run("should extract text from code blocks", func(t *testing.T) {
			t.Parallel()
			must.StrContains(t, c.ExtractPlainText("```\ncode\n```"), "code")
		})
	})

	t.Run("renderPostable", func(t *testing.T) {
		t.Parallel()

		t.Run("should render string directly", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "Hello world", c.RenderPostable("Hello world"))
		})

		t.Run("should render raw message", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "Raw content", c.RenderPostable(chat.PostableRaw{Raw: "Raw content"}))
		})

		t.Run("should render markdown message", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "**bold**", c.RenderPostable(chat.PostableMarkdown{Markdown: "**bold**"}))
		})

		t.Run("should render ast message", func(t *testing.T) {
			t.Parallel()
			node := chat.Root([]ast.Node{
				chat.Paragraph([]ast.Node{chat.Text("AST content")}),
			})
			must.Eq(t, "AST content", c.RenderPostable(chat.PostableAst{AST: node}))
		})

		t.Run("should render PostableText verbatim", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "Hello world", c.RenderPostable(chat.PostableText("Hello world")))
		})

		t.Run("should return PostableMarkdown verbatim", func(t *testing.T) {
			t.Parallel()
			md := "# Title\n\n- a\n- b\n\n```go\nx := 1\n```"
			must.Eq(t, md, c.RenderPostable(chat.PostableMarkdown{Markdown: md}))
		})

		t.Run("should panic on unsupported type", func(t *testing.T) {
			t.Parallel()
			must.Panic(t, func() {
				c.RenderPostable(42)
			})
		})
	})

	t.Run("roundtrip", func(t *testing.T) {
		t.Parallel()

		t.Run("should roundtrip simple text", func(t *testing.T) {
			t.Parallel()
			original := "Hello world"
			node := c.ToAst(original)
			// ParseMarkdown trees need the original bytes; nil src panics in goldmark.
			must.Eq(t, original, c.FromAst(node, []byte(original)))
		})

		t.Run("should roundtrip markdown with formatting", func(t *testing.T) {
			t.Parallel()
			original := "**bold** and *italic*"
			node := c.ToAst(original)
			result := c.FromAst(node, []byte(original))
			must.StrContains(t, result, "bold")
			must.StrContains(t, result, "italic")
		})
	})
}
