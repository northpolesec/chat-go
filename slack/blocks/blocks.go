// Ported from packages/adapter-slack/src/blocks/index.ts @ 6adca36 (chat v4.40.0).
// Divergences: markdownBoldToSlackMrkdwn is copied (this package cannot import
// parent slack without a later cycle); JS .length → Go bytes (ASCII fixtures);
// fenced fallback truncates to SectionText bytes so the Unicode ellipsis stays
// inside Slack's 3000 budget; MaxBlocks is *int (nil = default); ConvertEmoji
// nil uses ConvertSlackEmojiPlaceholders; unknown children panic SlackBlockError.
package blocks

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

const emptyText = " "

var (
	emojiPattern        = regexp.MustCompile(`\{\{emoji:([a-zA-Z0-9_+-]+)\}\}`)
	markdownBoldPattern = regexp.MustCompile(`\*\*(.+?)\*\*`)
)

func CardToSlackBlocks(card SlackCardElement, options ...SlackBlocksOptions) []SlackBlock {
	var opts SlackBlocksOptions
	if len(options) > 0 {
		opts = options[0]
	}
	convertEmoji := opts.ConvertEmoji
	if convertEmoji == nil {
		convertEmoji = ConvertSlackEmojiPlaceholders
	}
	maxBlocks := Blocks
	if opts.MaxBlocks != nil {
		maxBlocks = *opts.MaxBlocks
	}
	state := &blockState{convertEmoji: convertEmoji, maxBlocks: maxBlocks}
	var blocks []SlackBlock
	if card.Title != "" {
		blocks = append(blocks, SlackBlock{
			Text: ptrText(plainText(card.Title, state.convertEmoji, HeaderText)),
			Type: "header",
		})
	}
	if card.Subtitle != "" {
		blocks = append(blocks, SlackBlock{
			Elements: []any{mrkdwn(card.Subtitle, state.convertEmoji, TextObject)},
			Type:     "context",
		})
	}
	if card.ImageURL != "" {
		alt := card.Title
		if alt == "" {
			alt = "Card image"
		}
		blocks = append(blocks, SlackBlock{
			AltText:  truncateText(state.convertEmoji(alt), ImageAlt),
			ImageURL: truncateText(card.ImageURL, ImageURL),
			Type:     "image",
		})
	}
	for _, child := range card.Children {
		blocks = append(blocks, cardChildToSlackBlocks(child, state)...)
	}
	if maxBlocks < len(blocks) {
		return blocks[:maxBlocks]
	}
	return blocks
}

func CardToBlockKit(card SlackCardElement, options ...SlackBlocksOptions) []SlackBlock {
	return CardToSlackBlocks(card, options...)
}

func CardToSlackFallbackText(card SlackCardElement, options ...SlackBlocksOptions) string {
	var opts SlackBlocksOptions
	if len(options) > 0 {
		opts = options[0]
	}
	convertEmoji := opts.ConvertEmoji
	if convertEmoji == nil {
		convertEmoji = ConvertSlackEmojiPlaceholders
	}
	var lines []string
	if card.Title != "" {
		lines = append(lines, "*"+convertEmoji(card.Title)+"*")
	}
	if card.Subtitle != "" {
		lines = append(lines, convertEmoji(card.Subtitle))
	}
	for _, child := range card.Children {
		if text := cardChildToFallbackText(child, convertEmoji); text != "" {
			lines = append(lines, text)
		}
	}
	return strings.Join(lines, "\n")
}

func CardToFallbackText(card SlackCardElement, options ...SlackBlocksOptions) string {
	return CardToSlackFallbackText(card, options...)
}

func ConvertSlackEmojiPlaceholders(text string) string {
	return emojiPattern.ReplaceAllString(text, ":$1:")
}

type blockState struct {
	chartCount   int
	convertEmoji func(string) string
	maxBlocks    int
	usedTable    bool
}

