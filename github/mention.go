package github

import "strings"

// detectMention is the port of chat.ts detectMention (upstream's Chat core
// sets isMention; chat-go has no Chat core, so the adapter does): a bare
// @userName or @botUserID, case-insensitive, not preceded by a word
// character and not followed by a word character or '-'. RE2 has no
// lookbehind, so this is a byte walk.
func detectMention(text, userName, botUserID string) bool {
	return containsMention(text, userName) || containsMention(text, botUserID)
}

func containsMention(text, name string) bool {
	if name == "" {
		return false
	}
	lower := strings.ToLower(text)
	needle := "@" + strings.ToLower(name)
	for from := 0; ; {
		i := strings.Index(lower[from:], needle)
		if i < 0 {
			return false
		}
		i += from
		end := i + len(needle)
		before := i == 0 || !isWordByte(lower[i-1])
		after := end == len(lower) || (!isWordByte(lower[end]) && lower[end] != '-')
		if before && after {
			return true
		}
		from = i + 1
	}
}

func isWordByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
