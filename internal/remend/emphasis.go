// Ported from packages/remend/src/emphasis-handlers.ts @ streamdown v1.2.1.
// Divergences: scanner positions are byte offsets so isInsideCodeBlock,
// isWithinMathBlock, isWithinLinkOrImageUrl, and isWithinHtmlTag stay on
// Task 6's byte convention. prev/next characters and isWordChar use runes
// because fixtures include multibyte word characters (café, 기울임, 中文).
// handle* stay unexported. QF1001 rewrote one De Morgan guard
// (`!(a || b)` → `!a && !b`); behavior is identical.
package remend

import (
	"strings"
	"unicode/utf8"
)

func hasMathDelimiters(text string) bool {
	return strings.Contains(text, "$") || strings.Contains(text, "\\(") || strings.Contains(text, "\\[")
}

func prevRune(text string, index int) string {
	if index <= 0 {
		return ""
	}
	r, _ := utf8.DecodeLastRuneInString(text[:index])
	return string(r)
}

func nextRune(text string, index int) string {
	if index+1 >= len(text) {
		return ""
	}
	r, _ := utf8.DecodeRuneInString(text[index+1:])
	return string(r)
}

func byteAt(text string, index int) byte {
	if index < 0 || index >= len(text) {
		return 0
	}
	return text[index]
}

func shouldSkipAsterisk(text string, index int, prevChar, nextChar string) bool {
	if prevChar == "\\" {
		return true
	}

	if hasMathDelimiters(text) && isWithinMathBlock(text, index) {
		return true
	}

	if prevChar != "*" && nextChar == "*" {
		var nextNextChar string
		if index < len(text)-2 {
			nextNextChar = string(text[index+2])
		}
		if nextNextChar == "*" {
			return false
		}
		return true
	}

	if prevChar == "*" {
		return true
	}

	prevIsWhitespace := prevChar == "" || prevChar == " " || prevChar == "\t" || prevChar == "\n"
	nextIsWhitespace := nextChar == "" || nextChar == " " || nextChar == "\t" || nextChar == "\n"
	if prevIsWhitespace && nextIsWhitespace {
		return true
	}

	return false
}

func isWhitespaceChar(char string) bool {
	return char == " " || char == "\t" || char == "\n"
}

func isWordInternalAsterisk(prevChar, nextChar string) bool {
	return prevChar != "" && nextChar != "" && isWordChar(prevChar) && isWordChar(nextChar)
}

func shouldCountSingleAsterisk(prevChar, nextChar string, count int, inWordAsteriskChain bool) (countIt bool, nextInWord bool) {
	isWordInternal := isWordInternalAsterisk(prevChar, nextChar)
	canOpen := nextChar != "" && !isWhitespaceChar(nextChar)
	canClose := prevChar != "" && !isWhitespaceChar(prevChar)

	if isWordInternal && count%2 == 0 && !inWordAsteriskChain {
		return false, false
	}

	if (canClose && count%2 == 1) || canOpen {
		return true, isWordInternal
	}

	return false, false
}

func countSingleAsterisks(text string) int {
	count := 0
	inCodeBlock := false
	inWordAsteriskChain := false
	lenText := len(text)

	for index := 0; index < lenText; index++ {
		if text[index] == '`' && index+2 < lenText && text[index+1] == '`' && text[index+2] == '`' {
			inCodeBlock = !inCodeBlock
			index += 2
			continue
		}

		if inCodeBlock {
			continue
		}

		if text[index] != '*' {
			r, size := utf8.DecodeRuneInString(text[index:])
			if !isWordChar(string(r)) {
				inWordAsteriskChain = false
			}
			if size > 1 {
				index += size - 1
			}
			continue
		}

		prevChar := prevRune(text, index)
		nextChar := nextRune(text, index)

		if shouldSkipAsterisk(text, index, prevChar, nextChar) {
			continue
		}

		if countIt, nextInWord := shouldCountSingleAsterisk(prevChar, nextChar, count, inWordAsteriskChain); countIt {
			count++
			inWordAsteriskChain = nextInWord
		}
	}

	return count
}

func shouldSkipUnderscore(text string, index int, prevChar, nextChar string) bool {
	if prevChar == "\\" {
		return true
	}

	if hasMathDelimiters(text) && isWithinMathBlock(text, index) {
		return true
	}

	if isWithinLinkOrImageURL(text, index) {
		return true
	}

	if isWithinHTMLTag(text, index) {
		return true
	}

	if prevChar == "_" || nextChar == "_" {
		return true
	}

	if prevChar != "" && nextChar != "" && isWordChar(prevChar) && isWordChar(nextChar) {
		return true
	}

	return false
}