func cardChildToSlackBlocks(child SlackCardChild, state *blockState) []SlackBlock {
	switch c := child.(type) {
	case SlackActionsElement:
		return []SlackBlock{actionsToBlock(c, state.convertEmoji)}
	case SlackChartElement:
		return []SlackBlock{chartToBlock(c, state)}
	case SlackDividerElement:
		return []SlackBlock{{Type: "divider"}}
	case SlackFieldsElement:
		return []SlackBlock{fieldsToBlock(c, state.convertEmoji)}
	case SlackImageElement:
		return []SlackBlock{imageToBlock(c, state.convertEmoji)}
	case SlackLinkElement:
		return []SlackBlock{linkToBlock(c, state.convertEmoji)}
	case SlackSectionElement:
		var out []SlackBlock
		for _, nested := range c.Children {
			out = append(out, cardChildToSlackBlocks(nested, state)...)
		}
		return out
	case SlackTableElement:
		return tableToBlocks(c, state)
	case SlackTextElement:
		return []SlackBlock{textToBlock(c, state.convertEmoji)}
	default:
		return assertNever(child)
	}
}

func textToBlock(element SlackTextElement, convertEmoji func(string) string) SlackBlock {
	text := markdownBoldToSlackMrkdwn(convertEmoji(element.Content))
	if element.Style == "muted" {
		return SlackBlock{
			Elements: []any{mrkdwn(text, ident, TextObject)},
			Type:     "context",
		}
	}
	if element.Style == "bold" {
		text = "*" + text + "*"
	}
	return SlackBlock{
		Text: ptrText(mrkdwn(text, ident, SectionText)),
		Type: "section",
	}
}

func imageToBlock(element SlackImageElement, convertEmoji func(string) string) SlackBlock {
	alt := element.Alt
	if alt == "" {
		alt = "Image"
	}
	return SlackBlock{
		AltText:  truncateText(convertEmoji(alt), ImageAlt),
		ImageURL: truncateText(element.URL, ImageURL),
		Type:     "image",
	}
}

func linkToBlock(element SlackLinkElement, convertEmoji func(string) string) SlackBlock {
	return SlackBlock{
		Text: ptrText(mrkdwn("<"+element.URL+"|"+convertEmoji(element.Label)+">", ident, SectionText)),
		Type: "section",
	}
}

func actionsToBlock(element SlackActionsElement, convertEmoji func(string) string) SlackBlock {
	children := element.Children
	if len(children) > ActionsElements {
		children = children[:ActionsElements]
	}
	elements := make([]any, 0, len(children))
	for _, child := range children {
		elements = append(elements, actionToElement(child, convertEmoji))
	}
	return SlackBlock{Elements: elements, Type: "actions"}
}

func actionToElement(child SlackActionsChild, convertEmoji func(string) string) any {
	switch c := child.(type) {
	case SlackButtonElement:
		return buttonToElement(c, convertEmoji)
	case SlackLinkButtonElement:
		return linkButtonToElement(c, convertEmoji)
	case SlackRadioSelectElement:
		return radioSelectToElement(c, convertEmoji)
	case SlackSelectElement:
		return selectToElement(c, convertEmoji)
	default:
		return assertNeverVal(child)
	}
}

func buttonToElement(button SlackButtonElement, convertEmoji func(string) string) SlackBlockButton {
	el := SlackBlockButton{
		ActionID: truncateText(button.ID, ActionID),
		Style:    mapButtonStyle(button.Style),
		Text:     plainText(button.Label, convertEmoji, ButtonText),
		Type:     "button",
	}
	if button.Value != "" {
		el.Value = truncateText(button.Value, ButtonValue)
	}
	return el
}

func linkButtonToElement(button SlackLinkButtonElement, convertEmoji func(string) string) SlackBlockButton {
	id := button.ID
	if id == "" {
		id = "link-" + button.URL
	}
	return SlackBlockButton{
		ActionID: truncateText(id, ActionID),
		Style:    mapButtonStyle(button.Style),
		Text:     plainText(button.Label, convertEmoji, ButtonText),
		Type:     "button",
		URL:      truncateText(button.URL, ButtonURL),
	}
}

