// Ported from packages/adapter-slack/src/markdown.ts @ 6adca36 (chat v4.40.0).
// replaceBareMentions lives in internal/shared/mentions.go (adapter-shared).
// slackMrkdwnToMarkdown lives in format.go, matching upstream format/index.ts.
// Divergences: SlackFormatConverter is unexported until Task 30; AdapterPostableMessage
// string variant is accepted as string or chat.PostableText; goldmark ToAst source is
// the converted markdown (ToMarkdown/ToPlainText override BaseFormatConverter so
// segments read the rewritten bytes); JS lookbehind emphasis rewrites are a byte walk
// (RE2 has no lookbehind); mention/fence scanners use byte indices (ASCII fixtures).
package slack

import (
	"strings"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/yuin/goldmark/ast"
)

// slackTextPayload is the Slack API text vs markdown_text union.
type slackTextPayload struct {
	Text         string
	MarkdownText string
}

// slackFormatConverter is the upstream SlackFormatConverter.
type slackFormatConverter struct {
	chat.BaseFormatConverter
}

func newSlackFormatConverter() *slackFormatConverter {
	c := &slackFormatConverter{}
	c.Hooks = c
	return c
}

func (c *slackFormatConverter) FromAst(node ast.Node, src []byte) string {
	return chat.StringifyMarkdown(node, src, chat.StringifyOptions{})
}

func (c *slackFormatConverter) ToAst(mrkdwn string) ast.Node {
	return chat.ParseMarkdown(slackMrkdwnToMarkdown(mrkdwn))
}

func (c *slackFormatConverter) ToMarkdown(platformText string) string {
	md := slackMrkdwnToMarkdown(platformText)
	return chat.StringifyMarkdown(chat.ParseMarkdown(md), []byte(md), chat.StringifyOptions{})
}

func (c *slackFormatConverter) ToPlainText(platformText string) string {
	md := slackMrkdwnToMarkdown(platformText)
	return chat.ToPlainText(chat.ParseMarkdown(md), []byte(md))
}

func (c *slackFormatConverter) ExtractPlainText(platformText string) string {
	return c.ToPlainText(platformText)
}

func (c *slackFormatConverter) ToSlackPayload(message any) slackTextPayload {
	switch m := message.(type) {
	case string:
		return slackTextPayload{Text: c.finalize(m)}
	case chat.PostableText:
		return slackTextPayload{Text: c.finalize(string(m))}
	case chat.PostableRaw:
		return slackTextPayload{Text: c.finalize(m.Raw)}
	case chat.PostableMarkdown:
		return slackTextPayload{MarkdownText: c.finalize(m.Markdown)}
	case chat.PostableAst:
		node, _ := m.AST.(ast.Node)
		return slackTextPayload{MarkdownText: c.finalize(chat.StringifyMarkdown(node, nil, chat.StringifyOptions{}))}
	default:
		return slackTextPayload{Text: ""}
	}
}

func (c *slackFormatConverter) ToResponseUrlText(message any) string {
	switch m := message.(type) {
	case string:
		return c.finalize(m)
	case chat.PostableText:
		return c.finalize(string(m))
	case chat.PostableRaw:
		return c.finalize(m.Raw)
	case chat.PostableMarkdown:
		return chat.ConvertEmojiPlaceholders(c.astToMrkdwn(chat.ParseMarkdown(m.Markdown), []byte(m.Markdown)), "slack")
	case chat.PostableAst:
		node, _ := m.AST.(ast.Node)
		return chat.ConvertEmojiPlaceholders(c.astToMrkdwn(node, nil), "slack")
	default:
		return ""
	}
}

func (c *slackFormatConverter) finalize(text string) string {
	return chat.ConvertEmojiPlaceholders(linkBareMentionNames(text), "slack")
}

func (c *slackFormatConverter) astToMrkdwn(node ast.Node, src []byte) string {
	return c.FromAstWithNodeConverter(node, src, func(child ast.Node) string {
		return c.nodeToMrkdwn(child, src)
	})
}

func (c *slackFormatConverter) childrenMrkdwn(node ast.Node, src []byte) string {
	var b strings.Builder
	for _, child := range chat.GetNodeChildren(node) {
		b.WriteString(c.nodeToMrkdwn(child, src))
	}
	return b.String()
}

func (c *slackFormatConverter) nodeToMrkdwn(node ast.Node, src []byte) string {
	if chat.IsParagraphNode(node) {
		return c.childrenMrkdwn(node, src)
	}
	if chat.IsTextNode(node) {
		return linkBareMentionNames(textNodeValue(node, src))
	}
	if chat.IsStrongNode(node) {
		return "*" + c.childrenMrkdwn(node, src) + "*"
	}
	if chat.IsEmphasisNode(node) {
		return "_" + c.childrenMrkdwn(node, src) + "_"
	}
	if chat.IsDeleteNode(node) {
		return "~" + c.childrenMrkdwn(node, src) + "~"
	}
	if chat.IsInlineCodeNode(node) {
		return "`" + chat.GetNodeValue(node, src) + "`"
	}
	if chat.IsCodeNode(node) {
		return "```" + chat.CodeLanguage(node, src) + "\n" + chat.GetNodeValue(node, src) + "\n```"
	}
	if link, ok := node.(*ast.Link); ok {
		return "<" + string(link.Destination) + "|" + c.childrenMrkdwn(node, src) + ">"
	}
	if chat.IsBlockquoteNode(node) {
		var lines []string
		for _, child := range chat.GetNodeChildren(node) {
			lines = append(lines, "> "+c.nodeToMrkdwn(child, src))
		}
		return strings.Join(lines, "\n")
	}
	if chat.IsListNode(node) {
		return c.RenderList(node, src, 0, func(child ast.Node) string {
			return c.nodeToMrkdwn(child, src)
		}, "•")
	}
	if node.Kind() == ast.KindThematicBreak {
		return "---"
	}
	if al, ok := node.(*ast.AutoLink); ok {
		return string(al.URL(src))
	}
	if chat.IsTableNode(node) {
		return "```\n" + chat.TableToASCII(node, src) + "\n```"
	}
	return c.DefaultNodeToText(node, src, func(child ast.Node) string {
		return c.nodeToMrkdwn(child, src)
	})
}

func textNodeValue(node ast.Node, src []byte) string {
	switch t := node.(type) {
	case *ast.Text:
		v := chat.GetNodeValue(node, src)
		if t.SoftLineBreak() || t.HardLineBreak() {
			return v + "\n"
		}
		return v
	default:
		return chat.GetNodeValue(node, src)
	}
}

func linkBareMentionNames(text string) string {
	return shared.ReplaceBareMentions(text, func(_, name string) string {
		return "<@" + name + ">"
	})
}
