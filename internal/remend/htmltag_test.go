package remend

import (
	"testing"

	"github.com/shoenig/test/must"
)

func TestIncompleteHTMLTagStripping(t *testing.T) {
	t.Parallel()

	t.Run("should strip incomplete opening tags at end", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Hello", Mend("Hello <div"))
		must.Eq(t, "Hello", Mend("Hello <custom"))
		must.Eq(t, "Hello", Mend("Hello <casecard"))
		must.Eq(t, "Text", Mend("Text <MyComponent"))
	})

	t.Run("should strip incomplete closing tags at end", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Hello", Mend("Hello </div"))
		must.Eq(t, "Hello", Mend("Hello </custom"))
		must.Eq(t, "<div>content", Mend("<div>content</di"))
	})

	t.Run("should strip incomplete tags with partial attributes", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Hello", Mend("Hello <div class=\"foo"))
		must.Eq(t, "Hello", Mend("Hello <div class="))
		must.Eq(t, "Hello", Mend("Hello <a href=\"https://example.com"))
		must.Eq(t, "", Mend("<custom data-id"))
	})

	t.Run("should keep complete tags unchanged", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Hello <div>", Mend("Hello <div>"))
		must.Eq(t, "<div>content</div>", Mend("<div>content</div>"))
		must.Eq(t, "<br/>", Mend("<br/>"))
		must.Eq(t, "<img src='test'>", Mend("<img src='test'>"))
	})

	t.Run("should not strip < followed by space or number", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "3 < 5", Mend("3 < 5"))
		must.Eq(t, "x < y", Mend("x < y"))
		must.Eq(t, "if a <", Mend("if a <"))
		must.Eq(t, "value <1", Mend("value <1"))
	})

	t.Run("should not strip inside code blocks", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "```\n<div\n```", Mend("```\n<div\n```"))
		must.Eq(t, "```html\n<custom", Mend("```html\n<custom"))
	})

	t.Run("should not strip inside inline code", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "`<div`", Mend("`<div`"))
	})

	t.Run("should handle tag at start of text", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "", Mend("<div"))
		must.Eq(t, "", Mend("<custom"))
		must.Eq(t, "", Mend("</div"))
	})

	t.Run("should strip only the incomplete tag, preserving prior content", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Some text here", Mend("Some text here\n\n<casecard"))
		must.Eq(t, "# Heading\n\nParagraph", Mend("# Heading\n\nParagraph <custom"))
	})

	t.Run("should handle complete tag followed by incomplete tag", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "<div>Hello</div>", Mend("<div>Hello</div> <span"))
	})

	t.Run("should not add trailing underscore for HTML attributes with underscores", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "<a target=\"_blank\" href=\"https://link.com\">word</a>", Mend("<a target=\"_blank\" href=\"https://link.com\">word</a>"))
		must.Eq(t, "<a target=\"_blank\">link</a>", Mend("<a target=\"_blank\">link</a>"))
		must.Eq(t, "<iframe src=\"x\" sandbox=\"allow_scripts\">", Mend("<iframe src=\"x\" sandbox=\"allow_scripts\">"))
	})

	t.Run("should be disabled when htmlTags option is false", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Hello <div", remend("Hello <div", remendOptions{htmlTags: boolPtr(false)}))
	})
}

func TestHTMLCommentsAndSpecialHTML(t *testing.T) {
	t.Parallel()

	t.Run("should not strip HTML comment (pattern requires <[a-zA-Z/])", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "text <!-- incomplete comment", Mend("text <!-- incomplete comment"))
	})

	t.Run("should not strip complete script tag with trailing text", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "text <script>alert('", Mend("text <script>alert('"))
	})

	t.Run("should strip incomplete div with attributes", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "text", Mend("text <div class=\"test"))
	})

	t.Run("should keep complete HTML tags", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "text <br>", Mend("text <br>"))
	})

	t.Run("should keep complete HTML comments", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "text <!-- comment -->", Mend("text <!-- comment -->"))
	})
}
