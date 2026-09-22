// Ported from packages/adapter-github/src/markdown.ts and
// packages/adapter-linear/src/markdown.ts @ v4.40.0 (identical upstream);
// shared by both adapters.
// Divergence: PostableMarkdown is returned verbatim (GitHub speaks markdown
// natively and FromAst is lossy).
package shared

import (
	"strings"

	"github.com/northpolesec/chat-go/chat"
	"github.com/yuin/goldmark/ast"
)

// MarkdownFormatConverter is upstream GitHubFormatConverter: GFM in, GFM out.
type MarkdownFormatConverter struct {
	chat.BaseFormatConverter
}

func NewMarkdownFormatConverter() *MarkdownFormatConverter {
	c := &MarkdownFormatConverter{}
	c.Hooks = c
	return c
}

// FromAst renders an AST back to markdown. chat.StringifyMarkdown emits
// inline syntax only (no headings, lists, or fences); a tree from
// ParseMarkdown needs the original source bytes (nil src panics).
func (c *MarkdownFormatConverter) FromAst(node ast.Node, src []byte) string {
	return strings.TrimSpace(chat.StringifyMarkdown(node, src, chat.StringifyOptions{}))
}

// ToAst parses GFM.
func (c *MarkdownFormatConverter) ToAst(markdown string) ast.Node {
	return chat.ParseMarkdown(markdown)
}

// RenderPostable is upstream renderPostable with one Go divergence: a
// PostableMarkdown is returned verbatim rather than round-tripped through
// the AST (GitHub speaks markdown natively and FromAst is lossy).
func (c *MarkdownFormatConverter) RenderPostable(message any) string {
	switch m := message.(type) {
	case string:
		return m
	case chat.PostableText:
		return string(m)
	case chat.PostableRaw:
		return m.Raw
	case chat.PostableMarkdown:
		return m.Markdown
	case chat.PostableAst:
		if node, ok := m.AST.(ast.Node); ok {
			return c.FromAst(node, nil)
		}
	}
	if card := ExtractCard(message); card != nil {
		return CardToMarkdown(*card)
	}
	return c.BaseFormatConverter.RenderPostable(message)
}
