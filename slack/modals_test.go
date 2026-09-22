// Ported from packages/adapter-slack/src/modals.test.ts @ 6adca36 (chat v4.40.0).
// jsonEq/jsonVal/jsonMap live in cards_test.go (same package). JSX → chat
// struct literals. console.warn spy → slog.Warn count. JS undefined → "".
package slack

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/slack/blocks"
	"github.com/shoenig/test/must"
)

func TestModalToSlackView(t *testing.T) {
	t.Parallel()

	t.Run("converts a simple modal with text input", func(t *testing.T) {
		t.Parallel()
		modal := chat.NewModal("feedback_form", "Send Feedback")
		modal.Children = []any{chat.TextInput("message", "Your Feedback")}

		view := ModalToSlackView(modal, "")

		must.Eq(t, "modal", view.Type)
		must.Eq(t, "feedback_form", view.CallbackID)
		must.Eq(t, blocks.SlackTextObject{Type: "plain_text", Text: "Send Feedback"}, view.Title)
		must.Eq(t, blocks.SlackTextObject{Type: "plain_text", Text: "Submit"}, view.Submit)
		must.Eq(t, blocks.SlackTextObject{Type: "plain_text", Text: "Cancel"}, view.Close)
		must.Eq(t, 1, len(view.Blocks))
		jsonEq(t, map[string]any{
			"type":     "input",
			"block_id": "message",
			"optional": false,
			"label":    map[string]any{"type": "plain_text", "text": "Your Feedback"},
			"element": map[string]any{
				"type":      "plain_text_input",
				"action_id": "message",
				"multiline": false,
			},
		}, view.Blocks[0])
	})

	t.Run("converts a modal with custom submit/close labels", func(t *testing.T) {
		t.Parallel()
		modal := chat.NewModal("test", "Test Modal")
		modal.SubmitLabel = "Send"
		modal.CloseLabel = "Dismiss"
		modal.Children = []any{}

		view := ModalToSlackView(modal, "")

		must.Eq(t, blocks.SlackTextObject{Type: "plain_text", Text: "Send"}, view.Submit)
		must.Eq(t, blocks.SlackTextObject{Type: "plain_text", Text: "Dismiss"}, view.Close)
	})

	t.Run("converts multiline text input", func(t *testing.T) {
		t.Parallel()
		input := chat.TextInput("description", "Description")
		input.Multiline = true
		input.Placeholder = "Enter description..."
		input.MaxLength = 500
		modal := chat.NewModal("test", "Test")
		modal.Children = []any{input}

		view := ModalToSlackView(modal, "")

		got := jsonMap(t, view.Blocks[0])
		must.Eq(t, "input", got["type"])
		jsonEq(t, map[string]any{
			"type":        "plain_text_input",
			"action_id":   "description",
			"multiline":   true,
			"placeholder": map[string]any{"type": "plain_text", "text": "Enter description..."},
			"max_length":  500.0,
		}, got["element"])
	})

	t.Run("converts optional text input", func(t *testing.T) {
		t.Parallel()
		input := chat.TextInput("notes", "Notes")
		input.Optional = true
		modal := chat.NewModal("test", "Test")
		modal.Children = []any{input}

		view := ModalToSlackView(modal, "")

		got := jsonMap(t, view.Blocks[0])
		must.Eq(t, "input", got["type"])
		must.Eq(t, true, got["optional"])
	})

	t.Run("converts text input with initial value", func(t *testing.T) {
		t.Parallel()
		input := chat.TextInput("name", "Name")
		input.InitialValue = "John Doe"
		modal := chat.NewModal("test", "Test")
		modal.Children = []any{input}

		view := ModalToSlackView(modal, "")

		el := jsonMap(t, jsonMap(t, view.Blocks[0])["element"])
		must.Eq(t, "John Doe", el["initial_value"])
	})

	t.Run("converts select element with options", func(t *testing.T) {
		t.Parallel()
		modal := chat.NewModal("test", "Test")
		modal.Children = []any{
			chat.Select("category", "Category", []chat.SelectOptionElement{
				chat.SelectOption("Bug Report", "bug"),
				chat.SelectOption("Feature Request", "feature"),
			}),
		}

		view := ModalToSlackView(modal, "")

		got := jsonMap(t, view.Blocks[0])
		must.Eq(t, "input", got["type"])
		must.Eq(t, "category", got["block_id"])
		jsonEq(t, map[string]any{"type": "plain_text", "text": "Category"}, got["label"])
		jsonEq(t, map[string]any{
			"type":      "static_select",
			"action_id": "category",
			"options": []any{
				map[string]any{"text": map[string]any{"type": "plain_text", "text": "Bug Report"}, "value": "bug"},
				map[string]any{"text": map[string]any{"type": "plain_text", "text": "Feature Request"}, "value": "feature"},
			},
		}, got["element"])
	})

	t.Run("converts select with initial option", func(t *testing.T) {
		t.Parallel()
		sel := chat.Select("priority", "Priority", []chat.SelectOptionElement{
			chat.SelectOption("Low", "low"),
			chat.SelectOption("Medium", "medium"),
			chat.SelectOption("High", "high"),
		})
		sel.InitialOption = "medium"
		modal := chat.NewModal("test", "Test")
		modal.Children = []any{sel}

		view := ModalToSlackView(modal, "")

		el := jsonMap(t, jsonMap(t, view.Blocks[0])["element"])
		jsonEq(t, map[string]any{
			"text":  map[string]any{"type": "plain_text", "text": "Medium"},
			"value": "medium",
		}, el["initial_option"])
	})

	t.Run("converts select with placeholder", func(t *testing.T) {
		t.Parallel()
		sel := chat.Select("category", "Category", []chat.SelectOptionElement{
			chat.SelectOption("General", "general"),
		})
		sel.Placeholder = "Select a category"
		modal := chat.NewModal("test", "Test")
		modal.Children = []any{sel}

		view := ModalToSlackView(modal, "")

		el := jsonMap(t, jsonMap(t, view.Blocks[0])["element"])
		jsonEq(t, map[string]any{"type": "plain_text", "text": "Select a category"}, el["placeholder"])
	})

	t.Run("converts external select with placeholder and min query length", func(t *testing.T) {
		t.Parallel()
		sel := chat.ExternalSelect("person", "Person")
		sel.Placeholder = "Search people"
		sel.MinQueryLength = 1
		modal := chat.NewModal("test", "Test")
		modal.Children = []any{sel}

		view := ModalToSlackView(modal, "")

		got := jsonMap(t, view.Blocks[0])
		must.Eq(t, "input", got["type"])
		must.Eq(t, "person", got["block_id"])
		jsonEq(t, map[string]any{"type": "plain_text", "text": "Person"}, got["label"])
		jsonEq(t, map[string]any{
			"type":             "external_select",
			"action_id":        "person",
			"placeholder":      map[string]any{"type": "plain_text", "text": "Search people"},
			"min_query_length": 1.0,
		}, got["element"])
	})

	t.Run("converts external select with initialOption", func(t *testing.T) {
		t.Parallel()
		sel := chat.ExternalSelect("person", "Person")
		sel.InitialOption = chat.SelectOption("Alice", "u1")
		modal := chat.NewModal("test", "Test")
		modal.Children = []any{sel}

		view := ModalToSlackView(modal, "")

		got := jsonMap(t, view.Blocks[0])
		must.Eq(t, "input", got["type"])
		must.Eq(t, "person", got["block_id"])
		jsonEq(t, map[string]any{
			"type":      "external_select",
			"action_id": "person",
			"initial_option": map[string]any{
				"text":  map[string]any{"type": "plain_text", "text": "Alice"},
				"value": "u1",
			},
		}, got["element"])
	})

	t.Run("includes contextId as private_metadata when provided", func(t *testing.T) {
		t.Parallel()
		modal := chat.NewModal("test", "Test")
		modal.Children = []any{}

		view := ModalToSlackView(modal, "context-uuid-123")

		must.Eq(t, "context-uuid-123", view.PrivateMetadata)
	})

	t.Run("private_metadata is undefined when no contextId provided", func(t *testing.T) {
		t.Parallel()
		modal := chat.NewModal("test", "Test")
		modal.Children = []any{}

		view := ModalToSlackView(modal, "")

		must.Eq(t, "", view.PrivateMetadata)
	})

	t.Run("sets notify_on_close when provided", func(t *testing.T) {
		t.Parallel()
		modal := chat.NewModal("test", "Test")
		modal.NotifyOnClose = true
		modal.Children = []any{}

		view := ModalToSlackView(modal, "")

		must.True(t, view.NotifyOnClose)
	})

	t.Run("truncates long titles to 24 chars", func(t *testing.T) {
		t.Parallel()
		modal := chat.NewModal("test", "This is a very long modal title that exceeds the limit")
		modal.Children = []any{}

		view := ModalToSlackView(modal, "")

		must.True(t, len(view.Title.Text) <= 24)
	})

	t.Run("converts a complete modal with multiple inputs", func(t *testing.T) {
		t.Parallel()
		message := chat.TextInput("message", "Your Feedback")
		message.Placeholder = "Tell us what you think..."
		message.Multiline = true
		email := chat.TextInput("email", "Email (optional)")
		email.Optional = true
		modal := chat.NewModal("feedback_form", "Submit Feedback")
		modal.SubmitLabel = "Send"
		modal.CloseLabel = "Cancel"
		modal.NotifyOnClose = true
		modal.Children = []any{
			message,
			chat.Select("category", "Category", []chat.SelectOptionElement{
				chat.SelectOption("Bug", "bug"),
				chat.SelectOption("Feature", "feature"),
				chat.SelectOption("Other", "other"),
			}),
			email,
		}

		view := ModalToSlackView(modal, "thread-context-123")

		must.Eq(t, "feedback_form", view.CallbackID)
		must.Eq(t, "thread-context-123", view.PrivateMetadata)
		must.Eq(t, 3, len(view.Blocks))
		must.Eq(t, "input", view.Blocks[0].Type)
		must.Eq(t, "input", view.Blocks[1].Type)
		must.Eq(t, "input", view.Blocks[2].Type)
	})
}

