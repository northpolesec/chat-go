// Ported from packages/chat/src/cards.ts @ 6adca36 (chat v4.40.0).
// Divergences: JSX/fromReactElement not ported (struct literals are the tree;
// isJSX → CardChild / IsCardElement assertions); Text builder is CardText
// (markdown.go already owns Text); tableElementToAscii stays in markdown.go
// (cards.ts imports it); ChartElementToFallbackText lives here (markdown.ts
// export, one implementation); Card moved from types.go so the element model
// has one home. Card children stay []any (Task 2). No Header element exists
// upstream. Select/RadioSelect live in modals.go. Text and Fields are also
// ModalChild (VALID_MODAL_CHILD_TYPES).
package chat

import (
	"fmt"
	"strings"
)

// CardChild is the marker for card tree nodes (JSX-element stand-in).
type CardChild interface{ isCardChild() }

type CardWidth string

const (
	CardWidthDefault CardWidth = "default"
	CardWidthFull    CardWidth = "full"
)

// Card is the root card element. It is an AdapterPostableMessage.
type Card struct {
	Children []any
	ImageURL string
	Subtitle string
	Title    string
	Type     string
	Width    CardWidth
}

func (Card) isPostable() {}

type CardTextElement struct {
	Content string
	Style   string
	Type    string
}

func (CardTextElement) isCardChild()        {}
func (CardTextElement) isModalChild()       {}
func (e CardTextElement) modalType() string { return e.Type }

func CardText(content string) CardTextElement {
	return CardTextElement{Content: content, Type: "text"}
}

type ImageElement struct {
	Alt  string
	Type string
	URL  string
}

func (ImageElement) isCardChild() {}

func Image(url string, alt ...string) ImageElement {
	el := ImageElement{Type: "image", URL: url}
	if len(alt) > 0 {
		el.Alt = alt[0]
	}
	return el
}

type DividerElement struct {
	Type string
}

func (DividerElement) isCardChild() {}

func Divider() DividerElement {
	return DividerElement{Type: "divider"}
}

type ButtonElement struct {
	ActionType  string
	CallbackURL string
	Disabled    bool
	ID          string
	Label       string
	Style       string
	Tooltip     string
	Type        string
	Value       string
}

func (ButtonElement) isCardChild() {}

func Button(id, label string) ButtonElement {
	return ButtonElement{ID: id, Label: label, Type: "button"}
}

type LinkButtonElement struct {
	ID      string
	Label   string
	Style   string
	Tooltip string
	Type    string
	URL     string
}

func (LinkButtonElement) isCardChild() {}

func LinkButton(url, label string) LinkButtonElement {
	return LinkButtonElement{Type: "link-button", URL: url, Label: label}
}

type LinkElement struct {
	Label string
	Type  string
	URL   string
}

func (LinkElement) isCardChild() {}

func CardLink(url, label string) LinkElement {
	return LinkElement{Type: "link", URL: url, Label: label}
}

type ActionsElement struct {
	Children []any
	Type     string
}

func (ActionsElement) isCardChild() {}

func Actions(children ...any) ActionsElement {
	if children == nil {
		children = []any{}
	}
	return ActionsElement{Children: children, Type: "actions"}
}

type SectionElement struct {
	Children []any
	Type     string
}

func (SectionElement) isCardChild() {}

func Section(children ...any) SectionElement {
	return SectionElement{Children: children, Type: "section"}
}

type FieldElement struct {
	Label string
	Type  string
	Value string
}

func (FieldElement) isCardChild() {}

func Field(label, value string) FieldElement {
	return FieldElement{Label: label, Type: "field", Value: value}
}

type FieldsElement struct {
	Children []FieldElement
	Type     string
}

func (FieldsElement) isCardChild()        {}
func (FieldsElement) isModalChild()       {}
func (e FieldsElement) modalType() string { return e.Type }

func Fields(children ...FieldElement) FieldsElement {
	return FieldsElement{Children: children, Type: "fields"}
}

type TableElement struct {
	Align    []string
	Caption  string
	Headers  []string
	PageSize int
	Rows     [][]string
	Type     string
}

func (TableElement) isCardChild() {}

func CardTable(headers []string, rows [][]string) TableElement {
	return TableElement{Headers: headers, Rows: rows, Type: "table"}
}

type ChartSegment struct {
	Label string
	Value any
}

type ChartDataPoint struct {
	Label string
	Value any
}

type ChartSeries struct {
	Data []ChartDataPoint
	Name string
}

type ChartDefinition struct {
	Categories []string
	Segments   []ChartSegment
	Series     []ChartSeries
	Type       string
	XLabel     string
	YLabel     string
}

type ChartElement struct {
	Chart ChartDefinition
	Title string
	Type  string
}

func (ChartElement) isCardChild() {}

// IsCardElement is the upstream isCardElement type guard.
func IsCardElement(value any) bool {
	switch v := value.(type) {
	case Card:
		return v.Type == "card"
	case *Card:
		return v != nil && v.Type == "card"
	default:
		return false
	}
}

// CardChildToFallbackText is the cards.ts standalone fallback (string | null → "").
func CardChildToFallbackText(child any) string {
	switch ch := child.(type) {
	case CardTextElement:
		return ch.Content
	case LinkElement:
		return ch.Label + " (" + ch.URL + ")"
	case FieldsElement:
		lines := make([]string, len(ch.Children))
		for i, f := range ch.Children {
			lines[i] = f.Label + ": " + f.Value
		}
		return strings.Join(lines, "\n")
	case ActionsElement:
		return ""
	case TableElement:
		return TableElementToASCII(ch.Headers, ch.Rows)
	case ChartElement:
		return ChartElementToFallbackText(ch)
	case SectionElement:
		var parts []string
		for _, nested := range ch.Children {
			if text := CardChildToFallbackText(nested); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

// ChartElementToFallbackText renders a chart as title + ASCII table.
func ChartElementToFallbackText(element ChartElement) string {
	chart := element.Chart
	if chart.Type == "pie" {
		rows := make([][]string, len(chart.Segments))
		for i, s := range chart.Segments {
			rows[i] = []string{s.Label, fmt.Sprint(s.Value)}
		}
		return element.Title + "\n" + TableElementToASCII([]string{"Label", "Value"}, rows)
	}
	headers := make([]string, 0, 1+len(chart.Series))
	headers = append(headers, chart.XLabel)
	for _, s := range chart.Series {
		headers = append(headers, s.Name)
	}
	rows := make([][]string, len(chart.Categories))
	for i, category := range chart.Categories {
		row := make([]string, 0, 1+len(chart.Series))
		row = append(row, category)
		for _, s := range chart.Series {
			cell := ""
			for _, p := range s.Data {
				if p.Label == category {
					cell = fmt.Sprint(p.Value)
					break
				}
			}
			row = append(row, cell)
		}
		rows[i] = row
	}
	return element.Title + "\n" + TableElementToASCII(headers, rows)
}
