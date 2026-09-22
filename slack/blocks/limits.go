// Ported from packages/adapter-slack/src/blocks/limits.ts @ 6adca36 (chat v4.40.0).
// Divergences: LIMITS object → one const block ordered by value; ActionId →
// ActionID (and the other ID/URL keys) for Go initialisms.
package blocks

const (
	ChartsPerMessage  = 2
	Fields            = 10
	RadioOptions      = 10
	ChartSegments     = 12
	ChartSeries       = 12
	ChartDataPoints   = 20
	ChartLabel        = 20
	TableColumns      = 20
	ActionsElements   = 25
	Blocks            = 50
	ChartTitle        = 50
	ButtonText        = 75
	OptionDescription = 75
	OptionText        = 75
	Options           = 100
	TablePageSize     = 100
	TableRows         = 100
	HeaderText        = 150
	OptionValue       = 150
	Placeholder       = 150
	ActionID          = 255
	BlockID           = 255
	ButtonValue       = 2000
	FieldText         = 2000
	ImageAlt          = 2000
	ButtonURL         = 3000
	ImageURL          = 3000
	SectionText       = 3000
	TextObject        = 3000
	TableChars        = 10_000
)
