// Ported from packages/adapter-slack/src/modals.ts @ 6adca36 (chat v4.40.0).
// Divergences: console.warn → slog.Warn; JS undefined → ""; title slice(0, 24)
// is Go bytes (ASCII fixtures); SlackBlock.Optional is *bool (Task 18 fat
// struct) so optional:false is present in JSON. View-submission state walking
// lives in adapter index.ts (Phase G), not here.
package slack

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"time"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/slack/blocks"
)

const (
	modalTitleMax     = 24
	radioSelectMaxOpt = 10
	defaultSubmit     = "Submit"
	defaultClose      = "Cancel"
)

var isoDateRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

type SlackView struct {
	Blocks          []SlackBlock           `json:"blocks"`
	CallbackID      string                 `json:"callback_id"`
	Close           blocks.SlackTextObject `json:"close"`
	NotifyOnClose   bool                   `json:"notify_on_close,omitempty"`
	PrivateMetadata string                 `json:"private_metadata,omitempty"`
	Submit          blocks.SlackTextObject `json:"submit"`
	Title           blocks.SlackTextObject `json:"title"`
	Type            string                 `json:"type"`
}

type SlackModalResponse struct {
	Errors         map[string]string `json:"errors,omitempty"`
	ResponseAction string            `json:"response_action,omitempty"`
	View           *SlackView        `json:"view,omitempty"`
}

type SlackOptionObject struct {
	Description *blocks.SlackTextObject `json:"description,omitempty"`
	Text        blocks.SlackTextObject  `json:"text"`
	Value       string                  `json:"value"`
}

type ModalMetadata struct {
	ContextID       string
	PrivateMetadata string
}

type modalMetadataJSON struct {
	C string `json:"c,omitempty"`
	M string `json:"m,omitempty"`
}

type slackPlainTextInput struct {
	ActionID     string                  `json:"action_id"`
	InitialValue string                  `json:"initial_value,omitempty"`
	MaxLength    int                     `json:"max_length,omitzero"`
	Multiline    bool                    `json:"multiline"`
	Placeholder  *blocks.SlackTextObject `json:"placeholder,omitempty"`
	Type         string                  `json:"type"`
}

type slackDatepicker struct {
	ActionID    string                  `json:"action_id"`
	InitialDate string                  `json:"initial_date,omitempty"`
	Placeholder *blocks.SlackTextObject `json:"placeholder,omitempty"`
	Type        string                  `json:"type"`
}

type slackNumberInput struct {
	ActionID         string                  `json:"action_id"`
	InitialValue     string                  `json:"initial_value,omitempty"`
	IsDecimalAllowed bool                    `json:"is_decimal_allowed"`
	MaxValue         string                  `json:"max_value,omitempty"`
	MinValue         string                  `json:"min_value,omitempty"`
	Placeholder      *blocks.SlackTextObject `json:"placeholder,omitempty"`
	Type             string                  `json:"type"`
}

type slackStaticSelect struct {
	ActionID      string                  `json:"action_id"`
	InitialOption *SlackOptionObject      `json:"initial_option,omitempty"`
	Options       []SlackOptionObject     `json:"options"`
	Placeholder   *blocks.SlackTextObject `json:"placeholder,omitempty"`
	Type          string                  `json:"type"`
}

type slackExternalSelect struct {
	ActionID       string                  `json:"action_id"`
	InitialOption  *SlackOptionObject      `json:"initial_option,omitempty"`
	MinQueryLength int                     `json:"min_query_length,omitzero"`
	Placeholder    *blocks.SlackTextObject `json:"placeholder,omitempty"`
	Type           string                  `json:"type"`
}

type slackRadioButtons struct {
	ActionID      string              `json:"action_id"`
	InitialOption *SlackOptionObject  `json:"initial_option,omitempty"`
	Options       []SlackOptionObject `json:"options"`
	Type          string              `json:"type"`
}

