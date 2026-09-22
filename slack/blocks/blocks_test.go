// Ported from packages/adapter-slack/src/blocks/index.test.ts @ 6adca36 (chat v4.40.0).
package blocks

import (
	"strconv"
	"strings"
	"testing"

	"github.com/shoenig/test/must"
)

func card(children ...SlackCardChild) SlackCardElement {
	if children == nil {
		children = []SlackCardChild{}
	}
	return SlackCardElement{Children: children, Type: "card"}
}

func ptr[T any](v T) *T { return &v }

func TestSlackBlockKitPrimitives(t *testing.T) {
	t.Parallel()

	t.Run("converts card headers and context", func(t *testing.T) {
		t.Parallel()
		jsonEq(t, []any{
			map[string]any{
				"text": map[string]any{"emoji": true, "text": "Order", "type": "plain_text"},
				"type": "header",
			},
			map[string]any{
				"elements": []any{map[string]any{"text": "Status changed", "type": "mrkdwn"}},
				"type":     "context",
			},
			map[string]any{
				"alt_text":  "Order",
				"image_url": "https://example.com/image.png",
				"type":      "image",
			},
		}, CardToSlackBlocks(SlackCardElement{
			Children: []SlackCardChild{},
			ImageURL: "https://example.com/image.png",
			Subtitle: "Status changed",
			Title:    "Order",
			Type:     "card",
		}))
	})

	t.Run("truncates header text to Slack's header block limit", func(t *testing.T) {
		t.Parallel()
		title := strings.Repeat("a", 200)
		jsonEq(t, map[string]any{
			"text": map[string]any{"emoji": true, "text": strings.Repeat("a", 150), "type": "plain_text"},
			"type": "header",
		}, CardToSlackBlocks(SlackCardElement{Children: []SlackCardChild{}, Title: title, Type: "card"})[0])
	})

	t.Run("truncates image URLs to Slack's image block limit", func(t *testing.T) {
		t.Parallel()
		longURL := "https://example.com/" + strings.Repeat("a", 4000)
		topBlocks := CardToSlackBlocks(SlackCardElement{
			Children: []SlackCardChild{},
			ImageURL: longURL,
			Title:    "{{emoji:frame}}",
			Type:     "card",
		})
		jsonEq(t, map[string]any{
			"text": map[string]any{"emoji": true, "text": ":frame:", "type": "plain_text"},
			"type": "header",
		}, topBlocks[0])
		jsonEq(t, map[string]any{
			"alt_text":  ":frame:",
			"image_url": "https://example.com/" + strings.Repeat("a", 2980),
			"type":      "image",
		}, topBlocks[1])
		jsonEq(t, map[string]any{
			"alt_text":  "Image",
			"image_url": "https://example.com/" + strings.Repeat("a", 2980),
			"type":      "image",
		}, CardToSlackBlocks(SlackCardElement{
			Children: []SlackCardChild{SlackImageElement{Type: "image", URL: longURL}},
			Type:     "card",
		})[0])
	})

	t.Run("converts text and links", func(t *testing.T) {
		t.Parallel()
		jsonEq(t, []any{
			map[string]any{"text": map[string]any{"text": "plain", "type": "mrkdwn"}, "type": "section"},
			map[string]any{"text": map[string]any{"text": "*bold*", "type": "mrkdwn"}, "type": "section"},
			map[string]any{"elements": []any{map[string]any{"text": "muted", "type": "mrkdwn"}}, "type": "context"},
			map[string]any{
				"text": map[string]any{"text": "<https://example.com|Docs>", "type": "mrkdwn"},
				"type": "section",
			},
		}, CardToSlackBlocks(card(
			SlackTextElement{Content: "plain", Type: "text"},
			SlackTextElement{Content: "bold", Style: "bold", Type: "text"},
			SlackTextElement{Content: "muted", Style: "muted", Type: "text"},
			SlackLinkElement{Label: "Docs", Type: "link", URL: "https://example.com"},
		)))
	})

	t.Run("converts actions", func(t *testing.T) {
		t.Parallel()
		blocks := CardToSlackBlocks(card(SlackActionsElement{
			Children: []SlackActionsChild{
				SlackButtonElement{ID: "approve", Label: "Approve", Style: "primary", Type: "button"},
				SlackLinkButtonElement{
					ID:    "agent_slack_auth_signin",
					Label: "Docs",
					Style: "default",
					Type:  "link-button",
					URL:   "https://example.com/docs",
				},
				SlackSelectElement{
					ID:    "status",
					Label: "Status",
					Options: []SlackSelectOptionElement{
						{Label: "Open", Value: "open"},
						{Label: "Closed", Value: "closed"},
					},
					Placeholder: "Choose",
					Type:        "select",
				},
				SlackRadioSelectElement{
					ID:    "plan",
					Label: "Plan",
					Options: []SlackSelectOptionElement{
						{Description: "For teams", Label: "Pro", Value: "pro"},
					},
					Type: "radio_select",
				},
			},
			Type: "actions",
		}))
		jsonEq(t, map[string]any{
			"elements": []any{
				map[string]any{
					"action_id": "approve",
					"style":     "primary",
					"text":      map[string]any{"emoji": true, "text": "Approve", "type": "plain_text"},
					"type":      "button",
				},
				map[string]any{
					"action_id": "agent_slack_auth_signin",
					"text":      map[string]any{"emoji": true, "text": "Docs", "type": "plain_text"},
					"type":      "button",
					"url":       "https://example.com/docs",
				},
				map[string]any{
					"action_id": "status",
					"options": []any{
						map[string]any{"text": map[string]any{"text": "Open", "type": "plain_text"}, "value": "open"},
						map[string]any{"text": map[string]any{"text": "Closed", "type": "plain_text"}, "value": "closed"},
					},
					"placeholder": map[string]any{"emoji": true, "text": "Choose", "type": "plain_text"},
					"type":        "static_select",
				},
				map[string]any{
					"action_id": "plan",
					"options": []any{
						map[string]any{
							"description": map[string]any{"text": "For teams", "type": "mrkdwn"},
							"text":        map[string]any{"text": "Pro", "type": "mrkdwn"},
							"value":       "pro",
						},
					},
					"type": "radio_buttons",
				},
			},
			"type": "actions",
		}, blocks[0])
	})

	t.Run("limits action elements and select options to Slack limits", func(t *testing.T) {
		t.Parallel()
		buttons := make([]SlackActionsChild, 30)
		for i := range buttons {
			buttons[i] = SlackButtonElement{
				ID:    "b" + strconv.Itoa(i),
				Label: "Button " + strconv.Itoa(i),
				Type:  "button",
			}
		}
		options := make([]SlackSelectOptionElement, 120)
		for i := range options {
			options[i] = SlackSelectOptionElement{
				Label: "Option " + strconv.Itoa(i),
				Value: "value-" + strconv.Itoa(i),
			}
		}
		blocks := CardToSlackBlocks(card(
			SlackActionsElement{Children: buttons, Type: "actions"},
			SlackActionsElement{
				Children: []SlackActionsChild{
					SlackSelectElement{ID: "select", Label: "Select", Options: options, Type: "select"},
				},
				Type: "actions",
			},
		))
		must.Eq(t, 25, len(blocks[0].Elements))
		sel := blocks[1].Elements[0].(SlackBlockStaticSelect)
		must.Eq(t, 100, len(sel.Options))
	})

	t.Run("truncates option values to Slack's option object limit", func(t *testing.T) {
		t.Parallel()
		blocks := CardToSlackBlocks(card(SlackActionsElement{
			Children: []SlackActionsChild{
				SlackSelectElement{
					ID:      "select",
					Label:   "Select",
					Options: []SlackSelectOptionElement{{Label: "Option", Value: strings.Repeat("v", 200)}},
					Type:    "select",
				},
			},
			Type: "actions",
		}))
		sel := blocks[0].Elements[0].(SlackBlockStaticSelect)
		must.Eq(t, strings.Repeat("v", 150), sel.Options[0].Value)
	})

	t.Run("matches truncated initial options for select elements", func(t *testing.T) {
		t.Parallel()
		longValue := strings.Repeat("v", 200)
		blocks := CardToSlackBlocks(card(SlackActionsElement{
			Children: []SlackActionsChild{
				SlackSelectElement{
					ID:            "select",
					InitialOption: longValue,
					Label:         "Select",
					Options:       []SlackSelectOptionElement{{Label: "Option", Value: longValue}},
					Type:          "select",
				},
				SlackRadioSelectElement{
					ID:            "radio",
					InitialOption: longValue,
					Label:         "Radio",
					Options:       []SlackSelectOptionElement{{Label: "Option", Value: longValue}},
					Type:          "radio_select",
				},
			},
			Type: "actions",
		}))
		sel := blocks[0].Elements[0].(SlackBlockStaticSelect)
		must.Eq(t, strings.Repeat("v", 150), sel.InitialOption.Value)
		radio := blocks[0].Elements[1].(SlackBlockRadioButtons)
		must.Eq(t, strings.Repeat("v", 150), radio.InitialOption.Value)
	})

	t.Run("omits initial options when no initial value is provided", func(t *testing.T) {
		t.Parallel()
		blocks := CardToSlackBlocks(card(SlackActionsElement{
			Children: []SlackActionsChild{
				SlackSelectElement{
					ID:      "select",
					Label:   "Select",
					Options: []SlackSelectOptionElement{{Label: "Option", Value: ""}},
					Type:    "select",
				},
			},
			Type: "actions",
		}))
		sel := blocks[0].Elements[0].(SlackBlockStaticSelect)
		must.Nil(t, sel.InitialOption)
	})

	t.Run("converts fields and tables", func(t *testing.T) {
		t.Parallel()
		blocks := CardToSlackBlocks(card(
			SlackFieldsElement{
				Children: []SlackFieldElement{
					{Label: "Name", Type: "field", Value: "Ada"},
					{Label: "Role", Type: "field", Value: "Engineer"},
				},
				Type: "fields",
			},
			SlackTableElement{
				Align:   []SlackTableAlignment{"left", "right"},
				Headers: []string{"Name", "Score"},
				Rows:    [][]string{{"Ada", "10"}},
				Type:    "table",
			},
		))
		jsonEq(t, map[string]any{
			"fields": []any{
				map[string]any{"text": "*Name*\nAda", "type": "mrkdwn"},
				map[string]any{"text": "*Role*\nEngineer", "type": "mrkdwn"},
			},
			"type": "section",
		}, blocks[0])
		jsonEq(t, map[string]any{
			"caption": "Table",
			"rows": []any{
				[]any{
					map[string]any{"text": "Name", "type": "raw_text"},
					map[string]any{"text": "Score", "type": "raw_text"},
				},
				[]any{
					map[string]any{"text": "Ada", "type": "raw_text"},
					map[string]any{"text": "10", "type": "raw_text"},
				},
			},
			"type": "data_table",
		}, blocks[1])
	})

	t.Run("passes table caption and clamped page_size through", func(t *testing.T) {
		t.Parallel()
		blocks := CardToSlackBlocks(card(SlackTableElement{
			Caption:  "Scores",
			Headers:  []string{"Name"},
			PageSize: ptr(0),
			Rows:     [][]string{{"Ada"}},
			Type:     "table",
		}))
		jsonEq(t, map[string]any{
			"caption":   "Scores",
			"page_size": 1,
			"rows": []any{
				[]any{map[string]any{"text": "Name", "type": "raw_text"}},
				[]any{map[string]any{"text": "Ada", "type": "raw_text"}},
			},
			"type": "data_table",
		}, blocks[0])
	})

	t.Run("renders header-only tables as a plain table block", func(t *testing.T) {
		t.Parallel()
		blocks := CardToSlackBlocks(card(SlackTableElement{
			Align:   []SlackTableAlignment{"left"},
			Headers: []string{"Name"},
			Rows:    [][]string{},
			Type:    "table",
		}))
		jsonEq(t, map[string]any{
			"column_settings": []any{map[string]any{"align": "left"}},
			"rows":            []any{[]any{map[string]any{"text": "Name", "type": "raw_text"}}},
			"type":            "table",
		}, blocks[0])
	})

	t.Run("falls back to ASCII when combined table cells exceed the character limit", func(t *testing.T) {
		t.Parallel()
		bigCell := strings.Repeat("x", 10_001)
		blocks := CardToSlackBlocks(card(SlackTableElement{
			Headers: []string{"A"},
			Rows:    [][]string{{bigCell}},
			Type:    "table",
		}))
		must.Eq(t, "section", blocks[0].Type)
		text := blocks[0].Text.Text
		must.LessEq(t, 3000, len(text))
		must.True(t, strings.HasSuffix(text, "\n```"))
	})

	t.Run("converts pie charts to data_visualization blocks", func(t *testing.T) {
		t.Parallel()
		blocks := CardToSlackBlocks(card(SlackChartElement{
			Chart: SlackChartDefinition{
				Segments: []SlackChartSegment{
					{Label: "Kit Kat", Value: 45},
					{Label: "Twix", Value: 28},
				},
				Type: "pie",
			},
			Title: "Candy Bars",
			Type:  "chart",
		}))
		jsonEq(t, map[string]any{
			"chart": map[string]any{
				"segments": []any{
					map[string]any{"label": "Kit Kat", "value": 45},
					map[string]any{"label": "Twix", "value": 28},
				},
				"type": "pie",
			},
			"title": "Candy Bars",
			"type":  "data_visualization",
		}, blocks[0])
	})

	t.Run("converts series charts with axis config and normalized point order", func(t *testing.T) {
		t.Parallel()
		blocks := CardToSlackBlocks(card(SlackChartElement{
			Chart: SlackChartDefinition{
				Categories: []string{"Mon", "Tue"},
				Series: []SlackChartSeries{
					{
						Data: []SlackChartDataPoint{
							{Label: "Tue", Value: 60},
							{Label: "Mon", Value: 50},
						},
						Name: "Mobile",
					},
				},
				Type:   "line",
				XLabel: "Day",
				YLabel: "Users",
			},
			Title: "DAU",
			Type:  "chart",
		}))
		jsonEq(t, map[string]any{
			"chart": map[string]any{
				"axis_config": map[string]any{
					"categories": []any{"Mon", "Tue"},
					"x_label":    "Day",
					"y_label":    "Users",
				},
				"series": []any{
					map[string]any{
						"data": []any{
							map[string]any{"label": "Mon", "value": 50},
							map[string]any{"label": "Tue", "value": 60},
						},
						"name": "Mobile",
					},
				},
				"type": "line",
			},
			"title": "DAU",
			"type":  "data_visualization",
		}, blocks[0])
	})

	t.Run("falls back to a text section for invalid charts", func(t *testing.T) {
		t.Parallel()
		blocks := CardToSlackBlocks(card(SlackChartElement{
			Chart: SlackChartDefinition{
				Segments: []SlackChartSegment{{Label: "Zero", Value: 0}},
				Type:     "pie",
			},
			Title: "Bad Pie",
			Type:  "chart",
		}))
		must.Eq(t, "section", blocks[0].Type)
		must.True(t, strings.Contains(blocks[0].Text.Text, "Bad Pie"))
	})

	t.Run("falls back to a text section from the third chart in one message", func(t *testing.T) {
		t.Parallel()
		pie := func(title string) SlackChartElement {
			return SlackChartElement{
				Chart: SlackChartDefinition{
					Segments: []SlackChartSegment{{Label: "A", Value: 1}},
					Type:     "pie",
				},
				Title: title,
				Type:  "chart",
			}
		}
		blocks := CardToSlackBlocks(card(pie("One"), pie("Two"), pie("Three")))
		must.Eq(t, []string{"data_visualization", "data_visualization", "section"}, []string{
			blocks[0].Type, blocks[1].Type, blocks[2].Type,
		})
	})

	t.Run("includes chart data in Slack fallback text", func(t *testing.T) {
		t.Parallel()
		text := CardToSlackFallbackText(card(SlackChartElement{
			Chart: SlackChartDefinition{
				Segments: []SlackChartSegment{{Label: "Kit Kat", Value: 45}},
				Type:     "pie",
			},
			Title: "Candy Bars",
			Type:  "chart",
		}))
		must.True(t, strings.Contains(text, "Candy Bars"))
		must.True(t, strings.Contains(text, "Kit Kat"))
	})

	t.Run("falls back to ASCII tables after one native table", func(t *testing.T) {
		t.Parallel()
		table := SlackTableElement{
			Headers: []string{"A", "B"},
			Rows:    [][]string{{"1", "2"}},
			Type:    "table",
		}
		jsonEq(t, map[string]any{
			"text": map[string]any{"text": "```\nA | B\n1 | 2\n```", "type": "mrkdwn"},
			"type": "section",
		}, CardToSlackBlocks(card(table, table))[1])
	})

	t.Run("generates Slack fallback text", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "*Title*\nSub\nHello\nStatus: Ready", CardToSlackFallbackText(SlackCardElement{
			Children: []SlackCardChild{
				SlackTextElement{Content: "Hello", Type: "text"},
				SlackFieldsElement{
					Children: []SlackFieldElement{{Label: "Status", Type: "field", Value: "Ready"}},
					Type:     "fields",
				},
				SlackActionsElement{
					Children: []SlackActionsChild{SlackButtonElement{ID: "ok", Label: "OK", Type: "button"}},
					Type:     "actions",
				},
			},
			Subtitle: "Sub",
			Title:    "Title",
			Type:     "card",
		}))
	})

	t.Run("keeps compatibility aliases", func(t *testing.T) {
		t.Parallel()
		input := card(SlackTextElement{Content: "hello", Type: "text"})
		jsonEq(t, CardToSlackBlocks(input), CardToBlockKit(input))
		must.Eq(t, CardToSlackFallbackText(input), CardToFallbackText(input))
	})

	t.Run("supports custom emoji conversion", func(t *testing.T) {
		t.Parallel()
		input := card(SlackTextElement{Content: "{{emoji:thumbs_up}}", Type: "text"})
		jsonEq(t, map[string]any{
			"text": map[string]any{"text": ":thumbs_up:", "type": "mrkdwn"},
			"type": "section",
		}, CardToSlackBlocks(input)[0])
		jsonEq(t, map[string]any{
			"text": map[string]any{"text": ":+1:", "type": "mrkdwn"},
			"type": "section",
		}, CardToSlackBlocks(input, SlackBlocksOptions{ConvertEmoji: func(string) string { return ":+1:" }})[0])
		must.Eq(t, "hi :wave:", ConvertSlackEmojiPlaceholders("hi {{emoji:wave}}"))
	})

	t.Run("renders input requests as Slack buttons", func(t *testing.T) {
		t.Parallel()
		jsonEq(t, []any{
			map[string]any{
				"text": map[string]any{"text": "Approve deploy?", "type": "mrkdwn"},
				"type": "section",
			},
			map[string]any{
				"elements": []any{
					map[string]any{
						"action_id": "input:req-1:button:0",
						"style":     "primary",
						"text":      map[string]any{"text": "Approve", "type": "plain_text"},
						"type":      "button",
						"value":     "approve",
					},
					map[string]any{
						"action_id": "input:req-1:button:1",
						"style":     "danger",
						"text":      map[string]any{"text": "Deny", "type": "plain_text"},
						"type":      "button",
						"value":     "deny",
					},
				},
				"type": "actions",
			},
		}, InputRequestToSlackBlocks(SlackInputRequest{
			Options: []SlackInputOption{
				{ID: "approve", Label: "Approve", Style: "primary"},
				{ID: "deny", Label: "Deny", Style: "danger"},
			},
			Prompt:    "Approve deploy?",
			RequestID: "req-1",
		}))
	})

	t.Run("renders input requests as selects", func(t *testing.T) {
		t.Parallel()
		jsonEq(t, map[string]any{
			"elements": []any{
				map[string]any{
					"action_id": "input:req-1",
					"options": []any{
						map[string]any{
							"text":  map[string]any{"text": "One", "type": "plain_text"},
							"value": "one",
						},
					},
					"placeholder": map[string]any{"text": "Choose an option", "type": "plain_text"},
					"type":        "static_select",
				},
			},
			"type": "actions",
		}, InputRequestToSlackBlocks(SlackInputRequest{
			Display:   "select",
			Options:   []SlackInputOption{{ID: "one", Label: "One"}},
			Prompt:    "Pick one",
			RequestID: "req-1",
		})[1])
	})

	t.Run("renders input requests as radios", func(t *testing.T) {
		t.Parallel()
		jsonEq(t, map[string]any{
			"elements": []any{
				map[string]any{
					"action_id": "input:req-1",
					"options": []any{
						map[string]any{
							"text":  map[string]any{"text": "One", "type": "plain_text"},
							"value": "one",
						},
					},
					"type": "radio_buttons",
				},
			},
			"type": "actions",
		}, InputRequestToSlackBlocks(SlackInputRequest{
			Display:   "radio",
			Options:   []SlackInputOption{{ID: "one", Label: "One"}},
			Prompt:    "Pick one",
			RequestID: "req-1",
		})[1])
	})

	t.Run("renders freeform alongside options when allowed", func(t *testing.T) {
		t.Parallel()
		jsonEq(t, map[string]any{
			"elements": []any{
				map[string]any{
					"action_id": "input:req-1:button:0",
					"text":      map[string]any{"text": "Approve", "type": "plain_text"},
					"type":      "button",
					"value":     "approve",
				},
				map[string]any{
					"action_id": "input-freeform:req-1",
					"style":     "primary",
					"text":      map[string]any{"text": "Type your answer", "type": "plain_text"},
					"type":      "button",
					"value":     "req-1",
				},
			},
			"type": "actions",
		}, InputRequestToSlackBlocks(SlackInputRequest{
			AllowFreeform: true,
			Options:       []SlackInputOption{{ID: "approve", Label: "Approve"}},
			Prompt:        "Approve deploy?",
			RequestID:     "req-1",
		})[1])
	})

	t.Run("renders and reads freeform input modals", func(t *testing.T) {
		t.Parallel()
		view := BuildSlackFreeformView(SlackFreeformViewOptions{
			Metadata: map[string]any{"requestId": "req-1"},
			Prompt:   "Tell me why",
		})
		got := jsonMap(t, view)
		must.Eq(t, "input-freeform-submit", got["callback_id"])
		must.Eq(t, `{"requestId":"req-1"}`, got["private_metadata"])
		jsonEq(t, map[string]any{"text": "Tell me why", "type": "plain_text"}, got["title"])
		must.Eq(t, "modal", got["type"])
		must.Eq(t, "because", ParseSlackFreeformValue([]SlackViewValue{
			{ActionID: "input-freeform-text", BlockID: "input-freeform-block", Value: ptr("because")},
		}))
	})

	t.Run("parses input actions and answered blocks", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, &SlackInputResponse{OptionID: "approve", RequestID: "req-1"}, ParseSlackInputResponse(SlackInputAction{
			ActionID: "input:req-1:button:0",
			Value:    ptr("approve"),
		}))
		must.Eq(t, &SlackInputResponse{OptionID: "later", RequestID: "req-2"}, ParseSlackInputResponse(SlackInputAction{
			ActionID:            "input:req-2",
			SelectedOptionValue: ptr("later"),
		}))
		jsonEq(t, []any{
			map[string]any{
				"text": map[string]any{"text": ":white_check_mark: *Approve*", "type": "mrkdwn"},
				"type": "section",
			},
			map[string]any{
				"elements": []any{map[string]any{"text": "Answered by <@U123>", "type": "mrkdwn"}},
				"type":     "context",
			},
		}, AnsweredSlackInputBlocks(SlackAnsweredInput{Answer: "Approve", UserID: "U123"}))
	})
}
