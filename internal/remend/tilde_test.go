package remend

import (
	"testing"

	"github.com/shoenig/test/must"
)

func boolPtr(v bool) *bool { return &v }

func intPtr(v int) *int { return &v }

func TestSingleTildeEscape(t *testing.T) {
	t.Parallel()

	t.Run("should escape single ~ between numbers", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "20\\~25°C", Mend("20~25°C"))
	})

	t.Run("should escape multiple single tildes between numbers", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "20\\~25°C。20\\~25°C", Mend("20~25°C。20~25°C"))
	})

	t.Run("should escape single ~ between letters", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "foo\\~bar", Mend("foo~bar"))
	})

	t.Run("should not escape ~~ (double tilde strikethrough)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "~~strikethrough~~", Mend("~~strikethrough~~"))
	})

	t.Run("should not escape ~ at start or end of text", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "~hello", Mend("~hello"))
		must.Eq(t, "hello~", Mend("hello~"))
	})

	t.Run("should not escape ~ surrounded by spaces", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "hello ~ world", Mend("hello ~ world"))
	})

	t.Run("should not escape ~ inside code blocks", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "```\n20~25\n```", Mend("```\n20~25\n```"))
	})

	t.Run("should not escape ~ inside inline code", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "`20~25`", Mend("`20~25`"))
	})

	t.Run("should handle incomplete strikethrough separately from single tilde", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "20\\~25 and ~~strike~~", Mend("20~25 and ~~strike"))
	})

	t.Run("can be disabled via options", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "20~25°C", remend("20~25°C", remendOptions{singleTilde: boolPtr(false)}))
	})

	t.Run("should preserve Unicode letters around single ~", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "日本\\~語", Mend("日本~語"))
		must.Eq(t, "α\\~β", Mend("α~β"))
		must.Eq(t, "é\\~x", Mend("é~x"))
	})

	t.Run("should preserve supplementary-plane Unicode letters around single ~", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "𐐀\\~a", Mend("𐐀~a"))
	})

	t.Run("should escape multiple single tildes between letters", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "foo\\~bar\\~baz", Mend("foo~bar~baz"))
	})
}