func countSingleUnderscores(text string) int {
	count := 0
	inCodeBlock := false
	lenText := len(text)

	for index := 0; index < lenText; index++ {
		if text[index] == '`' && index+2 < lenText && text[index+1] == '`' && text[index+2] == '`' {
			inCodeBlock = !inCodeBlock
			index += 2
			continue
		}

		if inCodeBlock {
			continue
		}

		if text[index] != '_' {
			continue
		}

		prevChar := prevRune(text, index)
		nextChar := nextRune(text, index)

		if !shouldSkipUnderscore(text, index, prevChar, nextChar) {
			count++
		}
	}

	return count
}

func countTripleAsterisks(text string) int {
	count := 0
	consecutiveAsterisks := 0
	inCodeBlock := false

	for i := 0; i < len(text); i++ {
		if text[i] == '`' && i+2 < len(text) && text[i+1] == '`' && text[i+2] == '`' {
			if consecutiveAsterisks >= 3 {
				count += consecutiveAsterisks / 3
			}
			consecutiveAsterisks = 0
			inCodeBlock = !inCodeBlock
			i += 2
			continue
		}

		if inCodeBlock {
			continue
		}

		if text[i] == '*' {
			consecutiveAsterisks++
		} else {
			if consecutiveAsterisks >= 3 {
				count += consecutiveAsterisks / 3
			}
			consecutiveAsterisks = 0
		}
	}

	if consecutiveAsterisks >= 3 {
		count += consecutiveAsterisks / 3
	}

	return count
}

func countDoubleAsterisksOutsideCodeBlocks(text string) int {
	count := 0
	inCodeBlock := false

	for i := 0; i < len(text); i++ {
		if text[i] == '`' && i+2 < len(text) && text[i+1] == '`' && text[i+2] == '`' {
			inCodeBlock = !inCodeBlock
			i += 2
			continue
		}
		if inCodeBlock {
			continue
		}
		if text[i] == '*' && i+1 < len(text) && text[i+1] == '*' {
			count++
			i++
		}
	}
	return count
}

func countDoubleUnderscoresOutsideCodeBlocks(text string) int {
	count := 0
	inCodeBlock := false

	for i := 0; i < len(text); i++ {
		if text[i] == '`' && i+2 < len(text) && text[i+1] == '`' && text[i+2] == '`' {
			inCodeBlock = !inCodeBlock
			i += 2
			continue
		}
		if inCodeBlock {
			continue
		}
		if text[i] == '_' && i+1 < len(text) && text[i+1] == '_' {
			count++
			i++
		}
	}
	return count
}

func shouldSkipBoldCompletion(text, contentAfterMarker string, markerIndex int) bool {
	if contentAfterMarker == "" || whitespaceOrMarkersPattern.MatchString(contentAfterMarker) {
		return true
	}

	beforeMarker := text[:markerIndex]
	lastNewlineBeforeMarker := strings.LastIndex(beforeMarker, "\n")
	lineStart := 0
	if lastNewlineBeforeMarker != -1 {
		lineStart = lastNewlineBeforeMarker + 1
	}
	lineBeforeMarker := text[lineStart:markerIndex]

	if listItemPattern.MatchString(lineBeforeMarker) {
		hasNewlineInContent := strings.Contains(contentAfterMarker, "\n")
		if hasNewlineInContent {
			return true
		}
	}

	return isHorizontalRule(text, markerIndex, "*")
}

func handleIncompleteBold(text string) string {
	boldMatch := boldPattern.FindStringSubmatch(text)
	if boldMatch == nil {
		return text
	}

	contentAfterMarker := boldMatch[2]
	markerIndex := strings.LastIndex(text, boldMatch[1])

	if isInsideCodeBlock(text, markerIndex) || isWithinCompleteInlineCode(text, markerIndex) {
		return text
	}

	if shouldSkipBoldCompletion(text, contentAfterMarker, markerIndex) {
		return text
	}

	asteriskPairs := countDoubleAsterisksOutsideCodeBlocks(text)
	if asteriskPairs%2 == 1 {
		if strings.HasSuffix(contentAfterMarker, "*") {
			return text + "*"
		}
		return text + "**"
	}

	return text
}

