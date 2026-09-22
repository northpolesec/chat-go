package remend

import (
	"strings"
	"testing"

	"github.com/shoenig/test/must"
)

func inlineKatexOpts() remendOptions {
	return remendOptions{inlineKatex: true}
}

func TestKaTeXBlockFormatting(t *testing.T) {
	t.Parallel()

	t.Run("should complete incomplete block KaTeX", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text with $$formula$$", Mend("Text with $$formula"))
		must.Eq(t, "$$incomplete$$", Mend("$$incomplete"))
	})

	t.Run("should keep complete block KaTeX unchanged", func(t *testing.T) {
		t.Parallel()
		text := "Text with $$E = mc^2$$"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle multiple block KaTeX sections", func(t *testing.T) {
		t.Parallel()
		text := "$$formula1$$ and $$formula2$$"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should complete odd number of block KaTeX markers", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "$$first$$ and $$second$$", Mend("$$first$$ and $$second"))
	})

	t.Run("should handle block KaTeX at start of text", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "$$x + y = z$$", Mend("$$x + y = z"))
	})

	t.Run("should complete partial closing $ without duplicating it", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "$$formula$$", Mend("$$formula$"))
		must.Eq(t, "$$x = y$$", Mend("$$x = y$"))
	})

	t.Run("should handle multiline block KaTeX", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "$$\nx = 1\ny = 2\n$$", Mend("$$\nx = 1\ny = 2"))
	})
}

func TestKaTeXInlineFormatting(t *testing.T) {
	t.Parallel()

	t.Run("should NOT complete single dollar signs (likely currency)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text with $formula", Mend("Text with $formula"))
		must.Eq(t, "$incomplete", Mend("$incomplete"))
	})

	t.Run("should keep text with paired dollar signs unchanged", func(t *testing.T) {
		t.Parallel()
		text := "Text with $x^2 + y^2 = z^2$"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle multiple inline KaTeX sections", func(t *testing.T) {
		t.Parallel()
		text := "$a = 1$ and $b = 2$"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should NOT complete odd number of dollar signs", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "$first$ and $second", Mend("$first$ and $second"))
	})

	t.Run("should not complete single $ but should complete block $$", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "$$block$$ and $inline", Mend("$$block$$ and $inline"))
	})

	t.Run("should NOT complete dollar sign at start of text", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "$x + y = z", Mend("$x + y = z"))
	})

	t.Run("should handle escaped dollar signs", func(t *testing.T) {
		t.Parallel()
		text := "Price is \\$100"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle multiple consecutive dollar signs correctly", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "$$$$$", Mend("$$$"))
		must.Eq(t, "$$$$", Mend("$$$$"))
	})

	t.Run("should handle mathematical expression chunks", func(t *testing.T) {
		t.Parallel()
		chunks := []string{
			"The formula",
			"The formula $E",
			"The formula $E = mc",
			"The formula $E = mc^2",
			"The formula $E = mc^2$ shows",
		}
		must.Eq(t, chunks[0], Mend(chunks[0]))
		must.Eq(t, "The formula $E", Mend(chunks[1]))
		must.Eq(t, "The formula $E = mc", Mend(chunks[2]))
		must.Eq(t, "The formula $E = mc^2", Mend(chunks[3]))
		must.Eq(t, chunks[4], Mend(chunks[4]))
	})
}