func TestEncodeModalMetadata(t *testing.T) {
	t.Parallel()

	t.Run("returns undefined when both fields are empty", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "", EncodeModalMetadata(ModalMetadata{}))
	})

	t.Run("encodes contextId only", func(t *testing.T) {
		t.Parallel()
		encoded := EncodeModalMetadata(ModalMetadata{ContextID: "uuid-123"})
		must.NotEq(t, "", encoded)
		var parsed map[string]any
		must.NoError(t, json.Unmarshal([]byte(encoded), &parsed))
		must.Eq(t, "uuid-123", parsed["c"])
		_, hasM := parsed["m"]
		must.False(t, hasM)
	})

	t.Run("encodes privateMetadata only", func(t *testing.T) {
		t.Parallel()
		encoded := EncodeModalMetadata(ModalMetadata{PrivateMetadata: `{"chatId":"abc"}`})
		must.NotEq(t, "", encoded)
		var parsed map[string]any
		must.NoError(t, json.Unmarshal([]byte(encoded), &parsed))
		_, hasC := parsed["c"]
		must.False(t, hasC)
		must.Eq(t, `{"chatId":"abc"}`, parsed["m"])
	})

	t.Run("encodes both contextId and privateMetadata", func(t *testing.T) {
		t.Parallel()
		encoded := EncodeModalMetadata(ModalMetadata{
			ContextID:       "uuid-123",
			PrivateMetadata: `{"chatId":"abc"}`,
		})
		must.NotEq(t, "", encoded)
		var parsed map[string]any
		must.NoError(t, json.Unmarshal([]byte(encoded), &parsed))
		must.Eq(t, "uuid-123", parsed["c"])
		must.Eq(t, `{"chatId":"abc"}`, parsed["m"])
	})
}

