// Ported from packages/chat/src/streaming-markdown.ts @ 6adca36 (chat v4.40.0).
// Divergences: wrapTablesForAppend default-true → DisableTableWrapForAppend
// (Go zero value reproduces the upstream default); getCommittableText/getText
// → CommittableText/Text; JS string indices / .length → Go byte indices / len
// (ASCII fixtures); trimStart/trim → TrimLeftFunc(unicode.IsSpace)/TrimSpace;
// [\s:] is Go-regexp ASCII \s vs JS Unicode \s (ASCII fixtures).
package chat

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/northpolesec/chat-go/internal/remend"
)

// StreamingMarkdownOptions configures StreamingMarkdownRenderer.
// DisableTableWrapForAppend inverts upstream wrapTablesForAppend (default true).
type StreamingMarkdownOptions struct {
	DisableTableWrapForAppend bool
}

// StreamingMarkdownRenderer buffers potential table headers until a separator
// confirms them, preventing tables from flashing as raw pipe-delimited text
// during LLM streaming. Outputs markdown (not platform text).
//
// Not goroutine-safe: a single consumer owns each instance (matching upstream).
// The Slack adapter's stream loop (Task 27) owns synchronization.
type StreamingMarkdownRenderer struct {
	accumulated               string
	dirty                     bool
	cachedRender              string
	finished                  bool
	fenceToggles              int
	incompleteLine            string
	disableTableWrapForAppend bool
}

func NewStreamingMarkdownRenderer(opts StreamingMarkdownOptions) *StreamingMarkdownRenderer {
	return &StreamingMarkdownRenderer{
		dirty:                     true,
		disableTableWrapForAppend: opts.DisableTableWrapForAppend,
	}
}

// Push appends a chunk from the LLM stream.
func (r *StreamingMarkdownRenderer) Push(chunk string) {
	r.accumulated += chunk
	r.dirty = true

	r.incompleteLine += chunk
	parts := strings.Split(r.incompleteLine, "\n")
	r.incompleteLine = parts[len(parts)-1]
	for _, line := range parts[:len(parts)-1] {
		trimmed := trimStart(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			r.fenceToggles++
		}
	}
}

func (r *StreamingMarkdownRenderer) isAccumulatedInsideFence() bool {
	inside := r.fenceToggles%2 == 1
	trimmed := trimStart(r.incompleteLine)
	if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
		inside = !inside
	}
	return inside
}

// Render returns renderable markdown for an intermediate edit.
// Holds back trailing table-header lines until a separator confirms or the
// next line denies. Applies remend.Mend to close incomplete inline markers.
// Idempotent: returns the cached result if no Push since the last call.
func (r *StreamingMarkdownRenderer) Render() string {
	if !r.dirty {
		return r.cachedRender
	}

	r.dirty = false

	if r.finished {
		r.cachedRender = remend.Mend(r.accumulated)
		return r.cachedRender
	}

	if r.isAccumulatedInsideFence() {
		r.cachedRender = remend.Mend(r.accumulated)
		return r.cachedRender
	}

	committable := getCommittablePrefix(r.accumulated)
	r.cachedRender = remend.Mend(committable)
	return r.cachedRender
}

// CommittableText returns text safe for append-only streaming.
//
// Holds back unconfirmed table headers until a separator arrives. Optionally
// wraps confirmed tables in code fences (fence left OPEN while the table
// streams). Holds back unclosed inline markers (**, *, ~~, `, [).
func (r *StreamingMarkdownRenderer) CommittableText() string {
	if r.finished {
		return r.formatAppendOnlyText(r.accumulated, true)
	}

	text := r.accumulated
	if len(text) > 0 && !strings.HasSuffix(text, "\n") {
		lastNewline := strings.LastIndex(text, "\n")
		withoutIncompleteLine := ""
		if lastNewline >= 0 {
			withoutIncompleteLine = text[:lastNewline+1]
		}

		if isInsideCodeFence(withoutIncompleteLine) {
			return r.formatAppendOnlyText(text, false)
		}

		text = withoutIncompleteLine
	}

	if isInsideCodeFence(text) {
		return r.formatAppendOnlyText(text, false)
	}

	committed := getCommittablePrefix(text)
	wrapped := r.formatAppendOnlyText(committed, false)

	if isInsideCodeFence(wrapped) {
		return wrapped
	}

	return findCleanPrefix(wrapped)
}

