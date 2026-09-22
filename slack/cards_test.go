// Ported from packages/adapter-slack/src/cards.test.ts @ 6adca36 (chat v4.40.0).
// jsonEq/jsonVal/jsonMap copied from slack/blocks/json_test.go (unexported there;
// slack → blocks import is fine, but the helpers are not). JSX → chat struct
// literals. JS text.length on the 3,000-char pin is rune count (ASCII + one
// ellipsis); see PORTING.md.
package slack

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/slack/blocks"
	"github.com/shoenig/test/must"
)

func jsonEq(t *testing.T, expected, actual any) {
	t.Helper()
	must.Eq(t, jsonVal(t, expected), jsonVal(t, actual))
}

func jsonVal(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	must.NoError(t, err)
	var out any
	must.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func jsonMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := jsonVal(t, v).(map[string]any)
	must.True(t, ok)
	return m
}

func TestCardToBlockKit(t *testing.T) {
	t.Parallel()

	t.Run("converts a simple card with title", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{Type: "card", Title: "Welcome", Children: []any{}}
		got := CardToBlockKit(card)

		must.Eq(t, 1, len(got))
		jsonEq(t, map[string]any{
			"type": "header",
			"text": map[string]any{
				"type":  "plain_text",
				"text":  "Welcome",
				"emoji": true,
			},
		}, got[0])
	})

	t.Run("converts a card with title and subtitle", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:     "card",
			Title:    "Order Update",
			Subtitle: "Your order is on its way",
			Children: []any{},
		}
		got := CardToBlockKit(card)

		must.Eq(t, 2, len(got))
		must.Eq(t, "header", got[0].Type)
		jsonEq(t, map[string]any{
			"type":     "context",
			"elements": []any{map[string]any{"type": "mrkdwn", "text": "Your order is on its way"}},
		}, got[1])
	})

	t.Run("converts a card with header image", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:     "card",
			Title:    "Product",
			ImageURL: "https://example.com/product.png",
			Children: []any{},
		}
		got := CardToBlockKit(card)

		must.Eq(t, 2, len(got))
		jsonEq(t, map[string]any{
			"type":      "image",
			"image_url": "https://example.com/product.png",
			"alt_text":  "Product",
		}, got[1])
	})

	t.Run("converts text elements", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.CardText("Regular text"),
				chat.CardTextElement{Content: "Bold text", Style: "bold", Type: "text"},
				chat.CardTextElement{Content: "Muted text", Style: "muted", Type: "text"},
			},
		}
		got := CardToBlockKit(card)

		must.Eq(t, 3, len(got))
		jsonEq(t, map[string]any{
			"type": "section",
			"text": map[string]any{"type": "mrkdwn", "text": "Regular text"},
		}, got[0])
		jsonEq(t, map[string]any{
			"type": "section",
			"text": map[string]any{"type": "mrkdwn", "text": "*Bold text*"},
		}, got[1])
		jsonEq(t, map[string]any{
			"type":     "context",
			"elements": []any{map[string]any{"type": "mrkdwn", "text": "Muted text"}},
		}, got[2])
	})

	t.Run("converts image elements", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:     "card",
			Children: []any{chat.Image("https://example.com/img.png", "My image")},
		}
		got := CardToBlockKit(card)

		must.Eq(t, 1, len(got))
		jsonEq(t, map[string]any{
			"type":      "image",
			"image_url": "https://example.com/img.png",
			"alt_text":  "My image",
		}, got[0])
	})

	t.Run("converts divider elements", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{Type: "card", Children: []any{chat.Divider()}}
		got := CardToBlockKit(card)

		must.Eq(t, 1, len(got))
		jsonEq(t, map[string]any{"type": "divider"}, got[0])
	})

	t.Run("converts actions with buttons", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.Actions(
					chat.ButtonElement{ID: "approve", Label: "Approve", Style: "primary", Type: "button"},
					chat.ButtonElement{ID: "reject", Label: "Reject", Style: "danger", Type: "button", Value: "data-123"},
					chat.Button("skip", "Skip"),
				),
			},
		}
		got := CardToBlockKit(card)

		must.Eq(t, 1, len(got))
		must.Eq(t, "actions", got[0].Type)
		must.Eq(t, 3, len(got[0].Elements))
		jsonEq(t, map[string]any{
			"type":      "button",
			"text":      map[string]any{"type": "plain_text", "text": "Approve", "emoji": true},
			"action_id": "approve",
			"style":     "primary",
		}, got[0].Elements[0])
		jsonEq(t, map[string]any{
			"type":      "button",
			"text":      map[string]any{"type": "plain_text", "text": "Reject", "emoji": true},
			"action_id": "reject",
			"value":     "data-123",
			"style":     "danger",
		}, got[0].Elements[1])
		jsonEq(t, map[string]any{
			"type":      "button",
			"text":      map[string]any{"type": "plain_text", "text": "Skip", "emoji": true},
			"action_id": "skip",
		}, got[0].Elements[2])
	})

	t.Run("converts link buttons with url property", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.Actions(
					func() chat.LinkButtonElement {
						btn := chat.LinkButton("https://example.com/docs", "View Docs")
						btn.Style = "primary"
						return btn
					}(),
				),
			},
		}
		got := CardToBlockKit(card)

		must.Eq(t, 1, len(got))
		must.Eq(t, "actions", got[0].Type)
		must.Eq(t, 1, len(got[0].Elements))
		el := jsonMap(t, got[0].Elements[0])
		must.Eq(t, "button", el["type"])
		jsonEq(t, map[string]any{"type": "plain_text", "text": "View Docs", "emoji": true}, el["text"])
		must.Eq(t, "https://example.com/docs", el["url"])
		must.Eq(t, "primary", el["style"])
	})

	t.Run("uses custom link button action ids", func(t *testing.T) {
		t.Parallel()
		btn := chat.LinkButton("https://vercel.com/oauth/authorize", "Sign in")
		btn.ID = "agent_slack_auth_signin"
		card := chat.Card{Type: "card", Children: []any{chat.Actions(btn)}}
		got := CardToBlockKit(card)

		el := jsonMap(t, got[0].Elements[0])
		must.Eq(t, "agent_slack_auth_signin", el["action_id"])
	})

	t.Run("converts fields", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.Fields(
					chat.Field("Status", "Active"),
					chat.Field("Priority", "High"),
				),
			},
		}
		got := CardToBlockKit(card)

		must.Eq(t, 1, len(got))
		must.Eq(t, "section", got[0].Type)
		jsonEq(t, []any{
			map[string]any{"type": "mrkdwn", "text": "*Status*\nActive"},
			map[string]any{"type": "mrkdwn", "text": "*Priority*\nHigh"},
		}, got[0].Fields)
	})

	t.Run("flattens section children", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:     "card",
			Children: []any{chat.Section(chat.CardText("Inside section"), chat.Divider())},
		}
		got := CardToBlockKit(card)

		must.Eq(t, 2, len(got))
		must.Eq(t, "section", got[0].Type)
		must.Eq(t, "divider", got[1].Type)
	})

	t.Run("converts a complete card", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:     "card",
			Title:    "Order #1234",
			Subtitle: "Status update",
			Children: []any{
				chat.CardText("Your order has been shipped!"),
				chat.Divider(),
				chat.Fields(
					chat.Field("Tracking", "ABC123"),
					chat.Field("ETA", "Dec 25"),
				),
				chat.Actions(
					chat.ButtonElement{ID: "track", Label: "Track Package", Style: "primary", Type: "button"},
				),
			},
		}
		got := CardToBlockKit(card)

		must.Eq(t, 6, len(got))
		must.Eq(t, "header", got[0].Type)
		must.Eq(t, "context", got[1].Type)
		must.Eq(t, "section", got[2].Type)
		must.Eq(t, "divider", got[3].Type)
		must.Eq(t, "section", got[4].Type)
		must.Eq(t, "actions", got[5].Type)
	})
}

