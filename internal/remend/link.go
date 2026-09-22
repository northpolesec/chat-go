// Ported from packages/remend/src/link-image-handler.ts @ streamdown v1.2.1.
// Divergences: positions are byte indices (ASCII fixtures). Incomplete images
// keep the streamdown:incomplete-image placeholder (same as upstream remend);
// they are not stripped here.
package remend

import "strings"

func handleIncompleteURL(text string, lastParenIndex int, linkMode string) (string, bool) {
	afterParen := text[lastParenIndex+2:]
	if strings.Contains(afterParen, ")") {
		return "", false
	}

	openBracketIndex := findMatchingOpeningBracket(text, lastParenIndex)
	if openBracketIndex == -1 || isInsideCodeBlock(text, openBracketIndex) {
		return "", false
	}

	isImage := openBracketIndex > 0 && text[openBracketIndex-1] == '!'
	startIndex := openBracketIndex
	if isImage {
		startIndex = openBracketIndex - 1
	}

	beforeLink := text[:startIndex]
	altOrLinkText := text[openBracketIndex+1 : lastParenIndex]

	if isImage {
		return beforeLink + "![" + altOrLinkText + "](" + incompleteImagePlaceholder + ")", true
	}
	if linkMode == linkModeTextOnly {
		return beforeLink + altOrLinkText, true
	}
	return beforeLink + "[" + altOrLinkText + "](" + incompleteLinkMarker + ")", true
}

func findFirstIncompleteBracket(text string, maxPos int) int {
	for j := 0; j < maxPos; j++ {
		if text[j] == '[' && !isInsideCodeBlock(text, j) {
			if j > 0 && text[j-1] == '!' {
				continue
			}
			closingIdx := findMatchingClosingBracket(text, j)
			if closingIdx == -1 {
				return j
			}
			if closingIdx+1 < len(text) && text[closingIdx+1] == '(' {
				urlEnd := strings.IndexByte(text[closingIdx+2:], ')')
				if urlEnd != -1 {
					j = closingIdx + 2 + urlEnd
				}
			}
		}
	}
	return maxPos
}

func handleIncompleteText(text string, i int, linkMode string) (string, bool) {
	isImage := i > 0 && text[i-1] == '!'
	openIndex := i
	if isImage {
		openIndex = i - 1
	}

	afterOpen := text[i+1:]
	if !strings.Contains(afterOpen, "]") {
		beforeLink := text[:openIndex]
		if isImage {
			altText := text[i+1:]
			return beforeLink + "![" + altText + "](" + incompleteImagePlaceholder + ")", true
		}
		if linkMode == linkModeTextOnly {
			firstIncomplete := findFirstIncompleteBracket(text, i)
			return text[:firstIncomplete] + text[firstIncomplete+1:], true
		}
		return text + "](" + incompleteLinkMarker + ")", true
	}

	closingIndex := findMatchingClosingBracket(text, i)
	if closingIndex == -1 {
		beforeLink := text[:openIndex]
		if isImage {
			altText := text[i+1:]
			return beforeLink + "![" + altText + "](" + incompleteImagePlaceholder + ")", true
		}
		if linkMode == linkModeTextOnly {
			firstIncomplete := findFirstIncompleteBracket(text, i)
			return text[:firstIncomplete] + text[firstIncomplete+1:], true
		}
		return text + "](" + incompleteLinkMarker + ")", true
	}

	return "", false
}

func handleIncompleteLinksAndImages(text string, linkMode string) string {
	if linkMode == "" {
		linkMode = linkModeProtocol
	}
	lastParenIndex := strings.LastIndex(text, "](")
	if lastParenIndex != -1 && !isInsideCodeBlock(text, lastParenIndex) {
		if result, ok := handleIncompleteURL(text, lastParenIndex, linkMode); ok {
			return result
		}
	}

	for i := len(text) - 1; i >= 0; i-- {
		if text[i] == '[' && !isInsideCodeBlock(text, i) {
			if result, ok := handleIncompleteText(text, i, linkMode); ok {
				return result
			}
		}
	}
	return text
}

func withLinkMode(linkMode string) func(string) string {
	return func(text string) string {
		return handleIncompleteLinksAndImages(text, linkMode)
	}
}
