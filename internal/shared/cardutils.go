// Ported from packages/adapter-shared/src/card-utils.ts @ 6adca36 (chat v4.40.0).
// Divergences: undefined style/platform → empty string; Card is chat.Card
// (struct literal, no JSX); default child fallback is chat.CardChildToFallbackText.
package shared

import (
	"strings"

	"github.com/northpolesec/chat-go/chat"
)

// PlatformName is a supported adapter platform.
type PlatformName string

const (
	PlatformSlack   PlatformName = "slack"
	PlatformGChat   PlatformName = "gchat"
	PlatformTeams   PlatformName = "teams"
	PlatformDiscord PlatformName = "discord"
)

// ButtonStyleMappings maps standard button styles to platform values.
var ButtonStyleMappings = map[PlatformName]map[string]string{
	PlatformSlack:   {"primary": "primary", "danger": "danger"},
	PlatformGChat:   {"primary": "primary", "danger": "danger"},
	PlatformTeams:   {"primary": "positive", "danger": "destructive"},
	PlatformDiscord: {"primary": "primary", "danger": "danger"},
}

// CreateEmojiConverter returns a function that rewrites {{emoji:name}} tokens.
func CreateEmojiConverter(platform PlatformName) func(string) string {
	return func(text string) string {
		return chat.ConvertEmojiPlaceholders(text, string(platform))
	}
}

// MapButtonStyle maps a button style to the platform-specific value.
func MapButtonStyle(style string, platform PlatformName) string {
	if style == "" {
		return ""
	}
	return ButtonStyleMappings[platform][style]
}

// FallbackTextOptions controls card fallback text generation.
type FallbackTextOptions struct {
	BoldFormat string
	LineBreak  string
	Platform   PlatformName
}

// CardToFallbackText generates fallback plain text from a card element.
func CardToFallbackText(card chat.Card, options FallbackTextOptions) string {
	boldFormat := options.BoldFormat
	if boldFormat == "" {
		boldFormat = "*"
	}
	lineBreak := options.LineBreak
	if lineBreak == "" {
		lineBreak = "\n"
	}

	convertText := func(t string) string { return t }
	if options.Platform != "" {
		convertText = CreateEmojiConverter(options.Platform)
	}

	var parts []string
	if card.Title != "" {
		parts = append(parts, boldFormat+convertText(card.Title)+boldFormat)
	}
	if card.Subtitle != "" {
		parts = append(parts, convertText(card.Subtitle))
	}
	for _, child := range card.Children {
		if text := childToFallbackText(child, convertText); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, lineBreak)
}

func childToFallbackText(child any, convertText func(string) string) string {
	switch ch := child.(type) {
	case chat.CardTextElement:
		return convertText(ch.Content)
	case chat.LinkElement:
		return convertText(ch.Label) + " (" + ch.URL + ")"
	case chat.FieldsElement:
		lines := make([]string, len(ch.Children))
		for i, f := range ch.Children {
			lines[i] = convertText(f.Label) + ": " + convertText(f.Value)
		}
		return strings.Join(lines, "\n")
	case chat.ActionsElement:
		return ""
	case chat.SectionElement:
		var nested []string
		for _, c := range ch.Children {
			if text := childToFallbackText(c, convertText); text != "" {
				nested = append(nested, text)
			}
		}
		return strings.Join(nested, "\n")
	case chat.TableElement:
		return chat.TableElementToASCII(ch.Headers, ch.Rows)
	case chat.DividerElement:
		return "---"
	default:
		return chat.CardChildToFallbackText(child)
	}
}

// EscapeTableCell escapes a cell value for a GFM pipe table.
func EscapeTableCell(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `|`, `\|`), "\n", " ")
}

// RenderGfmTable renders a TableElement as GFM markdown table lines.
func RenderGfmTable(table chat.TableElement) []string {
	headers := make([]string, len(table.Headers))
	for i, h := range table.Headers {
		headers[i] = EscapeTableCell(h)
	}
	lines := make([]string, 0, 2+len(table.Rows))
	lines = append(lines, "| "+strings.Join(headers, " | ")+" |")
	seps := make([]string, len(headers))
	for i := range seps {
		seps[i] = "---"
	}
	lines = append(lines, "| "+strings.Join(seps, " | ")+" |")
	for _, row := range table.Rows {
		cells := make([]string, len(row))
		for i, c := range row {
			cells[i] = EscapeTableCell(c)
		}
		lines = append(lines, "| "+strings.Join(cells, " | ")+" |")
	}
	return lines
}