func TestKaTeXInlineFormattingOptIn(t *testing.T) {
	t.Parallel()

	t.Run("should complete incomplete inline math", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text with $formula$", remend("Text with $formula", inlineKatexOpts()))
		must.Eq(t, "$incomplete$", remend("$incomplete", inlineKatexOpts()))
	})

	t.Run("should keep already-complete inline math unchanged", func(t *testing.T) {
		t.Parallel()
		text := "Text with $x^2 + y^2 = z^2$"
		must.Eq(t, text, remend(text, inlineKatexOpts()))
	})

	t.Run("should complete the third unpaired dollar sign", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "$first$ and $second$", remend("$first$ and $second", inlineKatexOpts()))
	})

	t.Run("should complete inline $ but not affect complete block $$", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "$$block$$ and $inline$", remend("$$block$$ and $inline", inlineKatexOpts()))
	})

	t.Run("should handle streaming chunks of inline math", func(t *testing.T) {
		t.Parallel()
		chunks := []string{
			"The formula",
			"The formula $E",
			"The formula $E = mc",
			"The formula $E = mc^2",
			"The formula $E = mc^2$ shows",
		}
		must.Eq(t, chunks[0], remend(chunks[0], inlineKatexOpts()))
		must.Eq(t, "The formula $E$", remend(chunks[1], inlineKatexOpts()))
		must.Eq(t, "The formula $E = mc$", remend(chunks[2], inlineKatexOpts()))
		must.Eq(t, "The formula $E = mc^2$", remend(chunks[3], inlineKatexOpts()))
		must.Eq(t, chunks[4], remend(chunks[4], inlineKatexOpts()))
	})

	t.Run("should not complete escaped dollar signs", func(t *testing.T) {
		t.Parallel()
		text := "Price is \\$100"
		must.Eq(t, text, remend(text, inlineKatexOpts()))
	})

	t.Run("should not complete $ inside inline code", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Use `$var` for variables and $formula$", remend("Use `$var` for variables and $formula", inlineKatexOpts()))
	})

	t.Run("should handle multiple complete inline math expressions", func(t *testing.T) {
		t.Parallel()
		text := "$a = 1$ and $b = 2$"
		must.Eq(t, text, remend(text, inlineKatexOpts()))
	})

	t.Run("should handle mixed inline and block math", func(t *testing.T) {
		t.Parallel()
		text := "Inline $x$ and block $$y$$"
		must.Eq(t, text, remend(text, inlineKatexOpts()))
	})

	t.Run("should not complete $ inside a complete block math expression", func(t *testing.T) {
		t.Parallel()
		text := "$$x_1 + y_2 = z_3$$"
		must.Eq(t, text, remend(text, inlineKatexOpts()))
	})

	t.Run("should handle $$ followed by an unmatched $", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "$$block$$ then $x + y$", remend("$$block$$ then $x + y", inlineKatexOpts()))
	})

	t.Run("should not produce extra $ when block katex and inline katex both run", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "$$formula$$", remend("$$formula$", inlineKatexOpts()))
		must.Eq(t, "$$x = y$$", remend("$$x = y$", inlineKatexOpts()))
	})
}

func TestMathBlocksWithUnderscores(t *testing.T) {
	t.Parallel()

	t.Run("should not complete underscores within inline math blocks", func(t *testing.T) {
		t.Parallel()
		text := "The variable $x_1$ represents the first element"
		must.Eq(t, text, Mend(text))
		text2 := "Formula: $a_b + c_d = e_f$"
		must.Eq(t, text2, Mend(text2))
	})

	t.Run("should not complete underscores within block math", func(t *testing.T) {
		t.Parallel()
		text := "$$x_1 + y_2 = z_3$$"
		must.Eq(t, text, Mend(text))
		text2 := "$$\na_1 + b_2\nc_3 + d_4\n$$"
		must.Eq(t, text2, Mend(text2))
	})

	t.Run("should not add underscore when math block has incomplete underscore", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Math expression $x_", Mend("Math expression $x_"))
		must.Eq(t, "$$formula_$$", Mend("$$formula_"))
	})

	t.Run("should handle underscores outside math blocks normally", func(t *testing.T) {
		t.Parallel()
		text := "Text with _italic_ and math $x_1$"
		must.Eq(t, text, Mend(text))
		text2 := "_italic text_ followed by $a_b$"
		must.Eq(t, text2, Mend(text2))
	})

	t.Run("should complete italic underscore outside math but not inside", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Start _italic with $x_1$_", Mend("Start _italic with $x_1$"))
	})

	t.Run("should handle complex math expressions with multiple underscores", func(t *testing.T) {
		t.Parallel()
		text := "$x_1 + x_2 + x_3 = y_1$"
		must.Eq(t, text, Mend(text))
		text2 := "$$\\sum_{i=1}^{n} x_i = \\prod_{j=1}^{m} y_j$$"
		must.Eq(t, text2, Mend(text2))
	})

	t.Run("should handle escaped dollar signs correctly", func(t *testing.T) {
		t.Parallel()
		text := "Price is \\$50 and _this is italic_"
		must.Eq(t, text, Mend(text))
		must.Eq(t, "Cost \\$100 with _incomplete_", Mend("Cost \\$100 with _incomplete"))
	})

	t.Run("should handle mixed inline and block math", func(t *testing.T) {
		t.Parallel()
		text := "Inline $x_1$ and block $$y_2$$ math"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should not interfere with complete math blocks when adding underscores outside", func(t *testing.T) {
		t.Parallel()
		text := "_italic start $x_1$ italic end_"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should not complete dollar signs in inline code blocks (#296)", func(t *testing.T) {
		t.Parallel()
		str := "Streamdown uses double dollar signs (`$$`) to delimit mathematical expressions."
		must.Eq(t, str, Mend(str))
	})

	t.Run("should handle multiple inline code blocks with $$ correctly (#296)", func(t *testing.T) {
		t.Parallel()
		str := "Use `$$` for math blocks and `$$formula$$` for inline."
		must.Eq(t, str, Mend(str))
	})

	t.Run("should complete $$ outside inline code but not inside (#296)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Math: $$x+y and code: `$$`$$", Mend("Math: $$x+y and code: `$$`"))
	})

	t.Run("should handle mixed $$ inside and outside code blocks (#296)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "$$formula$$ and code `$$` and $$incomplete$$", Mend("$$formula$$ and code `$$` and $$incomplete"))
	})
}

