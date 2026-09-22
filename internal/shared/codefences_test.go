package shared

import (
	"strings"
	"testing"

	"github.com/shoenig/test/must"
)

func TestNormalizeCodeFences(t *testing.T) {
	t.Parallel()

	t.Run("puts fences on their own lines so the first code line survives", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "```\nfirst line\nsecond line\n```", NormalizeCodeFences("```first line\nsecond line```", NormalizeCodeFencesOptions{}))
	})

	t.Run("separates fences from surrounding text", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "before \n```\ncode\n```\n after", NormalizeCodeFences("before ```code``` after", NormalizeCodeFencesOptions{}))
	})

	t.Run("returns text without fences unchanged", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "plain text", NormalizeCodeFences("plain text", NormalizeCodeFencesOptions{}))
	})

	t.Run("keeps an unpaired ``` as literal text", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "use ``` to fence code", NormalizeCodeFences("use ``` to fence code", NormalizeCodeFencesOptions{}))
	})

	t.Run("keeps a ``` inside an inline code span as literal text", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "`use ``` here`", NormalizeCodeFences("`use ``` here`", NormalizeCodeFencesOptions{}))
	})

	t.Run("keeps a ``` on a blockquote line as literal text", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "> a ```c``` b", NormalizeCodeFences("> a ```c``` b", NormalizeCodeFencesOptions{}))
	})

	t.Run("escapes trailing text that would become a block construct", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "```\nx\n```\n \\> note", NormalizeCodeFences("```x``` > note", NormalizeCodeFencesOptions{}))
		must.Eq(t, "```\nx\n```\n \\# heading", NormalizeCodeFences("```x``` # heading", NormalizeCodeFencesOptions{}))
		must.Eq(t, "```\nx\n```\n \\- item", NormalizeCodeFences("```x``` - item", NormalizeCodeFencesOptions{}))
		must.Eq(t, "```\nx\n```\n 1\\. item", NormalizeCodeFences("```x``` 1. item", NormalizeCodeFencesOptions{}))
		must.Eq(t, "```\na\n```\n \\``` b", NormalizeCodeFences("```a``` ``` b", NormalizeCodeFencesOptions{}))
	})

	t.Run("collapses trailing indentation that would become indented code", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "see \n```\nx\n```\n result is 5", NormalizeCodeFences("see ```x```\tresult is 5", NormalizeCodeFencesOptions{}))
		must.Eq(t, "see \n```\nx\n```\n result is 5", NormalizeCodeFences("see ```x```     result is 5", NormalizeCodeFencesOptions{}))
	})

	t.Run("applies convertText to text segments only", func(t *testing.T) {
		t.Parallel()
		got := NormalizeCodeFences("*a* ```*b*``` *c*", NormalizeCodeFencesOptions{
			ConvertText: func(text string) string {
				return strings.ReplaceAll(text, "*", "**")
			},
		})
		must.Eq(t, "**a** \n```\n*b*\n```\n **c**", got)
	})

	t.Run("applies convertText on the fenceless fast path", func(t *testing.T) {
		t.Parallel()
		got := NormalizeCodeFences("*a*", NormalizeCodeFencesOptions{
			ConvertText: func(text string) string {
				return strings.ReplaceAll(text, "*", "**")
			},
		})
		must.Eq(t, "**a**", got)
	})

	t.Run("applies convertCode to fence content", func(t *testing.T) {
		t.Parallel()
		got := NormalizeCodeFences("```<@U1>```", NormalizeCodeFencesOptions{
			ConvertCode: func(code string) string {
				return strings.ReplaceAll(code, "<@U1>", "@jane")
			},
		})
		must.Eq(t, "```\n@jane\n```", got)
	})
}
