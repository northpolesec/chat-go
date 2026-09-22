// Ported from packages/remend/src/utils.ts and packages/remend/src/patterns.ts
// @ streamdown v1.2.1.
// Divergences: positions are byte indices (Go strings), not UTF-16 code units.
// isWordChar inspects the first rune (not UTF-16 code unit); Unicode letters
// use unicode.IsLetter/IsNumber instead of a \p{L}\p{N} regex. \s in compiled
// patterns is Go ASCII whitespace. Patterns live here (no patterns.go).
// linkImagePattern, incompleteLinkUrlPattern, and doubleUnderscoreGlobalPattern
// are unused by any remend handler upstream and are omitted.
package remend

import (
	"regexp"
	"unicode"
	"unicode/utf8"
)

var (
	boldPattern                   = regexp.MustCompile(`(\*\*)([^*]*\*?)$`)
	italicPattern                 = regexp.MustCompile(`(__)([^_]*?)$`)
	boldItalicPattern             = regexp.MustCompile(`(\*\*\*)([^*]*?)$`)
	singleAsteriskPattern         = regexp.MustCompile(`(\*)([^*]*?)$`)
	singleUnderscorePattern       = regexp.MustCompile(`(_)([^_]*?)$`)
	inlineCodePattern             = regexp.MustCompile("(`)([^`]*?)$")
	strikethroughPattern          = regexp.MustCompile(`(~~)([^~]*?)$`)
	whitespaceOrMarkersPattern    = regexp.MustCompile(`^[\s_~*` + "`" + `]*$`)
	listItemPattern               = regexp.MustCompile(`^[\s]*[-*+][\s]+$`)
	inlineTripleBacktickPattern   = regexp.MustCompile("^```[^`\\n]*```?$")
	fourOrMoreAsterisksPattern    = regexp.MustCompile(`^\*{4,}$`)
	halfCompleteUnderscorePattern = regexp.MustCompile(`(__)([^_]+)_$`)
	halfCompleteTildePattern      = regexp.MustCompile(`(~~)([^~]+)~$`)
	doubleTildeGlobalPattern      = regexp.MustCompile(`~~`)
)

func isWordChar(char string) bool {
	if char == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(char)
	if r <= 0x7F {
		return (r >= '0' && r <= '9') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= 'a' && r <= 'z') ||
			r == '_'
	}
	return unicode.IsLetter(r) || unicode.IsNumber(r)
}

func isWithinCodeBlock(text string, position int) bool {
	inCodeBlock := false
	for i := 0; i < position && i < len(text); i++ {
		if text[i] == '`' && i+2 < len(text) && text[i+1] == '`' && text[i+2] == '`' {
			inCodeBlock = !inCodeBlock
			i += 2
		}
	}
	return inCodeBlock
}

func findMatchingOpeningBracket(text string, closeIndex int) int {
	depth := 1
	for i := closeIndex - 1; i >= 0; i-- {
		switch text[i] {
		case ']':
			depth++
		case '[':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func findMatchingClosingBracket(text string, openIndex int) int {
	depth := 1
	for i := openIndex + 1; i < len(text); i++ {
		switch text[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

type mathContext int

const (
	mathNone mathContext = iota
	mathInlineDollar
	mathBlockDollar
	mathInlineLatex
	mathBlockLatex
)

func isLatexMathContext(context mathContext) bool {
	return context == mathInlineLatex || context == mathBlockLatex
}

func getLatexMathContext(context mathContext, nextChar byte) (mathContext, bool) {
	switch {
	case nextChar == '[' && context == mathNone:
		return mathBlockLatex, true
	case nextChar == ']' && context == mathBlockLatex:
		return mathNone, true
	case nextChar == '(' && context == mathNone:
		return mathInlineLatex, true
	case nextChar == ')' && context == mathInlineLatex:
		return mathNone, true
	default:
		return context, false
	}
}

func getDollarMathContext(context mathContext, isBlockDelimiter bool) mathContext {
	if isBlockDelimiter {
		if context == mathBlockDollar {
			return mathNone
		}
		return mathBlockDollar
	}
	if context == mathBlockDollar {
		return context
	}
	if context == mathInlineDollar {
		return mathNone
	}
	return mathInlineDollar
}

func isWithinMathBlock(text string, position int) bool {
	mathCtx := mathNone
	limit := min(len(text), position)
	for i := 0; i < limit; i++ {
		if text[i] == '\\' && i+1 < len(text) && text[i+1] == '$' {
			i++
			continue
		}
		if text[i] == '\\' && i+1 < len(text) {
			if next, ok := getLatexMathContext(mathCtx, text[i+1]); ok {
				mathCtx = next
				i++
				continue
			}
		}
		if text[i] == '$' && !isLatexMathContext(mathCtx) {
			isBlockDelimiter := i+1 < len(text) && text[i+1] == '$'
			mathCtx = getDollarMathContext(mathCtx, isBlockDelimiter)
			if isBlockDelimiter {
				i++
			}
		}
	}
	return mathCtx != mathNone
}

func isBeforeClosingParen(text string, position int) bool {
	for j := position; j < len(text); j++ {
		switch text[j] {
		case ')':
			return true
		case '\n':
			return false
		}
	}
	return false
}

func isWithinLinkOrImageURL(text string, position int) bool {
	for i := position - 1; i >= 0; i-- {
		switch text[i] {
		case ')':
			return false
		case '(':
			if i > 0 && text[i-1] == ']' {
				return isBeforeClosingParen(text, position)
			}
			return false
		case '\n':
			return false
		}
	}
	return false
}

func isWithinHTMLTag(text string, position int) bool {
	for i := position - 1; i >= 0; i-- {
		switch text[i] {
		case '>':
			return false
		case '<':
			var next byte
			if i+1 < len(text) {
				next = text[i+1]
			}
			if (next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z') || next == '/' {
				return true
			}
			return false
		case '\n':
			return false
		}
	}
	return false
}

func isHorizontalRule(text string, markerIndex int, marker string) bool {
	lineStart := 0
	for i := markerIndex - 1; i >= 0; i-- {
		if text[i] == '\n' {
			lineStart = i + 1
			break
		}
	}
	lineEnd := len(text)
	for i := markerIndex; i < len(text); i++ {
		if text[i] == '\n' {
			lineEnd = i
			break
		}
	}
	line := text[lineStart:lineEnd]
	markerCount := 0
	hasNonWhitespaceNonMarker := false
	for _, r := range line {
		if string(r) == marker {
			markerCount++
			continue
		}
		if r != ' ' && r != '\t' {
			hasNonWhitespaceNonMarker = true
			break
		}
	}
	return markerCount >= 3 && !hasNonWhitespaceNonMarker
}