func selectToElement(selectEl SlackSelectElement, convertEmoji func(string) string) SlackBlockStaticSelect {
	options := selectEl.Options
	if len(options) > Options {
		options = options[:Options]
	}
	out := make([]SlackBlockOption, 0, len(options))
	for _, option := range options {
		out = append(out, optionObject(option, convertEmoji, "plain_text"))
	}
	el := SlackBlockStaticSelect{
		ActionID: truncateText(selectEl.ID, ActionID),
		Options:  out,
		Type:     "static_select",
	}
	el.InitialOption = findInitialOption(out, selectEl.InitialOption)
	if selectEl.Placeholder != "" {
		p := plainText(selectEl.Placeholder, convertEmoji, Placeholder)
		el.Placeholder = &p
	}
	return el
}

func radioSelectToElement(selectEl SlackRadioSelectElement, convertEmoji func(string) string) SlackBlockRadioButtons {
	options := selectEl.Options
	if len(options) > RadioOptions {
		options = options[:RadioOptions]
	}
	out := make([]SlackBlockOption, 0, len(options))
	for _, option := range options {
		out = append(out, optionObject(option, convertEmoji, "mrkdwn"))
	}
	return SlackBlockRadioButtons{
		ActionID:      truncateText(selectEl.ID, ActionID),
		InitialOption: findInitialOption(out, selectEl.InitialOption),
		Options:       out,
		Type:          "radio_buttons",
	}
}

func findInitialOption(options []SlackBlockOption, initialOption string) *SlackBlockOption {
	if initialOption == "" {
		return nil
	}
	value := truncateText(initialOption, OptionValue)
	for i := range options {
		if options[i].Value == value {
			opt := options[i]
			return &opt
		}
	}
	return nil
}

func optionObject(option SlackSelectOptionElement, convertEmoji func(string) string, textType string) SlackBlockOption {
	opt := SlackBlockOption{
		Text: SlackTextObject{
			Text: truncateText(convertEmoji(option.Label), OptionText),
			Type: textType,
		},
		Value: truncateText(option.Value, OptionValue),
	}
	if option.Description != "" {
		opt.Description = &SlackTextObject{
			Text: truncateText(convertEmoji(option.Description), OptionDescription),
			Type: textType,
		}
	}
	return opt
}

func fieldsToBlock(element SlackFieldsElement, convertEmoji func(string) string) SlackBlock {
	children := element.Children
	if len(children) > Fields {
		children = children[:Fields]
	}
	fields := make([]SlackTextObject, 0, len(children))
	for _, field := range children {
		label := markdownBoldToSlackMrkdwn(convertEmoji(field.Label))
		value := markdownBoldToSlackMrkdwn(convertEmoji(field.Value))
		fields = append(fields, mrkdwn("*"+label+"*\n"+value, ident, FieldText))
	}
	return SlackBlock{Fields: fields, Type: "section"}
}

func tableToBlocks(element SlackTableElement, state *blockState) []SlackBlock {
	cellCharCount := 0
	for _, cell := range element.Headers {
		cellCharCount += len(cell)
	}
	for _, row := range element.Rows {
		for _, cell := range row {
			cellCharCount += len(cell)
		}
	}
	if state.usedTable || len(element.Rows)+1 > TableRows || len(element.Headers) > TableColumns || cellCharCount > TableChars {
		return []SlackBlock{{
			Text: ptrText(fencedFallbackText(tableToASCII(element))),
			Type: "section",
		}}
	}
	state.usedTable = true
	rows := make([][]SlackRawText, 0, 1+len(element.Rows))
	header := make([]SlackRawText, len(element.Headers))
	for i, h := range element.Headers {
		header[i] = rawText(h, state.convertEmoji)
	}
	rows = append(rows, header)
	for _, row := range element.Rows {
		cells := make([]SlackRawText, len(row))
		for i, cell := range row {
			cells[i] = rawText(cell, state.convertEmoji)
		}
		rows = append(rows, cells)
	}
	if len(element.Rows) == 0 {
		block := SlackBlock{Rows: rows, Type: "table"}
		if element.Align != nil {
			n := min(len(element.Align), TableColumns)
			settings := make([]*SlackColumnSetting, n)
			for i, align := range element.Align[:n] {
				if align != "" {
					settings[i] = &SlackColumnSetting{Align: string(align)}
				}
			}
			block.ColumnSettings = settings
		}
		return []SlackBlock{block}
	}
	block := SlackBlock{
		Caption: state.convertEmoji(element.Caption),
		Rows:    rows,
		Type:    "data_table",
	}
	if block.Caption == "" {
		block.Caption = state.convertEmoji("Table")
	}
	if element.PageSize != nil {
		block.PageSize = min(TablePageSize, max(1, *element.PageSize))
	}
	return []SlackBlock{block}
}