func TestCardToFallbackText(t *testing.T) {
	t.Parallel()

	t.Run("generates fallback text for a card", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:     "card",
			Title:    "Order Update",
			Subtitle: "Status changed",
			Children: []any{
				chat.CardText("Your order is ready"),
				chat.Fields(
					chat.Field("Order ID", "#1234"),
					chat.Field("Status", "Ready"),
				),
				chat.Actions(
					chat.Button("pickup", "Schedule Pickup"),
					chat.Button("delay", "Delay"),
				),
			},
		}
		text := CardToFallbackText(card)

		must.StrContains(t, text, "*Order Update*")
		must.StrContains(t, text, "Status changed")
		must.StrContains(t, text, "Your order is ready")
		must.StrContains(t, text, "Order ID: #1234")
		must.StrContains(t, text, "Status: Ready")
		must.False(t, strings.Contains(text, "[Schedule Pickup]"))
		must.False(t, strings.Contains(text, "[Delay]"))
	})

	t.Run("handles card with only title", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{Type: "card", Title: "Simple Card", Children: []any{}}
		must.Eq(t, "*Simple Card*", CardToFallbackText(card))
	})
}

func TestCardToBlockKitWithSelectElements(t *testing.T) {
	t.Parallel()

	t.Run("converts actions with select element", func(t *testing.T) {
		t.Parallel()
		sel := chat.Select("priority", "Priority", []chat.SelectOptionElement{
			chat.SelectOption("High", "high"),
			chat.SelectOption("Medium", "medium"),
			chat.SelectOption("Low", "low"),
		})
		sel.Placeholder = "Select priority"
		sel.InitialOption = "medium"
		card := chat.Card{Type: "card", Children: []any{chat.Actions(sel)}}
		got := CardToBlockKit(card)

		must.Eq(t, 1, len(got))
		must.Eq(t, "actions", got[0].Type)
		must.Eq(t, 1, len(got[0].Elements))
		el := jsonMap(t, got[0].Elements[0])
		must.Eq(t, "static_select", el["type"])
		must.Eq(t, "priority", el["action_id"])
		jsonEq(t, map[string]any{"type": "plain_text", "text": "Select priority"}, el["placeholder"])
		opts := el["options"].([]any)
		must.Eq(t, 3, len(opts))
		jsonEq(t, map[string]any{
			"text":  map[string]any{"type": "plain_text", "text": "High"},
			"value": "high",
		}, opts[0])
		jsonEq(t, map[string]any{
			"text":  map[string]any{"type": "plain_text", "text": "Medium"},
			"value": "medium",
		}, el["initial_option"])
	})

	t.Run("converts actions with mixed buttons and selects", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.Actions(
					chat.Select("status", "Status", []chat.SelectOptionElement{
						chat.SelectOption("Open", "open"),
						chat.SelectOption("Closed", "closed"),
					}),
					chat.ButtonElement{ID: "submit", Label: "Submit", Style: "primary", Type: "button"},
				),
			},
		}
		got := CardToBlockKit(card)

		must.Eq(t, 1, len(got))
		must.Eq(t, 2, len(got[0].Elements))
		el0 := jsonMap(t, got[0].Elements[0])
		el1 := jsonMap(t, got[0].Elements[1])
		must.Eq(t, "static_select", el0["type"])
		must.Eq(t, "status", el0["action_id"])
		must.Eq(t, "button", el1["type"])
		must.Eq(t, "submit", el1["action_id"])
	})

	t.Run("converts select without placeholder or initial option", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.Actions(
					chat.Select("category", "Category", []chat.SelectOptionElement{
						chat.SelectOption("Bug", "bug"),
						chat.SelectOption("Feature", "feature"),
					}),
				),
			},
		}
		got := CardToBlockKit(card)
		el := jsonMap(t, got[0].Elements[0])
		must.Eq(t, "static_select", el["type"])
		_, hasPlaceholder := el["placeholder"]
		_, hasInitial := el["initial_option"]
		must.False(t, hasPlaceholder)
		must.False(t, hasInitial)
	})
}

