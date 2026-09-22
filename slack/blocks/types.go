// Ported from packages/adapter-slack/src/blocks/types.ts @ 6adca36 (chat v4.40.0).
// Divergences: SlackCardChild / SlackActionsChild / SlackChartDefinition are
// interfaces or a flat struct (TS unions); SlackBlock is a marshal-only fat
// struct (TS index signature); optional strings are ""; table PageSize is
// *int so 0 is distinct from unset; chart values are float64; no unmarshal
// machinery (webhook package owns Slack→us payloads). omitempty/omitzero
// chosen per field so marshaled JSON matches the pinned tests.
package blocks

type SlackButtonStyle string
type SlackTextStyle string
type SlackTableAlignment string

// SlackCardChild is the card-tree union.
type SlackCardChild interface{ slackCardChild() }

// SlackActionsChild is the actions-block child union.
type SlackActionsChild interface{ slackActionsChild() }

type SlackCardElement struct {
	Children []SlackCardChild
	ImageURL string
	Subtitle string
	Title    string
	Type     string
}

type SlackTextElement struct {
	Content string
	Style   SlackTextStyle
	Type    string
}

func (SlackTextElement) slackCardChild() {}

type SlackImageElement struct {
	Alt  string
	Type string
	URL  string
}

func (SlackImageElement) slackCardChild() {}

type SlackDividerElement struct {
	Type string
}

func (SlackDividerElement) slackCardChild() {}

type SlackActionsElement struct {
	Children []SlackActionsChild
	Type     string
}

func (SlackActionsElement) slackCardChild() {}

type SlackButtonElement struct {
	CallbackURL string
	Disabled    bool
	ID          string
	Label       string
	Style       SlackButtonStyle
	Type        string
	Value       string
}

func (SlackButtonElement) slackActionsChild() {}

type SlackLinkButtonElement struct {
	ID    string
	Label string
	Style SlackButtonStyle
	Type  string
	URL   string
}

func (SlackLinkButtonElement) slackActionsChild() {}

type SlackSelectOptionElement struct {
	Description string
	Label       string
	Value       string
}

type SlackSelectElement struct {
	ID            string
	InitialOption string
	Label         string
	Options       []SlackSelectOptionElement
	Placeholder   string
	Type          string
}

func (SlackSelectElement) slackActionsChild() {}

type SlackRadioSelectElement struct {
	ID            string
	InitialOption string
	Label         string
	Options       []SlackSelectOptionElement
	Type          string
}

func (SlackRadioSelectElement) slackActionsChild() {}

type SlackSectionElement struct {
	Children []SlackCardChild
	Type     string
}

func (SlackSectionElement) slackCardChild() {}

type SlackLinkElement struct {
	Label string
	Type  string
	URL   string
}

func (SlackLinkElement) slackCardChild() {}

type SlackFieldElement struct {
	Label string
	Type  string
	Value string
}

type SlackFieldsElement struct {
	Children []SlackFieldElement
	Type     string
}

func (SlackFieldsElement) slackCardChild() {}

type SlackTableElement struct {
	Align    []SlackTableAlignment
	Caption  string
	Headers  []string
	PageSize *int
	Rows     [][]string
	Type     string
}

func (SlackTableElement) slackCardChild() {}

type SlackChartSegment struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

type SlackChartDataPoint struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

type SlackChartSeries struct {
	Data []SlackChartDataPoint `json:"data"`
	Name string                `json:"name"`
}

// SlackChartDefinition is the pie | series union as one struct.
type SlackChartDefinition struct {
	Categories []string
	Segments   []SlackChartSegment
	Series     []SlackChartSeries
	Type       string
	XLabel     string
	YLabel     string
}

type SlackChartElement struct {
	Chart SlackChartDefinition
	Title string
	Type  string
}

func (SlackChartElement) slackCardChild() {}

type SlackTextObject struct {
	Emoji bool   `json:"emoji,omitzero"`
	Text  string `json:"text"`
	Type  string `json:"type"`
}

// SlackBlock is a marshal-only Block Kit block. Optional fields are omitted
// when unset so the JSON matches what the builders emit. Optional is *bool
// so modal input blocks can pin optional:false (nil omits; false marshals).
type SlackBlock struct {
	AltText        string                `json:"alt_text,omitempty"`
	BlockID        string                `json:"block_id,omitempty"`
	Caption        string                `json:"caption,omitempty"`
	Chart          any                   `json:"chart,omitzero"`
	ColumnSettings []*SlackColumnSetting `json:"column_settings,omitzero"`
	Element        any                   `json:"element,omitzero"`
	Elements       []any                 `json:"elements,omitzero"`
	Fields         []SlackTextObject     `json:"fields,omitzero"`
	ImageURL       string                `json:"image_url,omitempty"`
	Label          *SlackTextObject      `json:"label,omitempty"`
	Optional       *bool                 `json:"optional,omitempty"`
	PageSize       int                   `json:"page_size,omitzero"`
	Rows           [][]SlackRawText      `json:"rows,omitzero"`
	Text           *SlackTextObject      `json:"text,omitempty"`
	Title          string                `json:"title,omitempty"`
	Type           string                `json:"type"`
}

type SlackRawText struct {
	Text string `json:"text"`
	Type string `json:"type"`
}

type SlackColumnSetting struct {
	Align string `json:"align,omitempty"`
}

type SlackBlockButton struct {
	ActionID string          `json:"action_id"`
	Style    string          `json:"style,omitempty"`
	Text     SlackTextObject `json:"text"`
	Type     string          `json:"type"`
	URL      string          `json:"url,omitempty"`
	Value    string          `json:"value,omitempty"`
}

type SlackBlockOption struct {
	Description *SlackTextObject `json:"description,omitempty"`
	Text        SlackTextObject  `json:"text"`
	Value       string           `json:"value"`
}

type SlackBlockStaticSelect struct {
	ActionID      string             `json:"action_id"`
	InitialOption *SlackBlockOption  `json:"initial_option,omitempty"`
	Options       []SlackBlockOption `json:"options,omitzero"`
	Placeholder   *SlackTextObject   `json:"placeholder,omitempty"`
	Type          string             `json:"type"`
}

type SlackBlockRadioButtons struct {
	ActionID      string             `json:"action_id"`
	InitialOption *SlackBlockOption  `json:"initial_option,omitempty"`
	Options       []SlackBlockOption `json:"options,omitzero"`
	Type          string             `json:"type"`
}

type SlackPieChart struct {
	Segments []SlackChartSegment `json:"segments"`
	Type     string              `json:"type"`
}

type SlackAxisConfig struct {
	Categories []string `json:"categories"`
	XLabel     string   `json:"x_label,omitempty"`
	YLabel     string   `json:"y_label,omitempty"`
}

type SlackSeriesChart struct {
	AxisConfig SlackAxisConfig    `json:"axis_config"`
	Series     []SlackChartSeries `json:"series"`
	Type       string             `json:"type"`
}

type SlackPlainTextInput struct {
	ActionID  string `json:"action_id"`
	Multiline bool   `json:"multiline,omitzero"`
	Type      string `json:"type"`
}

type SlackModalView struct {
	Blocks          []SlackBlock    `json:"blocks"`
	CallbackID      string          `json:"callback_id"`
	Close           SlackTextObject `json:"close"`
	PrivateMetadata string          `json:"private_metadata"`
	Submit          SlackTextObject `json:"submit"`
	Title           SlackTextObject `json:"title"`
	Type            string          `json:"type"`
}

type SlackBlocksOptions struct {
	ConvertEmoji func(string) string
	MaxBlocks    *int
}
