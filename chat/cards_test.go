package chat

import (
	"testing"

	"github.com/shoenig/test/must"
)

var (
	_ AdapterPostableMessage = Card{}
	_ CardChild              = CardTextElement{}
	_ CardChild              = ImageElement{}
	_ CardChild              = DividerElement{}
	_ CardChild              = ButtonElement{}
	_ CardChild              = LinkButtonElement{}
	_ CardChild              = LinkElement{}
	_ CardChild              = ActionsElement{}
	_ CardChild              = SectionElement{}
	_ CardChild              = FieldElement{}
	_ CardChild              = FieldsElement{}
	_ CardChild              = TableElement{}
	_ CardChild              = ChartElement{}
)

func TestCardBuilderFunctions(t *testing.T) {
	t.Parallel()

	t.Run("Card", func(t *testing.T) {
		t.Parallel()

		t.Run("creates a card with title", func(t *testing.T) {
			t.Parallel()
			card := Card{Type: "card", Title: "My Card", Children: []any{}}
			must.Eq(t, "card", card.Type)
			must.Eq(t, "My Card", card.Title)
			must.Eq(t, []any{}, card.Children)
		})

		t.Run("creates a card with all options", func(t *testing.T) {
			t.Parallel()
			card := Card{
				Type:     "card",
				Title:    "Order #1234",
				Subtitle: "Processing",
				ImageURL: "https://example.com/image.png",
				Children: []any{CardText("Hello")},
			}
			must.Eq(t, "Order #1234", card.Title)
			must.Eq(t, "Processing", card.Subtitle)
			must.Eq(t, "https://example.com/image.png", card.ImageURL)
			must.Eq(t, 1, len(card.Children))
		})

		t.Run("creates a card with a width hint", func(t *testing.T) {
			t.Parallel()
			card := Card{Type: "card", Title: "Wide", Width: CardWidthFull, Children: []any{}}
			must.Eq(t, CardWidthFull, card.Width)
			must.Eq(t, CardWidth(""), Card{Type: "card", Title: "Default", Children: []any{}}.Width)
		})

		t.Run("creates an empty card", func(t *testing.T) {
			t.Parallel()
			card := Card{Type: "card", Children: []any{}}
			must.Eq(t, "card", card.Type)
			must.Eq(t, []any{}, card.Children)
		})
	})

	t.Run("Text", func(t *testing.T) {
		t.Parallel()

		t.Run("creates a text element", func(t *testing.T) {
			t.Parallel()
			text := CardText("Hello, world!")
			must.Eq(t, "text", text.Type)
			must.Eq(t, "Hello, world!", text.Content)
			must.Eq(t, "", text.Style)
		})

		t.Run("creates a bold text element", func(t *testing.T) {
			t.Parallel()
			text := CardTextElement{Type: "text", Content: "Important", Style: "bold"}
			must.Eq(t, "Important", text.Content)
			must.Eq(t, "bold", text.Style)
		})

		t.Run("creates a muted text element", func(t *testing.T) {
			t.Parallel()
			text := CardTextElement{Type: "text", Content: "Subtle note", Style: "muted"}
			must.Eq(t, "muted", text.Style)
		})
	})

	t.Run("Image", func(t *testing.T) {
		t.Parallel()

		t.Run("creates an image element", func(t *testing.T) {
			t.Parallel()
			img := Image("https://example.com/img.png")
			must.Eq(t, "image", img.Type)
			must.Eq(t, "https://example.com/img.png", img.URL)
			must.Eq(t, "", img.Alt)
		})

		t.Run("creates an image with alt text", func(t *testing.T) {
			t.Parallel()
			img := ImageElement{
				Type: "image",
				URL:  "https://example.com/img.png",
				Alt:  "A beautiful sunset",
			}
			must.Eq(t, "A beautiful sunset", img.Alt)
		})
	})

	t.Run("Divider", func(t *testing.T) {
		t.Parallel()

		t.Run("creates a divider element", func(t *testing.T) {
			t.Parallel()
			div := Divider()
			must.Eq(t, "divider", div.Type)
		})
	})

	t.Run("Button", func(t *testing.T) {
		t.Parallel()

		t.Run("creates a button element", func(t *testing.T) {
			t.Parallel()
			btn := Button("submit", "Submit")
			must.Eq(t, "button", btn.Type)
			must.Eq(t, "submit", btn.ID)
			must.Eq(t, "Submit", btn.Label)
			must.Eq(t, "", btn.Style)
			must.Eq(t, "", btn.Value)
		})

		t.Run("creates a primary button", func(t *testing.T) {
			t.Parallel()
			btn := ButtonElement{Type: "button", ID: "ok", Label: "OK", Style: "primary"}
			must.Eq(t, "primary", btn.Style)
		})

		t.Run("creates a danger button with value", func(t *testing.T) {
			t.Parallel()
			btn := ButtonElement{
				Type:  "button",
				ID:    "delete",
				Label: "Delete",
				Style: "danger",
				Value: "item-123",
			}
			must.Eq(t, "danger", btn.Style)
			must.Eq(t, "item-123", btn.Value)
		})

		t.Run("creates a button with a tooltip", func(t *testing.T) {
			t.Parallel()
			btn := ButtonElement{
				Type:    "button",
				ID:      "ok",
				Label:   "OK",
				Tooltip: "Confirm the order",
			}
			must.Eq(t, "Confirm the order", btn.Tooltip)
			must.Eq(t, "", Button("ok", "OK").Tooltip)
		})
	})

	t.Run("LinkButton", func(t *testing.T) {
		t.Parallel()

		t.Run("creates a link button element", func(t *testing.T) {
			t.Parallel()
			btn := LinkButton("https://example.com", "Visit Site")
			must.Eq(t, "link-button", btn.Type)
			must.Eq(t, "https://example.com", btn.URL)
			must.Eq(t, "Visit Site", btn.Label)
			must.Eq(t, "", btn.Style)
		})

		t.Run("creates a styled link button", func(t *testing.T) {
			t.Parallel()
			btn := LinkButtonElement{
				Type:  "link-button",
				URL:   "https://docs.example.com",
				Label: "View Docs",
				Style: "primary",
			}
			must.Eq(t, "primary", btn.Style)
		})

		t.Run("creates a link button with a tooltip", func(t *testing.T) {
			t.Parallel()
			btn := LinkButtonElement{
				Type:    "link-button",
				URL:     "https://example.com",
				Label:   "Visit Site",
				Tooltip: "Opens example.com",
			}
			must.Eq(t, "Opens example.com", btn.Tooltip)
		})
	})

	t.Run("CardLink", func(t *testing.T) {
		t.Parallel()

		t.Run("creates a link element", func(t *testing.T) {
			t.Parallel()
			link := CardLink("https://example.com", "Visit Site")
			must.Eq(t, "link", link.Type)
			must.Eq(t, "https://example.com", link.URL)
			must.Eq(t, "Visit Site", link.Label)
		})
	})

	t.Run("Actions", func(t *testing.T) {
		t.Parallel()

		t.Run("creates an actions container", func(t *testing.T) {
			t.Parallel()
			actions := Actions(Button("ok", "OK"), Button("cancel", "Cancel"))
			must.Eq(t, "actions", actions.Type)
			must.Eq(t, 2, len(actions.Children))
			must.Eq(t, "OK", actions.Children[0].(ButtonElement).Label)
			must.Eq(t, "Cancel", actions.Children[1].(ButtonElement).Label)
		})

		t.Run("creates actions with mixed button types", func(t *testing.T) {
			t.Parallel()
			actions := Actions(
				ButtonElement{Type: "button", ID: "submit", Label: "Submit", Style: "primary"},
				LinkButton("https://example.com/help", "Help"),
			)
			must.Eq(t, 2, len(actions.Children))
			must.Eq(t, "button", actions.Children[0].(ButtonElement).Type)
			must.Eq(t, "link-button", actions.Children[1].(LinkButtonElement).Type)
		})

		t.Run("creates empty actions", func(t *testing.T) {
			t.Parallel()
			actions := Actions()
			must.Eq(t, []any{}, actions.Children)
		})
	})

	t.Run("Section", func(t *testing.T) {
		t.Parallel()

		t.Run("creates a section container", func(t *testing.T) {
			t.Parallel()
			section := Section(CardText("Content"), Divider())
			must.Eq(t, "section", section.Type)
			must.Eq(t, 2, len(section.Children))
		})
	})

	t.Run("Field", func(t *testing.T) {
		t.Parallel()

		t.Run("creates a field element", func(t *testing.T) {
			t.Parallel()
			field := Field("Status", "Active")
			must.Eq(t, "field", field.Type)
			must.Eq(t, "Status", field.Label)
			must.Eq(t, "Active", field.Value)
		})
	})

	t.Run("Fields", func(t *testing.T) {
		t.Parallel()

		t.Run("creates a fields container", func(t *testing.T) {
			t.Parallel()
			fields := Fields(Field("Name", "John"), Field("Email", "john@example.com"))
			must.Eq(t, "fields", fields.Type)
			must.Eq(t, 2, len(fields.Children))
		})
	})

	t.Run("Table", func(t *testing.T) {
		t.Parallel()

		t.Run("creates a table with caption and pageSize", func(t *testing.T) {
			t.Parallel()
			table := TableElement{
				Type:     "table",
				Headers:  []string{"Name", "Score"},
				Rows:     [][]string{{"Ada", "10"}},
				Caption:  "Scores",
				PageSize: 25,
			}
			must.Eq(t, "table", table.Type)
			must.Eq(t, "Scores", table.Caption)
			must.Eq(t, 25, table.PageSize)
		})

		t.Run("leaves caption and pageSize undefined when omitted", func(t *testing.T) {
			t.Parallel()
			table := CardTable([]string{"A"}, [][]string{{"1"}})
			must.Eq(t, "", table.Caption)
			must.Eq(t, 0, table.PageSize)
		})
	})

	t.Run("Chart", func(t *testing.T) {
		t.Parallel()

		t.Run("creates a pie chart", func(t *testing.T) {
			t.Parallel()
			chart := ChartElement{
				Type:  "chart",
				Title: "Candy Bars",
				Chart: ChartDefinition{
					Type: "pie",
					Segments: []ChartSegment{
						{Label: "Kit Kat", Value: 45},
						{Label: "Twix", Value: 28},
					},
				},
			}
			must.Eq(t, "chart", chart.Type)
			must.Eq(t, "Candy Bars", chart.Title)
			must.Eq(t, "pie", chart.Chart.Type)
		})

		t.Run("creates a line chart with series and categories", func(t *testing.T) {
			t.Parallel()
			chart := ChartElement{
				Type:  "chart",
				Title: "Weekly Sales",
				Chart: ChartDefinition{
					Type:       "line",
					Categories: []string{"Week 1", "Week 2"},
					XLabel:     "Week",
					YLabel:     "Sales",
					Series: []ChartSeries{
						{
							Name: "Scranton",
							Data: []ChartDataPoint{
								{Label: "Week 1", Value: 120},
								{Label: "Week 2", Value: 135},
							},
						},
					},
				},
			}
			must.Eq(t, ChartDefinition{
				Type:       "line",
				Categories: []string{"Week 1", "Week 2"},
				XLabel:     "Week",
				YLabel:     "Sales",
				Series: []ChartSeries{
					{
						Name: "Scranton",
						Data: []ChartDataPoint{
							{Label: "Week 1", Value: 120},
							{Label: "Week 2", Value: 135},
						},
					},
				},
			}, chart.Chart)
		})
	})

	t.Run("chart fallback text", func(t *testing.T) {
		t.Parallel()

		t.Run("renders pie chart data as a labelled ASCII table", func(t *testing.T) {
			t.Parallel()
			text := CardChildToFallbackText(ChartElement{
				Type:  "chart",
				Title: "Candy Bars",
				Chart: ChartDefinition{
					Type: "pie",
					Segments: []ChartSegment{
						{Label: "Kit Kat", Value: 45},
						{Label: "Twix", Value: 28},
					},
				},
			})
			must.StrContains(t, text, "Candy Bars")
			must.StrContains(t, text, "Kit Kat | 45")
			must.StrContains(t, text, "Twix")
		})

		t.Run("renders series chart data with one column per series", func(t *testing.T) {
			t.Parallel()
			text := CardChildToFallbackText(ChartElement{
				Type:  "chart",
				Title: "DAU",
				Chart: ChartDefinition{
					Type:       "area",
					Categories: []string{"Mon", "Tue"},
					XLabel:     "Day",
					Series: []ChartSeries{
						{
							Name: "Web",
							Data: []ChartDataPoint{
								{Label: "Mon", Value: 100},
								{Label: "Tue", Value: 110},
							},
						},
						{
							Name: "Mobile",
							Data: []ChartDataPoint{
								{Label: "Tue", Value: 60},
								{Label: "Mon", Value: 50},
							},
						},
					},
				},
			})
			must.StrContains(t, text, "DAU")
			must.StrContains(t, text, "Day")
			must.StrContains(t, text, "Web")
			must.StrContains(t, text, "Mobile")
			must.StrContains(t, text, "Mon | 100 | 50")
			must.StrContains(t, text, "Tue | 110 | 60")
		})
	})

	t.Run("isCardElement", func(t *testing.T) {
		t.Parallel()

		t.Run("returns true for CardElement", func(t *testing.T) {
			t.Parallel()
			card := Card{Type: "card", Title: "Test", Children: []any{}}
			must.True(t, IsCardElement(card))
		})

		t.Run("returns false for non-card objects", func(t *testing.T) {
			t.Parallel()
			must.False(t, IsCardElement(CardTextElement{Type: "text", Content: "hello"}))
			must.False(t, IsCardElement(ButtonElement{Type: "button", ID: "x", Label: "X"}))
			must.False(t, IsCardElement("string"))
			must.False(t, IsCardElement(nil))
			must.False(t, IsCardElement(123))
			must.False(t, IsCardElement(struct{}{}))
		})
	})
}

