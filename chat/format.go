// Ported from packages/chat/src/markdown.ts @ 6adca36 (chat v4.40.0)
// (FormatConverter, MarkdownConverter, BaseFormatConverter).
// Divergences: abstract class → BaseFormatConverter + FormatHooks (FromAst/ToAst);
// FromAst/DefaultNodeToText/RenderList/FromAstWithNodeConverter take src []byte;
// RenderPostable takes any (TS AdapterPostableMessage + runtime throw);
// card element types live in cards.go (one definition each).
package chat

import (
	"fmt"
	"strings"

	"github.com/yuin/goldmark/ast"
)

// FormatConverter is the upstream FormatConverter interface.
type FormatConverter interface {
	ExtractPlainText(platformText string) string
	FromAst(node ast.Node, src []byte) string
	ToAst(platformText string) ast.Node
}

// MarkdownConverter is the deprecated upstream MarkdownConverter interface.
type MarkdownConverter interface {
	FormatConverter
	FromMarkdown(markdown string) string
	ToMarkdown(platformText string) string
	ToPlainText(platformText string) string
}

// FormatHooks are the abstract FromAst/ToAst methods of BaseFormatConverter.
type FormatHooks interface {
	FromAst(node ast.Node, src []byte) string
	ToAst(platformText string) ast.Node
}

// BaseFormatConverter is the upstream abstract base as a concrete walker.
type BaseFormatConverter struct {
	Hooks FormatHooks
}

func (c *BaseFormatConverter) hooks() FormatHooks {
	if c == nil || c.Hooks == nil {
		panic("BaseFormatConverter: Hooks is nil")
	}
	return c.Hooks
}

func (c *BaseFormatConverter) FromAst(node ast.Node, src []byte) string {
	return c.hooks().FromAst(node, src)
}

func (c *BaseFormatConverter) ToAst(platformText string) ast.Node {
	return c.hooks().ToAst(platformText)
}

func (c *BaseFormatConverter) ExtractPlainText(platformText string) string {
	return ToPlainText(c.ToAst(platformText), []byte(platformText))
}

func (c *BaseFormatConverter) FromMarkdown(markdown string) string {
	return c.FromAst(ParseMarkdown(markdown), []byte(markdown))
}

func (c *BaseFormatConverter) ToMarkdown(platformText string) string {
	return StringifyMarkdown(c.ToAst(platformText), []byte(platformText), StringifyOptions{})
}

func (c *BaseFormatConverter) ToPlainText(platformText string) string {
	return c.ExtractPlainText(platformText)
}

func (c *BaseFormatConverter) RenderList(node ast.Node, src []byte, depth int, nodeConverter func(ast.Node) string, unorderedBullet string) string {
	list, ok := node.(*ast.List)
	if !ok {
		return ""
	}
	if unorderedBullet == "" {
		unorderedBullet = "-"
	}
	indent := strings.Repeat("  ", depth)
	start := list.Start
	if start == 0 {
		start = 1
	}
	var lines []string
	for i, item := range GetNodeChildren(list) {
		prefix := unorderedBullet
		if list.IsOrdered() {
			prefix = fmt.Sprintf("%d.", start+i)
		}
		isFirstContent := true
		for _, child := range GetNodeChildren(item) {
			if IsListNode(child) {
				lines = append(lines, c.RenderList(child, src, depth+1, nodeConverter, unorderedBullet))
				continue
			}
			text := nodeConverter(child)
			if strings.TrimSpace(text) == "" {
				continue
			}
			if isFirstContent {
				lines = append(lines, indent+prefix+" "+text)
				isFirstContent = false
			} else {
				lines = append(lines, indent+"  "+text)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func (c *BaseFormatConverter) DefaultNodeToText(node ast.Node, src []byte, nodeConverter func(ast.Node) string) string {
	children := GetNodeChildren(node)
	if len(children) > 0 {
		var b strings.Builder
		for _, child := range children {
			b.WriteString(nodeConverter(child))
		}
		return b.String()
	}
	return GetNodeValue(node, src)
}

func (c *BaseFormatConverter) FromAstWithNodeConverter(node ast.Node, src []byte, nodeConverter func(ast.Node) string) string {
	var parts []string
	for _, child := range GetNodeChildren(node) {
		parts = append(parts, nodeConverter(child))
	}
	return strings.Join(parts, "\n\n")
}

func (c *BaseFormatConverter) RenderPostable(message any) string {
	switch m := message.(type) {
	case string:
		return m
	case PostableText:
		return string(m)
	case PostableRaw:
		return m.Raw
	case PostableMarkdown:
		return c.FromMarkdown(m.Markdown)
	case PostableAst:
		node, _ := m.AST.(ast.Node)
		return c.FromAst(node, nil)
	case PostableCard:
		if m.FallbackText != "" {
			return m.FallbackText
		}
		return c.CardToFallbackText(m.Card)
	case Card:
		return c.CardToFallbackText(m)
	default:
		panic("Invalid PostableMessage format")
	}
}

func (c *BaseFormatConverter) CardToFallbackText(card Card) string {
	var parts []string
	if card.Title != "" {
		parts = append(parts, "**"+card.Title+"**")
	}
	if card.Subtitle != "" {
		parts = append(parts, card.Subtitle)
	}
	for _, child := range card.Children {
		if text := c.CardChildToFallbackText(child); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func (c *BaseFormatConverter) CardChildToFallbackText(child any) string {
	switch ch := child.(type) {
	case CardTextElement:
		return ch.Content
	case FieldsElement:
		lines := make([]string, len(ch.Children))
		for i, f := range ch.Children {
			lines[i] = "**" + f.Label + "**: " + f.Value
		}
		return strings.Join(lines, "\n")
	case ActionsElement:
		return ""
	case TableElement:
		return TableElementToASCII(ch.Headers, ch.Rows)
	case ChartElement:
		return ChartElementToFallbackText(ch)
	case SectionElement:
		var parts []string
		for _, nested := range ch.Children {
			if text := c.CardChildToFallbackText(nested); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}