func TestCardToBlockKitWithRadioSelectElements(t *testing.T) {
	t.Parallel()

	t.Run("converts actions with radio select element", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.Actions(
					chat.RadioSelect("plan", "Choose Plan", []chat.SelectOptionElement{
						chat.SelectOption("Basic", "basic"),
						chat.SelectOption("Pro", "pro"),
						chat.SelectOption("Enterprise", "enterprise"),
					}),
				),
			},
		}
		got := CardToBlockKit(card)

		must.Eq(t, 1, len(got))
		must.Eq(t, "actions", got[0].Type)
		must.Eq(t, 1, len(got[0].Elements))
		el := jsonMap(t, got[0].Elements[0])
		must.Eq(t, "radio_buttons", el["type"])
		must.Eq(t, "plan", el["action_id"])
		must.Eq(t, 3, len(el["options"].([]any)))
	})

	t.Run("uses mrkdwn type for radio select labels", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.Actions(
					chat.RadioSelect("option", "Choose", []chat.SelectOptionElement{
						chat.SelectOption("Option A", "a"),
					}),
				),
			},
		}
		got := CardToBlockKit(card)
		el := jsonMap(t, got[0].Elements[0])
		opt := el["options"].([]any)[0].(map[string]any)
		text := opt["text"].(map[string]any)
		must.Eq(t, "mrkdwn", text["type"])
		must.Eq(t, "Option A", text["text"])
	})

	t.Run("limits radio select options to 10", func(t *testing.T) {
		t.Parallel()
		options := make([]chat.SelectOptionElement, 15)
		for i := range options {
			options[i] = chat.SelectOption("Option "+strconv.Itoa(i+1), "opt"+strconv.Itoa(i+1))
		}
		card := chat.Card{
			Type:     "card",
			Children: []any{chat.Actions(chat.RadioSelect("many_options", "Many Options", options))},
		}
		got := CardToBlockKit(card)
		el := jsonMap(t, got[0].Elements[0])
		must.Eq(t, 10, len(el["options"].([]any)))
	})
}

