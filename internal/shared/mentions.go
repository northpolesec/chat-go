// Ported from packages/adapter-shared/src/mentions.ts @ 6adca36 (chat v4.40.0).
// Divergences: positions are Go byte indices (ASCII fixtures); isBoundary
// uses unicode.IsSpace (TS char.trim() === ""); index 0 does not read
// text[-1] (JS undefined).
package shared

import (
	"strings"
	"unicode"
)

const (
	mentionHTTP  = "http://"
	mentionHTTPS = "https://"
	mentionFence = "```"
)

// MentionReplacer rewrites one bare @name.
type MentionReplacer func(mention, name string) string

// ReplaceBareMentions rewrites bare @name tokens, skipping code, URLs, hosts,
// and existing angle-bracket tokens.
func ReplaceBareMentions(text string, replacer MentionReplacer) string {
	return replaceRange(text, 0, len(text), replacer, true)
}

func isLetter(char byte) bool {
	return (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z')
}

func isNumber(char byte) bool {
	return char >= '0' && char <= '9'
}

func isWord(char byte) bool {
	return isLetter(char) || isNumber(char) || char == '_'
}

func isHost(char byte) bool {
	return isLetter(char) || isNumber(char) || char == '.' || char == '-'
}

func isBoundary(char byte) bool {
	return char == '<' || char == '>' || unicode.IsSpace(rune(char))
}

func startsWithFold(text string, index int, value string) bool {
	if index+len(value) > len(text) {
		return false
	}
	return strings.EqualFold(text[index:index+len(value)], value)
}

func findURLEnd(text string, index, end int) int {
	prefix := 0
	switch {
	case startsWithFold(text, index, mentionHTTPS):
		prefix = len(mentionHTTPS)
	case startsWithFold(text, index, mentionHTTP):
		prefix = len(mentionHTTP)
	}
	if prefix == 0 || index+prefix >= end {
		return index
	}
	cursor := index + prefix
	for cursor < end && !isBoundary(text[cursor]) {
		cursor++
	}
	return cursor
}

func findHostEnd(text string, index, end int) int {
	if !isLetter(text[index]) && !isNumber(text[index]) {
		return index
	}
	if index > 0 && isHost(text[index-1]) {
		return index
	}
	cursor := index
	for cursor < end && isHost(text[cursor]) {
		cursor++
	}
	if cursor >= end {
		return index
	}
	sep := text[cursor]
	if sep != '/' && sep != '?' && sep != '#' {
		return index
	}
	host := text[index:cursor]
	dot := strings.LastIndex(host, ".")
	suffix := host[dot+1:]
	if dot <= 0 || len(suffix) < 2 {
		return index
	}
	for i := 0; i < len(suffix); i++ {
		if !isLetter(suffix[i]) {
			return index
		}
	}
	cursor++
	for cursor < end && !isBoundary(text[cursor]) {
		cursor++
	}
	return cursor
}

func findCodeEnd(text string, index, end int) int {
	if text[index] != '`' {
		return index
	}
	fence := strings.HasPrefix(text[index:], mentionFence)
	marker := "`"
	if fence {
		marker = mentionFence
	}
	start := index + len(marker)
	closeAt := indexFrom(text, marker, start)
	if closeAt == -1 || closeAt >= end {
		return index
	}
	if !fence {
		newline := indexFrom(text, "\n", start)
		if newline != -1 && newline < closeAt {
			return index
		}
	}
	return closeAt + len(marker)
}

func replaceRange(text string, start, end int, replacer MentionReplacer, angles bool) string {
	var result strings.Builder
	index := start
	for index < end {
		codeEnd := findCodeEnd(text, index, end)
		if codeEnd > index {
			result.WriteString(text[index:codeEnd])
			index = codeEnd
			continue
		}
		if angles && text[index] == '<' {
			cursor := index + 1
			for cursor < end && text[cursor] != '>' && text[cursor] != '\n' && text[cursor] != '\r' {
				cursor++
			}
			if cursor < end && text[cursor] == '>' {
				result.WriteString(text[index : cursor+1])
				index = cursor + 1
				continue
			}
			result.WriteString(replaceRange(text, index, cursor, replacer, false))
			index = cursor
			continue
		}
		urlEnd := findURLEnd(text, index, end)
		if urlEnd > index {
			result.WriteString(text[index:urlEnd])
			index = urlEnd
			continue
		}
		hostEnd := findHostEnd(text, index, end)
		if hostEnd > index {
			result.WriteString(text[index:hostEnd])
			index = hostEnd
			continue
		}
		if text[index] == '@' && (index == 0 || text[index-1] != '<') && (index == 0 || !isWord(text[index-1])) && index+1 < end && isWord(text[index+1]) {
			cursor := index + 2
			for cursor < end && isWord(text[cursor]) {
				cursor++
			}
			mention := text[index:cursor]
			result.WriteString(replacer(mention, mention[1:]))
			index = cursor
			continue
		}
		result.WriteByte(text[index])
		index++
	}
	return result.String()
}
