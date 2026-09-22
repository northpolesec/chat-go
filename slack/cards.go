// Ported from packages/adapter-slack/src/cards.ts @ 6adca36 (chat v4.40.0).
//
// Layering: cards.ts and blocks/index.ts are independent converters that
// happen to export the same names. Neither wraps the other. Adapter
// index.ts re-exports cardToBlockKit / cardToFallbackText / SlackBlock from
// ./cards (chat.Card). blocks/index.ts aliases cardToBlockKit =
// cardToSlackBlocks for SlackCardElement (Task 18). This file ports cards.ts
// only; it does not call slack/blocks builders.
//
// Divergences: SlackBlock is a type alias of blocks.SlackBlock (marshal-only
// fat struct for the TS index signature); output element structs are the
// Task 18 types (same JSON); CardToBlockKit returns []SlackBlock with no
// error (upstream does not throw); chat-side cardToFallbackText (cards.ts:921)
// is not imported here (this file uses shared.CardToFallbackText); JS
// string .length → Go bytes on ASCII fixtures; fenced ASCII fallback uses
// cards.ts's budget-1 + ellipsis cut (JS char length; the 3,000-char test
// asserts rune count).
package slack

import (
	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/northpolesec/chat-go/slack/blocks"
)

const (
	dataTableMinPageSize = 1
	chartMaxPerMessage   = 2
	chartMaxSegments     = 12
	chartMaxSeries       = 12
	chartMaxDataPoints   = 20
	chartMaxLabelChars   = 20
	dataTableMaxCols     = 20
	chartMaxTitleChars   = 50
	dataTableMaxPageSize = 100
	dataTableMaxRows     = 100
	linkButtonURLIDMax   = 200
	sectionTextMaxChars  = 3000
	dataTableMaxChars    = 10_000
)

var convertEmoji = shared.CreateEmojiConverter(shared.PlatformSlack)

// SlackBlock is the cards.ts SlackBlock re-export (TS index signature).
type SlackBlock = blocks.SlackBlock

func CardToBlockKit(card chat.Card) []SlackBlock {
	var out []SlackBlock

	if card.Title != "" {
		out = append(out, SlackBlock{
			Type: "header",
			Text: &blocks.SlackTextObject{
				Type:  "plain_text",
				Text:  convertEmoji(card.Title),
				Emoji: true,
			},
		})
	}
	if card.Subtitle != "" {
		out = append(out, SlackBlock{
			Type: "context",
			Elements: []any{
				blocks.SlackTextObject{Type: "mrkdwn", Text: convertEmoji(card.Subtitle)},
			},
		})
	}
	if card.ImageURL != "" {
		alt := card.Title
		if alt == "" {
			alt = "Card image"
		}
		out = append(out, SlackBlock{
			Type:     "image",
			ImageURL: card.ImageURL,
			AltText:  alt,
		})
	}

	state := cardRenderState{}
	for _, child := range card.Children {
		out = append(out, convertChildToBlocks(child, &state)...)
	}
	return out
}

type cardRenderState struct {
	chartCount      int
	usedNativeTable bool
}

func convertChildToBlocks(child any, state *cardRenderState) []SlackBlock {
	switch c := child.(type) {
	case chat.CardTextElement:
		return []SlackBlock{ConvertTextToBlock(c)}
	case chat.ImageElement:
		return []SlackBlock{convertImageToBlock(c)}
	case chat.DividerElement:
		return []SlackBlock{convertDividerToBlock(c)}
	case chat.ActionsElement:
		return []SlackBlock{convertActionsToBlock(c)}
	case chat.SectionElement:
		return convertSectionToBlocks(c, state)
	case chat.FieldsElement:
		return []SlackBlock{ConvertFieldsToBlock(c)}
	case chat.LinkElement:
		return []SlackBlock{convertLinkToBlock(c)}
	case chat.TableElement:
		return convertTableToBlocks(c, state)
	case chat.ChartElement:
		return []SlackBlock{convertChartToBlock(c, state)}
	default:
		if text := chat.CardChildToFallbackText(child); text != "" {
			return []SlackBlock{{
				Type: "section",
				Text: &blocks.SlackTextObject{Type: "mrkdwn", Text: text},
			}}
		}
		return nil
	}
}

