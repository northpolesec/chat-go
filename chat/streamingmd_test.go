package chat

import (
	"regexp"
	"strings"
	"testing"

	"github.com/northpolesec/chat-go/internal/remend"
	"github.com/shoenig/test/must"
)

var (
	codeFenceSplitRe = regexp.MustCompile("```|~~~")
	tablePipeRe      = regexp.MustCompile(`^\|.*\|$`)
)

func TestStreamingMarkdownRenderer(t *testing.T) {
	t.Parallel()

	t.Run("should accumulate basic text", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Hello")
		r.Push(" World")
		must.Eq(t, "Hello World", r.Render())
	})

	t.Run("should heal inline markers with remend", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Hello **wor")
		result := r.Render()
		openCount := strings.Count(result, "**")
		must.Eq(t, 0, openCount%2)
		must.StrContains(t, result, "Hello **wor")
	})

	t.Run("should hold back trailing table header lines", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Text\n\n| A | B |\n")
		result := r.Render()
		must.StrNotContains(t, result, "| A | B |")
		must.StrContains(t, result, "Text")
	})

	t.Run("should confirm table when separator arrives", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Text\n\n| A | B |\n")
		must.StrNotContains(t, r.Render(), "| A | B |")

		r.Push("|---|---|\n")
		result := r.Render()
		must.StrContains(t, result, "| A | B |")
		must.StrContains(t, result, "|---|---|")
	})

	t.Run("should release held lines when next line is not a table row", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Text\n\n| A | B |\n")
		must.StrNotContains(t, r.Render(), "| A | B |")

		r.Push("Not a table\n")
		result := r.Render()
		must.StrContains(t, result, "| A | B |")
		must.StrContains(t, result, "Not a table")
	})

	t.Run("should not hold back pipe lines inside code fences", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("```\n| A |\n")
		result := r.Render()
		must.StrContains(t, result, "| A |")
	})

	t.Run("should flush held lines on finish()", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Text\n\n| A | B |\n")
		must.StrNotContains(t, r.Render(), "| A | B |")

		final := r.Finish()
		must.StrContains(t, final, "| A | B |")
	})

	t.Run("should be idempotent when no push between renders", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Hello **wor")
		first := r.Render()
		second := r.Render()
		must.Eq(t, first, second)
	})

	t.Run("should return raw text from getText() without remend", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Hello **wor")
		r.Render()
		must.Eq(t, "Hello **wor", r.Text())
	})

	t.Run("should handle table with data rows after separator", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("| A | B |\n|---|---|\n| 1 | 2 |\n")
		result := r.Render()
		must.StrContains(t, result, "| A | B |")
		must.StrContains(t, result, "|---|---|")
		must.StrContains(t, result, "| 1 | 2 |")
	})

	t.Run("should handle multiple consecutive table rows held back", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Intro\n\n| A | B |\n| C | D |\n")
		result := r.Render()
		must.StrNotContains(t, result, "| A | B |")
		must.StrNotContains(t, result, "| C | D |")
	})

	t.Run("should not buffer lines that don't match table pattern", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Just normal text\n")
		must.StrContains(t, r.Render(), "Just normal text")
	})

	t.Run("should handle code fence with tilde syntax", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("~~~\n| A |\n")
		result := r.Render()
		must.StrContains(t, result, "| A |")
	})

	t.Run("should resume buffering after code fence closes", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("```\n| inside |\n```\n| A | B |\n")
		result := r.Render()
		must.StrContains(t, result, "| inside |")
		must.StrNotContains(t, result, "| A | B |")
	})

	t.Run("should handle empty input", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		must.Eq(t, "", r.Render())
		must.Eq(t, "", r.Text())
		must.Eq(t, "", r.Finish())
	})

	t.Run("should handle text with no trailing newline", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Hello world")
		must.Eq(t, "Hello world", r.Render())
	})

	t.Run("should handle table header without trailing newline (incomplete line)", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Text\n\n| A | B |")
		result := r.Render()
		must.StrContains(t, result, "Text")
	})

	t.Run("should still work after push() following finish()", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Hello")
		r.Finish()
		r.Push(" World")
		result := r.Render()
		must.StrContains(t, result, "Hello World")
	})

	t.Run("should be idempotent for render() after finish()", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Text\n\n| A | B |\n")
		r.Finish()
		first := r.Render()
		second := r.Render()
		must.Eq(t, first, second)
		must.StrContains(t, first, "| A | B |")
	})

	t.Run("should handle finish() with no held lines", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Just plain text\n")
		rendered := r.Render()
		finished := r.Finish()
		must.StrContains(t, rendered, "Just plain text")
		must.StrContains(t, finished, "Just plain text")
	})

	t.Run("should handle table header split across chunks", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Text\n\n| A")
		must.StrContains(t, r.Render(), "Text")

		r.Push(" | B |\n")
		must.StrNotContains(t, r.Render(), "| A | B |")

		r.Push("|---|---|\n")
		must.StrContains(t, r.Render(), "| A | B |")
	})

	t.Run("should break held block at empty line", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("| A | B |\n\n| C | D |\n")
		result := r.Render()
		must.StrContains(t, result, "| A | B |")
		must.StrNotContains(t, result, "| C | D |")
	})

	t.Run("should hold table at very start of text (no preceding content)", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("| A | B |\n")
		result := r.Render()
		must.StrNotContains(t, result, "| A | B |")
	})

	t.Run("should hold second table after confirmed first table", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("| A | B |\n|---|---|\n| 1 | 2 |\n")
		must.StrContains(t, r.Render(), "|---|---|")

		r.Push("\n| X | Y |\n")
		result := r.Render()
		must.StrContains(t, result, "| A | B |")
		must.StrContains(t, result, "| 1 | 2 |")
		must.StrNotContains(t, result, "| X | Y |")
	})

	t.Run("should handle held → released → new hold sequence", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})

		r.Push("| A | B |\n")
		must.StrNotContains(t, r.Render(), "| A | B |")

		r.Push("Normal text\n")
		must.StrContains(t, r.Render(), "| A | B |")
		must.StrContains(t, r.Render(), "Normal text")

		r.Push("| X | Y |\n")
		result := r.Render()
		must.StrContains(t, result, "| A | B |")
		must.StrContains(t, result, "Normal text")
		must.StrNotContains(t, result, "| X | Y |")
	})

	t.Run("should confirm table with alignment markers in separator", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("| Left | Center | Right |\n")
		must.StrNotContains(t, r.Render(), "| Left |")

		r.Push("|:---|:---:|---:|\n")
		result := r.Render()
		must.StrContains(t, result, "| Left | Center | Right |")
		must.StrContains(t, result, "|:---|:---:|---:|")
	})

	t.Run("should not hold data rows after confirmed separator", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("| A | B |\n|---|---|\n")
		must.StrContains(t, r.Render(), "|---|---|")

		r.Push("| 1 | 2 |\n")
		result := r.Render()
		must.StrContains(t, result, "| 1 | 2 |")
	})

	t.Run("should handle multiple push() calls before single render()", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("| A ")
		r.Push("| B |\n")
		r.Push("|---|---|\n")
		r.Push("| 1 | 2 |\n")
		result := r.Render()
		must.StrContains(t, result, "| A | B |")
		must.StrContains(t, result, "|---|---|")
		must.StrContains(t, result, "| 1 | 2 |")
	})

	t.Run("should render real-world table with single-dash separators progressively", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})

		r.Push("Here's a table with 20 rows of sample data:\n\n")
		must.StrContains(t, r.Render(), "Here's a table")

		r.Push("| ID | Name | Department | Age | Salary | City | Join Date | Status |\n")
		result := r.Render()
		must.StrNotContains(t, result, "| ID |")
		must.StrContains(t, result, "Here's a table")

		r.Push("| - | - | - | - | - | - | - | - |\n")
		result = r.Render()
		must.StrContains(t, result, "| ID |")
		must.StrContains(t, result, "| - |")

		r.Push("| 1 | Sarah Johnson | Engineering | 32 | $95,000 | Seattle | 2019-03-15 | Active |\n")
		result = r.Render()
		must.StrContains(t, result, "Sarah Johnson")

		r.Push("| 2 | Michael")
		result = r.Render()
		must.StrContains(t, result, "Sarah Johnson")

		r.Push(" Chen | Marketing | 28 | $72,000 | Austin | 2020-07-22 | Active |\n")
		result = r.Render()
		must.StrContains(t, result, "Michael Chen")
	})

	t.Run("getCommittableText should hold back incomplete line with unclosed bold", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Hello **wor")
		must.Eq(t, "", r.CommittableText())
	})

	t.Run("getCommittableText should hold back unclosed bold on complete line", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Hello **wor\n")
		committable := r.CommittableText()
		must.Eq(t, "Hello ", committable)
		must.StrNotContains(t, committable, "**")
	})

	t.Run("getCommittableText should release when bold closes", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Hello **wor")
		must.Eq(t, "", r.CommittableText())

		r.Push("ld** done\n")
		must.Eq(t, "Hello **world** done\n", r.CommittableText())
	})

	t.Run("getCommittableText should hold back unclosed italic", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Hello *ita\n")
		must.Eq(t, "Hello ", r.CommittableText())
	})

	t.Run("getCommittableText should hold back unclosed strikethrough", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Hello ~~str\n")
		must.Eq(t, "Hello ", r.CommittableText())
	})

	t.Run("getCommittableText should hold back unclosed inline code", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Hello `cod\n")
		must.Eq(t, "Hello ", r.CommittableText())
	})

	t.Run("getCommittableText should hold back unclosed link", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("See [link text\n")
		must.Eq(t, "See ", r.CommittableText())
	})

	t.Run("getCommittableText should release when link closes", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("See [link text\n")
		must.Eq(t, "See ", r.CommittableText())

		r.Push("](https://example.com)\n")
		committable := r.CommittableText()
		must.StrContains(t, committable, "See ")
	})

	t.Run("getCommittableText should return clean text when all markers balanced", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Hello **world** and *italic* done\n")
		must.Eq(t, "Hello **world** and *italic* done\n", r.CommittableText())
	})

	t.Run("getCommittableText should hold back table rows", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Text\n\n| A | B |\n")
		committable := r.CommittableText()
		must.StrNotContains(t, committable, "| A | B |")
		must.StrContains(t, committable, "Text")
	})

	t.Run("getCommittableText should wrap confirmed table in code fence", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Text\n\n| A | B |\n|---|---|\n| 1 | 2 |\n")
		committable := r.CommittableText()
		must.StrContains(t, committable, "```")
		must.StrContains(t, committable, "| A | B |")
		must.StrContains(t, committable, "| 1 | 2 |")
		must.StrContains(t, committable, "Text")
	})

	t.Run("getCommittableText should not buffer inside code fence", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("```\n| A |\n")
		must.StrContains(t, r.CommittableText(), "| A |")
	})

	t.Run("getCommittableText should return full text after finish", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Text\n\n| A | B |\n")
		must.StrNotContains(t, r.CommittableText(), "| A | B |")
		r.Finish()
		must.StrContains(t, r.CommittableText(), "| A | B |")
	})

	t.Run("getCommittableText should flush unclosed markers after finish", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Hello **wor\n")
		must.Eq(t, "Hello ", r.CommittableText())
		r.Finish()
		must.Eq(t, "Hello **wor\n", r.CommittableText())
	})

	t.Run("getCommittableText delta should stream table in code fence", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		lastAppended := ""

		r.Push("Hello\n\n")
		committable := r.CommittableText()
		delta := committable[len(lastAppended):]
		must.Eq(t, "Hello\n\n", delta)
		lastAppended = committable

		r.Push("| A | B |\n")
		committable = r.CommittableText()
		delta = committable[len(lastAppended):]
		must.Eq(t, "", delta)

		r.Push("|---|---|\n")
		committable = r.CommittableText()
		delta = committable[len(lastAppended):]
		must.StrContains(t, delta, "```")
		must.StrContains(t, delta, "| A | B |")
		must.StrContains(t, delta, "|---|---|")
		lastAppended = committable

		r.Push("| 1 | 2 |\n")
		committable = r.CommittableText()
		delta = committable[len(lastAppended):]
		must.StrContains(t, delta, "| 1 | 2 |")
		must.StrNotContains(t, delta, "```")
		lastAppended = committable

		r.Push("\nMore text\n")
		committable = r.CommittableText()
		delta = committable[len(lastAppended):]
		must.StrContains(t, delta, "```")
		must.StrContains(t, delta, "More text")
	})

	t.Run("getCommittableText delta should work for inline markers in append-only streaming", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		lastAppended := ""

		r.Push("Hello ")
		must.Eq(t, "", r.CommittableText())

		r.Push("**world** done\n")
		committable := r.CommittableText()
		delta := committable[len(lastAppended):]
		must.Eq(t, "Hello **world** done\n", delta)
		lastAppended = committable

		r.Push("More **text")
		committable = r.CommittableText()
		delta = committable[len(lastAppended):]
		must.Eq(t, "", delta)

		r.Push("** end\n")
		committable = r.CommittableText()
		delta = committable[len(lastAppended):]
		must.Eq(t, "More **text** end\n", delta)
	})

	t.Run("append-only: plain text streams without modification", func(t *testing.T) {
		t.Parallel()
		got := simulateAppendStream([]string{"Hello ", "World", "!\n"}, StreamingMarkdownOptions{})
		must.Eq(t, "Hello World!\n", got.appendedText)
	})

	t.Run("append-only: bold markers are held then released", func(t *testing.T) {
		t.Parallel()
		got := simulateAppendStream([]string{"Hello ", "**bold", "** text\n"}, StreamingMarkdownOptions{})
		must.StrContains(t, got.appendedText, "**bold**")
		must.StrContains(t, got.appendedText, "Hello ")
	})

	t.Run("append-only: table is wrapped in code fence", func(t *testing.T) {
		t.Parallel()
		got := simulateAppendStream([]string{
			"Intro\n\n",
			"| A | B |\n",
			"|---|---|\n",
			"| 1 | 2 |\n",
			"| 3 | 4 |\n",
			"\nAfter table\n",
		}, StreamingMarkdownOptions{})
		must.StrContains(t, got.appendedText, "```\n| A | B |")
		must.StrContains(t, got.appendedText, "| 1 | 2 |")
		must.StrContains(t, got.appendedText, "| 3 | 4 |")
		must.StrContains(t, got.appendedText, "```\n\nAfter table")
		must.True(t, strings.Index(got.appendedText, "Intro") < strings.Index(got.appendedText, "```"))
	})

	t.Run("append-only: table can stream without code fence when wrapping is disabled", func(t *testing.T) {
		t.Parallel()
		got := simulateAppendStream([]string{
			"Intro\n\n",
			"| A | B |\n",
			"|---|---|\n",
			"| 1 | 2 |\n",
			"| 3 | 4 |\n",
			"\nAfter table\n",
		}, StreamingMarkdownOptions{DisableTableWrapForAppend: true})
		must.StrContains(t, got.appendedText, "| A | B |")
		must.StrContains(t, got.appendedText, "| 1 | 2 |")
		must.StrContains(t, got.appendedText, "| 3 | 4 |")
		must.StrContains(t, got.appendedText, "After table")
		must.StrNotContains(t, got.appendedText, "```")
	})

	t.Run("append-only: table at end of stream is flushed on finish", func(t *testing.T) {
		t.Parallel()
		got := simulateAppendStream([]string{
			"Text\n\n",
			"| A | B |\n",
			"|---|---|\n",
			"| 1 | 2 |\n",
		}, StreamingMarkdownOptions{})
		must.StrContains(t, got.appendedText, "| A | B |")
		must.StrContains(t, got.appendedText, "```")
		must.True(t, len(got.deltas) > 0 && got.deltas[len(got.deltas)-1] != "")
	})

	t.Run("append-only: concatenated deltas equal getCommittableText after finish", func(t *testing.T) {
		t.Parallel()
		got := simulateAppendStream([]string{
			"Hello **world**\n",
			"\n",
			"| H1 | H2 |\n",
			"| - | - |\n",
			"| a | b |\n",
			"| c | d |\n",
			"\nDone\n",
		}, StreamingMarkdownOptions{})
		must.StrContains(t, got.appendedText, "Hello **world**")
		must.StrContains(t, got.appendedText, "| H1 | H2 |")
		must.StrContains(t, got.appendedText, "| a | b |")
		must.StrContains(t, got.appendedText, "| c | d |")
		must.StrContains(t, got.appendedText, "Done")
		must.StrContains(t, got.appendedText, "```")
	})

	t.Run("append-only: concatenated deltas equal final text when table wrapping is disabled", func(t *testing.T) {
		t.Parallel()
		got := simulateAppendStream([]string{
			"Hello **world**\n",
			"\n",
			"| H1 | H2 |\n",
			"| - | - |\n",
			"| a | b |\n",
			"| c | d |\n",
			"\nDone\n",
		}, StreamingMarkdownOptions{DisableTableWrapForAppend: true})
		must.StrContains(t, got.appendedText, "Hello **world**")
		must.StrContains(t, got.appendedText, "| H1 | H2 |")
		must.StrContains(t, got.appendedText, "| a | b |")
		must.StrContains(t, got.appendedText, "| c | d |")
		must.StrContains(t, got.appendedText, "Done")
		must.StrNotContains(t, got.appendedText, "```")
	})

	t.Run("append-only: concatenated deltas are monotonic (each is a suffix)", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		lastAppended := ""
		var deltas []string
		chunks := []string{
			"Hello **world**\n",
			"\n",
			"| A | B |\n",
			"| - | - |\n",
			"| 1 | 2 |\n",
			"\nDone\n",
		}

		for _, chunk := range chunks {
			r.Push(chunk)
			committable := r.CommittableText()
			must.True(t, strings.HasPrefix(committable, lastAppended))
			delta := committable[len(lastAppended):]
			if len(delta) > 0 {
				deltas = append(deltas, delta)
				lastAppended = committable
			}
		}

		r.Finish()
		finalCommittable := r.CommittableText()
		must.True(t, strings.HasPrefix(finalCommittable, lastAppended))
		finalDelta := finalCommittable[len(lastAppended):]
		if len(finalDelta) > 0 {
			deltas = append(deltas, finalDelta)
		}

		must.Eq(t, finalCommittable, strings.Join(deltas, ""))
	})

	t.Run("append-only: final flush uses transformed text not raw text", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		lastAppended := ""

		for _, chunk := range []string{
			"Intro\n\n",
			"| ID | Name |\n",
			"|---|---|\n",
			"| 1 | Alice |\n",
		} {
			r.Push(chunk)
			committable := r.CommittableText()
			delta := committable[len(lastAppended):]
			if len(delta) > 0 {
				lastAppended = committable
			}
		}

		r.Finish()
		raw := r.Text()
		transformed := r.CommittableText()

		must.StrContains(t, transformed, "```")
		must.Greater(t, len(raw), len(transformed))

		correctDelta := transformed[len(lastAppended):]
		must.Eq(t, transformed, lastAppended+correctDelta)

		buggyDelta := ""
		if len(lastAppended) < len(raw) {
			buggyDelta = raw[len(lastAppended):]
		}
		must.False(t, lastAppended+buggyDelta == transformed)
	})

	t.Run("append-only: real-world 20-row table streams correctly", func(t *testing.T) {
		t.Parallel()
		header := "| ID | Name | Department | Age | Salary | City | Join Date |\n"
		sep := "| - | - | - | - | - | - | - |\n"
		rows := []string{
			"| 1 | Alice Johnson | Engineering | 28 | $75,000 | New York | 2021-03-15 |\n",
			"| 2 | Bob Smith | Marketing | 35 | $68,000 | Los Angeles | 2019-07-22 |\n",
			"| 3 | Carol Davis | Finance | 31 | $82,000 | Chicago | 2021-01-10 |\n",
		}

		chunks := append([]string{"Here's a table:\n\n", header, sep}, rows...)
		got := simulateAppendStream(chunks, StreamingMarkdownOptions{})

		must.StrContains(t, got.appendedText, "Alice Johnson")
		must.StrContains(t, got.appendedText, "Bob Smith")
		must.StrContains(t, got.appendedText, "Carol Davis")
		must.StrContains(t, got.appendedText, "```")
		must.StrContains(t, got.appendedText, "Join Date")
		must.StrNotContains(t, got.appendedText, "JoinJoin")
		must.StrContains(t, got.finalText, "Alice Johnson")
		must.StrContains(t, got.finalText, "| 3 |")
	})

	t.Run("append-only: table rows split mid-token stream correctly", func(t *testing.T) {
		t.Parallel()
		got := simulateAppendStream([]string{
			"Text\n\n",
			"| A",
			" | B |\n",
			"|---|",
			"---|\n",
			"| 1 | ",
			"2 |\n",
		}, StreamingMarkdownOptions{})
		must.StrContains(t, got.appendedText, "```")
		must.StrContains(t, got.appendedText, "| A | B |")
		must.StrContains(t, got.appendedText, "| 1 | 2 |")
		beforeFence := got.appendedText[:strings.Index(got.appendedText, "```")]
		must.StrNotContains(t, beforeFence, "| A")
	})

	t.Run("append-only: multiple tables in sequence", func(t *testing.T) {
		t.Parallel()
		got := simulateAppendStream([]string{
			"First table:\n\n",
			"| A |\n",
			"|---|\n",
			"| 1 |\n",
			"\nSecond table:\n\n",
			"| X |\n",
			"|---|\n",
			"| 9 |\n",
			"\nDone\n",
		}, StreamingMarkdownOptions{})
		must.Eq(t, 4, strings.Count(got.appendedText, "```"))
		must.StrContains(t, got.appendedText, "| 1 |")
		must.StrContains(t, got.appendedText, "| 9 |")
	})

	t.Run("should track dirty flag correctly across push-render-push-render", func(t *testing.T) {
		t.Parallel()
		r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
		r.Push("Hello")
		r1 := r.Render()
		must.Eq(t, "Hello", r1)

		must.Eq(t, r1, r.Render())

		r.Push(" **bold")
		r2 := r.Render()
		must.False(t, r2 == r1)
		must.StrContains(t, r2, "Hello **bold")
		count := strings.Count(r2, "**")
		must.Eq(t, 0, count%2)
	})

	t.Run("exhaustive prefix invariants", func(t *testing.T) {
		t.Parallel()
		const complexMarkdown = "# Heading\n" +
			"\n" +
			"Some **bold** and *italic* text with `inline code` here.\n" +
			"\n" +
			"A [link](https://example.com) and ~~deleted~~ stuff.\n" +
			"\n" +
			"## Table section\n" +
			"\n" +
			"| Name | Age | City |\n" +
			"| - | - | - |\n" +
			"| Alice | 30 | NYC |\n" +
			"| Bob | 25 | LA |\n" +
			"\n" +
			"Text after table with **bold again**.\n" +
			"\n" +
			"```\n" +
			"code block with | pipes | inside\n" +
			"and **markers** that are literal\n" +
			"```\n" +
			"\n" +
			"Final paragraph.\n"

		t.Run("render() output is always valid markdown (remend is idempotent)", func(t *testing.T) {
			t.Parallel()
			r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
			for i := range len(complexMarkdown) {
				r.Push(complexMarkdown[i : i+1])
				rendered := r.Render()
				doubleRemended := remend.Mend(rendered)
				if len(doubleRemended) > len(rendered) {
					prefix := complexMarkdown[:i+1]
					if len(prefix) > 20 {
						prefix = prefix[len(prefix)-20:]
					}
					t.Fatalf("render() at position %d (%q) produced text that remend would still modify", i, prefix)
				}
			}
		})

		t.Run("getCommittableText() output is always monotonic (append-only safe)", func(t *testing.T) {
			t.Parallel()
			r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
			prev := ""
			for i := range len(complexMarkdown) {
				r.Push(complexMarkdown[i : i+1])
				committable := r.CommittableText()
				if !strings.HasPrefix(committable, prev) {
					diffAt := 0
					for diffAt < len(prev) && diffAt < len(committable) && prev[diffAt] == committable[diffAt] {
						diffAt++
					}
					start := max(0, i-20)
					prevStart := max(0, diffAt-10)
					prevEnd := min(len(prev), diffAt+20)
					nowEnd := min(len(committable), diffAt+20)
					t.Fatalf("Monotonicity broke at char %d (%q)\n  prefix: ...%q\n  prev[%d]: ...%q\n  now [%d]: ...%q\n  diverge at offset %d",
						i, complexMarkdown[i:i+1], complexMarkdown[start:i+1],
						len(prev), prev[prevStart:prevEnd],
						len(committable), committable[prevStart:nowEnd],
						diffAt)
				}
				prev = committable
			}
		})

		t.Run("getCommittableText() never contains raw table pipes outside code fences", func(t *testing.T) {
			t.Parallel()
			r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
			for i := range len(complexMarkdown) {
				r.Push(complexMarkdown[i : i+1])
				committable := r.CommittableText()

				sections := codeFenceSplitRe.Split(committable, -1)
				for s := 0; s < len(sections); s += 2 {
					outside := sections[s]
					for line := range strings.SplitSeq(outside, "\n") {
						trimmed := strings.TrimSpace(line)
						if trimmed == "" {
							continue
						}
						looksLikeTable := tablePipeRe.MatchString(trimmed) && strings.Count(trimmed, "|") >= 3
						if looksLikeTable {
							start := max(0, i-20)
							tail := committable
							if len(tail) > 80 {
								tail = tail[len(tail)-80:]
							}
							t.Fatalf("Table-like line outside code fence at char %d: %q\n  prefix: ...%q\n  committable: ...%q",
								i, trimmed, complexMarkdown[start:i+1], tail)
						}
					}
				}
			}
		})

		t.Run("getCommittableText() is always clean (remend would not add markers)", func(t *testing.T) {
			t.Parallel()
			r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
			for i := range len(complexMarkdown) {
				r.Push(complexMarkdown[i : i+1])
				committable := r.CommittableText()
				if committable == "" {
					continue
				}
				if isInsideCodeFence(committable) {
					continue
				}
				if len(remend.Mend(committable)) > len(committable) {
					prefix := complexMarkdown[:i+1]
					if len(prefix) > 20 {
						prefix = prefix[len(prefix)-20:]
					}
					tail := committable
					if len(tail) > 40 {
						tail = tail[len(tail)-40:]
					}
					t.Fatalf("getCommittableText() at position %d (%q) has unclosed markers: %q", i, prefix, tail)
				}
			}
		})

		t.Run("finish() always produces the full text", func(t *testing.T) {
			t.Parallel()
			cutPoints := []int{0, 10, 50, 100, 150, len(complexMarkdown)}
			for _, cut := range cutPoints {
				if cut > len(complexMarkdown) {
					continue
				}
				r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
				r.Push(complexMarkdown[:cut])
				r.Finish()
				finished := r.Text()
				must.Eq(t, complexMarkdown[:cut], finished)
			}
		})

		t.Run("append-only delta reconstruction works for character-by-character streaming", func(t *testing.T) {
			t.Parallel()
			r := NewStreamingMarkdownRenderer(StreamingMarkdownOptions{})
			lastAppended := ""
			var deltas []string

			for i := range len(complexMarkdown) {
				r.Push(complexMarkdown[i : i+1])
				committable := r.CommittableText()
				if !strings.HasPrefix(committable, lastAppended) {
					tailLast := lastAppended
					if len(tailLast) > 40 {
						tailLast = tailLast[len(tailLast)-40:]
					}
					tailNow := committable
					if len(tailNow) > 40 {
						tailNow = tailNow[len(tailNow)-40:]
					}
					t.Fatalf("Delta broke monotonicity at char %d (%q)\n  lastAppended[%d]: ...%q\n  committable [%d]: ...%q",
						i, complexMarkdown[i:i+1], len(lastAppended), tailLast, len(committable), tailNow)
				}
				delta := committable[len(lastAppended):]
				if len(delta) > 0 {
					deltas = append(deltas, delta)
					lastAppended = committable
				}
			}

			r.Finish()
			finalCommittable := r.CommittableText()
			if !strings.HasPrefix(finalCommittable, lastAppended) {
				tailLast := lastAppended
				if len(tailLast) > 40 {
					tailLast = tailLast[len(tailLast)-40:]
				}
				tailNow := finalCommittable
				if len(tailNow) > 40 {
					tailNow = tailNow[len(tailNow)-40:]
				}
				t.Fatalf("Final flush broke monotonicity\n  lastAppended[%d]: ...%q\n  final       [%d]: ...%q",
					len(lastAppended), tailLast, len(finalCommittable), tailNow)
			}
			finalDelta := finalCommittable[len(lastAppended):]
			if len(finalDelta) > 0 {
				deltas = append(deltas, finalDelta)
			}

			joined := strings.Join(deltas, "")
			if joined != finalCommittable {
				diffAt := 0
				for diffAt < len(joined) && diffAt < len(finalCommittable) && joined[diffAt] == finalCommittable[diffAt] {
					diffAt++
				}
				jStart := max(0, diffAt-10)
				jEnd := min(len(joined), diffAt+20)
				fEnd := min(len(finalCommittable), diffAt+20)
				t.Fatalf("Deltas (%d chars) != final (%d chars)\n  diverge at offset %d\n  deltas: ...%q\n  final:  ...%q",
					len(joined), len(finalCommittable), diffAt, joined[jStart:jEnd], finalCommittable[jStart:fEnd])
			}

			must.Eq(t, complexMarkdown, r.Text())
		})
	})
}

type appendStreamResult struct {
	appendedText string
	finalText    string
	deltas       []string
}

func simulateAppendStream(chunks []string, opts StreamingMarkdownOptions) appendStreamResult {
	r := NewStreamingMarkdownRenderer(opts)
	lastAppended := ""
	var deltas []string

	for _, chunk := range chunks {
		r.Push(chunk)
		committable := r.CommittableText()
		delta := committable[len(lastAppended):]
		if len(delta) > 0 {
			deltas = append(deltas, delta)
			lastAppended = committable
		}
	}

	r.Finish()
	finalCommittable := r.CommittableText()
	finalDelta := finalCommittable[len(lastAppended):]
	if len(finalDelta) > 0 {
		deltas = append(deltas, finalDelta)
	}

	return appendStreamResult{
		appendedText: strings.Join(deltas, ""),
		finalText:    r.Text(),
		deltas:       deltas,
	}
}