func TestCardToBlockKitWithSelectOptionDescriptions(t *testing.T) {
	t.Parallel()

	t.Run("includes description in select options with plain_text type", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.Actions(
					chat.Select("plan", "Plan", []chat.SelectOptionElement{
						{Label: "Basic", Value: "basic", Description: "For individuals"},
						{Label: "Pro", Value: "pro", Description: "For teams"},
					}),
				),
			},
		}
		got := CardToBlockKit(card)
		el := jsonMap(t, got[0].Elements[0])
		opts := el["options"].([]any)
		jsonEq(t, map[string]any{"type": "plain_text", "text": "For individuals"}, opts[0].(map[string]any)["description"])
		jsonEq(t, map[string]any{"type": "plain_text", "text": "For teams"}, opts[1].(map[string]any)["description"])
	})

	t.Run("includes description in radio select options with mrkdwn type", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.Actions(
					chat.RadioSelect("plan", "Plan", []chat.SelectOptionElement{
						{Label: "Basic", Value: "basic", Description: "For *individuals*"},
						{Label: "Pro", Value: "pro", Description: "For _teams_"},
					}),
				),
			},
		}
		got := CardToBlockKit(card)
		el := jsonMap(t, got[0].Elements[0])
		opts := el["options"].([]any)
		jsonEq(t, map[string]any{"type": "mrkdwn", "text": "For *individuals*"}, opts[0].(map[string]any)["description"])
		jsonEq(t, map[string]any{"type": "mrkdwn", "text": "For _teams_"}, opts[1].(map[string]any)["description"])
	})

	t.Run("omits description when not provided", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.Actions(
					chat.Select("category", "Category", []chat.SelectOptionElement{
						chat.SelectOption("Bug", "bug"),
						chat.SelectOption("Feature", "feature"),
					}),
				),
			},
		}
		got := CardToBlockKit(card)
		el := jsonMap(t, got[0].Elements[0])
		opts := el["options"].([]any)
		_, d0 := opts[0].(map[string]any)["description"]
		_, d1 := opts[1].(map[string]any)["description"]
		must.False(t, d0)
		must.False(t, d1)
	})
}