// Text returns raw accumulated text (no remend, no buffering). For the final edit.
func (r *StreamingMarkdownRenderer) Text() string {
	return r.accumulated
}

// Finish signals stream end. Flushes held-back lines. Returns the final render.
func (r *StreamingMarkdownRenderer) Finish() string {
	r.finished = true
	r.dirty = true
	return r.Render()
}

func (r *StreamingMarkdownRenderer) formatAppendOnlyText(text string, closeFences bool) string {
	if r.disableTableWrapForAppend {
		return text
	}
	return wrapTablesForAppend(text, closeFences)
}

func isInlineMarker(c byte) bool {
	switch c {
	case '*', '~', '`', '[':
		return true
	default:
		return false
	}
}

func isClean(text string) bool {
	return len(remend.Mend(text)) <= len(text)
}

func findCleanPrefix(text string) string {
	if len(text) == 0 || isClean(text) {
		return text
	}

	for i := len(text) - 1; i >= 0; i-- {
		if isInlineMarker(text[i]) {
			for i > 0 && text[i-1] == text[i] {
				i--
			}
			candidate := text[:i]
			if isClean(candidate) {
				return candidate
			}
		}
	}

	return ""
}

var (
	tableRowRe       = regexp.MustCompile(`^\|.*\|$`)
	tableSeparatorRe = regexp.MustCompile(`^\|[\s:]*-{1,}[\s:]*(\|[\s:]*-{1,}[\s:]*)*\|$`)
)

func trimStart(s string) string {
	return strings.TrimLeftFunc(s, unicode.IsSpace)
}

func isInsideCodeFence(text string) bool {
	inside := false
	for line := range strings.SplitSeq(text, "\n") {
		trimmed := trimStart(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inside = !inside
		}
	}
	return inside
}

func getCommittablePrefix(text string) string {
	endsWithNewline := strings.HasSuffix(text, "\n")
	lines := strings.Split(text, "\n")

	if !endsWithNewline && len(lines) > 0 {
		lines = lines[:len(lines)-1]
	}

	if endsWithNewline && len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	heldCount := 0
	separatorFound := false

	for _, line := range slices.Backward(lines) {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			break
		}

		if tableSeparatorRe.MatchString(trimmed) {
			separatorFound = true
			break
		}

		if tableRowRe.MatchString(trimmed) {
			heldCount++
		} else {
			break
		}
	}

	if separatorFound || heldCount == 0 {
		return text
	}

	commitLineCount := len(lines) - heldCount
	committedLines := lines[:commitLineCount]

	result := strings.Join(committedLines, "\n")
	if len(committedLines) > 0 {
		result += "\n"
	}

	return result
}

func wrapTablesForAppend(text string, closeFences bool) string {
	hadTrailingNewline := strings.HasSuffix(text, "\n")
	lines := strings.Split(text, "\n")

	if hadTrailingNewline && len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	result := make([]string, 0, len(lines)+2)
	inTable := false
	inUserCodeFence := false

	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])

		if !inTable && (strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")) {
			inUserCodeFence = !inUserCodeFence
			result = append(result, lines[i])
			continue
		}

		if inUserCodeFence {
			result = append(result, lines[i])
			continue
		}

		isTableLine := trimmed != "" &&
			(tableRowRe.MatchString(trimmed) || tableSeparatorRe.MatchString(trimmed))

		if isTableLine && !inTable {
			hasSeparator := false
			for j := i; j < len(lines); j++ {
				t := strings.TrimSpace(lines[j])
				if tableSeparatorRe.MatchString(t) {
					hasSeparator = true
					break
				}
				if t == "" || !tableRowRe.MatchString(t) {
					break
				}
			}
			if hasSeparator {
				result = append(result, "```")
				inTable = true
			}
		} else if !isTableLine && inTable {
			result = append(result, "```")
			inTable = false
		}

		result = append(result, lines[i])
	}

	if inTable && closeFences {
		result = append(result, "```")
	}

	output := strings.Join(result, "\n")
	if hadTrailingNewline {
		output += "\n"
	}
	return output
}
