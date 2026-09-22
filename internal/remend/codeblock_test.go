package remend

import (
	"strings"
	"testing"

	"github.com/shoenig/test/must"
)

// Reference implementation: the previous per-call scan, kept verbatim so the
// lookup-based rewrite can be checked against it position by position.
func referenceIsInsideCodeBlock(text string, position int) bool {
	inInlineCode := false
	inMultilineCode := false
	for i := 0; i < position && i < len(text); i++ {
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
			inInlineCode = !inInlineCode
		}
	}
	return inInlineCode || inMultilineCode
}

func parityMismatches(text string) []int {
	var mismatches []int
	for p := 0; p <= len(text)+1; p++ {
		if isInsideCodeBlock(text, p) != referenceIsInsideCodeBlock(text, p) {
			mismatches = append(mismatches, p)
		}
	}
	return mismatches
}

func TestIsInsideCodeBlock(t *testing.T) {
	t.Parallel()

	t.Run("reports positions inside a fenced code block", func(t *testing.T) {
		t.Parallel()
		text := "before ```js\nconst x = arr[0];\n``` after"
		must.True(t, isInsideCodeBlock(text, strings.Index(text, "arr")))
		must.False(t, isInsideCodeBlock(text, strings.Index(text, "before")))
		must.False(t, isInsideCodeBlock(text, strings.Index(text, "after")))
	})

	t.Run("reports positions inside inline code", func(t *testing.T) {
		t.Parallel()
		text := "use `map[key]` here"
		must.True(t, isInsideCodeBlock(text, strings.Index(text, "key")))
		must.False(t, isInsideCodeBlock(text, strings.Index(text, "here")))
	})

	t.Run("ignores escaped backticks", func(t *testing.T) {
		t.Parallel()
		text := "not code \\` still [not] code"
		must.False(t, isInsideCodeBlock(text, strings.Index(text, "[not]")))
	})

	t.Run("treats an unclosed fence as extending to the end", func(t *testing.T) {
		t.Parallel()
		text := "```python\nvalues[0] = 1"
		must.True(t, isInsideCodeBlock(text, strings.Index(text, "values")))
		must.True(t, isInsideCodeBlock(text, len(text)))
	})

	t.Run("matches the per-call scan at every position on mixed input", func(t *testing.T) {
		t.Parallel()
		cases := []string{
			"a `b` c ```\nd [e] `f`\n``` g \\` h ``` i",
			"``````",
			"\\`",
			"`unclosed inline [x]",
			"text \\``real` code",
		}
		for _, text := range cases {
			must.Eq(t, []int(nil), parityMismatches(text))
		}
	})

	t.Run("stays correct when queried texts alternate", func(t *testing.T) {
		t.Parallel()
		inCode := "```\n[x]"
		inProse := "plain [x]"
		for range 3 {
			must.True(t, isInsideCodeBlock(inCode, strings.Index(inCode, "[x]")))
			must.False(t, isInsideCodeBlock(inProse, strings.Index(inProse, "[x]")))
		}
	})
}
