package remend

import (
	"testing"

	"github.com/shoenig/test/must"
)

func incompleteImage(alt string) string {
	return "![" + alt + "](" + incompleteImagePlaceholder + ")"
}

func textOnlyOpts() remendOptions {
	return remendOptions{linkMode: linkModeTextOnly}
}

func TestLinkHandling(t *testing.T) {
	t.Parallel()

	t.Run("should preserve incomplete links with special marker", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text with [incomplete link](streamdown:incomplete-link)", Mend("Text with [incomplete link"))
		must.Eq(t, "Text [partial](streamdown:incomplete-link)", Mend("Text [partial"))
	})

	t.Run("should keep complete links unchanged", func(t *testing.T) {
		t.Parallel()
		text := "Text with [complete link](url)"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle multiple complete links", func(t *testing.T) {
		t.Parallel()
		text := "[link1](url1) and [link2](url2)"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle nested brackets in incomplete links", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "[outer [nested] text](streamdown:incomplete-link)", Mend("[outer [nested] text](incomplete"))
		must.Eq(t, "[link with [inner] content](streamdown:incomplete-link)", Mend("[link with [inner] content](http://incomplete"))
		must.Eq(t, "Text [foo [bar] baz](streamdown:incomplete-link)", Mend("Text [foo [bar] baz]("))
	})

	t.Run("should handle nested brackets in complete links", func(t *testing.T) {
		t.Parallel()
		text := "[link with [brackets] inside](https://example.com)"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle partial link at chunk boundary - #165", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Check out [this lin](streamdown:incomplete-link)", Mend("Check out [this lin"))
		must.Eq(t, "Visit [our site](streamdown:incomplete-link)", Mend("Visit [our site](https://exa"))
	})

	t.Run("should handle nested brackets without matching closing bracket", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text [outer [inner](streamdown:incomplete-link)", Mend("Text [outer [inner"))
		must.Eq(t, "[foo [bar [baz](streamdown:incomplete-link)", Mend("[foo [bar [baz"))
		must.Eq(t, "Text [outer [inner]](streamdown:incomplete-link)", Mend("Text [outer [inner]"))
		must.Eq(t, "[link [nested] text](streamdown:incomplete-link)", Mend("[link [nested] text"))
	})
}

func TestLinkHandlingWithLinkModeTextOnly(t *testing.T) {
	t.Parallel()

	t.Run("should show plain text for incomplete links", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text with incomplete link", remend("Text with [incomplete link", textOnlyOpts()))
		must.Eq(t, "Text partial", remend("Text [partial", textOnlyOpts()))
	})

	t.Run("should keep complete links unchanged", func(t *testing.T) {
		t.Parallel()
		text := "Text with [complete link](url)"
		must.Eq(t, text, remend(text, textOnlyOpts()))
	})

	t.Run("should handle multiple complete links", func(t *testing.T) {
		t.Parallel()
		text := "[link1](url1) and [link2](url2)"
		must.Eq(t, text, remend(text, textOnlyOpts()))
	})

	t.Run("should handle nested brackets in incomplete links", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "outer [nested] text", remend("[outer [nested] text](incomplete", textOnlyOpts()))
		must.Eq(t, "link with [inner] content", remend("[link with [inner] content](http://incomplete", textOnlyOpts()))
		must.Eq(t, "Text foo [bar] baz", remend("Text [foo [bar] baz](", textOnlyOpts()))
	})

	t.Run("should handle partial link at chunk boundary", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Check out this lin", remend("Check out [this lin", textOnlyOpts()))
		must.Eq(t, "Visit our site", remend("Visit [our site](https://exa", textOnlyOpts()))
	})

	t.Run("should handle nested brackets without matching closing bracket", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text outer [inner", remend("Text [outer [inner", textOnlyOpts()))
		must.Eq(t, "foo [bar [baz", remend("[foo [bar [baz", textOnlyOpts()))
		must.Eq(t, "Text outer [inner]", remend("Text [outer [inner]", textOnlyOpts()))
		must.Eq(t, "link [nested] text", remend("[link [nested] text", textOnlyOpts()))
	})

	t.Run("should still use placeholder for incomplete images regardless of linkMode", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text ![incomplete image](streamdown:incomplete-image)", remend("Text ![incomplete image", textOnlyOpts()))
		must.Eq(t, "Text ![alt](streamdown:incomplete-image)", remend("Text ![alt](http://partial", textOnlyOpts()))
	})
}

func TestImageHandling(t *testing.T) {
	t.Parallel()

	t.Run("should replace incomplete images with placeholder", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text with "+incompleteImage("incomplete image"), Mend("Text with ![incomplete image"))
		must.Eq(t, incompleteImage("partial"), Mend("![partial"))
	})

	t.Run("should keep complete images unchanged", func(t *testing.T) {
		t.Parallel()
		text := "Text with ![alt text](image.png)"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle partial image at chunk boundary", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "See "+incompleteImage("the diag"), Mend("See ![the diag"))
		must.Eq(t, incompleteImage("logo"), Mend("![logo](./assets/log"))
	})

	t.Run("should handle nested brackets in incomplete images", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text "+incompleteImage("outer [inner]"), Mend("Text ![outer [inner]"))
		must.Eq(t, incompleteImage("nested [brackets] text"), Mend("![nested [brackets] text"))
		must.Eq(t, "Start "+incompleteImage("foo [bar] baz"), Mend("Start ![foo [bar] baz"))
	})

	t.Run("should not add trailing underscore for images with underscores in URL (#284)", func(t *testing.T) {
		t.Parallel()
		markdown := "textContent ![image](https://img.alicdn.com/imgextra/i4/6000000003603/O1CN01ApW8bQ1cUE8LduPra_!!6000000003603-2-skyky.png)"
		must.Eq(t, markdown, Mend(markdown))

		linkMarkdown := "textContent [link](https://example.com/path_name!!test)"
		must.Eq(t, linkMarkdown, Mend(linkMarkdown))

		multipleImages := "textContent ![image1](https://example.com/path_1!!test.png) ![image2](https://example.com/path_2!!test.png)"
		must.Eq(t, multipleImages, Mend(multipleImages))
	})
}

func TestLinkHandlerEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("should handle ]( without matching opening bracket", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "](partial", Mend("](partial"))
	})

	t.Run("should skip image brackets in text-only mode", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "![img text", remend("![img [text", textOnlyOpts()))
	})

	t.Run("should skip complete links in text-only mode", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "[link](url) incomplete", remend("[link](url) [incomplete", textOnlyOpts()))
	})

	t.Run("should handle complete bracket pair without link in text-only mode", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "[text] incomplete", remend("[text] [incomplete", textOnlyOpts()))
	})
}

