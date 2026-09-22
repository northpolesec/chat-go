package chat

import (
	"fmt"
	"testing"

	"github.com/shoenig/test/must"
)

var (
	_ ModalChild = TextInputElement{}
	_ ModalChild = DateInputElement{}
	_ ModalChild = NumberInputElement{}
	_ ModalChild = SelectElement{}
	_ ModalChild = ExternalSelectElement{}
	_ ModalChild = RadioSelectElement{}
	_ ModalChild = CardTextElement{}
	_ ModalChild = FieldsElement{}
)

func panicMessage(fn func()) string {
	var msg string
	func() {
		defer func() {
			if r := recover(); r != nil {
				msg = fmt.Sprint(r)
			}
		}()
		fn()
	}()
	return msg
}

func TestBuilderFunctions(t *testing.T) {
	t.Parallel()

	t.Run("Modal", func(t *testing.T) {
		t.Parallel()

		t.Run("should create a modal with required fields", func(t *testing.T) {
			t.Parallel()
			modal := NewModal("cb-1", "My Modal")
			must.Eq(t, "modal", modal.Type)
			must.Eq(t, "cb-1", modal.CallbackID)
			must.Eq(t, "My Modal", modal.Title)
			must.Eq(t, []any{}, modal.Children)
		})

		t.Run("should include optional fields", func(t *testing.T) {
			t.Parallel()
			modal := NewModal("cb-1", "Test")
			modal.SubmitLabel = "Submit"
			modal.CloseLabel = "Cancel"
			modal.NotifyOnClose = true
			modal.PrivateMetadata = `{"key":"val"}`
			must.Eq(t, "Submit", modal.SubmitLabel)
			must.Eq(t, "Cancel", modal.CloseLabel)
			must.True(t, modal.NotifyOnClose)
			must.Eq(t, `{"key":"val"}`, modal.PrivateMetadata)
		})

		t.Run("should accept children", func(t *testing.T) {
			t.Parallel()
			input := TextInput("t1", "Name")
			modal := NewModal("cb-1", "Test")
			modal.Children = []any{input}
			must.Eq(t, 1, len(modal.Children))
			must.Eq(t, input, modal.Children[0].(TextInputElement))
		})
	})

	t.Run("TextInput", func(t *testing.T) {
		t.Parallel()

		t.Run("should create with required fields", func(t *testing.T) {
			t.Parallel()
			input := TextInput("t1", "Name")
			must.Eq(t, "text_input", input.Type)
			must.Eq(t, "t1", input.ID)
			must.Eq(t, "Name", input.Label)
		})

		t.Run("should include optional fields", func(t *testing.T) {
			t.Parallel()
			input := TextInput("t1", "Name")
			input.Placeholder = "Enter name"
			input.InitialValue = "John"
			input.Multiline = true
			input.Optional = true
			input.MaxLength = 100
			must.Eq(t, "Enter name", input.Placeholder)
			must.Eq(t, "John", input.InitialValue)
			must.True(t, input.Multiline)
			must.True(t, input.Optional)
			must.Eq(t, 100, input.MaxLength)
		})
	})

	t.Run("Select", func(t *testing.T) {
		t.Parallel()

		t.Run("should create with options", func(t *testing.T) {
			t.Parallel()
			sel := Select("s1", "Pick one", []SelectOptionElement{SelectOption("A", "a")})
			must.Eq(t, "select", sel.Type)
			must.Eq(t, 1, len(sel.Options))
		})

		t.Run("should throw with empty options", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "Select requires at least one option", panicMessage(func() {
				Select("s1", "Pick", []SelectOptionElement{})
			}))
		})

		t.Run("should include optional fields", func(t *testing.T) {
			t.Parallel()
			sel := Select("s1", "Pick", []SelectOptionElement{SelectOption("A", "a")})
			sel.Placeholder = "Choose"
			sel.InitialOption = "a"
			sel.Optional = true
			must.Eq(t, "Choose", sel.Placeholder)
			must.Eq(t, "a", sel.InitialOption)
			must.True(t, sel.Optional)
		})
	})

	t.Run("ExternalSelect", func(t *testing.T) {
		t.Parallel()

		t.Run("should create with required fields", func(t *testing.T) {
			t.Parallel()
			externalSelect := ExternalSelect("person", "Person")
			must.Eq(t, "external_select", externalSelect.Type)
			must.Eq(t, "person", externalSelect.ID)
			must.Eq(t, "Person", externalSelect.Label)
		})

		t.Run("should include optional fields", func(t *testing.T) {
			t.Parallel()
			externalSelect := ExternalSelect("person", "Person")
			externalSelect.Placeholder = "Search people"
			externalSelect.MinQueryLength = 1
			externalSelect.Optional = true
			must.Eq(t, "Search people", externalSelect.Placeholder)
			must.Eq(t, 1, externalSelect.MinQueryLength)
			must.True(t, externalSelect.Optional)
		})
	})

	t.Run("DateInput", func(t *testing.T) {
		t.Parallel()

		t.Run("should create with required fields", func(t *testing.T) {
			t.Parallel()
			input := DateInput("d1", "Due date")
			must.Eq(t, "date_input", input.Type)
			must.Eq(t, "d1", input.ID)
			must.Eq(t, "Due date", input.Label)
		})

		t.Run("should include optional fields", func(t *testing.T) {
			t.Parallel()
			input := DateInput("d1", "Due date")
			input.Placeholder = "Pick a date"
			input.InitialValue = "2026-08-01"
			input.Optional = true
			must.Eq(t, "Pick a date", input.Placeholder)
			must.Eq(t, "2026-08-01", input.InitialValue)
			must.True(t, input.Optional)
		})
	})

	t.Run("NumberInput", func(t *testing.T) {
		t.Parallel()

		t.Run("should create with required fields", func(t *testing.T) {
			t.Parallel()
			input := NumberInput("n1", "Quantity")
			must.Eq(t, "number_input", input.Type)
			must.Eq(t, "n1", input.ID)
			must.Eq(t, "Quantity", input.Label)
		})

		t.Run("should include optional fields", func(t *testing.T) {
			t.Parallel()
			input := NumberInput("n1", "Quantity")
			input.Placeholder = "How many?"
			input.InitialValue = Float(3)
			input.Min = Float(1)
			input.Max = Float(10)
			input.Decimal = true
			input.Optional = true
			must.Eq(t, "How many?", input.Placeholder)
			must.Eq(t, 3.0, *input.InitialValue)
			must.Eq(t, 1.0, *input.Min)
			must.Eq(t, 10.0, *input.Max)
			must.True(t, input.Decimal)
			must.True(t, input.Optional)
		})

		t.Run("should keep a zero initial value", func(t *testing.T) {
			t.Parallel()
			input := NumberInput("n1", "Quantity")
			input.InitialValue = Float(0)
			must.Eq(t, 0.0, *input.InitialValue)
		})
	})

	t.Run("SelectOption", func(t *testing.T) {
		t.Parallel()

		t.Run("should create with label and value", func(t *testing.T) {
			t.Parallel()
			opt := SelectOption("Option A", "a")
			must.Eq(t, "Option A", opt.Label)
			must.Eq(t, "a", opt.Value)
		})

		t.Run("should include description", func(t *testing.T) {
			t.Parallel()
			opt := SelectOption("Option A", "a")
			opt.Description = "First option"
			must.Eq(t, "First option", opt.Description)
		})
	})

	t.Run("RadioSelect", func(t *testing.T) {
		t.Parallel()

		t.Run("should create with options", func(t *testing.T) {
			t.Parallel()
			radio := RadioSelect("r1", "Choose", []SelectOptionElement{SelectOption("X", "x")})
			must.Eq(t, "radio_select", radio.Type)
			must.Eq(t, 1, len(radio.Options))
		})

		t.Run("should throw with empty options", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "RadioSelect requires at least one option", panicMessage(func() {
				RadioSelect("r1", "Choose", []SelectOptionElement{})
			}))
		})
	})
}

