// Ported from packages/adapter-slack/src/blocks/input.ts @ 6adca36 (chat v4.40.0).
// Divergences: selectedOptionValue / value / view value are *string so unset
// is distinct from ""; title/prompt "" uses the next fallback (JS ?? only
// skips null/undefined); non-string metadata is json.Marshal (panic on
// error); parse returns *SlackInputResponse (nil = null).
package blocks

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

const (
	SlackInputActionPrefix    = "input:"
	SlackFreeformActionPrefix = "input-freeform:"
	SlackFreeformCallbackID   = "input-freeform-submit"
	SlackFreeformBlockID      = "input-freeform-block"
	SlackFreeformActionID     = "input-freeform-text"
	freeformTitleMax          = 24
)

var buttonActionPattern = regexp.MustCompile(`^(.+):button:\d+$`)

type SlackInputOption struct {
	Description string
	ID          string
	Label       string
	Style       SlackButtonStyle
}

type SlackInputRequest struct {
	AllowFreeform bool
	Display       string
	Options       []SlackInputOption
	Prompt        string
	RequestID     string
}

type SlackInputAction struct {
	ActionID            string
	SelectedOptionValue *string
	Value               *string
}

type SlackInputResponse struct {
	OptionID  string
	RequestID string
}

type SlackFreeformViewOptions struct {
	Metadata any
	Prompt   string
	Title    string
}

type SlackViewValue struct {
	ActionID string
	BlockID  string
	Value    *string
}

type SlackAnsweredInput struct {
	Answer      string
	PromptBlock any
	UserID      string
}

func InputRequestToSlackBlocks(request SlackInputRequest) []SlackBlock {
	prompt := SlackBlock{
		Text: &SlackTextObject{Text: truncateText(request.Prompt, SectionText), Type: "mrkdwn"},
		Type: "section",
	}
	options := request.Options
	if options == nil {
		options = []SlackInputOption{}
	}
	if len(options) == 0 {
		return []SlackBlock{prompt, {Elements: []any{freeformButton(request.RequestID)}, Type: "actions"}}
	}
	var extras []any
	if request.AllowFreeform {
		extras = []any{freeformButton(request.RequestID)}
	}
	if request.Display == "radio" {
		return []SlackBlock{prompt, {Elements: append([]any{radioElement(request)}, extras...), Type: "actions"}}
	}
	if request.Display == "select" {
		return []SlackBlock{prompt, {Elements: append([]any{selectElement(request)}, extras...), Type: "actions"}}
	}
	limit := ActionsElements
	if len(extras) > 0 {
		limit = ActionsElements - 1
	}
	limit = min(limit, len(options))
	elements := make([]any, 0, limit+len(extras))
	for i, option := range options[:limit] {
		elements = append(elements, buttonElement(request.RequestID, option, i))
	}
	elements = append(elements, extras...)
	return []SlackBlock{prompt, {Elements: elements, Type: "actions"}}
}

func ParseSlackInputResponse(action SlackInputAction) *SlackInputResponse {
	if !strings.HasPrefix(action.ActionID, SlackInputActionPrefix) {
		return nil
	}
	id := action.ActionID[len(SlackInputActionPrefix):]
	if action.SelectedOptionValue != nil {
		if id == "" {
			return nil
		}
		return &SlackInputResponse{OptionID: *action.SelectedOptionValue, RequestID: id}
	}
	match := buttonActionPattern.FindStringSubmatch(id)
	if len(match) == 2 && action.Value != nil {
		return &SlackInputResponse{OptionID: *action.Value, RequestID: match[1]}
	}
	return nil
}

