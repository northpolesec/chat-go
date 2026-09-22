// Ported from packages/adapter-slack/src/format/index.ts @ 6adca36 (chat v4.40.0).
// Divergences: helpers are unexported until Task 30; TypeError throws are
// panics; Date|number timestamps are any (int, int64, time.Time); optional
// emoji/verbatim are *bool; optional link/label are empty string; lookbehind
// mention/emphasis rewrites are a byte walk (RE2 has no lookbehind); string
// lengths are Go bytes (ASCII fixtures). format/boundary.test.ts is not
// ported: this file imports only the standard library (compile-time stand-in
// for the TS import boundary). replaceBareMentions lives in
// internal/shared/mentions.go (adapter-shared/mentions.ts), not format/.
package slack

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	codeFence             = "```"
	textObjectMaxLength   = 3000
	slackControlChars     = "<>|"
	slackDateControlChars = "^|>"
)

var (
	slackIDPattern        = regexp.MustCompile(`^[A-Z0-9_]+$`)
	markdownBoldPattern   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	userMentionLabeled    = regexp.MustCompile(`<@([A-Z0-9_]+)\|([^<>]+)>`)
	userMentionBare       = regexp.MustCompile(`<@([A-Z0-9_]+)>`)
	channelMentionLabeled = regexp.MustCompile(`<#([A-Z0-9_]+)\|([^<>]+)>`)
	channelMentionBare    = regexp.MustCompile(`<#([A-Z0-9_]+)>`)
	swappedLabelURL       = regexp.MustCompile(`<([^<>|]+)\|(https?://[^|<>]+)>`)
	labeledURL            = regexp.MustCompile(`<(https?://[^|<>]+)\|([^<>]+)>`)
	bareURL               = regexp.MustCompile(`<(https?://[^<>]+)>`)
)

// slackPlainTextObject is the upstream SlackPlainTextObject.
type slackPlainTextObject struct {
	Emoji *bool
	Text  string
	Type  string
}

// slackMrkdwnTextObject is the upstream SlackMrkdwnTextObject.
type slackMrkdwnTextObject struct {
	Text     string
	Type     string
	Verbatim *bool
}

// slackTextOptions is the upstream SlackTextOptions.
type slackTextOptions struct {
	Emoji    *bool
	Verbatim *bool
}

// slackDateOptions is the upstream SlackDateOptions.
type slackDateOptions struct {
	Link string
}

func escapeSlackText(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	return text
}

func unescapeSlackText(text string) string {
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&amp;", "&")
	return text
}

func createSlackPlainText(text string, options slackTextOptions) slackPlainTextObject {
	assertSlackTextObjectText(text)
	return slackPlainTextObject{
		Emoji: options.Emoji,
		Text:  text,
		Type:  "plain_text",
	}
}

func createSlackMrkdwn(text string, options slackTextOptions) slackMrkdwnTextObject {
	assertSlackTextObjectText(text)
	return slackMrkdwnTextObject{
		Text:     text,
		Type:     "mrkdwn",
		Verbatim: options.Verbatim,
	}
}

func formatSlackUser(userID string) string {
	assertSlackID(userID, "userId")
	return "<@" + userID + ">"
}

func formatSlackChannel(channelID string) string {
	assertSlackID(channelID, "channelId")
	return "<#" + channelID + ">"
}

func formatSlackUserGroup(userGroupID string) string {
	assertSlackID(userGroupID, "userGroupId")
	return "<!subteam^" + userGroupID + ">"
}

func formatSlackSpecialMention(mention string) string {
	return "<!" + mention + ">"
}

func formatSlackLink(url, label string) string {
	assertNoSlackControl(url, "url")
	if label == "" {
		return "<" + url + ">"
	}
	return "<" + url + "|" + escapeSlackText(label) + ">"
}

func formatSlackDate(timestamp any, token, fallback string, options slackDateOptions) string {
	assertNoSlackDateControl(token, "token")
	seconds := slackDateSeconds(timestamp)
	link := ""
	if options.Link != "" {
		link = "^" + assertSlackDateLink(options.Link)
	}
	return fmt.Sprintf("<!date^%d^%s%s|%s>", seconds, token, link, escapeSlackText(fallback))
}

func slackDateSeconds(timestamp any) int64 {
	switch t := timestamp.(type) {
	case time.Time:
		return t.Unix()
	case int:
		return int64(t)
	case int64:
		return t
	case float64:
		if t != float64(int64(t)) {
			panic("timestamp must be an integer unix timestamp or Date")
		}
		return int64(t)
	default:
		panic("timestamp must be an integer unix timestamp or Date")
	}
}