func TestDecodeModalMetadata(t *testing.T) {
	t.Parallel()

	t.Run("returns empty object for undefined input", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, ModalMetadata{}, DecodeModalMetadata(""))
	})

	t.Run("returns empty object for empty string", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, ModalMetadata{}, DecodeModalMetadata(""))
	})

	t.Run("decodes contextId only", func(t *testing.T) {
		t.Parallel()
		encoded, err := json.Marshal(map[string]any{"c": "uuid-123"})
		must.NoError(t, err)
		must.Eq(t, ModalMetadata{ContextID: "uuid-123"}, DecodeModalMetadata(string(encoded)))
	})

	t.Run("decodes privateMetadata only", func(t *testing.T) {
		t.Parallel()
		encoded, err := json.Marshal(map[string]any{"m": `{"chatId":"abc"}`})
		must.NoError(t, err)
		must.Eq(t, ModalMetadata{PrivateMetadata: `{"chatId":"abc"}`}, DecodeModalMetadata(string(encoded)))
	})

	t.Run("decodes both contextId and privateMetadata", func(t *testing.T) {
		t.Parallel()
		encoded, err := json.Marshal(map[string]any{
			"c": "uuid-123",
			"m": `{"chatId":"abc"}`,
		})
		must.NoError(t, err)
		must.Eq(t, ModalMetadata{
			ContextID:       "uuid-123",
			PrivateMetadata: `{"chatId":"abc"}`,
		}, DecodeModalMetadata(string(encoded)))
	})

	t.Run("falls back to treating plain string as contextId (backward compat)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, ModalMetadata{ContextID: "plain-uuid-456"}, DecodeModalMetadata("plain-uuid-456"))
	})

	t.Run("falls back for JSON without c/m keys", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, ModalMetadata{ContextID: `{"other":"value"}`}, DecodeModalMetadata(`{"other":"value"}`))
	})

	t.Run("roundtrips encode then decode", func(t *testing.T) {
		t.Parallel()
		original := ModalMetadata{
			ContextID:       "ctx-1",
			PrivateMetadata: `{"key":"val"}`,
		}
		must.Eq(t, original, DecodeModalMetadata(EncodeModalMetadata(original)))
	})
}

