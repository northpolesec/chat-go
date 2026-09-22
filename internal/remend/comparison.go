// Ported from packages/remend/src/comparison-operator-handler.ts @ streamdown v1.2.1.
// Divergences: ReplaceAllStringFunc cannot supply match offset, so matches are
// rewritten via FindAllStringSubmatchIndex. Positions are bytes. Go regexp has
// no /g flag; FindAll is the global walk. `(?m)` stands in for the JS `m` flag.
package remend

import (
	"regexp"
	"strings"
)

var listComparisonPattern = regexp.MustCompile(`(?m)^(\s*(?:[-*+]|\d+[.)]) +)>(=?\s*[$]?\d)`)

func handleComparisonOperators(text string) string {
	if text == "" || !strings.Contains(text, ">") {
		return text
	}
	matches := listComparisonPattern.FindAllStringSubmatchIndex(text, -1)
	if matches == nil {
		return text
	}
	var b strings.Builder
	last := 0
	for _, loc := range matches {
		b.WriteString(text[last:loc[0]])
		if isInsideCodeBlock(text, loc[0]) {
			b.WriteString(text[loc[0]:loc[1]])
		} else {
			b.WriteString(text[loc[2]:loc[3]])
			b.WriteString("\\>")
			b.WriteString(text[loc[4]:loc[5]])
		}
		last = loc[1]
	}
	b.WriteString(text[last:])
	return b.String()
}