func TestMarkdownBoldToSlackMrkdwnConversion(t *testing.T) {
	t.Parallel()

	t.Run("converts **bold** to *bold* in CardText content", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{Type: "card", Children: []any{chat.CardText("The **domain** is example.com")}}
		got := CardToBlockKit(card)
		jsonEq(t, map[string]any{
			"type": "section",
			"text": map[string]any{"type": "mrkdwn", "text": "The *domain* is example.com"},
		}, got[0])
	})

	t.Run("converts multiple **bold** segments in one CardText", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:     "card",
			Children: []any{chat.CardText("**Project**: my-app, **Status**: active, **Branch**: main")},
		}
		got := CardToBlockKit(card)
		must.Eq(t, "*Project*: my-app, *Status*: active, *Branch*: main", got[0].Text.Text)
	})

	t.Run("converts **bold** across multiple lines", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{chat.CardText(
				"**Domain**: example.com\n**Project**: my-app\n**Status**: deployed",
			)},
		}
		got := CardToBlockKit(card)
		must.Eq(t, "*Domain*: example.com\n*Project*: my-app\n*Status*: deployed", got[0].Text.Text)
	})

	t.Run("preserves existing single *asterisk* formatting", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{Type: "card", Children: []any{chat.CardText("Already *bold* in Slack format")}}
		got := CardToBlockKit(card)
		must.Eq(t, "Already *bold* in Slack format", got[0].Text.Text)
	})

	t.Run("handles text with no markdown formatting", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{Type: "card", Children: []any{chat.CardText("Plain text with no formatting")}}
		got := CardToBlockKit(card)
		must.Eq(t, "Plain text with no formatting", got[0].Text.Text)
	})

	t.Run("converts **bold** in muted style CardText", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:     "card",
			Children: []any{chat.CardTextElement{Content: "Info about **thing**", Style: "muted", Type: "text"}},
		}
		got := CardToBlockKit(card)
		jsonEq(t, map[string]any{
			"type":     "context",
			"elements": []any{map[string]any{"type": "mrkdwn", "text": "Info about *thing*"}},
		}, got[0])
	})

	t.Run("converts **bold** in field values", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:     "card",
			Children: []any{chat.Fields(chat.Field("Status", "**Active**"))},
		}
		got := CardToBlockKit(card)
		must.StrContains(t, got[0].Fields[0].Text, "*Active*")
		must.False(t, strings.Contains(got[0].Fields[0].Text, "**Active**"))
	})

	t.Run("does not convert empty double asterisks", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{Type: "card", Children: []any{chat.CardText("text **** more")}}
		got := CardToBlockKit(card)
		must.Eq(t, "text **** more", got[0].Text.Text)
	})

	t.Run("handles **bold** at start and end of content", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{Type: "card", Children: []any{chat.CardText("**Start** and **end**")}}
		got := CardToBlockKit(card)
		must.Eq(t, "*Start* and *end*", got[0].Text.Text)
	})
}