func ConvertTextToBlock(element chat.CardTextElement) SlackBlock {
	text := markdownBoldToSlackMrkdwn(convertEmoji(element.Content))
	switch element.Style {
	case "bold":
		text = "*" + text + "*"
	case "muted":
		return SlackBlock{
			Type:     "context",
			Elements: []any{blocks.SlackTextObject{Type: "mrkdwn", Text: text}},
		}
	}
	return SlackBlock{
		Type: "section",
		Text: &blocks.SlackTextObject{Type: "mrkdwn", Text: text},
	}
}

func convertLinkToBlock(element chat.LinkElement) SlackBlock {
	return SlackBlock{
		Type: "section",
		Text: &blocks.SlackTextObject{
			Type: "mrkdwn",
			Text: "<" + element.URL + "|" + convertEmoji(element.Label) + ">",
		},
	}
}

func convertImageToBlock(element chat.ImageElement) SlackBlock {
	alt := element.Alt
	if alt == "" {
		alt = "Image"
	}
	return SlackBlock{
		Type:     "image",
		ImageURL: element.URL,
		AltText:  alt,
	}
}

func convertDividerToBlock(_ chat.DividerElement) SlackBlock {
	return SlackBlock{Type: "divider"}
}

func convertActionsToBlock(element chat.ActionsElement) SlackBlock {
	elements := make([]any, 0, len(element.Children))
	for _, child := range element.Children {
		switch c := child.(type) {
		case chat.LinkButtonElement:
			elements = append(elements, convertLinkButtonToElement(c))
		case chat.SelectElement:
			elements = append(elements, convertSelectToElement(c))
		case chat.RadioSelectElement:
			elements = append(elements, convertRadioSelectToElement(c))
		case chat.ButtonElement:
			elements = append(elements, convertButtonToElement(c))
		}
	}
	return SlackBlock{Type: "actions", Elements: elements}
}

func convertButtonToElement(button chat.ButtonElement) blocks.SlackBlockButton {
	el := blocks.SlackBlockButton{
		Type: "button",
		Text: blocks.SlackTextObject{
			Type:  "plain_text",
			Text:  convertEmoji(button.Label),
			Emoji: true,
		},
		ActionID: button.ID,
	}
	if button.Value != "" {
		el.Value = button.Value
	}
	if style := shared.MapButtonStyle(button.Style, shared.PlatformSlack); style != "" {
		el.Style = style
	}
	return el
}

func convertLinkButtonToElement(button chat.LinkButtonElement) blocks.SlackBlockButton {
	id := button.ID
	if id == "" {
		url := button.URL
		if len(url) > linkButtonURLIDMax {
			url = url[:linkButtonURLIDMax]
		}
		id = "link-" + url
	}
	el := blocks.SlackBlockButton{
		Type: "button",
		Text: blocks.SlackTextObject{
			Type:  "plain_text",
			Text:  convertEmoji(button.Label),
			Emoji: true,
		},
		ActionID: id,
		URL:      button.URL,
	}
	if style := shared.MapButtonStyle(button.Style, shared.PlatformSlack); style != "" {
		el.Style = style
	}
	return el
}

func convertSelectToElement(selectEl chat.SelectElement) blocks.SlackBlockStaticSelect {
	options := make([]blocks.SlackBlockOption, len(selectEl.Options))
	for i, opt := range selectEl.Options {
		option := blocks.SlackBlockOption{
			Text:  blocks.SlackTextObject{Type: "plain_text", Text: convertEmoji(opt.Label)},
			Value: opt.Value,
		}
		if opt.Description != "" {
			option.Description = &blocks.SlackTextObject{
				Type: "plain_text",
				Text: convertEmoji(opt.Description),
			}
		}
		options[i] = option
	}
	el := blocks.SlackBlockStaticSelect{
		Type:     "static_select",
		ActionID: selectEl.ID,
		Options:  options,
	}
	if selectEl.Placeholder != "" {
		el.Placeholder = &blocks.SlackTextObject{
			Type: "plain_text",
			Text: convertEmoji(selectEl.Placeholder),
		}
	}
	if selectEl.InitialOption != "" {
		for i := range options {
			if options[i].Value == selectEl.InitialOption {
				opt := options[i]
				el.InitialOption = &opt
				break
			}
		}
	}
	return el
}

