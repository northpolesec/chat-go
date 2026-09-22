// Ported from packages/chat/src/modals.ts @ 6adca36 (chat v4.40.0).
// Divergences: JSX/fromReactModalElement not ported (struct literals are the
// tree). Modal moved from types.go so the element model has one home. Children
// stay []any (Task 2). Builders that share a type name use the cards.go
// Element-suffix + ctor pattern (Select → SelectElement / Select()). Empty
// Select/RadioSelect options panic with the upstream Error message.
// console.warn → slog.Warn. toModalElement (jsx-runtime.ts) is ToModalElement
// (*Modal or nil); JSX resolution is omitted. NumberInput InitialValue/Min/Max
// are *float64 (nil = unset). Float is a Go-divergence helper for literals.
package chat

import (
	"log/slog"
	"slices"
)

const modalUnsupportedChildrenWarn = "[chat] Modal contains unsupported child elements that were ignored"

// validModalChildTypes is VALID_MODAL_CHILD_TYPES.
var validModalChildTypes = []string{
	"text_input",
	"date_input",
	"number_input",
	"select",
	"external_select",
	"radio_select",
	"text",
	"fields",
}

// ModalChild is the marker for modal tree nodes (JSX-element stand-in).
// Upstream: TextInput | DateInput | NumberInput | Select | ExternalSelect |
// RadioSelect | TextElement | FieldsElement. Modal and SelectOption are not
// children.
type ModalChild interface{ isModalChild() }

type typedModalChild interface {
	ModalChild
	modalType() string
}

// Modal is the root modal element. Moved from types.go.
type Modal struct {
	CallbackID      string
	CallbackURL     string
	Children        []any
	CloseLabel      string
	NotifyOnClose   bool
	PrivateMetadata string
	SubmitLabel     string
	Title           string
	Type            string
}

func NewModal(callbackID, title string) Modal {
	return Modal{CallbackID: callbackID, Children: []any{}, Title: title, Type: "modal"}
}

type TextInputElement struct {
	ID           string
	InitialValue string
	Label        string
	MaxLength    int
	Multiline    bool
	Optional     bool
	Placeholder  string
	Type         string
}

func (TextInputElement) isModalChild()       {}
func (e TextInputElement) modalType() string { return e.Type }

func TextInput(id, label string) TextInputElement {
	return TextInputElement{ID: id, Label: label, Type: "text_input"}
}

type DateInputElement struct {
	ID           string
	InitialValue string
	Label        string
	Optional     bool
	Placeholder  string
	Type         string
}

func (DateInputElement) isModalChild()       {}
func (e DateInputElement) modalType() string { return e.Type }

func DateInput(id, label string) DateInputElement {
	return DateInputElement{ID: id, Label: label, Type: "date_input"}
}

type NumberInputElement struct {
	Decimal      bool
	ID           string
	InitialValue *float64
	Label        string
	Max          *float64
	Min          *float64
	Optional     bool
	Placeholder  string
	Type         string
}

func (NumberInputElement) isModalChild()       {}
func (e NumberInputElement) modalType() string { return e.Type }

func NumberInput(id, label string) NumberInputElement {
	return NumberInputElement{ID: id, Label: label, Type: "number_input"}
}

// Float is a Go-divergence helper for NumberInput *float64 literals
// (JS number | undefined).
func Float(v float64) *float64 { return &v }

type SelectOptionElement struct {
	Description string
	Label       string
	Value       string
}

func SelectOption(label, value string) SelectOptionElement {
	return SelectOptionElement{Label: label, Value: value}
}

type SelectElement struct {
	ID            string
	InitialOption string
	Label         string
	Optional      bool
	Options       []SelectOptionElement
	Placeholder   string
	Type          string
}

func (SelectElement) isModalChild()       {}
func (e SelectElement) modalType() string { return e.Type }

func Select(id, label string, options []SelectOptionElement) SelectElement {
	if len(options) == 0 {
		panic("Select requires at least one option")
	}
	return SelectElement{ID: id, Label: label, Options: options, Type: "select"}
}

type ExternalSelectElement struct {
	ID             string
	InitialOption  SelectOptionElement
	Label          string
	MinQueryLength int
	Optional       bool
	Placeholder    string
	Type           string
}

func (ExternalSelectElement) isModalChild()       {}
func (e ExternalSelectElement) modalType() string { return e.Type }

func ExternalSelect(id, label string) ExternalSelectElement {
	return ExternalSelectElement{ID: id, Label: label, Type: "external_select"}
}

type RadioSelectElement struct {
	ID            string
	InitialOption string
	Label         string
	Optional      bool
	Options       []SelectOptionElement
	Type          string
}

func (RadioSelectElement) isModalChild()       {}
func (e RadioSelectElement) modalType() string { return e.Type }

func RadioSelect(id, label string, options []SelectOptionElement) RadioSelectElement {
	if len(options) == 0 {
		panic("RadioSelect requires at least one option")
	}
	return RadioSelectElement{ID: id, Label: label, Options: options, Type: "radio_select"}
}

// IsModalElement is the upstream isModalElement type guard.
func IsModalElement(value any) bool {
	switch v := value.(type) {
	case Modal:
		return v.Type == "modal"
	case *Modal:
		return v != nil && v.Type == "modal"
	default:
		return false
	}
}

// ToModalElement is jsx-runtime toModalElement without JSX resolution.
func ToModalElement(value any) *Modal {
	switch v := value.(type) {
	case Modal:
		if v.Type == "modal" {
			return &v
		}
	case *Modal:
		if v != nil && v.Type == "modal" {
			return v
		}
	}
	return nil
}

// FilterModalChildren drops unsupported children and warns when any were ignored.
func FilterModalChildren(children []any) []ModalChild {
	var out []ModalChild
	for _, c := range children {
		child, ok := c.(typedModalChild)
		if !ok || !slices.Contains(validModalChildTypes, child.modalType()) {
			continue
		}
		out = append(out, child)
	}
	if len(out) < len(children) {
		slog.Warn(modalUnsupportedChildrenWarn)
	}
	return out
}