func TestCardComposition(t *testing.T) {
	t.Parallel()

	t.Run("creates a complete card with all element types", func(t *testing.T) {
		t.Parallel()
		card := Card{
			Type:     "card",
			Title:    "Order #1234",
			Subtitle: "Processing your order",
			ImageURL: "https://example.com/order.png",
			Children: []any{
				CardText("Thank you for your order!"),
				CardLink("https://example.com/order/1234", "View order details"),
				Divider(),
				Fields(
					Field("Order ID", "#1234"),
					Field("Total", "$99.99"),
				),
				Section(
					CardTextElement{Type: "text", Content: "Items:", Style: "bold"},
					CardTextElement{Type: "text", Content: "2x Widget, 1x Gadget", Style: "muted"},
				),
				Divider(),
				Actions(
					ButtonElement{Type: "button", ID: "track", Label: "Track Order", Style: "primary"},
					ButtonElement{Type: "button", ID: "cancel", Label: "Cancel Order", Style: "danger", Value: "order-1234"},
				),
			},
		}

		must.Eq(t, "card", card.Type)
		must.Eq(t, "Order #1234", card.Title)
		must.Eq(t, 7, len(card.Children))

		must.Eq(t, "text", card.Children[0].(CardTextElement).Type)
		must.Eq(t, "link", card.Children[1].(LinkElement).Type)
		must.Eq(t, "divider", card.Children[2].(DividerElement).Type)
		must.Eq(t, "fields", card.Children[3].(FieldsElement).Type)
		must.Eq(t, "section", card.Children[4].(SectionElement).Type)
		must.Eq(t, "divider", card.Children[5].(DividerElement).Type)
		must.Eq(t, "actions", card.Children[6].(ActionsElement).Type)

		fields := card.Children[3].(FieldsElement)
		must.Eq(t, 2, len(fields.Children))

		actions := card.Children[6].(ActionsElement)
		must.Eq(t, 2, len(actions.Children))
		must.Eq(t, "track", actions.Children[0].(ButtonElement).ID)
		must.Eq(t, "order-1234", actions.Children[1].(ButtonElement).Value)
	})
}

func TestSelectAndRadioSelectBuilderValidation(t *testing.T) {
	t.Parallel()

	t.Run("Select", func(t *testing.T) {
		t.Parallel()

		t.Run("throws when options array is empty", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "Select requires at least one option", panicMessage(func() {
				Select("test", "Test", []SelectOptionElement{})
			}))
		})

		t.Run("creates select with valid options", func(t *testing.T) {
			t.Parallel()
			selectEl := Select("test", "Test", []SelectOptionElement{SelectOption("A", "a")})
			must.Eq(t, "select", selectEl.Type)
			must.Eq(t, 1, len(selectEl.Options))
		})
	})

	t.Run("RadioSelect", func(t *testing.T) {
		t.Parallel()

		t.Run("throws when options array is empty", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "RadioSelect requires at least one option", panicMessage(func() {
				RadioSelect("test", "Test", []SelectOptionElement{})
			}))
		})

		t.Run("creates radio select with valid options", func(t *testing.T) {
			t.Parallel()
			radioSelect := RadioSelect("test", "Test", []SelectOptionElement{SelectOption("A", "a")})
			must.Eq(t, "radio_select", radioSelect.Type)
			must.Eq(t, 1, len(radioSelect.Options))
		})
	})
}
