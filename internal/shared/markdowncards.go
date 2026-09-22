// Ported from packages/adapter-github/src/cards.ts @ 6adca36 (chat v4.40.0).
package shared

import (
	"strings"

	"github.com/northpolesec/chat-go/chat"
)

// CardToMarkdown renders a card as GFM (title/subtitle, fields, links, tables).
func CardToMarkdown(card chat.Card) string {
	var lines []string

	if card.Title != "" {
		lines = append(lines, "**"+escapeMarkdown(card.Title)+"**")
	}
	if card.Subtitle != "" {
		lines = append(lines, escapeMarkdown(card.Subtitle))
	}
	if (card.Title != "" || card.Subtitle != "") && len(card.Children) > 0 {
		lines = append(lines, "")
	}
	if card.ImageURL != "" {
		lines = append(lines, "![]("+card.ImageURL+")", "")
	}

	for i, child := range card.Children {
		childLines := renderChild(child)
		if len(childLines) == 0 {
			continue
		}
		lines = append(lines, childLines...)
		if i < len(card.Children)-1 {
			lines = append(lines, "")
		}
	}
	return strings.Join(lines, "\n")
}

func renderChild(child any) []string {
	switch ch := child.(type) {
	case chat.CardTextElement:
		return renderText(ch)
	case chat.FieldsElement:
		return renderFields(ch)
	case chat.ActionsElement:
		return renderActions(ch)
	case chat.SectionElement:
		var out []string
		for _, nested := range ch.Children {
			out = append(out, renderChild(nested)...)
		}
		return out
	case chat.ImageElement:
		if ch.Alt != "" {
			return []string{"![" + escapeMarkdown(ch.Alt) + "](" + ch.URL + ")"}
		}
		return []string{"![](" + ch.URL + ")"}
	case chat.LinkElement:
		return []string{"[" + escapeMarkdown(ch.Label) + "](" + ch.URL + ")"}
	case chat.DividerElement:
		return []string{"---"}
	case chat.TableElement:
		return RenderGfmTable(ch)
	default:
		if text := chat.CardChildToFallbackText(child); text != "" {
			return []string{text}
		}
		return nil
	}
}

func renderText(text chat.CardTextElement) []string {
	switch text.Style {
	case "bold":
		return []string{"**" + text.Content + "**"}
	case "muted":
		return []string{"_" + text.Content + "_"}
	default:
		return []string{text.Content}
	}
}

func renderFields(fields chat.FieldsElement) []string {
	lines := make([]string, len(fields.Children))
	for i, field := range fields.Children {
		lines[i] = "**" + escapeMarkdown(field.Label) + ":** " + escapeMarkdown(field.Value)
	}
	return lines
}

func renderActions(actions chat.ActionsElement) []string {
	buttonTexts := make([]string, 0, len(actions.Children))
	for _, button := range actions.Children {
		switch b := button.(type) {
		case chat.LinkButtonElement:
			buttonTexts = append(buttonTexts, "["+escapeMarkdown(b.Label)+"]("+b.URL+")")
		case chat.ButtonElement:
			buttonTexts = append(buttonTexts, "**["+escapeMarkdown(b.Label)+"]**")
		}
	}
	return []string{strings.Join(buttonTexts, " • ")}
}

func escapeMarkdown(text string) string {
	text = strings.ReplaceAll(text, `\`, `\\`)
	text = strings.ReplaceAll(text, `*`, `\*`)
	text = strings.ReplaceAll(text, `_`, `\_`)
	text = strings.ReplaceAll(text, `[`, `\[`)
	text = strings.ReplaceAll(text, `]`, `\]`)
	return text
}

// CardToPlainText is the no-markdown fallback (actions omitted).
func CardToPlainText(card chat.Card) string {
	var parts []string
	if card.Title != "" {
		parts = append(parts, card.Title)
	}
	if card.Subtitle != "" {
		parts = append(parts, card.Subtitle)
	}
	for _, child := range card.Children {
		if text := childToPlainText(child); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func childToPlainText(child any) string {
	switch ch := child.(type) {
	case chat.CardTextElement:
		return ch.Content
	case chat.FieldsElement:
		lines := make([]string, len(ch.Children))
		for i, f := range ch.Children {
			lines[i] = f.Label + ": " + f.Value
		}
		return strings.Join(lines, "\n")
	case chat.ActionsElement:
		return ""
	case chat.TableElement:
		return strings.Join(RenderGfmTable(ch), "\n")
	case chat.SectionElement:
		var parts []string
		for _, nested := range ch.Children {
			if text := childToPlainText(nested); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return chat.CardChildToFallbackText(child)
	}
}
