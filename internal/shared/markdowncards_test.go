// Ported from packages/adapter-github/src/cards.test.ts @ 6adca36 (chat v4.40.0).
package shared

import (
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/shoenig/test/must"
)

func TestCardToMarkdown(t *testing.T) {
	t.Parallel()

	t.Run("should render a simple card with title", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{Type: "card", Title: "Hello World", Children: []any{}}
		must.Eq(t, "**Hello World**", CardToMarkdown(card))
	})

	t.Run("should render card with title and subtitle", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:     "card",
			Title:    "Order #1234",
			Subtitle: "Status update",
			Children: []any{},
		}
		must.Eq(t, "**Order #1234**\nStatus update", CardToMarkdown(card))
	})

	t.Run("should render card with text content", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:  "card",
			Title: "Notification",
			Children: []any{
				chat.CardTextElement{Type: "text", Content: "Your order has been shipped!"},
			},
		}
		must.Eq(t, "**Notification**\n\nYour order has been shipped!", CardToMarkdown(card))
	})

	t.Run("should render card with fields", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:  "card",
			Title: "Order Details",
			Children: []any{
				chat.FieldsElement{
					Type: "fields",
					Children: []chat.FieldElement{
						{Type: "field", Label: "Order ID", Value: "12345"},
						{Type: "field", Label: "Status", Value: "Shipped"},
					},
				},
			},
		}
		result := CardToMarkdown(card)
		must.StrContains(t, result, "**Order ID:** 12345")
		must.StrContains(t, result, "**Status:** Shipped")
	})

	t.Run("should render card with link buttons", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:  "card",
			Title: "Actions",
			Children: []any{
				chat.ActionsElement{
					Type: "actions",
					Children: []any{
						chat.LinkButtonElement{
							Type:  "link-button",
							URL:   "https://example.com/track",
							Label: "Track Order",
						},
						chat.LinkButtonElement{
							Type:  "link-button",
							URL:   "https://example.com/help",
							Label: "Get Help",
						},
					},
				},
			},
		}
		result := CardToMarkdown(card)
		must.StrContains(t, result, "[Track Order](https://example.com/track)")
		must.StrContains(t, result, "[Get Help](https://example.com/help)")
	})

	t.Run("should render card with action buttons as bold text", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:  "card",
			Title: "Approve?",
			Children: []any{
				chat.ActionsElement{
					Type: "actions",
					Children: []any{
						chat.ButtonElement{Type: "button", ID: "approve", Label: "Approve", Style: "primary"},
						chat.ButtonElement{Type: "button", ID: "reject", Label: "Reject", Style: "danger"},
					},
				},
			},
		}
		result := CardToMarkdown(card)
		must.StrContains(t, result, "**[Approve]**")
		must.StrContains(t, result, "**[Reject]**")
	})

	t.Run("should render card with image", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:  "card",
			Title: "Image Card",
			Children: []any{
				chat.ImageElement{Type: "image", URL: "https://example.com/image.png", Alt: "Example image"},
			},
		}
		must.StrContains(t, CardToMarkdown(card), "![Example image](https://example.com/image.png)")
	})

	t.Run("should render card with divider", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.CardTextElement{Type: "text", Content: "Before"},
				chat.DividerElement{Type: "divider"},
				chat.CardTextElement{Type: "text", Content: "After"},
			},
		}
		must.StrContains(t, CardToMarkdown(card), "---")
	})

	t.Run("should render card with section", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.SectionElement{
					Type:     "section",
					Children: []any{chat.CardTextElement{Type: "text", Content: "Section content"}},
				},
			},
		}
		must.StrContains(t, CardToMarkdown(card), "Section content")
	})

	t.Run("should handle text with different styles", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.CardTextElement{Type: "text", Content: "Normal text"},
				chat.CardTextElement{Type: "text", Content: "Bold text", Style: "bold"},
				chat.CardTextElement{Type: "text", Content: "Muted text", Style: "muted"},
			},
		}
		result := CardToMarkdown(card)
		must.StrContains(t, result, "Normal text")
		must.StrContains(t, result, "**Bold text**")
		must.StrContains(t, result, "_Muted text_")
	})

	t.Run("should escape markdown in title", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{Type: "card", Title: `\*_[]`, Children: []any{}}
		must.Eq(t, `**\\\*\_\[\]**`, CardToMarkdown(card))
	})

	t.Run("should render card with table", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.TableElement{
					Type:    "table",
					Headers: []string{"Name", "Qty"},
					Rows:    [][]string{{"Widget", "2"}},
				},
			},
		}
		result := CardToMarkdown(card)
		must.StrContains(t, result, "| Name | Qty |")
		must.StrContains(t, result, "| --- | --- |")
		must.StrContains(t, result, "| Widget | 2 |")
	})
}

func TestCardToPlainText(t *testing.T) {
	t.Parallel()

	t.Run("should generate plain text from card", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:     "card",
			Title:    "Hello",
			Subtitle: "World",
			Children: []any{
				chat.CardTextElement{Type: "text", Content: "Some content"},
				chat.FieldsElement{
					Type:     "fields",
					Children: []chat.FieldElement{{Type: "field", Label: "Key", Value: "Value"}},
				},
			},
		}
		result := CardToPlainText(card)
		must.StrContains(t, result, "Hello")
		must.StrContains(t, result, "World")
		must.StrContains(t, result, "Some content")
		must.StrContains(t, result, "Key: Value")
	})

	t.Run("should exclude actions from plain text", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:  "card",
			Title: "Hello",
			Children: []any{
				chat.CardTextElement{Type: "text", Content: "Some content"},
				chat.ActionsElement{
					Type:     "actions",
					Children: []any{chat.ButtonElement{Type: "button", ID: "go", Label: "ClickMe"}},
				},
			},
		}
		result := CardToPlainText(card)
		must.StrContains(t, result, "Hello")
		must.StrContains(t, result, "Some content")
		must.StrNotContains(t, result, "ClickMe")
	})
}

func TestCardToMarkdownWithCardLink(t *testing.T) {
	t.Parallel()

	t.Run("renders CardLink as markdown link", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:     "card",
			Children: []any{chat.CardLink("https://example.com", "Click here")},
		}
		must.Eq(t, "[Click here](https://example.com)", CardToMarkdown(card))
	})
}
