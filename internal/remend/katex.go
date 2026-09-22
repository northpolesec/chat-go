// Ported from packages/remend/src/katex-handler.ts @ streamdown v1.2.1.
// Divergences: positions are byte indices (ASCII fixtures). Inline $...$
// completion is opt-in via Options.InlineKatex / remendOptions.inlineKatex.
package remend

import "strings"

func isTripleBacktick(text string, index int) bool {
	if index >= 2 && index+1 <= len(text) && text[index-2:index+1] == "```" {
		return true
	}
	if index >= 1 && index+2 <= len(text) && text[index-1:index+2] == "```" {
		return true
	}
	if index+3 <= len(text) && text[index:index+3] == "```" {
		return true
	}
	return false
}

func countDollarPairs(text string) int {
	dollarPairs := 0
	inInlineCode := false
	for i := 0; i < len(text)-1; i++ {
		if text[i] == '`' && !isTripleBacktick(text, i) {
			inInlineCode = !inInlineCode
		}
		if !inInlineCode && text[i] == '$' && text[i+1] == '$' {
			dollarPairs++
			i++
		}
	}
	return dollarPairs
}

func countSingleDollars(text string) int {
	count := 0
	inInlineCode := false
	for i := 0; i < len(text); i++ {
		if text[i] == '\\' {
			i++
			continue
		}
		if text[i] == '`' && !isTripleBacktick(text, i) {
			inInlineCode = !inInlineCode
			continue
		}
		if !inInlineCode && text[i] == '$' {
			if i+1 < len(text) && text[i+1] == '$' {
				i++
			} else {
				count++
			}
		}
	}
	return count
}

func addClosingKatex(text string) string {
	if strings.HasSuffix(text, "$") && !strings.HasSuffix(text, "$$") {
		return text + "$"
	}
	firstDollarIndex := strings.Index(text, "$$")
	hasNewlineAfterStart := firstDollarIndex != -1 && strings.Contains(text[firstDollarIndex:], "\n")
	if hasNewlineAfterStart && !strings.HasSuffix(text, "\n") {
		return text + "\n$$"
	}
	return text + "$$"
}

func handleIncompleteBlockKatex(text string) string {
	if countDollarPairs(text)%2 == 0 {
		return text
	}
	return addClosingKatex(text)
}

func handleIncompleteInlineKatex(text string) string {
	if countSingleDollars(text)%2 == 1 {
		return text + "$"
	}
	return text
}