func slackMrkdwnToMarkdown(mrkdwn string) string {
	var markdown string
	if strings.Contains(mrkdwn, codeFence) {
		markdown = convertMrkdwnWithCodeFences(mrkdwn)
	} else {
		markdown = convertMrkdwnText(mrkdwn)
	}
	return unescapeSlackText(markdown)
}

func convertSlackTokens(mrkdwn string) string {
	markdown := userMentionLabeled.ReplaceAllString(mrkdwn, "@$2")
	markdown = userMentionBare.ReplaceAllString(markdown, "@$1")
	markdown = channelMentionLabeled.ReplaceAllString(markdown, "#$2 ($1)")
	markdown = channelMentionBare.ReplaceAllString(markdown, "#$1")
	markdown = swappedLabelURL.ReplaceAllStringFunc(markdown, func(match string) string {
		parts := swappedLabelURL.FindStringSubmatch(match)
		if strings.HasPrefix(parts[1], "http://") || strings.HasPrefix(parts[1], "https://") {
			return match
		}
		return "<" + parts[2] + "|" + parts[1] + ">"
	})
	markdown = labeledURL.ReplaceAllString(markdown, "[$2]($1)")
	return bareURL.ReplaceAllString(markdown, "$1")
}

func convertMrkdwnText(mrkdwn string) string {
	return convertMrkdwnMarkers(convertSlackTokens(mrkdwn))
}

