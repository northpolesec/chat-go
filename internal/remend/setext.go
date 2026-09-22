// Ported from packages/remend/src/setext-heading-handler.ts @ streamdown v1.2.1.
// Divergences: empty string is the Go stand-in for TS !text / non-string.
// \s in the underline patterns is Go ASCII whitespace.
package remend

import (
	"regexp"
	"strings"
)

var (
	dashOnlyPattern        = regexp.MustCompile(`^-{1,2}$`)
	dashWithSpacePattern   = regexp.MustCompile(`^[\s]*-{1,2}[\s]+$`)
	equalsOnlyPattern      = regexp.MustCompile(`^={1,2}$`)
	equalsWithSpacePattern = regexp.MustCompile(`^[\s]*={1,2}[\s]+$`)
)

func handleIncompleteSetextHeading(text string) string {
	if text == "" {
		return text
	}

	lastNewlineIndex := strings.LastIndex(text, "\n")
	if lastNewlineIndex == -1 {
		return text
	}

	lastLine := text[lastNewlineIndex+1:]
	previousContent := text[:lastNewlineIndex]
	trimmedLastLine := strings.TrimSpace(lastLine)

	if dashOnlyPattern.MatchString(trimmedLastLine) && !dashWithSpacePattern.MatchString(lastLine) {
		if previousLineHasContent(previousContent) {
			return text + "\u200B"
		}
	}

	if equalsOnlyPattern.MatchString(trimmedLastLine) && !equalsWithSpacePattern.MatchString(lastLine) {
		if previousLineHasContent(previousContent) {
			return text + "\u200B"
		}
	}

	return text
}

func previousLineHasContent(previousContent string) bool {
	lines := strings.Split(previousContent, "\n")
	previousLine := lines[len(lines)-1]
	return strings.TrimSpace(previousLine) != ""
}