func convertRadioSelectToElement(radioSelect chat.RadioSelectElement) blocks.SlackBlockRadioButtons {
	limited := radioSelect.Options
	if len(limited) > 10 {
		limited = limited[:10]
	}
	options := make([]blocks.SlackBlockOption, len(limited))
	for i, opt := range limited {
		option := blocks.SlackBlockOption{
			Text:  blocks.SlackTextObject{Type: "mrkdwn", Text: convertEmoji(opt.Label)},
			Value: opt.Value,
		}
		if opt.Description != "" {
			option.Description = &blocks.SlackTextObject{
				Type: "mrkdwn",
				Text: convertEmoji(opt.Description),
			}
		}
		options[i] = option
	}
	el := blocks.SlackBlockRadioButtons{
		Type:     "radio_buttons",
		ActionID: radioSelect.ID,
		Options:  options,
	}
	if radioSelect.InitialOption != "" {
		for i := range options {
			if options[i].Value == radioSelect.InitialOption {
				opt := options[i]
				el.InitialOption = &opt
				break
			}
		}
	}
	return el
}

func asciiFallbackBlock(content string) SlackBlock {
	fence := func(body string) string { return "```\n" + body + "\n```" }
	budget := sectionTextMaxChars - len(fence(""))
	text := fence(content)
	if len(content) > budget {
		text = fence(content[:budget-1] + "…")
	}
	return SlackBlock{
		Type: "section",
		Text: &blocks.SlackTextObject{Type: "mrkdwn", Text: text},
	}
}

func convertTableToBlocks(element chat.TableElement, state *cardRenderState) []SlackBlock {
	cellCharCount := 0
	for _, cell := range element.Headers {
		cellCharCount += len(cell)
	}
	for _, row := range element.Rows {
		for _, cell := range row {
			cellCharCount += len(cell)
		}
	}
	if state.usedNativeTable ||
		len(element.Rows) > dataTableMaxRows ||
		len(element.Headers) > dataTableMaxCols ||
		cellCharCount > dataTableMaxChars {
		return []SlackBlock{asciiFallbackBlock(chat.TableElementToASCII(element.Headers, element.Rows))}
	}

	state.usedNativeTable = true

	headerRow := make([]blocks.SlackRawText, len(element.Headers))
	for i, header := range element.Headers {
		text := convertEmoji(header)
		if text == "" {
			text = " "
		}
		headerRow[i] = blocks.SlackRawText{Type: "raw_text", Text: text}
	}
	dataRows := make([][]blocks.SlackRawText, len(element.Rows))
	for i, row := range element.Rows {
		cells := make([]blocks.SlackRawText, len(row))
		for j, cell := range row {
			text := convertEmoji(cell)
			if text == "" {
				text = " "
			}
			cells[j] = blocks.SlackRawText{Type: "raw_text", Text: text}
		}
		dataRows[i] = cells
	}

	if len(dataRows) == 0 {
		return []SlackBlock{{Type: "table", Rows: [][]blocks.SlackRawText{headerRow}}}
	}

	rows := make([][]blocks.SlackRawText, 0, 1+len(dataRows))
	rows = append(rows, headerRow)
	rows = append(rows, dataRows...)
	block := SlackBlock{
		Type:    "data_table",
		Caption: convertEmoji(element.Caption),
		Rows:    rows,
	}
	if block.Caption == "" {
		block.Caption = convertEmoji("Table")
	}
	if element.PageSize != 0 {
		block.PageSize = min(dataTableMaxPageSize, max(dataTableMinPageSize, element.PageSize))
	}
	return []SlackBlock{block}
}

func convertChartToBlock(element chat.ChartElement, state *cardRenderState) SlackBlock {
	var block *SlackBlock
	if state.chartCount < chartMaxPerMessage {
		block = chartToDataVisualization(element)
	}
	if block != nil {
		state.chartCount++
		return *block
	}
	return asciiFallbackBlock(chat.ChartElementToFallbackText(element))
}

