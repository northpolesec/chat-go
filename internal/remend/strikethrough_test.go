package remend

import (
	"testing"

	"github.com/shoenig/test/must"
)

func TestStrikethroughFormatting(t *testing.T) {
	t.Parallel()

	t.Run("should complete incomplete strikethrough", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text with ~~strike~~", Mend("Text with ~~strike"))
		must.Eq(t, "~~incomplete~~", Mend("~~incomplete"))
	})

	t.Run("should keep complete strikethrough unchanged", func(t *testing.T) {
		t.Parallel()
		text := "Text with ~~strikethrough text~~"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle multiple strikethrough sections", func(t *testing.T) {
		t.Parallel()
		text := "~~strike1~~ and ~~strike2~~"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should complete odd number of strikethrough markers", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "~~first~~ and ~~second~~", Mend("~~first~~ and ~~second"))
	})

	t.Run("should complete half-complete ~~ closing marker (#313)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "~~xxx~~", Mend("~~xxx~"))
		must.Eq(t, "~~strike text~~", Mend("~~strike text~"))
		must.Eq(t, "Text with ~~strike~~", Mend("Text with ~~strike~"))
		must.Eq(t, "This is ~~strikethrough~~", Mend("This is ~~strikethrough~"))
	})
}

func TestStrikethroughEvenTildePairs(t *testing.T) {
	t.Parallel()

	t.Run("should not close when tilde pairs are balanced", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "a~~b~~text", Mend("a~~b~~text"))
	})

	t.Run("should not close half-complete tilde when pairs are balanced", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "a~~b~~c~", Mend("a~~b~~c~"))
	})
}

func TestStrikethroughBrokenMarkdownRemainder(t *testing.T) {
	t.Parallel()

	t.Run("should not close single tilde at end (not a valid marker alone)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "text~", Mend("text~"))
	})

	t.Run("should close second strikethrough after complete strikethrough", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "~~done~~ and ~~undone~~", Mend("~~done~~ and ~~undone"))
	})

	t.Run("should close strikethrough with emoji content", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "~~🎉 celebration~~", Mend("~~🎉 celebration"))
	})
}
