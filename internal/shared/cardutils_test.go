package shared

import (
	"strings"
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/shoenig/test/must"
)

func TestCreateEmojiConverter(t *testing.T) {
	t.Parallel()

	t.Run("creates a Slack emoji converter", func(t *testing.T) {
		t.Parallel()
		convert := CreateEmojiConverter(PlatformSlack)
		must.Eq(t, ":wave: Hello", convert("{{emoji:wave}} Hello"))
		must.Eq(t, ":fire:", convert("{{emoji:fire}}"))
	})

	t.Run("creates a Teams emoji converter", func(t *testing.T) {
		t.Parallel()
		convert := CreateEmojiConverter(PlatformTeams)
		result := convert("{{emoji:wave}} Hello")
		must.True(t, strings.Contains(result, "Hello"))
		must.False(t, strings.Contains(result, "{{emoji:"))
	})

	t.Run("creates a GChat emoji converter", func(t *testing.T) {
		t.Parallel()
		convert := CreateEmojiConverter(PlatformGChat)
		result := convert("{{emoji:wave}} Hello")
		must.True(t, strings.Contains(result, "Hello"))
		must.False(t, strings.Contains(result, "{{emoji:"))
	})

	t.Run("returns text unchanged when no emoji placeholders", func(t *testing.T) {
		t.Parallel()
		convert := CreateEmojiConverter(PlatformSlack)
		must.Eq(t, "Hello world", convert("Hello world"))
	})
}

func TestMapButtonStyle(t *testing.T) {
	t.Parallel()

	t.Run("Slack", func(t *testing.T) {
		t.Parallel()

		t.Run("maps primary to primary", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "primary", MapButtonStyle("primary", PlatformSlack))
		})

		t.Run("maps danger to danger", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "danger", MapButtonStyle("danger", PlatformSlack))
		})

		t.Run("returns undefined for undefined style", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "", MapButtonStyle("", PlatformSlack))
		})
	})

	t.Run("Teams", func(t *testing.T) {
		t.Parallel()

		t.Run("maps primary to positive", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "positive", MapButtonStyle("primary", PlatformTeams))
		})

		t.Run("maps danger to destructive", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "destructive", MapButtonStyle("danger", PlatformTeams))
		})

		t.Run("returns undefined for undefined style", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "", MapButtonStyle("", PlatformTeams))
		})
	})

	t.Run("GChat", func(t *testing.T) {
		t.Parallel()

		t.Run("maps primary to primary", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "primary", MapButtonStyle("primary", PlatformGChat))
		})

		t.Run("maps danger to danger", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "danger", MapButtonStyle("danger", PlatformGChat))
		})
	})
}

func TestButtonStyleMappings(t *testing.T) {
	t.Parallel()

	t.Run("has mappings for all platforms", func(t *testing.T) {
		t.Parallel()
		must.True(t, ButtonStyleMappings[PlatformSlack] != nil)
		must.True(t, ButtonStyleMappings[PlatformTeams] != nil)
		must.True(t, ButtonStyleMappings[PlatformGChat] != nil)
	})

	t.Run("has primary and danger for each platform", func(t *testing.T) {
		t.Parallel()
		for _, platform := range []PlatformName{PlatformSlack, PlatformTeams, PlatformGChat} {
			must.True(t, ButtonStyleMappings[platform]["primary"] != "")
			must.True(t, ButtonStyleMappings[platform]["danger"] != "")
		}
	})
}

