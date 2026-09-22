// Ported from packages/remend/src/strikethrough-handler.ts @ streamdown v1.2.1.
// Divergences: none beyond byte-index lastIndexOf (fixtures are ASCII).
package remend

import "strings"

func handleIncompleteStrikethrough(text string) string {
	if m := strikethroughPattern.FindStringSubmatch(text); m != nil {
		contentAfterMarker := m[2]
		if contentAfterMarker == "" || whitespaceOrMarkersPattern.MatchString(contentAfterMarker) {
			return text
		}
		markerIndex := strings.LastIndex(text, m[1])
		if isInsideCodeBlock(text, markerIndex) || isWithinCompleteInlineCode(text, markerIndex) {
			return text
		}
		if len(doubleTildeGlobalPattern.FindAllString(text, -1))%2 == 1 {
			return text + "~~"
		}
	} else if half := halfCompleteTildePattern.FindStringSubmatch(text); half != nil {
		markerIndex := strings.LastIndex(text, half[0][:2])
		if isInsideCodeBlock(text, markerIndex) || isWithinCompleteInlineCode(text, markerIndex) {
			return text
		}
		if len(doubleTildeGlobalPattern.FindAllString(text, -1))%2 == 1 {
			return text + "~"
		}
	}
	return text
}