func TestCardToBlockKitWithCardLink(t *testing.T) {
	t.Parallel()

	t.Run("converts CardLink to a mrkdwn section block with Slack link syntax", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:     "card",
			Children: []any{chat.CardLink("https://example.com", "Click here")},
		}
		got := CardToBlockKit(card)

		must.Eq(t, 1, len(got))
		jsonEq(t, map[string]any{
			"type": "section",
			"text": map[string]any{
				"type": "mrkdwn",
				"text": "<https://example.com|Click here>",
			},
		}, got[0])
	})

	t.Run("converts CardLink alongside other children", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:  "card",
			Title: "Test",
			Children: []any{
				chat.CardText("Hello"),
				chat.CardLink("https://example.com", "Link"),
			},
		}
		got := CardToBlockKit(card)

		must.Eq(t, 3, len(got))
		jsonEq(t, map[string]any{
			"type": "section",
			"text": map[string]any{
				"type": "mrkdwn",
				"text": "<https://example.com|Link>",
			},
		}, got[2])
	})

	t.Run("converts a card with table element to a Block Kit data table", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.TableElement{
					Type:    "table",
					Headers: []string{"Name", "Age"},
					Rows:    [][]string{{"Alice", "30"}, {"Bob", "25"}},
				},
			},
		}
		got := CardToBlockKit(card)
		must.Eq(t, 1, len(got))
		must.Eq(t, "data_table", got[0].Type)
		must.Eq(t, "Table", got[0].Caption)
		m := jsonMap(t, got[0])
		_, hasPageSize := m["page_size"]
		must.False(t, hasPageSize)
		jsonEq(t, []any{
			[]any{
				map[string]any{"type": "raw_text", "text": "Name"},
				map[string]any{"type": "raw_text", "text": "Age"},
			},
			[]any{
				map[string]any{"type": "raw_text", "text": "Alice"},
				map[string]any{"type": "raw_text", "text": "30"},
			},
			[]any{
				map[string]any{"type": "raw_text", "text": "Bob"},
				map[string]any{"type": "raw_text", "text": "25"},
			},
		}, got[0].Rows)
	})

	t.Run("falls back to ASCII for second table in same card", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.TableElement{Type: "table", Headers: []string{"A"}, Rows: [][]string{{"1"}}},
				chat.TableElement{Type: "table", Headers: []string{"B"}, Rows: [][]string{{"2"}}},
			},
		}
		got := CardToBlockKit(card)
		must.Eq(t, 2, len(got))
		must.Eq(t, "data_table", got[0].Type)
		must.Eq(t, "section", got[1].Type)
		must.StrContains(t, got[1].Text.Text, "```")
	})

	t.Run("replaces empty table cells with a space to satisfy Slack API", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.TableElement{
					Type:    "table",
					Headers: []string{"Kind", ""},
					Rows:    [][]string{{"FORM", "Form Submission"}, {"and more...", ""}},
				},
			},
		}
		got := CardToBlockKit(card)
		tableBlock := got[0]
		must.Eq(t, "data_table", tableBlock.Type)
		for _, row := range tableBlock.Rows {
			for _, cell := range row {
				must.Greater(t, 0, len(cell.Text))
			}
		}
		must.Eq(t, " ", tableBlock.Rows[0][1].Text)
		must.Eq(t, " ", tableBlock.Rows[2][1].Text)
	})
}