func TestModalToSlackViewWithRadioSelect(t *testing.T) {
	t.Parallel()

	t.Run("converts radio select element with options", func(t *testing.T) {
		t.Parallel()
		modal := chat.NewModal("test", "Test")
		modal.Children = []any{
			chat.RadioSelect("plan", "Choose Plan", []chat.SelectOptionElement{
				chat.SelectOption("Basic", "basic"),
				chat.SelectOption("Pro", "pro"),
				chat.SelectOption("Enterprise", "enterprise"),
			}),
		}

		view := ModalToSlackView(modal, "")

		must.Eq(t, 1, len(view.Blocks))
		got := jsonMap(t, view.Blocks[0])
		must.Eq(t, "input", got["type"])
		must.Eq(t, "plan", got["block_id"])
		jsonEq(t, map[string]any{"type": "plain_text", "text": "Choose Plan"}, got["label"])
		el := jsonMap(t, got["element"])
		must.Eq(t, "radio_buttons", el["type"])
		must.Eq(t, "plan", el["action_id"])
		opts, ok := el["options"].([]any)
		must.True(t, ok)
		must.Eq(t, 3, len(opts))
	})

	t.Run("converts optional radio select", func(t *testing.T) {
		t.Parallel()
		sel := chat.RadioSelect("preference", "Preference", []chat.SelectOptionElement{
			chat.SelectOption("Yes", "yes"),
			chat.SelectOption("No", "no"),
		})
		sel.Optional = true
		modal := chat.NewModal("test", "Test")
		modal.Children = []any{sel}

		view := ModalToSlackView(modal, "")

		got := jsonMap(t, view.Blocks[0])
		must.Eq(t, "input", got["type"])
		must.Eq(t, true, got["optional"])
	})

	t.Run("uses mrkdwn type for radio select labels", func(t *testing.T) {
		t.Parallel()
		modal := chat.NewModal("test", "Test")
		modal.Children = []any{
			chat.RadioSelect("option", "Choose", []chat.SelectOptionElement{
				chat.SelectOption("Option A", "a"),
			}),
		}

		view := ModalToSlackView(modal, "")

		el := jsonMap(t, jsonMap(t, view.Blocks[0])["element"])
		opts := el["options"].([]any)
		text := jsonMap(t, jsonMap(t, opts[0])["text"])
		must.Eq(t, "mrkdwn", text["type"])
		must.Eq(t, "Option A", text["text"])
	})

	t.Run("limits radio select options to 10", func(t *testing.T) {
		t.Parallel()
		options := make([]chat.SelectOptionElement, 15)
		for i := range options {
			options[i] = chat.SelectOption(fmt.Sprintf("Option %d", i+1), fmt.Sprintf("opt%d", i+1))
		}
		modal := chat.NewModal("test", "Test")
		modal.Children = []any{
			chat.RadioSelect("many_options", "Many Options", options),
		}

		view := ModalToSlackView(modal, "")

		el := jsonMap(t, jsonMap(t, view.Blocks[0])["element"])
		opts, ok := el["options"].([]any)
		must.True(t, ok)
		must.Eq(t, 10, len(opts))
	})
}