func chartToDataVisualization(element chat.ChartElement) *SlackBlock {
	title := convertEmoji(element.Title)
	if len(title) == 0 || len(title) > chartMaxTitleChars {
		return nil
	}

	chart := element.Chart
	if chart.Type == "pie" {
		if len(chart.Segments) < 1 || len(chart.Segments) > chartMaxSegments {
			return nil
		}
		segments := make([]blocks.SlackChartSegment, len(chart.Segments))
		for i, segment := range chart.Segments {
			if !isValidChartLabel(segment.Label) {
				return nil
			}
			value, ok := asFloat64(segment.Value)
			if !ok || value <= 0 {
				return nil
			}
			segments[i] = blocks.SlackChartSegment{Label: segment.Label, Value: value}
		}
		return &SlackBlock{
			Type:  "data_visualization",
			Title: title,
			Chart: blocks.SlackPieChart{Type: "pie", Segments: segments},
		}
	}

	categories := chart.Categories
	series := chart.Series
	if len(categories) < 1 || len(categories) > chartMaxDataPoints {
		return nil
	}
	seenCat := make(map[string]struct{}, len(categories))
	for _, category := range categories {
		if !isValidChartLabel(category) {
			return nil
		}
		if _, dup := seenCat[category]; dup {
			return nil
		}
		seenCat[category] = struct{}{}
	}
	if len(series) < 1 || len(series) > chartMaxSeries {
		return nil
	}
	seenName := make(map[string]struct{}, len(series))
	for _, s := range series {
		if !isValidChartLabel(s.Name) {
			return nil
		}
		if _, dup := seenName[s.Name]; dup {
			return nil
		}
		seenName[s.Name] = struct{}{}
	}
	if (chart.XLabel != "" && len(chart.XLabel) > chartMaxTitleChars) ||
		(chart.YLabel != "" && len(chart.YLabel) > chartMaxTitleChars) {
		return nil
	}

	normalized := make([]blocks.SlackChartSeries, 0, len(series))
	for _, s := range series {
		if len(s.Data) != len(categories) {
			return nil
		}
		byLabel := make(map[string]chat.ChartDataPoint, len(s.Data))
		for _, point := range s.Data {
			byLabel[point.Label] = point
		}
		data := make([]blocks.SlackChartDataPoint, 0, len(categories))
		for _, category := range categories {
			point, ok := byLabel[category]
			if !ok {
				return nil
			}
			value, ok := asFloat64(point.Value)
			if !ok {
				return nil
			}
			data = append(data, blocks.SlackChartDataPoint{Label: category, Value: value})
		}
		normalized = append(normalized, blocks.SlackChartSeries{Name: s.Name, Data: data})
	}

	return &SlackBlock{
		Type:  "data_visualization",
		Title: title,
		Chart: blocks.SlackSeriesChart{
			Type:   chart.Type,
			Series: normalized,
			AxisConfig: blocks.SlackAxisConfig{
				Categories: categories,
				XLabel:     chart.XLabel,
				YLabel:     chart.YLabel,
			},
		},
	}
}

func isValidChartLabel(label string) bool {
	return len(label) >= 1 && len(label) <= chartMaxLabelChars
}

func convertSectionToBlocks(element chat.SectionElement, state *cardRenderState) []SlackBlock {
	var out []SlackBlock
	for _, child := range element.Children {
		out = append(out, convertChildToBlocks(child, state)...)
	}
	return out
}

func ConvertFieldsToBlock(element chat.FieldsElement) SlackBlock {
	fields := make([]blocks.SlackTextObject, 0, len(element.Children))
	for _, field := range element.Children {
		label := markdownBoldToSlackMrkdwn(convertEmoji(field.Label))
		value := markdownBoldToSlackMrkdwn(convertEmoji(field.Value))
		fields = append(fields, blocks.SlackTextObject{
			Type: "mrkdwn",
			Text: "*" + label + "*\n" + value,
		})
	}
	return SlackBlock{Type: "section", Fields: fields}
}

func CardToFallbackText(card chat.Card) string {
	return shared.CardToFallbackText(card, shared.FallbackTextOptions{
		BoldFormat: "*",
		LineBreak:  "\n",
		Platform:   shared.PlatformSlack,
	})
}

func asFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}
