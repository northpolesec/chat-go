package remend

import (
	"testing"

	"github.com/shoenig/test/must"
)

func TestIsWordChar(t *testing.T) {
	t.Parallel()

	t.Run("should return false for empty string", func(t *testing.T) {
		t.Parallel()
		must.False(t, isWordChar(""))
	})

	t.Run("should return true for ASCII word characters", func(t *testing.T) {
		t.Parallel()
		must.True(t, isWordChar("a"))
		must.True(t, isWordChar("Z"))
		must.True(t, isWordChar("5"))
		must.True(t, isWordChar("_"))
	})

	t.Run("should return false for non-word characters", func(t *testing.T) {
		t.Parallel()
		must.False(t, isWordChar(" "))
		must.False(t, isWordChar("*"))
		must.False(t, isWordChar("-"))
	})

	t.Run("should handle unicode word characters", func(t *testing.T) {
		t.Parallel()
		must.True(t, isWordChar("é"))
		must.True(t, isWordChar("ñ"))
	})
}

func TestFindMatchingOpeningBracket(t *testing.T) {
	t.Parallel()

	t.Run("should return -1 when no matching opening bracket exists", func(t *testing.T) {
		t.Parallel()
		text := "some text]"
		must.Eq(t, -1, findMatchingOpeningBracket(text, 9))
	})

	t.Run("should find matching opening bracket for simple case", func(t *testing.T) {
		t.Parallel()
		text := "[text]"
		must.Eq(t, 0, findMatchingOpeningBracket(text, 5))
	})

	t.Run("should handle nested brackets", func(t *testing.T) {
		t.Parallel()
		text := "[outer [inner] text]"
		must.Eq(t, 0, findMatchingOpeningBracket(text, 19))
		must.Eq(t, 7, findMatchingOpeningBracket(text, 13))
	})
}

func TestFindMatchingClosingBracket(t *testing.T) {
	t.Parallel()

	t.Run("should return -1 when no matching closing bracket exists", func(t *testing.T) {
		t.Parallel()
		text := "[some text"
		must.Eq(t, -1, findMatchingClosingBracket(text, 0))
	})

	t.Run("should find matching closing bracket for simple case", func(t *testing.T) {
		t.Parallel()
		text := "[text]"
		must.Eq(t, 5, findMatchingClosingBracket(text, 0))
	})

	t.Run("should handle nested brackets", func(t *testing.T) {
		t.Parallel()
		text := "[outer [inner] text]"
		must.Eq(t, 19, findMatchingClosingBracket(text, 0))
		must.Eq(t, 13, findMatchingClosingBracket(text, 7))
	})
}

func TestIsBeforeClosingParenEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("should return false when newline found before )", func(t *testing.T) {
		t.Parallel()
		must.False(t, isWithinLinkOrImageURL("[t](_\nmore)", 4))
	})

	t.Run("should return false when text ends without ) or newline", func(t *testing.T) {
		t.Parallel()
		must.False(t, isWithinLinkOrImageURL("[t](_noclose", 4))
	})
}

func TestIsWithinLinkOrImageURLClosingParenBeforeOpen(t *testing.T) {
	t.Parallel()

	t.Run("should return false when ) precedes the position", func(t *testing.T) {
		t.Parallel()
		must.False(t, isWithinLinkOrImageURL("[text](url) _after", 12))
	})
}

func TestIsWithinLinkOrImageURLEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("should return false for bare ( not preceded by ]", func(t *testing.T) {
		t.Parallel()
		must.False(t, isWithinLinkOrImageURL("func(arg)", 5))
	})
}

func TestIsWithinHTMLTagEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("should return false when > is found first", func(t *testing.T) {
		t.Parallel()
		must.False(t, isWithinHTMLTag("div>text", 5))
	})

	t.Run("should return false for invalid tag start after <", func(t *testing.T) {
		t.Parallel()
		must.False(t, isWithinHTMLTag("3<5 text", 4))
	})

	t.Run("should return false when newline found before < or >", func(t *testing.T) {
		t.Parallel()
		must.False(t, isWithinHTMLTag("<div\ntext", 6))
	})

	t.Run("should return true for uppercase tag", func(t *testing.T) {
		t.Parallel()
		must.True(t, isWithinHTMLTag("<DIV class='_test'>", 13))
	})

	t.Run("should return true for closing tag with /", func(t *testing.T) {
		t.Parallel()
		must.True(t, isWithinHTMLTag("</div _attr>", 6))
	})

	t.Run("should return false when < is at end of text", func(t *testing.T) {
		t.Parallel()
		must.False(t, isWithinHTMLTag("text<", 5))
	})
}

func TestIsWithinMathBlockBranchCoverage(t *testing.T) {
	t.Parallel()

	t.Run("should ignore single $ inside block math", func(t *testing.T) {
		t.Parallel()
		must.True(t, isWithinMathBlock("$$x$y$$z", 5))
	})
}

func TestIsHorizontalRuleBranchCoverage(t *testing.T) {
	t.Parallel()

	t.Run("should detect horizontal rule with spaces between markers", func(t *testing.T) {
		t.Parallel()
		must.True(t, isHorizontalRule("* * *", 0, "*"))
	})

	t.Run("should detect horizontal rule with tabs between markers", func(t *testing.T) {
		t.Parallel()
		must.True(t, isHorizontalRule("*\t*\t*", 0, "*"))
	})
}