// convertMrkdwnMarkers is the TS lookbehind pair
// /(?<![_*\\])\*([^*\n]+)\*(?![_*])/g and /(?<!~)~([^~\n]+)~(?!~)/g.
func convertMrkdwnMarkers(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '*' {
			if prev, ok := byteAt(s, i-1); !ok || (prev != '_' && prev != '*' && prev != '\\') {
				j := i + 1
				for j < len(s) && s[j] != '*' && s[j] != '\n' {
					j++
				}
				if j > i+1 && j < len(s) && s[j] == '*' {
					if next, ok := byteAt(s, j+1); !ok || (next != '_' && next != '*') {
						b.WriteString("**")
						b.WriteString(s[i+1 : j])
						b.WriteString("**")
						i = j + 1
						continue
					}
				}
			}
		}
		if s[i] == '~' {
			if prev, ok := byteAt(s, i-1); !ok || prev != '~' {
				j := i + 1
				for j < len(s) && s[j] != '~' && s[j] != '\n' {
					j++
				}
				if j > i+1 && j < len(s) && s[j] == '~' {
					if next, ok := byteAt(s, j+1); !ok || next != '~' {
						b.WriteString("~~")
						b.WriteString(s[i+1 : j])
						b.WriteString("~~")
						i = j + 1
						continue
					}
				}
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func byteAt(s string, i int) (byte, bool) {
	if i < 0 || i >= len(s) {
		return 0, false
	}
	return s[i], true
}

func convertMrkdwnWithCodeFences(mrkdwn string) string {
	var result strings.Builder
	textStart := 0
	cursor := 0
	movedToOwnLine := false

	flushTextBefore := func(end int) {
		text := mrkdwn[textStart:end]
		if movedToOwnLine {
			text = escapeLeadingBlockMarker(text)
			movedToOwnLine = false
		}
		result.WriteString(convertMrkdwnText(text))
	}

	for cursor < len(mrkdwn) {
		switch mrkdwn[cursor] {
		case '<':
			tokenEnd := findAngleTokenEnd(mrkdwn, cursor)
			if tokenEnd == -1 {
				cursor++
			} else {
				cursor = tokenEnd
			}
		case '`':
			if !strings.HasPrefix(mrkdwn[cursor:], codeFence) {
				spanEnd := findInlineCodeEnd(mrkdwn, cursor)
				if spanEnd == -1 {
					cursor++
				} else {
					cursor = spanEnd
				}
				continue
			}
			contentStart := cursor + len(codeFence)
			contentEnd := indexFrom(mrkdwn, codeFence, contentStart)
			if contentEnd == -1 || isOnBlockquoteLine(mrkdwn, cursor) {
				cursor = contentStart
				continue
			}
			flushTextBefore(cursor)
			if result.Len() > 0 && !strings.HasSuffix(result.String(), "\n") {
				result.WriteByte('\n')
			}
			content := mrkdwn[contentStart:contentEnd]
			result.WriteString(codeFence)
			if !strings.HasPrefix(content, "\n") {
				result.WriteByte('\n')
			}
			result.WriteString(convertSlackTokens(content))
			if !strings.HasSuffix(content, "\n") {
				result.WriteByte('\n')
			}
			result.WriteString(codeFence)
			cursor = contentEnd + len(codeFence)
			textStart = cursor
			if cursor < len(mrkdwn) && mrkdwn[cursor] != '\n' {
				result.WriteByte('\n')
				movedToOwnLine = true
			}
		default:
			cursor++
		}
	}
	flushTextBefore(len(mrkdwn))
	return result.String()
}

func findAngleTokenEnd(mrkdwn string, index int) int {
	cursor := index + 1
	for cursor < len(mrkdwn) && mrkdwn[cursor] != '>' && mrkdwn[cursor] != '\n' && mrkdwn[cursor] != '\r' {
		cursor++
	}
	if cursor < len(mrkdwn) && mrkdwn[cursor] == '>' {
		return cursor + 1
	}
	return -1
}

func findInlineCodeEnd(mrkdwn string, index int) int {
	closeAt := indexFrom(mrkdwn, "`", index+1)
	if closeAt == -1 {
		return -1
	}
	newline := indexFrom(mrkdwn, "\n", index+1)
	if newline != -1 && newline < closeAt {
		return -1
	}
	return closeAt + 1
}

func isOnBlockquoteLine(mrkdwn string, index int) bool {
	lineStart := 0
	if index > 0 {
		lineStart = strings.LastIndex(mrkdwn[:index], "\n") + 1
	}
	prefix := strings.TrimLeftFunc(mrkdwn[lineStart:index], unicode.IsSpace)
	return strings.HasPrefix(prefix, "&gt;")
}

func escapeLeadingBlockMarker(text string) string {
	whitespace := leadingASCIIWhitespace(text)
	prefix := ""
	if len(whitespace) > 0 {
		prefix = " "
	}
	rest := text[len(whitespace):]
	if hasMrkdwnBlockMarker(rest) {
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

func hasMrkdwnBlockMarker(rest string) bool {
	if rest == "" {
		return false
	}
	if strings.HasPrefix(rest, "&gt;") || strings.HasPrefix(rest, "&lt;") {
		return true
	}
	switch rest[0] {
	case '#':
		n := 0
		for n < len(rest) && rest[n] == '#' && n < 6 {
			n++
		}
		return n > 0 && n <= 6 && (n == len(rest) || rest[n] == ' ' || rest[n] == '\t' || rest[n] == '\n')
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

func markdownBoldToSlackMrkdwn(markdown string) string {
	return markdownBoldPattern.ReplaceAllString(markdown, "*$1*")
}

func linkBareSlackMentions(text string) string {
	var b strings.Builder
	i := 0
	for i < len(text) {
		if text[i] == '@' && i+1 < len(text) && isSlackMentionIDStart(text[i+1]) {
			if i == 0 || !isSlackMentionLookbehind(text[i-1]) {
				j := i + 2
				for j < len(text) && isSlackMentionIDRest(text[j]) {
					j++
				}
				if j >= i+3 {
					b.WriteString("<@")
					b.WriteString(text[i+1 : j])
					b.WriteByte('>')
					i = j
					continue
				}
			}
		}
		b.WriteByte(text[i])
		i++
	}
	return b.String()
}

func isSlackMentionLookbehind(c byte) bool {
	return c == '<' || isWordByte(c)
}

func isWordByte(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_'
}

func isSlackMentionIDStart(c byte) bool {
	return c >= 'A' && c <= 'Z'
}

func isSlackMentionIDRest(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

func assertSlackTextObjectText(text string) {
	if len(text) < 1 || len(text) > textObjectMaxLength {
		panic("text must be between 1 and " + strconv.Itoa(textObjectMaxLength) + " characters")
	}
}

func assertSlackID(value, name string) {
	if !slackIDPattern.MatchString(value) {
		panic(name + " must be a Slack ID")
	}
}

func assertNoSlackControl(value, name string) {
	if strings.ContainsAny(value, slackControlChars) {
		panic(name + " cannot contain Slack control characters")
	}
}

func assertNoSlackDateControl(value, name string) {
	if strings.ContainsAny(value, slackDateControlChars) {
		panic(name + " cannot contain Slack date control characters")
	}
}

func assertSlackDateLink(value string) string {
	assertNoSlackDateControl(value, "link")
	return value
}