func shouldSkipItalicCompletion(text, contentAfterMarker string, markerIndex int) bool {
	if contentAfterMarker == "" || whitespaceOrMarkersPattern.MatchString(contentAfterMarker) {
		return true
	}

	beforeMarker := text[:markerIndex]
	lastNewlineBeforeMarker := strings.LastIndex(beforeMarker, "\n")
	lineStart := 0
	if lastNewlineBeforeMarker != -1 {
		lineStart = lastNewlineBeforeMarker + 1
	}
	lineBeforeMarker := text[lineStart:markerIndex]

	if listItemPattern.MatchString(lineBeforeMarker) {
		hasNewlineInContent := strings.Contains(contentAfterMarker, "\n")
		if hasNewlineInContent {
			return true
		}
	}

	return isHorizontalRule(text, markerIndex, "_")
}

func handleIncompleteDoubleUnderscoreItalic(text string) string {
	italicMatch := italicPattern.FindStringSubmatch(text)
	if italicMatch == nil {
		halfCompleteMatch := halfCompleteUnderscorePattern.FindStringSubmatch(text)
		if halfCompleteMatch != nil {
			markerIndex := strings.LastIndex(text, halfCompleteMatch[1])
			if !isInsideCodeBlock(text, markerIndex) && !isWithinCompleteInlineCode(text, markerIndex) {
				underscorePairs := countDoubleUnderscoresOutsideCodeBlocks(text)
				if underscorePairs%2 == 1 {
					return text + "_"
				}
			}
		}
		return text
	}

	contentAfterMarker := italicMatch[2]
	markerIndex := strings.LastIndex(text, italicMatch[1])

	if isInsideCodeBlock(text, markerIndex) || isWithinCompleteInlineCode(text, markerIndex) {
		return text
	}

	if shouldSkipItalicCompletion(text, contentAfterMarker, markerIndex) {
		return text
	}

	underscorePairs := countDoubleUnderscoresOutsideCodeBlocks(text)
	if underscorePairs%2 == 1 {
		return text + "__"
	}

	return text
}

func findFirstSingleAsteriskIndex(text string) int {
	inCodeBlock := false

	for i := 0; i < len(text); i++ {
		if text[i] == '`' && i+2 < len(text) && text[i+1] == '`' && text[i+2] == '`' {
			inCodeBlock = !inCodeBlock
			i += 2
			continue
		}

		if inCodeBlock {
			continue
		}

		if text[i] == '*' &&
			byteAt(text, i-1) != '*' &&
			byteAt(text, i+1) != '*' &&
			byteAt(text, i-1) != '\\' &&
			!isWithinMathBlock(text, i) {
			prevChar := prevRune(text, i)
			nextChar := nextRune(text, i)

			prevIsWs := prevChar == "" || isWhitespaceChar(prevChar)
			nextIsWs := nextChar == "" || isWhitespaceChar(nextChar)
			if prevIsWs && nextIsWs {
				continue
			}

			if prevChar != "" && nextChar != "" && isWordChar(prevChar) && isWordChar(nextChar) {
				continue
			}

			if nextIsWs {
				continue
			}

			return i
		}
	}
	return -1
}

func handleIncompleteSingleAsteriskItalic(text string) string {
	if !singleAsteriskPattern.MatchString(text) {
		return text
	}

	firstSingleAsteriskIndex := findFirstSingleAsteriskIndex(text)

	if firstSingleAsteriskIndex == -1 {
		return text
	}

	if isInsideCodeBlock(text, firstSingleAsteriskIndex) || isWithinCompleteInlineCode(text, firstSingleAsteriskIndex) {
		return text
	}

	contentAfterFirstAsterisk := text[firstSingleAsteriskIndex+1:]

	if contentAfterFirstAsterisk == "" || whitespaceOrMarkersPattern.MatchString(contentAfterFirstAsterisk) {
		return text
	}

	singleAsterisks := countSingleAsterisks(text)
	if singleAsterisks%2 == 1 {
		return text + "*"
	}

	return text
}

