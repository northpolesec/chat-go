// Ported from packages/remend/src/html-tag-handler.ts @ streamdown v1.2.1.
// Divergences: JS trimEnd → strings.TrimRightFunc(..., unicode.IsSpace).
// Match index is a byte offset.
package remend

import (
	"regexp"
	"strings"
	"unicode"
)

var incompleteHTMLTagPattern = regexp.MustCompile(`<[a-zA-Z/][^>]*$`)

func handleIncompleteHTMLTag(text string) string {
	loc := incompleteHTMLTagPattern.FindStringIndex(text)
	if loc == nil {
		return text
	}
	if isInsideCodeBlock(text, loc[0]) {
		return text
	}
	return strings.TrimRightFunc(text[:loc[0]], unicode.IsSpace)
}
