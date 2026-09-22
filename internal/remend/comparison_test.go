package remend

import (
	"strings"
	"testing"

	"github.com/shoenig/test/must"
)

func TestComparisonOperatorsInListItems(t *testing.T) {
	t.Parallel()

	t.Run("should escape > followed by a digit in dash list items", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "- \\> 25: rich", Mend("- > 25: rich"))
	})

	t.Run("should escape > followed by a digit in asterisk list items", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "* \\> 25: rich", Mend("* > 25: rich"))
	})

	t.Run("should escape > followed by a digit in plus list items", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "+ \\> 25: rich", Mend("+ > 25: rich"))
	})

	t.Run("should escape > in ordered list items", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "1. \\> 25: rich", Mend("1. > 25: rich"))
		must.Eq(t, "2) \\> 10: high", Mend("2) > 10: high"))
	})

	t.Run("should escape > in indented (nested) list items", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "  - \\> 25: rich", Mend("  - > 25: rich"))
		must.Eq(t, "    - \\> 5: expensive", Mend("    - > 5: expensive"))
	})

	t.Run("should escape >= comparison operators", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "- \\>= 10: high", Mend("- >= 10: high"))
	})

	t.Run("should escape > before dollar amounts", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "- \\> $100: expensive", Mend("- > $100: expensive"))
	})

	t.Run("should handle the issue example correctly", func(t *testing.T) {
		t.Parallel()
		input := strings.Join([]string{
			"- < 10: potentially cheap.",
			"- 10–20: reasonable/normal zone.",
			"- > 25–30: rich; you need strong growth + quality to justify.",
		}, "\n")
		expected := strings.Join([]string{
			"- < 10: potentially cheap.",
			"- 10–20: reasonable/normal zone.",
			"- \\> 25–30: rich; you need strong growth + quality to justify.",
		}, "\n")
		must.Eq(t, expected, Mend(input))
	})

	t.Run("should handle multiple comparison operators in a list", func(t *testing.T) {
		t.Parallel()
		input := strings.Join([]string{"- > 5: expensive", "- > 25: very expensive"}, "\n")
		expected := strings.Join([]string{"- \\> 5: expensive", "- \\> 25: very expensive"}, "\n")
		must.Eq(t, expected, Mend(input))
	})

	t.Run("should not escape > in actual blockquotes (no list marker)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "> Some blockquote", Mend("> Some blockquote"))
		must.Eq(t, "> 25 is a number", Mend("> 25 is a number"))
	})

	t.Run("should not escape > when followed by non-digit text", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "- > Some quoted text", Mend("- > Some quoted text"))
		must.Eq(t, "- > Read more about this", Mend("- > Read more about this"))
	})

	t.Run("should not escape > without a space before digit (no list marker)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, ">25", Mend(">25"))
	})

	t.Run("should not escape > inside code blocks", func(t *testing.T) {
		t.Parallel()
		input := "```\n- > 25: in code\n```"
		must.Eq(t, input, Mend(input))
	})

	t.Run("should handle > with no space before digit in list items", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "- \\>25: rich", Mend("- >25: rich"))
	})

	t.Run("should be disabled when comparisonOperators option is false", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "- > 25: rich", remend("- > 25: rich", remendOptions{comparisonOperators: boolPtr(false)}))
	})

	t.Run("should work alongside other remend handlers", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "- \\> 25: **bold**", Mend("- > 25: **bold"))
	})

	t.Run("should handle the full issue example with nested lists", func(t *testing.T) {
		t.Parallel()
		input := strings.Join([]string{
			"*P/E*",
			"  - < 10: potentially cheap.",
			"  - 10–20: reasonable/normal zone.",
			"  - > 25–30: rich; you need strong growth.",
			"",
			"*P/S*",
			"  - < 1: often cheap for mature businesses.",
			"  - 1–3: okay range.",
			"  - > 5: expensive unless high-margin.",
		}, "\n")
		expected := strings.Join([]string{
			"*P/E*",
			"  - < 10: potentially cheap.",
			"  - 10–20: reasonable/normal zone.",
			"  - \\> 25–30: rich; you need strong growth.",
			"",
			"*P/S*",
			"  - < 1: often cheap for mature businesses.",
			"  - 1–3: okay range.",
			"  - \\> 5: expensive unless high-margin.",
		}, "\n")
		must.Eq(t, expected, Mend(input))
	})
}