func TestMathBlocksWithAsterisks(t *testing.T) {
	t.Parallel()

	t.Run("should not complete asterisks within block math", func(t *testing.T) {
		t.Parallel()
		text := "$$\\mathbf{w}^{*}$$"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should not complete asterisks in complex math expressions", func(t *testing.T) {
		t.Parallel()
		text := "$$\n\\mathbf{w}^{*} = \\underset{\\|\\mathbf{w}\\|=1}{\\arg\\max} \\;\\; \\mathbf{w}^T S \\mathbf{w}\n$$"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle asterisks outside math blocks normally", func(t *testing.T) {
		t.Parallel()
		text := "Text with *italic* and math $$x^{*}$$"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should complete italic asterisk outside math but not inside", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Start *italic with $$x^{*}$$*", Mend("Start *italic with $$x^{*}$$"))
	})
}

func TestLaTeXDelimitedMathWithEmphasisMarkers(t *testing.T) {
	t.Parallel()

	t.Run("should not complete underscores within paren-style inline math", func(t *testing.T) {
		t.Parallel()
		text := "\\(P(x_{t+1})\\)\n\nplain trailing text."
		must.Eq(t, text, Mend(text))
	})

	t.Run("should not complete underscores within bracket-style display math", func(t *testing.T) {
		t.Parallel()
		text := "\\[\nP(x_{t+1} | x_t)\n\\]"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should not complete asterisks within paren-style inline math", func(t *testing.T) {
		t.Parallel()
		text := "\\(w^{*}\\)\n\nplain trailing text."
		must.Eq(t, text, Mend(text))
	})

	t.Run("should complete bold after LaTeX inline math without leaking subscripts", func(t *testing.T) {
		t.Parallel()
		text := strings.Join([]string{
			"> Given tokens \\(x_1, ..., x_t\\), maximize \\(P(x_{t+1} | x_1, ..., x_t)\\)",
			"",
			"**2. Instruction Tuning (SFT)**",
			"- Dataset size is much smaller than pre-training",
		}, "\n")
		partial := text[:strings.Index(text, "(SFT)")+len("(SFT")]
		must.Eq(t, partial+"**", Mend(partial))
	})

	t.Run("should complete ordinary underscore emphasis outside LaTeX math", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Inline math \\(x_1\\) and _ordinary italic_", Mend("Inline math \\(x_1\\) and _ordinary italic"))
	})
}

func TestKaTeXWithComplexContent(t *testing.T) {
	t.Parallel()

	t.Run("should close block katex with braces inside", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "$$\\frac{x}{y$$", Mend("$$\\frac{x}{y"))
	})

	t.Run("should close block katex with latex environments", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "$$\\begin{matrix} a$$", Mend("$$\\begin{matrix} a"))
	})

	t.Run("should close inline katex when enabled", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "$x^2 + y^2$", remend("$x^2 + y^2", inlineKatexOpts()))
	})

	t.Run("should not treat currency as katex without inlineKatex", func(t *testing.T) {
		t.Parallel()
		text := "The price is $50 and $100"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should close odd inline katex with currency-like text", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "The price is $50 and $100", remend("The price is $50 and $100", inlineKatexOpts()))
	})

	t.Run("should close multiline block katex with complex content", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "$$\n\\sum_{i=0}^{n} x_i\n$$", Mend("$$\n\\sum_{i=0}^{n} x_i"))
	})
}

func TestKaTeXDisabledAndMidMarker(t *testing.T) {
	t.Parallel()

	t.Run("should not close single dollar at end without inlineKatex", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "text$", Mend("text$"))
	})

	t.Run("should close single dollar at end with inlineKatex enabled", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "text$$", remend("text$", inlineKatexOpts()))
	})

	t.Run("should not close katex when katex disabled", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "$$formula", remend("$$formula", remendOptions{katex: boolPtr(false)}))
	})
}