func TestModalToSlackViewWithSelectOptionDescriptions(t *testing.T) {
	t.Parallel()

	t.Run("includes description in select options with plain_text type", func(t *testing.T) {
		t.Parallel()
		basic := chat.SelectOption("Basic", "basic")
		basic.Description = "For individuals"
		pro := chat.SelectOption("Pro", "pro")
		pro.Description = "For teams"
		modal := chat.NewModal("test", "Test")
		modal.Children = []any{
			chat.Select("plan", "Plan", []chat.SelectOptionElement{basic, pro}),
		}

		view := ModalToSlackView(modal, "")

		el := jsonMap(t, jsonMap(t, view.Blocks[0])["element"])
		opts := el["options"].([]any)
		jsonEq(t, map[string]any{"type": "plain_text", "text": "For individuals"}, jsonMap(t, opts[0])["description"])
		jsonEq(t, map[string]any{"type": "plain_text", "text": "For teams"}, jsonMap(t, opts[1])["description"])
	})

	t.Run("includes description in radio select options with mrkdwn type", func(t *testing.T) {
		t.Parallel()
		basic := chat.SelectOption("Basic", "basic")
		basic.Description = "For *individuals*"
		pro := chat.SelectOption("Pro", "pro")
		pro.Description = "For _teams_"
		modal := chat.NewModal("test", "Test")
		modal.Children = []any{
			chat.RadioSelect("plan", "Plan", []chat.SelectOptionElement{basic, pro}),
		}

		view := ModalToSlackView(modal, "")

		el := jsonMap(t, jsonMap(t, view.Blocks[0])["element"])
		opts := el["options"].([]any)
		jsonEq(t, map[string]any{"type": "mrkdwn", "text": "For *individuals*"}, jsonMap(t, opts[0])["description"])
		jsonEq(t, map[string]any{"type": "mrkdwn", "text": "For _teams_"}, jsonMap(t, opts[1])["description"])
	})
}