func TestCardToBlockKitWithDataTables(t *testing.T) {
	t.Parallel()

	t.Run("passes caption and clamped page_size through", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.TableElement{
					Type:     "table",
					Headers:  []string{"Name"},
					Rows:     [][]string{{"Ada"}},
					Caption:  "People",
					PageSize: 250,
				},
			},
		}
		got := CardToBlockKit(card)
		must.Eq(t, "data_table", got[0].Type)
		must.Eq(t, "People", got[0].Caption)
		must.Eq(t, 100, got[0].PageSize)
	})

	t.Run("renders header-only tables as a plain table block", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:     "card",
			Children: []any{chat.TableElement{Type: "table", Headers: []string{"Name", "Age"}, Rows: [][]string{}}},
		}
		got := CardToBlockKit(card)
		must.Eq(t, "table", got[0].Type)
		jsonEq(t, []any{
			[]any{
				map[string]any{"type": "raw_text", "text": "Name"},
				map[string]any{"type": "raw_text", "text": "Age"},
			},
		}, got[0].Rows)
	})

	t.Run("falls back to ASCII when combined cells exceed 10,000 characters", func(t *testing.T) {
		t.Parallel()
		bigCell := strings.Repeat("x", 5001)
		card := chat.Card{
			Type:     "card",
			Children: []any{chat.TableElement{Type: "table", Headers: []string{"A"}, Rows: [][]string{{bigCell}, {bigCell}}}},
		}
		got := CardToBlockKit(card)
		must.Eq(t, "section", got[0].Type)
		must.StrContains(t, got[0].Text.Text, "```")
	})

	t.Run("truncates ASCII fallback to Slack's 3,000-char section limit", func(t *testing.T) {
		t.Parallel()
		bigCell := strings.Repeat("x", 5001)
		card := chat.Card{
			Type:     "card",
			Children: []any{chat.TableElement{Type: "table", Headers: []string{"A"}, Rows: [][]string{{bigCell}, {bigCell}}}},
		}
		text := CardToBlockKit(card)[0].Text.Text
		must.LessEq(t, 3000, utf8.RuneCountInString(text))
		must.True(t, strings.HasSuffix(text, "\n```"))
	})
}

