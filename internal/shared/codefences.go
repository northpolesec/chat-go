// Ported from packages/adapter-shared/src/code-fences.ts @ 6adca36 (chat v4.40.0).
// Divergences: positions are Go byte indices (ASCII fixtures); trimStart uses
// unicode.IsSpace (same as other ports); BLOCK_MARKER / ordered-list patterns
// are hand-checked because RE2 has no lookahead.
package shared

import (
	"strings"
	"unicode"
)

const codeFence = "```"

// NormalizeCodeFencesOptions is the upstream NormalizeCodeFencesOptions.
type NormalizeCodeFencesOptions struct {
	ConvertCode func(string) string
	ConvertText func(string) string
}

// NormalizeCodeFences rewrites Slack-style ``` fences onto their own lines.
func NormalizeCodeFences(text string, options NormalizeCodeFencesOptions) string {
	convertText := options.ConvertText
	if convertText == nil {
		convertText = passThrough
	}
	convertCode := options.ConvertCode
	if convertCode == nil {
		convertCode = passThrough
	}
	if !strings.Contains(text, codeFence) {
		return convertText(text)
	}

	var result strings.Builder
	textStart := 0
	cursor := 0
	movedToOwnLine := false

	flushTextBefore := func(end int) {
		segment := text[textStart:end]
		if movedToOwnLine {
			segment = escapeLeadingBlockMarker(segment)
			movedToOwnLine = false
		}
		result.WriteString(convertText(segment))
	}

	for cursor < len(text) {
		if text[cursor] != '`' {
			cursor++
			continue
		}
		if !strings.HasPrefix(text[cursor:], codeFence) {
			spanEnd := findInlineCodeEnd(text, cursor)
			if spanEnd == -1 {
				cursor++
			} else {
				cursor = spanEnd
			}
			continue
		}

		contentStart := cursor + len(codeFence)
		contentEnd := indexFrom(text, codeFence, contentStart)
		if contentEnd == -1 || isOnBlockquoteLine(text, cursor) {
			cursor = contentStart
			continue
		}

		flushTextBefore(cursor)
		if result.Len() > 0 && !strings.HasSuffix(result.String(), "\n") {
			result.WriteByte('\n')
		}
		content := text[contentStart:contentEnd]
		result.WriteString(codeFence)
		if !strings.HasPrefix(content, "\n") {
			result.WriteByte('\n')
		}
		result.WriteString(convertCode(content))
		if !strings.HasSuffix(content, "\n") {
			result.WriteByte('\n')
		}
		result.WriteString(codeFence)

		cursor = contentEnd + len(codeFence)
		textStart = cursor
		if cursor < len(text) && text[cursor] != '\n' {
			result.WriteByte('\n')
			movedToOwnLine = true
		}
	}

	flushTextBefore(len(text))
	return result.String()
}

func passThrough(value string) string {
	return value
}

func indexFrom(text, substr string, start int) int {
	if start > len(text) {
		return -1
	}
	i := strings.Index(text[start:], substr)
	if i == -1 {
		return -1
	}
	return start + i
}

func findInlineCodeEnd(text string, index int) int {
	closeAt := indexFrom(text, "`", index+1)
	if closeAt == -1 {
		return -1
	}
	newline := indexFrom(text, "\n", index+1)
	if newline != -1 && newline < closeAt {
		return -1
	}
	return closeAt + 1
}

func isOnBlockquoteLine(text string, index int) bool {
	lineStart := 0
	if index > 0 {
		lineStart = strings.LastIndex(text[:index], "\n") + 1
	}
	prefix := strings.TrimLeftFunc(text[lineStart:index], unicode.IsSpace)
	return strings.HasPrefix(prefix, ">")
}

func escapeLeadingBlockMarker(text string) string {
	whitespace := leadingASCIIWhitespace(text)
	prefix := ""
	if len(whitespace) > 0 {
		prefix = " "
	}
	rest := text[len(whitespace):]
	if hasBlockMarker(rest) {
		return prefix + `\` + rest
	}
	return prefix + escapeOrderedListMarker(rest)
}

func leadingASCIIWhitespace(text string) string {
	i := 0
	for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
		i++
	}
	return text[:i]
}

// hasBlockMarker is BLOCK_MARKER_PATTERN without lookahead (Go RE2).
func hasBlockMarker(rest string) bool {
	if rest == "" {
		return false
	}
	switch rest[0] {
	case '<', '>':
		return true
	case '#':
		n := 0
		for n < len(rest) && rest[n] == '#' && n < 6 {
			n++
		}
		if n == 0 || n > 6 {
			return false
		}
		return n == len(rest) || rest[n] == ' ' || rest[n] == '\t' || rest[n] == '\n'
	case '-', '+', '*':
		if len(rest) == 1 || rest[1] == ' ' || rest[1] == '\t' || rest[1] == '\n' {
			return true
		}
		return isThematicBreak(rest)
	case '`':
		n := 0
		for n < len(rest) && rest[n] == '`' {
			n++
		}
		return n >= 3
	case '~':
		n := 0
		for n < len(rest) && rest[n] == '~' {
			n++
		}
		return n >= 3
	case '_':
		return isThematicBreak(rest)
	default:
		return false
	}
}

func isThematicBreak(rest string) bool {
	// (?:[-*_][ \t]*){3,}(?=\n|$)
	count := 0
	i := 0
	for i < len(rest) {
		if rest[i] != '-' && rest[i] != '*' && rest[i] != '_' {
			break
		}
		i++
		count++
		for i < len(rest) && (rest[i] == ' ' || rest[i] == '\t') {
			i++
		}
	}
	if count < 3 {
		return false
	}
	return i == len(rest) || rest[i] == '\n'
}

func escapeOrderedListMarker(rest string) string {
	// ^(\d{1,9})([.)])(?=[ \t\n]|$)
	n := 0
	for n < len(rest) && n < 9 && rest[n] >= '0' && rest[n] <= '9' {
		n++
	}
	if n == 0 || n >= len(rest) {
		return rest
	}
	mark := rest[n]
	if mark != '.' && mark != ')' {
		return rest
	}
	if n+1 < len(rest) && rest[n+1] != ' ' && rest[n+1] != '\t' && rest[n+1] != '\n' {
		return rest
	}
	return rest[:n] + `\` + rest[n:]
}
