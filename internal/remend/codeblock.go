// Ported from packages/remend/src/code-block-utils.ts @ streamdown v1.2.1.
// Divergences: positions and lookup indices are bytes, not UTF-16 code units.
// The single-entry lookup cache is mutex-guarded (TS is single-threaded).
package remend

import "sync"

func buildCodeBlockLookup(text string) []byte {
	lookup := make([]byte, len(text)+1)
	inInlineCode := false
	inMultilineCode := false
	i := 0
	for i < len(text) {
		if text[i] == '\\' && i+1 < len(text) && text[i+1] == '`' {
			state := byte(0)
			if inInlineCode || inMultilineCode {
				state = 1
			}
			lookup[i+1] = state
			lookup[i+2] = state
			i += 2
			continue
		}
		if i+3 <= len(text) && text[i:i+3] == "```" {
			inMultilineCode = !inMultilineCode
			state := byte(0)
			if inInlineCode || inMultilineCode {
				state = 1
			}
			next := min(i+3, len(text))
			for p := i + 1; p <= next; p++ {
				lookup[p] = state
			}
			i = next
			continue
		}
		if !inMultilineCode && text[i] == '`' {
			inInlineCode = !inInlineCode
		}
		if inInlineCode || inMultilineCode {
			lookup[i+1] = 1
		}
		i++
	}
	return lookup
}

type codeBlockCacheEntry struct {
	text   string
	lookup []byte
}

var (
	codeBlockCacheMu sync.Mutex
	codeBlockCache   *codeBlockCacheEntry
)

func isInsideCodeBlock(text string, position int) bool {
	codeBlockCacheMu.Lock()
	defer codeBlockCacheMu.Unlock()
	current := codeBlockCache
	if current == nil || current.text != text {
		current = &codeBlockCacheEntry{text: text, lookup: buildCodeBlockLookup(text)}
		codeBlockCache = current
	}
	p := min(position, len(text))
	if p < 0 {
		return false
	}
	return current.lookup[p] == 1
}

func isPartOfTripleBacktick(text string, i int) bool {
	isTripleStart := i+3 <= len(text) && text[i:i+3] == "```"
	isTripleMiddle := i > 0 && i+2 <= len(text) && text[i-1:i+2] == "```"
	isTripleEnd := i > 1 && i+1 <= len(text) && text[i-2:i+1] == "```"
	return isTripleStart || isTripleMiddle || isTripleEnd
}

func countSingleBackticks(text string) int {
	count := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\\' && i+1 < len(text) && text[i+1] == '`' {
			i++
			continue
		}
		if text[i] == '`' && !isPartOfTripleBacktick(text, i) {
			count++
		}
	}
	return count
}

func isWithinCompleteInlineCode(text string, position int) bool {
	inInlineCode := false
	inMultilineCode := false
	inlineCodeStart := -1
	for i := 0; i < len(text); i++ {
		if text[i] == '\\' && i+1 < len(text) && text[i+1] == '`' {
			i++
			continue
		}
		if i+3 <= len(text) && text[i:i+3] == "```" {
			inMultilineCode = !inMultilineCode
			i += 2
			continue
		}
		if !inMultilineCode && text[i] == '`' {
			if inInlineCode {
				if inlineCodeStart < position && position < i {
					return true
				}
				inInlineCode = false
				inlineCodeStart = -1
			} else {
				inInlineCode = true
				inlineCodeStart = i
			}
		}
	}
	return false
}