func TestCardToBlockKitWithCharts(t *testing.T) {
	t.Parallel()

	t.Run("converts a pie chart to a data_visualization block", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.ChartElement{
					Type:  "chart",
					Title: "Candy Bars",
					Chart: chat.ChartDefinition{
						Type: "pie",
						Segments: []chat.ChartSegment{
							{Label: "Kit Kat", Value: 45},
							{Label: "Twix", Value: 28},
						},
					},
				},
			},
		}
		got := CardToBlockKit(card)
		must.Eq(t, 1, len(got))
		jsonEq(t, map[string]any{
			"type":  "data_visualization",
			"title": "Candy Bars",
			"chart": map[string]any{
				"type": "pie",
				"segments": []any{
					map[string]any{"label": "Kit Kat", "value": 45},
					map[string]any{"label": "Twix", "value": 28},
				},
			},
		}, got[0])
	})

	t.Run("converts a bar chart with axis config and normalizes point order", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.ChartElement{
					Type:  "chart",
					Title: "DAU",
					Chart: chat.ChartDefinition{
						Type:       "bar",
						Categories: []string{"Mon", "Tue"},
						XLabel:     "Day",
						YLabel:     "Users",
						Series: []chat.ChartSeries{
							{
								Name: "Mobile",
								Data: []chat.ChartDataPoint{
									{Label: "Tue", Value: 60},
									{Label: "Mon", Value: 50},
								},
							},
						},
					},
				},
			},
		}
		got := CardToBlockKit(card)
		jsonEq(t, map[string]any{
			"type":  "data_visualization",
			"title": "DAU",
			"chart": map[string]any{
				"type": "bar",
				"series": []any{
					map[string]any{
						"name": "Mobile",
						"data": []any{
							map[string]any{"label": "Mon", "value": 50},
							map[string]any{"label": "Tue", "value": 60},
						},
					},
				},
				"axis_config": map[string]any{
					"categories": []any{"Mon", "Tue"},
					"x_label":    "Day",
					"y_label":    "Users",
				},
			},
		}, got[0])
	})

	t.Run("omits axis labels that are not provided", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.ChartElement{
					Type:  "chart",
					Title: "Sales",
					Chart: chat.ChartDefinition{
						Type:       "line",
						Categories: []string{"W1"},
						Series: []chat.ChartSeries{
							{Name: "A", Data: []chat.ChartDataPoint{{Label: "W1", Value: 1}}},
						},
					},
				},
			},
		}
		got := CardToBlockKit(card)
		jsonEq(t, map[string]any{
			"type": "line",
			"series": []any{
				map[string]any{"name": "A", "data": []any{map[string]any{"label": "W1", "value": 1}}},
			},
			"axis_config": map[string]any{"categories": []any{"W1"}},
		}, got[0].Chart)
	})

	t.Run("falls back to text when a pie segment value is not positive", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.ChartElement{
					Type:  "chart",
					Title: "Bad Pie",
					Chart: chat.ChartDefinition{
						Type:     "pie",
						Segments: []chat.ChartSegment{{Label: "Zero", Value: 0}},
					},
				},
			},
		}
		got := CardToBlockKit(card)
		must.Eq(t, "section", got[0].Type)
		must.StrContains(t, got[0].Text.Text, "Bad Pie")
		must.StrContains(t, got[0].Text.Text, "```")
	})

	t.Run("falls back to text when a series misses a category", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.ChartElement{
					Type:  "chart",
					Title: "Gaps",
					Chart: chat.ChartDefinition{
						Type:       "area",
						Categories: []string{"Mon", "Tue"},
						Series: []chat.ChartSeries{
							{Name: "A", Data: []chat.ChartDataPoint{{Label: "Mon", Value: 1}}},
						},
					},
				},
			},
		}
		got := CardToBlockKit(card)
		must.Eq(t, "section", got[0].Type)
		must.StrContains(t, got[0].Text.Text, "```")
	})

	t.Run("falls back to text when the title exceeds 50 characters", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.ChartElement{
					Type:  "chart",
					Title: strings.Repeat("t", 51),
					Chart: chat.ChartDefinition{
						Type:     "pie",
						Segments: []chat.ChartSegment{{Label: "A", Value: 1}},
					},
				},
			},
		}
		got := CardToBlockKit(card)
		must.Eq(t, "section", got[0].Type)
	})

	t.Run("falls back to text when there are more than 12 segments", func(t *testing.T) {
		t.Parallel()
		segments := make([]chat.ChartSegment, 13)
		for i := range segments {
			segments[i] = chat.ChartSegment{Label: "S" + strconv.Itoa(i), Value: i + 1}
		}
		card := chat.Card{
			Type: "card",
			Children: []any{
				chat.ChartElement{
					Type:  "chart",
					Title: "Too Many",
					Chart: chat.ChartDefinition{Type: "pie", Segments: segments},
				},
			},
		}
		got := CardToBlockKit(card)
		must.Eq(t, "section", got[0].Type)
	})

	t.Run("falls back to text from the third chart in one message", func(t *testing.T) {
		t.Parallel()
		pie := func(title string) chat.ChartElement {
			return chat.ChartElement{
				Type:  "chart",
				Title: title,
				Chart: chat.ChartDefinition{
					Type:     "pie",
					Segments: []chat.ChartSegment{{Label: "A", Value: 1}},
				},
			}
		}
		card := chat.Card{Type: "card", Children: []any{pie("One"), pie("Two"), pie("Three")}}
		got := CardToBlockKit(card)
		types := make([]string, len(got))
		for i, b := range got {
			types[i] = b.Type
		}
		must.Eq(t, []string{"data_visualization", "data_visualization", "section"}, types)
		must.StrContains(t, got[2].Text.Text, "Three")
	})

	t.Run("includes chart data in card fallback text", func(t *testing.T) {
		t.Parallel()
		card := chat.Card{
			Type:  "card",
			Title: "Report",
			Children: []any{
				chat.ChartElement{
					Type:  "chart",
					Title: "Candy Bars",
					Chart: chat.ChartDefinition{
						Type:     "pie",
						Segments: []chat.ChartSegment{{Label: "Kit Kat", Value: 45}},
					},
				},
			},
		}
		text := CardToFallbackText(card)
		must.StrContains(t, text, "Candy Bars")
		must.StrContains(t, text, "Kit Kat")
	})
}

var _ blocks.SlackBlock = SlackBlock{}