func BuildSlackFreeformView(options SlackFreeformViewOptions) SlackModalView {
	title := options.Title
	if title == "" {
		title = options.Prompt
	}
	if title == "" {
		title = "Your answer"
	}
	title = truncateText(title, freeformTitleMax)
	var blocks []SlackBlock
	if options.Prompt != "" {
		blocks = append(blocks, SlackBlock{
			Text: &SlackTextObject{Text: truncateText(options.Prompt, SectionText), Type: "mrkdwn"},
			Type: "section",
		})
	}
	blocks = append(blocks, SlackBlock{
		BlockID: SlackFreeformBlockID,
		Element: SlackPlainTextInput{
			ActionID:  SlackFreeformActionID,
			Multiline: true,
			Type:      "plain_text_input",
		},
		Label: &SlackTextObject{Text: "Answer", Type: "plain_text"},
		Type:  "input",
	})
	return SlackModalView{
		Blocks:          blocks,
		CallbackID:      SlackFreeformCallbackID,
		Close:           SlackTextObject{Text: "Cancel", Type: "plain_text"},
		PrivateMetadata: metadataString(options.Metadata),
		Submit:          SlackTextObject{Text: "Submit", Type: "plain_text"},
		Title:           SlackTextObject{Text: title, Type: "plain_text"},
		Type:            "modal",
	}
}

func ParseSlackFreeformValue(values []SlackViewValue) string {
	for _, value := range values {
		if value.BlockID == SlackFreeformBlockID && value.ActionID == SlackFreeformActionID {
			if value.Value == nil {
				return ""
			}
			return *value.Value
		}
	}
	return ""
}

func AnsweredSlackInputBlocks(input SlackAnsweredInput) []SlackBlock {
	var blocks []SlackBlock
	switch b := input.PromptBlock.(type) {
	case SlackBlock:
		blocks = append(blocks, b)
	case *SlackBlock:
		if b != nil {
			blocks = append(blocks, *b)
		}
	}
	blocks = append(blocks, SlackBlock{
		Text: &SlackTextObject{Text: ":white_check_mark: *" + input.Answer + "*", Type: "mrkdwn"},
		Type: "section",
	})
	if input.UserID != "" {
		blocks = append(blocks, SlackBlock{
			Elements: []any{SlackTextObject{Text: "Answered by <@" + input.UserID + ">", Type: "mrkdwn"}},
			Type:     "context",
		})
	}
	return blocks
}

func freeformButton(requestID string) SlackBlockButton {
	return SlackBlockButton{
		ActionID: SlackFreeformActionPrefix + requestID,
		Style:    "primary",
		Text:     SlackTextObject{Text: "Type your answer", Type: "plain_text"},
		Type:     "button",
		Value:    requestID,
	}
}

func buttonElement(requestID string, option SlackInputOption, index int) SlackBlockButton {
	style := ""
	if option.Style == "primary" || option.Style == "danger" {
		style = string(option.Style)
	}
	return SlackBlockButton{
		ActionID: SlackInputActionPrefix + requestID + ":button:" + strconv.Itoa(index),
		Style:    style,
		Text:     SlackTextObject{Text: truncateText(option.Label, ButtonText), Type: "plain_text"},
		Type:     "button",
		Value:    truncateText(option.ID, ButtonValue),
	}
}

func selectElement(request SlackInputRequest) SlackBlockStaticSelect {
	options := request.Options
	if options == nil {
		options = []SlackInputOption{}
	}
	out := make([]SlackBlockOption, 0, len(options))
	for _, option := range options {
		out = append(out, SlackBlockOption{
			Text:  SlackTextObject{Text: truncateText(option.Label, OptionText), Type: "plain_text"},
			Value: truncateText(option.ID, OptionValue),
		})
	}
	if len(out) > Options {
		out = out[:Options]
	}
	placeholder := SlackTextObject{Text: "Choose an option", Type: "plain_text"}
	return SlackBlockStaticSelect{
		ActionID:    SlackInputActionPrefix + request.RequestID,
		Options:     out,
		Placeholder: &placeholder,
		Type:        "static_select",
	}
}

func radioElement(request SlackInputRequest) any {
	options := request.Options
	if options == nil {
		options = []SlackInputOption{}
	}
	out := make([]SlackBlockOption, 0, len(options))
	for _, option := range options {
		out = append(out, SlackBlockOption{
			Text:  SlackTextObject{Text: truncateText(option.Label, OptionText), Type: "plain_text"},
			Value: truncateText(option.ID, OptionValue),
		})
	}
	if len(out) > RadioOptions {
		return selectElement(request)
	}
	return SlackBlockRadioButtons{
		ActionID: SlackInputActionPrefix + request.RequestID,
		Options:  out,
		Type:     "radio_buttons",
	}
}

func metadataString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(raw)
}