func chartToBlock(element SlackChartElement, state *blockState) SlackBlock {
	var block *SlackBlock
	if state.chartCount < ChartsPerMessage {
		block = chartToDataVisualization(element, state.convertEmoji)
	}
	if block != nil {
		state.chartCount++
		return *block
	}
	return SlackBlock{
		Text: ptrText(fencedFallbackText(chartToASCII(element))),
		Type: "section",
	}
}

func fencedFallbackText(content string) SlackTextObject {
	const openFence, closeFence, ellipsis = "```\n", "\n```", "…"
	budget := SectionText - len(openFence) - len(closeFence)
	if len(content) > budget {
		cut := max(0, budget-len(ellipsis))
		content = content[:cut] + ellipsis
	}
	return SlackTextObject{Text: openFence + content + closeFence, Type: "mrkdwn"}
}

func chartToDataVisualization(element SlackChartElement, convertEmoji func(string) string) *SlackBlock {
	title := convertEmoji(element.Title)
	if len(title) == 0 || len(title) > ChartTitle {
		return nil
	}
	chart := element.Chart
	if chart.Type == "pie" {
		if len(chart.Segments) < 1 || len(chart.Segments) > ChartSegments {
			return nil
		}
		for _, segment := range chart.Segments {
			if !isValidChartLabel(segment.Label) || segment.Value <= 0 {
				return nil
			}
		}
		segments := make([]SlackChartSegment, len(chart.Segments))
		copy(segments, chart.Segments)
		return &SlackBlock{
			Chart: SlackPieChart{Segments: segments, Type: "pie"},
			Title: title,
			Type:  "data_visualization",
		}
	}
	categories := chart.Categories
	series := chart.Series
	if len(categories) < 1 || len(categories) > ChartDataPoints {
		return nil
	}
	seenCat := make(map[string]struct{}, len(categories))
	for _, category := range categories {
		if !isValidChartLabel(category) {
			return nil
		}
		if _, ok := seenCat[category]; ok {
			return nil
		}
		seenCat[category] = struct{}{}
	}
	if len(series) < 1 || len(series) > ChartSeries {
		return nil
	}
	seenName := make(map[string]struct{}, len(series))
	for _, s := range series {
		if !isValidChartLabel(s.Name) {
			return nil
		}
		if _, ok := seenName[s.Name]; ok {
			return nil
		}
		seenName[s.Name] = struct{}{}
	}
	if (chart.XLabel != "" && len(chart.XLabel) > ChartTitle) || (chart.YLabel != "" && len(chart.YLabel) > ChartTitle) {
		return nil
	}
	normalized := make([]SlackChartSeries, 0, len(series))
	for _, s := range series {
		if len(s.Data) != len(categories) {
			return nil
		}
		byLabel := make(map[string]SlackChartDataPoint, len(s.Data))
		for _, point := range s.Data {
			byLabel[point.Label] = point
		}
		data := make([]SlackChartDataPoint, 0, len(categories))
		for _, category := range categories {
			point, ok := byLabel[category]
			if !ok {
				return nil
			}
			data = append(data, SlackChartDataPoint{Label: category, Value: point.Value})
		}
		normalized = append(normalized, SlackChartSeries{Data: data, Name: s.Name})
	}
	return &SlackBlock{
		Chart: SlackSeriesChart{
			AxisConfig: SlackAxisConfig{
				Categories: categories,
				XLabel:     chart.XLabel,
				YLabel:     chart.YLabel,
			},
			Series: normalized,
			Type:   chart.Type,
		},
		Title: title,
		Type:  "data_visualization",
	}
}

func isValidChartLabel(label string) bool {
	return len(label) >= 1 && len(label) <= ChartLabel
}