func TestDateAndNumberInputs(t *testing.T) {
	t.Parallel()

	t.Run("converts a date input to a Block Kit datepicker", func(t *testing.T) {
		t.Parallel()
		input := chat.DateInput("renewal_date", "Renewal Date")
		input.Placeholder = "Pick a date"
		input.InitialValue = "2026-08-01"
		modal := chat.NewModal("renewal_form", "Renewal")
		modal.Children = []any{input}

		view := ModalToSlackView(modal, "")

		jsonEq(t, map[string]any{
			"type":     "input",
			"block_id": "renewal_date",
			"optional": false,
			"label":    map[string]any{"type": "plain_text", "text": "Renewal Date"},
			"element": map[string]any{
				"type":         "datepicker",
				"action_id":    "renewal_date",
				"placeholder":  map[string]any{"type": "plain_text", "text": "Pick a date"},
				"initial_date": "2026-08-01",
			},
		}, view.Blocks[0])
	})

	t.Run("omits datepicker fields that were not provided", func(t *testing.T) {
		t.Parallel()
		input := chat.DateInput("renewal_date", "Renewal Date")
		input.Optional = true
		modal := chat.NewModal("renewal_form", "Renewal")
		modal.Children = []any{input}

		view := ModalToSlackView(modal, "")

		jsonEq(t, map[string]any{
			"type":     "input",
			"block_id": "renewal_date",
			"optional": true,
			"label":    map[string]any{"type": "plain_text", "text": "Renewal Date"},
			"element":  map[string]any{"type": "datepicker", "action_id": "renewal_date"},
		}, view.Blocks[0])
	})

	t.Run("drops an initial date that Slack would reject", func(t *testing.T) {
		t.Parallel()
		for _, initialValue := range []string{
			"30 days from signing",
			"2026-2-1",
			"2026-02-31",
			"not a date",
		} {
			input := chat.DateInput("renewal_date", "Renewal Date")
			input.InitialValue = initialValue
			modal := chat.NewModal("renewal_form", "Renewal")
			modal.Children = []any{input}

			view := ModalToSlackView(modal, "")

			jsonEq(t, map[string]any{
				"type":     "input",
				"block_id": "renewal_date",
				"optional": false,
				"label":    map[string]any{"type": "plain_text", "text": "Renewal Date"},
				"element":  map[string]any{"type": "datepicker", "action_id": "renewal_date"},
			}, view.Blocks[0])
		}
	})

	t.Run("converts a number input to a Block Kit number_input with string bounds", func(t *testing.T) {
		t.Parallel()
		input := chat.NumberInput("quantity", "Quantity")
		input.Placeholder = "How many?"
		input.InitialValue = chat.Float(3)
		input.Min = chat.Float(1)
		input.Max = chat.Float(10)
		input.Decimal = true
		modal := chat.NewModal("order_form", "Order")
		modal.Children = []any{input}

		view := ModalToSlackView(modal, "")

		jsonEq(t, map[string]any{
			"type":     "input",
			"block_id": "quantity",
			"optional": false,
			"label":    map[string]any{"type": "plain_text", "text": "Quantity"},
			"element": map[string]any{
				"type":               "number_input",
				"action_id":          "quantity",
				"is_decimal_allowed": true,
				"placeholder":        map[string]any{"type": "plain_text", "text": "How many?"},
				"initial_value":      "3",
				"min_value":          "1",
				"max_value":          "10",
			},
		}, view.Blocks[0])
	})

	t.Run("defaults number_input to integers only and keeps zero bounds", func(t *testing.T) {
		t.Parallel()
		input := chat.NumberInput("quantity", "Quantity")
		input.InitialValue = chat.Float(0)
		input.Min = chat.Float(0)
		modal := chat.NewModal("order_form", "Order")
		modal.Children = []any{input}

		view := ModalToSlackView(modal, "")

		el := jsonMap(t, jsonMap(t, view.Blocks[0])["element"])
		must.Eq(t, "number_input", el["type"])
		must.Eq(t, false, el["is_decimal_allowed"])
		must.Eq(t, "0", el["initial_value"])
		must.Eq(t, "0", el["min_value"])
	})

	t.Run("omits number_input bounds that were not provided", func(t *testing.T) {
		t.Parallel()
		modal := chat.NewModal("order_form", "Order")
		modal.Children = []any{chat.NumberInput("quantity", "Quantity")}

		view := ModalToSlackView(modal, "")

		jsonEq(t, map[string]any{
			"type":     "input",
			"block_id": "quantity",
			"optional": false,
			"label":    map[string]any{"type": "plain_text", "text": "Quantity"},
			"element": map[string]any{
				"type":               "number_input",
				"action_id":          "quantity",
				"is_decimal_allowed": false,
			},
		}, view.Blocks[0])
	})
}