func TestFindFirstIncompleteBracketWithIncompleteURL(t *testing.T) {
	t.Parallel()

	t.Run("should handle [text]( without ) before incomplete bracket", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "[a]( b](c incomplete", remend("[a]( b](c [incomplete", textOnlyOpts()))
	})
}

func TestMultipleIncompleteLinks(t *testing.T) {
	t.Parallel()

	t.Run("should handle two incomplete links", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "[link1 and [link2](streamdown:incomplete-link)", Mend("[link1 and [link2"))
	})

	t.Run("should handle one complete and one incomplete link", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "[first](url1) and [second](streamdown:incomplete-link)", Mend("[first](url1) and [second"))
	})

	t.Run("should handle nested incomplete brackets", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "[outer [inner]](streamdown:incomplete-link)", Mend("[outer [inner]"))
	})

	t.Run("should handle incomplete link in text-only mode", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "incomplete link", remend("[incomplete link", textOnlyOpts()))
	})

	t.Run("should handle two incomplete links in text-only mode", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "link1 and [link2", remend("[link1 and [link2", textOnlyOpts()))
	})
}

func TestLinkTextWithFormattingMarkers(t *testing.T) {
	t.Parallel()

	t.Run("should handle bold inside incomplete link URL", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "[**bold link**](streamdown:incomplete-link)", Mend("[**bold link**](incomplete-url"))
	})

	t.Run("should handle italic inside incomplete link URL", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "[*italic link*](streamdown:incomplete-link)", Mend("[*italic link*](incomplete"))
	})

	t.Run("should handle code inside incomplete link URL", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "[`code link`](streamdown:incomplete-link)", Mend("[`code link`](incomplete"))
	})

	t.Run("should handle incomplete formatting inside incomplete link text", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "[**bold link](streamdown:incomplete-link)", Mend("[**bold link"))
	})
}

func TestReferenceStyleLinksAndFootnotes(t *testing.T) {
	t.Parallel()

	t.Run("should handle reference-style link with complete brackets", func(t *testing.T) {
		t.Parallel()
		text := "[text][ref]"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle footnote reference", func(t *testing.T) {
		t.Parallel()
		text := "[^1]"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle incomplete reference link", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "[text][](streamdown:incomplete-link)", Mend("[text]["))
	})

	t.Run("should keep complete footnote definition", func(t *testing.T) {
		t.Parallel()
		text := "[^1]: footnote text"
		must.Eq(t, text, Mend(text))
	})
}

func TestDisabledLinksOptions(t *testing.T) {
	t.Parallel()

	t.Run("should still close links when only links disabled (images defaults to true)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "[link text](streamdown:incomplete-link)", remend("[link text", remendOptions{links: boolPtr(false)}))
	})

	t.Run("should not close links when both links and images disabled", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "[link text", remend("[link text", remendOptions{links: boolPtr(false), images: boolPtr(false)}))
	})
}

func TestRealWorldIncompleteLinksAndImages(t *testing.T) {
	t.Parallel()

	t.Run("should handle incomplete link in explanation", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Check the [documentation](streamdown:incomplete-link)", Mend("Check the [documentation"))
	})

	t.Run("should handle incomplete image (placeholder keeps preceding content)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Here's the diagram:\n\n![architecture](streamdown:incomplete-image)", Mend("Here's the diagram:\n\n![architecture"))
	})

	t.Run("should handle incomplete image with partial URL (placeholder keeps preceding content)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "See ![diagram](streamdown:incomplete-image)", Mend("See ![diagram](http://example.com/img"))
	})
}