func chartToASCII(element SlackChartElement) string {
	chart := element.Chart
	if chart.Type == "pie" {
		rows := make([][]string, len(chart.Segments))
		for i, segment := range chart.Segments {
			rows[i] = []string{segment.Label, formatNum(segment.Value)}
		}
		return element.Title + "\n" + tableToASCII(SlackTableElement{
			Headers: []string{"Label", "Value"},
			Rows:    rows,
			Type:    "table",
		})
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
					cell = formatNum(p.Value)
					break
				}
			}
			row = append(row, cell)
		}
		rows[i] = row
	}
	return element.Title + "\n" + tableToASCII(SlackTableElement{
		Headers: headers,
		Rows:    rows,
		Type:    "table",
	})
}

func cardChildToFallbackText(child SlackCardChild, convertEmoji func(string) string) string {
	switch c := child.(type) {
	case SlackActionsElement:
		return ""
	case SlackChartElement:
		return chartToASCII(c)
	case SlackDividerElement:
		return "---"
	case SlackFieldsElement:
		lines := make([]string, len(c.Children))
		for i, field := range c.Children {
			lines[i] = convertEmoji(field.Label) + ": " + convertEmoji(field.Value)
		}
		return strings.Join(lines, "\n")
	case SlackImageElement:
		if c.Alt == "" {
			return ""
		}
		return convertEmoji(c.Alt)
	case SlackLinkElement:
		return convertEmoji(c.Label) + " (" + c.URL + ")"
	case SlackSectionElement:
		var parts []string
		for _, nested := range c.Children {
			if text := cardChildToFallbackText(nested, convertEmoji); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case SlackTableElement:
		return tableToASCII(c)
	case SlackTextElement:
		return convertEmoji(c.Content)
	default:
		assertNeverVal(child)
		return ""
	}
}

func mrkdwn(text string, convertEmoji func(string) string, maxLength int) SlackTextObject {
	return SlackTextObject{
		Text: nonemptyText(truncateText(convertEmoji(text), maxLength)),
		Type: "mrkdwn",
	}
}

func plainText(text string, convertEmoji func(string) string, maxLength int) SlackTextObject {
	return SlackTextObject{
		Emoji: true,
		Text:  nonemptyText(truncateText(convertEmoji(text), maxLength)),
		Type:  "plain_text",
	}
}

func rawText(text string, convertEmoji func(string) string) SlackRawText {
	return SlackRawText{
		Text: nonemptyText(convertEmoji(text)),
		Type: "raw_text",
	}
}

func mapButtonStyle(style SlackButtonStyle) string {
	if style == "danger" || style == "primary" {
		return string(style)
	}
	return ""
}

func truncateText(text string, maxLength int) string {
	if len(text) > maxLength {
		return text[:maxLength]
	}
	return text
}

func nonemptyText(text string) string {
	if len(text) > 0 {
		return text
	}
	return emptyText
}

func assertNever(value any) []SlackBlock {
	panic(newSlackBlockError(fmt.Sprintf("Unsupported Slack card element: %v", value)))
}

func assertNeverVal(value any) any {
	panic(newSlackBlockError(fmt.Sprintf("Unsupported Slack card element: %v", value)))
}

func tableToASCII(table SlackTableElement) string {
	rows := make([][]string, 0, 1+len(table.Rows))
	rows = append(rows, table.Headers)
	rows = append(rows, table.Rows...)
	widths := make([]int, len(table.Headers))
	for col := range table.Headers {
		for _, row := range rows {
			if col < len(row) && len(row[col]) > widths[col] {
				widths[col] = len(row[col])
			}
		}
	}
	lines := make([]string, len(rows))
	for i, row := range rows {
		cells := make([]string, len(row))
		for col, cell := range row {
			w := 0
			if col < len(widths) {
				w = widths[col]
			}
			cells[col] = padEnd(cell, w)
		}
		lines[i] = strings.TrimRightFunc(strings.Join(cells, " | "), unicode.IsSpace)
	}
	return strings.Join(lines, "\n")
}

func padEnd(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func markdownBoldToSlackMrkdwn(markdown string) string {
	return markdownBoldPattern.ReplaceAllString(markdown, "*$1*")
}

func ident(value string) string { return value }

func ptrText(t SlackTextObject) *SlackTextObject { return &t }

func formatNum(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