func TestModalTypeGuards(t *testing.T) {
	t.Parallel()

	t.Run("isModalElement", func(t *testing.T) {
		t.Parallel()

		t.Run("should return true for modal elements", func(t *testing.T) {
			t.Parallel()
			modal := NewModal("cb", "T")
			must.True(t, IsModalElement(modal))
		})

		t.Run("should return false for non-modal elements", func(t *testing.T) {
			t.Parallel()
			must.False(t, IsModalElement(nil))
			must.False(t, IsModalElement("string"))
			must.False(t, IsModalElement(map[string]any{"type": "text_input"}))
		})
	})

	t.Run("filterModalChildren", func(t *testing.T) {
		t.Parallel()

		t.Run("should keep valid child types", func(t *testing.T) {
			t.Parallel()
			children := []any{
				TextInput("t1", "Name"),
				DateInput("d1", "Due date"),
				NumberInput("n1", "Quantity"),
				ExternalSelect("person", "Person"),
				Select("s1", "Pick", []SelectOptionElement{SelectOption("A", "a")}),
			}
			result := FilterModalChildren(children)
			must.Eq(t, 5, len(result))
		})

		t.Run("should filter invalid children and warn", func(t *testing.T) {
			t.Parallel()
			children := []any{
				TextInput("t1", "Name"),
				map[string]any{"type": "unknown_widget"},
			}
			result := FilterModalChildren(children)
			must.Eq(t, 1, len(result))
			must.Eq(t, "[chat] Modal contains unsupported child elements that were ignored", modalUnsupportedChildrenWarn)
		})

		t.Run("should filter non-object items", func(t *testing.T) {
			t.Parallel()
			result := FilterModalChildren([]any{"string", nil, 42})
			must.Eq(t, 0, len(result))
		})
	})
}