func EncodeModalMetadata(meta ModalMetadata) string {
	if meta.ContextID == "" && meta.PrivateMetadata == "" {
		return ""
	}
	raw, err := json.Marshal(modalMetadataJSON{C: meta.ContextID, M: meta.PrivateMetadata})
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func DecodeModalMetadata(raw string) ModalMetadata {
	if raw == "" {
		return ModalMetadata{}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil || parsed == nil {
		return ModalMetadata{ContextID: raw}
	}
	_, hasC := parsed["c"]
	_, hasM := parsed["m"]
	if !hasC && !hasM {
		return ModalMetadata{ContextID: raw}
	}
	return ModalMetadata{
		ContextID:       stringOrEmpty(parsed["c"]),
		PrivateMetadata: stringOrEmpty(parsed["m"]),
	}
}

func stringOrEmpty(v any) string {
	s, _ := v.(string)
	return s
}

func ModalToSlackView(modal chat.Modal, contextID string) SlackView {
	submit := modal.SubmitLabel
	if submit == "" {
		submit = defaultSubmit
	}
	closeLabel := modal.CloseLabel
	if closeLabel == "" {
		closeLabel = defaultClose
	}
	title := modal.Title
	if len(title) > modalTitleMax {
		title = title[:modalTitleMax]
	}
	out := make([]SlackBlock, 0, len(modal.Children))
	for _, child := range modal.Children {
		out = append(out, modalChildToBlock(child))
	}
	return SlackView{
		Type:            "modal",
		CallbackID:      modal.CallbackID,
		Title:           blocks.SlackTextObject{Type: "plain_text", Text: title},
		Submit:          blocks.SlackTextObject{Type: "plain_text", Text: submit},
		Close:           blocks.SlackTextObject{Type: "plain_text", Text: closeLabel},
		NotifyOnClose:   modal.NotifyOnClose,
		PrivateMetadata: contextID,
		Blocks:          out,
	}
}

func modalChildToBlock(child any) SlackBlock {
	switch c := child.(type) {
	case chat.TextInputElement:
		return textInputToBlock(c)
	case chat.DateInputElement:
		return dateInputToBlock(c)
	case chat.NumberInputElement:
		return numberInputToBlock(c)
	case chat.SelectElement:
		return selectToBlock(c)
	case chat.ExternalSelectElement:
		return externalSelectToBlock(c)
	case chat.RadioSelectElement:
		return radioSelectToBlock(c)
	case chat.CardTextElement:
		return ConvertTextToBlock(c)
	case chat.FieldsElement:
		return ConvertFieldsToBlock(c)
	default:
		panic(fmt.Sprintf("Unknown modal child type: %T", child))
	}
}

func SelectOptionToSlackOption(option chat.SelectOptionElement) SlackOptionObject {
	out := SlackOptionObject{
		Text:  blocks.SlackTextObject{Type: "plain_text", Text: option.Label},
		Value: option.Value,
	}
	if option.Description != "" {
		d := blocks.SlackTextObject{Type: "plain_text", Text: option.Description}
		out.Description = &d
	}
	return out
}

func textInputToBlock(input chat.TextInputElement) SlackBlock {
	element := slackPlainTextInput{
		Type:      "plain_text_input",
		ActionID:  input.ID,
		Multiline: input.Multiline,
	}
	if input.Placeholder != "" {
		element.Placeholder = plainTextPtr(input.Placeholder)
	}
	if input.InitialValue != "" {
		element.InitialValue = input.InitialValue
	}
	if input.MaxLength != 0 {
		element.MaxLength = input.MaxLength
	}
	return inputBlock(input.ID, input.Label, input.Optional, element)
}

func toInitialDate(value string) string {
	if value == "" {
		return ""
	}
	parsed, err := time.Parse(time.RFC3339, value+"T00:00:00Z")
	if !isoDateRE.MatchString(value) || err != nil || parsed.Format("2006-01-02") != value {
		slog.Warn(fmt.Sprintf(`[chat] DateInput "%s" is not a YYYY-MM-DD date — ignoring initialValue`, value))
		return ""
	}
	return value
}

func dateInputToBlock(input chat.DateInputElement) SlackBlock {
	element := slackDatepicker{
		Type:     "datepicker",
		ActionID: input.ID,
	}
	if input.Placeholder != "" {
		element.Placeholder = plainTextPtr(input.Placeholder)
	}
	if initialDate := toInitialDate(input.InitialValue); initialDate != "" {
		element.InitialDate = initialDate
	}
	return inputBlock(input.ID, input.Label, input.Optional, element)
}

func numberInputToBlock(input chat.NumberInputElement) SlackBlock {
	element := slackNumberInput{
		Type:             "number_input",
		ActionID:         input.ID,
		IsDecimalAllowed: input.Decimal,
	}
	if input.Placeholder != "" {
		element.Placeholder = plainTextPtr(input.Placeholder)
	}
	if input.InitialValue != nil {
		element.InitialValue = formatNumber(*input.InitialValue)
	}
	if input.Min != nil {
		element.MinValue = formatNumber(*input.Min)
	}
	if input.Max != nil {
		element.MaxValue = formatNumber(*input.Max)
	}
	return inputBlock(input.ID, input.Label, input.Optional, element)
}

func selectToBlock(sel chat.SelectElement) SlackBlock {
	options := make([]SlackOptionObject, len(sel.Options))
	for i, opt := range sel.Options {
		options[i] = SelectOptionToSlackOption(opt)
	}
	element := slackStaticSelect{
		Type:     "static_select",
		ActionID: sel.ID,
		Options:  options,
	}
	if sel.Placeholder != "" {
		element.Placeholder = plainTextPtr(sel.Placeholder)
	}
	if sel.InitialOption != "" {
		for i := range options {
			if options[i].Value == sel.InitialOption {
				opt := options[i]
				element.InitialOption = &opt
				break
			}
		}
	}
	return inputBlock(sel.ID, sel.Label, sel.Optional, element)
}

func externalSelectToBlock(sel chat.ExternalSelectElement) SlackBlock {
	element := slackExternalSelect{
		Type:     "external_select",
		ActionID: sel.ID,
	}
	if sel.Placeholder != "" {
		element.Placeholder = plainTextPtr(sel.Placeholder)
	}
	if sel.MinQueryLength != 0 {
		element.MinQueryLength = sel.MinQueryLength
	}
	if sel.InitialOption.Label != "" || sel.InitialOption.Value != "" {
		opt := SelectOptionToSlackOption(sel.InitialOption)
		element.InitialOption = &opt
	}
	return inputBlock(sel.ID, sel.Label, sel.Optional, element)
}

func radioSelectToBlock(radio chat.RadioSelectElement) SlackBlock {
	limited := radio.Options
	if len(limited) > radioSelectMaxOpt {
		limited = limited[:radioSelectMaxOpt]
	}
	options := make([]SlackOptionObject, len(limited))
	for i, opt := range limited {
		option := SlackOptionObject{
			Text:  blocks.SlackTextObject{Type: "mrkdwn", Text: opt.Label},
			Value: opt.Value,
		}
		if opt.Description != "" {
			d := blocks.SlackTextObject{Type: "mrkdwn", Text: opt.Description}
			option.Description = &d
		}
		options[i] = option
	}
	element := slackRadioButtons{
		Type:     "radio_buttons",
		ActionID: radio.ID,
		Options:  options,
	}
	if radio.InitialOption != "" {
		for i := range options {
			if options[i].Value == radio.InitialOption {
				opt := options[i]
				element.InitialOption = &opt
				break
			}
		}
	}
	return inputBlock(radio.ID, radio.Label, radio.Optional, element)
}

func inputBlock(id, label string, optional bool, element any) SlackBlock {
	lbl := blocks.SlackTextObject{Type: "plain_text", Text: label}
	return SlackBlock{
		Type:     "input",
		BlockID:  id,
		Optional: boolPtr(optional),
		Label:    &lbl,
		Element:  element,
	}
}

func plainTextPtr(text string) *blocks.SlackTextObject {
	t := blocks.SlackTextObject{Type: "plain_text", Text: text}
	return &t
}

func boolPtr(v bool) *bool { return &v }

func formatNumber(n float64) string {
	return strconv.FormatFloat(n, 'f', -1, 64)
}