func TestCardToFallbackText(t *testing.T) {
	t.Parallel()

	t.Run("formats title with bold", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{Type: "card", Title: "Test Title"}
		must.Eq(t, "*Test Title*", CardToFallbackText(card, FallbackTextOptions{}))
	})

	t.Run("formats title and subtitle", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{Type: "card", Title: "Title", Subtitle: "Subtitle"}
		must.Eq(t, "*Title*\nSubtitle", CardToFallbackText(card, FallbackTextOptions{}))
	})

	t.Run("uses double asterisks for markdown bold format", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{Type: "card", Title: "Title"}
		must.Eq(t, "**Title**", CardToFallbackText(card, FallbackTextOptions{BoldFormat: "**"}))
	})

	t.Run("uses double line breaks when specified", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{Type: "card", Title: "Title", Subtitle: "Subtitle"}
		must.Eq(t, "*Title*\n\nSubtitle", CardToFallbackText(card, FallbackTextOptions{LineBreak: "\n\n"}))
	})

	t.Run("formats text children", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:     "card",
			Title:    "Card",
			Children: []any{chat.CardText("Some content")},
		}
		must.Eq(t, "*Card*\nSome content", CardToFallbackText(card, FallbackTextOptions{}))
	})

	t.Run("formats fields", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.Fields(
					chat.Field("Name", "John"),
					chat.Field("Age", "30"),
				),
			},
		}
		must.Eq(t, "Name: John\nAge: 30", CardToFallbackText(card, FallbackTextOptions{}))
	})

	t.Run("formats fields as label-value pairs", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.Fields(chat.Field("Key", "Value")),
			},
		}
		must.Eq(t, "Key: Value", CardToFallbackText(card, FallbackTextOptions{BoldFormat: "**"}))
	})

	t.Run("excludes actions from fallback text", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.Actions(
					chat.Button("ok", "OK"),
					chat.Button("cancel", "Cancel"),
				),
			},
		}
		must.Eq(t, "", CardToFallbackText(card, FallbackTextOptions{}))
	})

	t.Run("formats dividers as horizontal rules", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:     "card",
			Title:    "Title",
			Children: []any{chat.Divider(), chat.CardText("After divider")},
		}
		must.Eq(t, "*Title*\n---\nAfter divider", CardToFallbackText(card, FallbackTextOptions{}))
	})

	t.Run("converts emoji placeholders when platform specified", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:     "card",
			Title:    "{{emoji:wave}} Welcome",
			Children: []any{chat.CardText("{{emoji:fire}} Hot stuff")},
		}
		result := CardToFallbackText(card, FallbackTextOptions{Platform: PlatformSlack})
		must.Eq(t, "*:wave: Welcome*\n:fire: Hot stuff", result)
	})

	t.Run("leaves emoji placeholders when no platform specified", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{Type: "card", Title: "{{emoji:wave}} Welcome"}
		must.Eq(t, "*{{emoji:wave}} Welcome*", CardToFallbackText(card, FallbackTextOptions{}))
	})

	t.Run("handles complex card with all elements", func(t *testing.T) {
		t.Parallel()
		view := chat.Button("view", "View Order")
		view.Style = "primary"
		cancel := chat.Button("cancel", "Cancel")
		cancel.Style = "danger"
		card := chat.Card{
			Type:     "card",
			Title:    "Order #123",
			Subtitle: "Your order is confirmed",
			Children: []any{
				chat.CardText("Thank you for your purchase!"),
				chat.Divider(),
				chat.Fields(
					chat.Field("Status", "Processing"),
					chat.Field("Total", "$99.99"),
				),
				chat.Actions(view, cancel),
			},
		}
		result := CardToFallbackText(card, FallbackTextOptions{
			BoldFormat: "**",
			LineBreak:  "\n\n",
		})
		must.True(t, strings.Contains(result, "**Order #123**"))
		must.True(t, strings.Contains(result, "Your order is confirmed"))
		must.True(t, strings.Contains(result, "Thank you for your purchase!"))
		must.True(t, strings.Contains(result, "---"))
		must.True(t, strings.Contains(result, "Status: Processing"))
		must.True(t, strings.Contains(result, "Total: $99.99"))
		must.False(t, strings.Contains(result, "[View Order]"))
		must.False(t, strings.Contains(result, "[Cancel]"))
	})

	t.Run("handles empty card", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{Type: "card"}
		must.Eq(t, "", CardToFallbackText(card, FallbackTextOptions{}))
	})

	t.Run("handles card with only children", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:     "card",
			Children: []any{chat.CardText("Just text")},
		}
		must.Eq(t, "Just text", CardToFallbackText(card, FallbackTextOptions{}))
	})
}

func TestEscapeTableCell(t *testing.T) {
	t.Parallel()

	t.Run("escapes pipe characters", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, `a\|b`, EscapeTableCell("a|b"))
	})

	t.Run("escapes multiple pipes", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, `a\|b\|c`, EscapeTableCell("a|b|c"))
	})

	t.Run("escapes backslashes before pipes", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, `a\\\|b`, EscapeTableCell(`a\|b`))
	})

	t.Run("escapes standalone backslashes", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, `a\\b`, EscapeTableCell(`a\b`))
	})

	t.Run("replaces newlines with spaces", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "line1 line2", EscapeTableCell("line1\nline2"))
	})

	t.Run("handles text with no special characters", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "hello", EscapeTableCell("hello"))
	})

	t.Run("handles empty string", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "", EscapeTableCell(""))
	})
}

func TestRenderGfmTable(t *testing.T) {
	t.Parallel()

	t.Run("renders a basic table", func(t *testing.T) {
		t.Parallel()
		table := chat.CardTable([]string{"Name", "Age"}, [][]string{
			{"Alice", "30"},
			{"Bob", "25"},
		})
		must.Eq(t, []string{
			"| Name | Age |",
			"| --- | --- |",
			"| Alice | 30 |",
			"| Bob | 25 |",
		}, RenderGfmTable(table))
	})

	t.Run("escapes pipes in cell values", func(t *testing.T) {
		t.Parallel()
		table := chat.CardTable([]string{"Command", "Description"}, [][]string{
			{"a|b", "pipes|here"},
		})
		must.Eq(t, []string{
			"| Command | Description |",
			"| --- | --- |",
			"| a\\|b | pipes\\|here |",
		}, RenderGfmTable(table))
	})

	t.Run("escapes backslashes in cell values", func(t *testing.T) {
		t.Parallel()
		table := chat.CardTable([]string{"Path"}, [][]string{
			{`C:\Users\test`},
		})
		lines := RenderGfmTable(table)
		must.Eq(t, `| C:\\Users\\test |`, lines[2])
	})

	t.Run("handles empty rows", func(t *testing.T) {
		t.Parallel()
		table := chat.CardTable([]string{"A", "B"}, [][]string{})
		must.Eq(t, []string{"| A | B |", "| --- | --- |"}, RenderGfmTable(table))
	})
}
