// Ported from packages/remend/src/inline-code-handler.ts @ streamdown v1.2.1.
// Divergences: none beyond byte-index string ops (fixtures are ASCII).
package remend

import "strings"

func handleInlineTripleBackticks(text string) (string, bool) {
	if !inlineTripleBacktickPattern.MatchString(text) || strings.Contains(text, "\n") {
		return "", false
	}
	if strings.HasSuffix(text, "``") && !strings.HasSuffix(text, "```") {
		return text + "`", true
	}
	return text, true
}

func isInsideIncompleteCodeBlock(text string) bool {
	return strings.Count(text, "```")%2 == 1
}

func handleIncompleteInlineCode(text string) string {
	if result, ok := handleInlineTripleBackticks(text); ok {
		return result
	}
	m := inlineCodePattern.FindStringSubmatch(text)
	if m != nil && !isInsideIncompleteCodeBlock(text) {
		contentAfterMarker := m[2]
		if contentAfterMarker == "" || whitespaceOrMarkersPattern.MatchString(contentAfterMarker) {
			return text
		}
		if countSingleBackticks(text)%2 == 1 {
			return text + "`"
		}
	}
	return text
}