func findFirstSingleUnderscoreIndex(text string) int {
	inCodeBlock := false

	for i := 0; i < len(text); i++ {
		if text[i] == '`' && i+2 < len(text) && text[i+1] == '`' && text[i+2] == '`' {
			inCodeBlock = !inCodeBlock
			i += 2
			continue
		}

		if inCodeBlock {
			continue
		}

		if text[i] == '_' &&
			byteAt(text, i-1) != '_' &&
			byteAt(text, i+1) != '_' &&
			byteAt(text, i-1) != '\\' &&
			!isWithinMathBlock(text, i) &&
			!isWithinLinkOrImageURL(text, i) {
			prevChar := prevRune(text, i)
			nextChar := nextRune(text, i)
			if prevChar != "" && nextChar != "" && isWordChar(prevChar) && isWordChar(nextChar) {
				continue
			}

			return i
		}
	}
	return -1
}

func insertClosingUnderscore(text string) string {
	endIndex := len(text)
	for endIndex > 0 && text[endIndex-1] == '\n' {
		endIndex--
	}
	if endIndex < len(text) {
		textBeforeNewlines := text[:endIndex]
		trailingNewlines := text[endIndex:]
		return textBeforeNewlines + "_" + trailingNewlines
	}
	return text + "_"
}

func handleTrailingAsterisksForUnderscore(text string) (string, bool) {
	if !strings.HasSuffix(text, "**") {
		return "", false
	}

	textWithoutTrailingAsterisks := text[:len(text)-2]
	asteriskPairsAfterRemoval := countDoubleAsterisksOutsideCodeBlocks(textWithoutTrailingAsterisks)

	if asteriskPairsAfterRemoval%2 != 1 {
		return "", false
	}

	firstDoubleAsteriskIndex := strings.Index(textWithoutTrailingAsterisks, "**")
	underscoreIndex := findFirstSingleUnderscoreIndex(textWithoutTrailingAsterisks)

	if firstDoubleAsteriskIndex != -1 && underscoreIndex != -1 && firstDoubleAsteriskIndex < underscoreIndex {
		return textWithoutTrailingAsterisks + "_**", true
	}

	return "", false
}

func handleIncompleteSingleUnderscoreItalic(text string) string {
	if !singleUnderscorePattern.MatchString(text) {
		return text
	}

	firstSingleUnderscoreIndex := findFirstSingleUnderscoreIndex(text)

	if firstSingleUnderscoreIndex == -1 {
		return text
	}

	contentAfterFirstUnderscore := text[firstSingleUnderscoreIndex+1:]

	if contentAfterFirstUnderscore == "" || whitespaceOrMarkersPattern.MatchString(contentAfterFirstUnderscore) {
		return text
	}

	if isInsideCodeBlock(text, firstSingleUnderscoreIndex) || isWithinCompleteInlineCode(text, firstSingleUnderscoreIndex) {
		return text
	}

	singleUnderscores := countSingleUnderscores(text)
	if singleUnderscores%2 == 1 {
		if trailingResult, ok := handleTrailingAsterisksForUnderscore(text); ok {
			return trailingResult
		}
		return insertClosingUnderscore(text)
	}

	return text
}

func areBoldItalicMarkersBalanced(text string) bool {
	asteriskPairs := countDoubleAsterisksOutsideCodeBlocks(text)
	singleAsterisks := countSingleAsterisks(text)
	return asteriskPairs%2 == 0 && singleAsterisks%2 == 0
}

func shouldSkipBoldItalicCompletion(text, contentAfterMarker string, markerIndex int) bool {
	if contentAfterMarker == "" || whitespaceOrMarkersPattern.MatchString(contentAfterMarker) {
		return true
	}

	if isInsideCodeBlock(text, markerIndex) || isWithinCompleteInlineCode(text, markerIndex) {
		return true
	}

	return isHorizontalRule(text, markerIndex, "*")
}

func handleIncompleteBoldItalic(text string) string {
	if fourOrMoreAsterisksPattern.MatchString(text) {
		return text
	}

	boldItalicMatch := boldItalicPattern.FindStringSubmatch(text)
	if boldItalicMatch == nil {
		return text
	}

	contentAfterMarker := boldItalicMatch[2]
	markerIndex := strings.LastIndex(text, boldItalicMatch[1])

	if shouldSkipBoldItalicCompletion(text, contentAfterMarker, markerIndex) {
		return text
	}

	tripleAsteriskCount := countTripleAsterisks(text)
	if tripleAsteriskCount%2 == 1 {
		if areBoldItalicMarkersBalanced(text) {
			return text
		}
		return text + "***"
	}

	return text
}
