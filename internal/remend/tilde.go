// Ported from packages/remend/src/single-tilde-handler.ts @ streamdown v1.2.1.
// Divergences: no JS lookahead; walk runes and test the next rune. Word-char
// class is unicode.IsLetter/IsNumber (same as \p{L}\p{N}). isInsideCodeBlock
// is called with the byte offset of `~` (not a UTF-16 offset). Supplementary-
// plane letters (e.g. 𐐀) are one rune in Go; tests require that behavior.
package remend

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_'
}

func handleSingleTildeEscape(text string) string {
	if text == "" || !strings.Contains(text, "~") {
		return text
	}
	var b strings.Builder
	b.Grow(len(text) + 8)
	changed := false
	i := 0
	for i < len(text) {
		r, size := utf8.DecodeRuneInString(text[i:])
		if isWordRune(r) {
			next := i + size
			if next < len(text) && text[next] == '~' && (next+1 >= len(text) || text[next+1] != '~') {
				if next+1 < len(text) {
					r2, _ := utf8.DecodeRuneInString(text[next+1:])
					if isWordRune(r2) && !isInsideCodeBlock(text, next) {
						b.WriteString(text[i:next])
						b.WriteString("\\~")
						i = next + 1
						changed = true
						continue
					}
				}
			}
		}
		b.WriteString(text[i : i+size])
		i += size
	}
	if !changed {
		return text
	}
	return b.String()
}
