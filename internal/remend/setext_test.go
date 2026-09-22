package remend

import (
	"testing"

	"github.com/shoenig/test/must"
)

func TestSetextHeadingHandling(t *testing.T) {
	t.Parallel()

	t.Run("should prevent partial list items from being interpreted as setext headings", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "here is a list\n-\u200B", Mend("here is a list\n-"))
	})

	t.Run("should handle double dash that could be setext heading", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Some text\n--\u200B", Mend("Some text\n--"))
	})

	t.Run("should handle single equals that could be setext heading", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Some text\n=\u200B", Mend("Some text\n="))
	})

	t.Run("should handle double equals that could be setext heading", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Some text\n==\u200B", Mend("Some text\n=="))
	})

	t.Run("should NOT modify valid horizontal rules with three dashes", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Some text\n---", Mend("Some text\n---"))
	})

	t.Run("should NOT modify valid setext headings with three equals", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Heading\n===", Mend("Heading\n==="))
	})

	t.Run("should NOT modify when there's no previous content", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "-", Mend("-"))
	})

	t.Run("should NOT modify when previous line is empty", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "\n-", Mend("\n-"))
	})

	t.Run("should handle the streaming list scenario", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "here is a list\n-\u200B", Mend("here is a list\n-"))
		must.Eq(t, "here is a list\n-\u200B", Mend("here is a list\n- "))
		must.Eq(t, "here is a list\n- list item 1", Mend("here is a list\n- list item 1"))
	})

	t.Run("should handle multiple lines with potential setext heading at end", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Line 1\nLine 2\nLine 3\n-\u200B", Mend("Line 1\nLine 2\nLine 3\n-"))
	})

	t.Run("should handle text with whitespace before dash", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Some text\n  -\u200B", Mend("Some text\n  -"))
	})

	t.Run("should NOT modify complete list items", func(t *testing.T) {
		t.Parallel()
		text := "Some text\n- Item 1\n- Item 2"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should NOT modify when last line has other characters", func(t *testing.T) {
		t.Parallel()
		text := "Some text\n-x"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle four or more dashes (horizontal rule)", func(t *testing.T) {
		t.Parallel()
		text := "Some text\n----"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle mixed whitespace and dashes", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Some text\n-\u200B", Mend("Some text\n- "))
	})

	t.Run("should handle the original issue example precisely", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "here is a list", Mend("here is a list"))
		must.Eq(t, "here is a list\n", Mend("here is a list\n"))
		must.Eq(t, "here is a list\n-\u200B", Mend("here is a list\n-"))
		must.Eq(t, "here is a list\n- list item 1", Mend("here is a list\n- list item 1"))
	})

	t.Run("should handle setext heading with equals signs during streaming", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "This is a title\n=\u200B", Mend("This is a title\n="))
		must.Eq(t, "This is a title\n==\u200B", Mend("This is a title\n=="))
		must.Eq(t, "This is a title\n===", Mend("This is a title\n==="))
	})

	t.Run("should not interfere with other markdown syntax", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "**bold text**\n-\u200B", Mend("**bold text**\n-"))
		must.Eq(t, "*italic text*\n-\u200B", Mend("*italic text*\n-"))
		must.Eq(t, "`code`\n-\u200B", Mend("`code`\n-"))
	})

	t.Run("should handle multiple potential setext headings in sequence", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text 1\n-\nText 2\n-\u200B", Mend("Text 1\n-\nText 2\n-"))
	})
}

func TestSetextHeadingWithEqualsSignEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("should not modify equals when previous line is empty", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "\n=", Mend("\n="))
	})

	t.Run("should not modify double equals when previous line is empty", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "\n==", Mend("\n=="))
	})
}
