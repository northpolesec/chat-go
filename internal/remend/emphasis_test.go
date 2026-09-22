package remend

import (
	"regexp"
	"strings"
	"testing"

	"github.com/shoenig/test/must"
)

func TestBoldFormatting(t *testing.T) {
	t.Parallel()

	t.Run("should complete incomplete bold formatting", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text with **bold**", Mend("Text with **bold"))
		must.Eq(t, "**incomplete**", Mend("**incomplete"))
	})

	t.Run("should keep complete bold formatting unchanged", func(t *testing.T) {
		t.Parallel()
		text := "Text with **bold text**"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle multiple bold sections", func(t *testing.T) {
		t.Parallel()
		text := "**bold1** and **bold2**"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should complete odd number of bold markers", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "**first** and **second**", Mend("**first** and **second"))
	})

	t.Run("should handle partial bold text at chunk boundary", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Here is some **bold tex**", Mend("Here is some **bold tex"))
	})

	t.Run("should complete half-complete bold closing marker (#313)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "**xxx**", Mend("**xxx*"))
		must.Eq(t, "**bold text**", Mend("**bold text*"))
		must.Eq(t, "Text with **bold**", Mend("Text with **bold*"))
		must.Eq(t, "This is **bold text**", Mend("This is **bold text*"))
	})
}

func TestItalicFormattingWithUnderscores(t *testing.T) {
	t.Parallel()

	t.Run("should complete incomplete italic formatting with double underscores", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text with __italic__", Mend("Text with __italic"))
		must.Eq(t, "__incomplete__", Mend("__incomplete"))
	})

	t.Run("should keep complete italic formatting unchanged", func(t *testing.T) {
		t.Parallel()
		text := "Text with __italic text__"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle odd number of double underscore pairs", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "__first__ and __second__", Mend("__first__ and __second"))
	})

	t.Run("should complete half-complete __ closing marker (#313)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "__xxx__", Mend("__xxx_"))
		must.Eq(t, "__bold text__", Mend("__bold text_"))
		must.Eq(t, "Text with __bold__", Mend("Text with __bold_"))
		must.Eq(t, "This is __bold text__", Mend("This is __bold text_"))
	})
}

func TestItalicFormattingWithAsterisks(t *testing.T) {
	t.Parallel()

	t.Run("should complete incomplete italic formatting with single asterisks", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text with *italic*", Mend("Text with *italic"))
		must.Eq(t, "*incomplete*", Mend("*incomplete"))
	})

	t.Run("should keep complete italic formatting unchanged", func(t *testing.T) {
		t.Parallel()
		text := "Text with *italic text*"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should not confuse single asterisks with bold markers", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "**bold** and *italic*", Mend("**bold** and *italic"))
	})

	t.Run("should not treat asterisks in the middle of words as italic markers - #189", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "234234*123", Mend("234234*123"))
		must.Eq(t, "hello*world", Mend("hello*world"))
		must.Eq(t, "test*123*test", Mend("test*123*test"))
		must.Eq(t, "*italic with some*var*name inside*", Mend("*italic with some*var*name inside"))
		must.Eq(t, "test*var and *incomplete italic*", Mend("test*var and *incomplete italic"))
	})

	t.Run("should handle escaped asterisks correctly in countSingleAsterisks", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "\\*escaped asterisk and *italic*", Mend("\\*escaped asterisk and *italic"))
		must.Eq(t, "*start \\* middle \\* end*", Mend("*start \\* middle \\* end"))
	})

	t.Run("should handle asterisks between letters and numbers", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "abc*123", Mend("abc*123"))
		must.Eq(t, "123*abc", Mend("123*abc"))
	})

	t.Run("should still complete italic formatting with asterisks when not word-internal", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "This is *italic*", Mend("This is *italic"))
		must.Eq(t, "*word* and more text", Mend("*word* and more text"))
	})
}

func TestItalicFormattingWithSingleUnderscores(t *testing.T) {
	t.Parallel()

	t.Run("should complete incomplete italic formatting with single underscores", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text with _italic_", Mend("Text with _italic"))
		must.Eq(t, "_incomplete_", Mend("_incomplete"))
	})

	t.Run("should keep complete italic formatting unchanged", func(t *testing.T) {
		t.Parallel()
		text := "Text with _italic text_"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should not confuse single underscores with double underscore markers", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "__bold__ and _italic_", Mend("__bold__ and _italic"))
	})

	t.Run("should handle escaped single underscores", func(t *testing.T) {
		t.Parallel()
		text := "Text with \\_escaped underscore"
		must.Eq(t, text, Mend(text))
		text2 := "some\\_text_with_underscores"
		must.Eq(t, "some\\_text_with_underscores", Mend(text2))
	})

	t.Run("should handle mixed escaped and unescaped underscores correctly", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "\\_escaped\\_ and _unescaped_", Mend("\\_escaped\\_ and _unescaped"))
		must.Eq(t, "Start \\_escaped\\_ middle _incomplete_", Mend("Start \\_escaped\\_ middle _incomplete"))
		must.Eq(t, "\\_fully\\_escaped\\_", Mend("\\_fully\\_escaped\\_"))
		must.Eq(t, "\\_escaped\\_ _complete_ pair", Mend("\\_escaped\\_ _complete_ pair"))
	})

	t.Run("should handle underscores with unicode word characters", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "café_price", Mend("café_price"))
		must.Eq(t, "naïve_approach", Mend("naïve_approach"))
	})

	t.Run("should not count word-internal single underscores in countSingleUnderscores", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "some_variable_name", Mend("some_variable_name"))
		must.Eq(t, "test_123_value", Mend("test_123_value"))
		must.Eq(t, "_start with underscore_", Mend("_start with underscore"))
		must.Eq(t, "_italic with some_var_name inside_", Mend("_italic with some_var_name inside"))
		must.Eq(t, "test_var and _incomplete italic_", Mend("test_var and _incomplete italic"))
	})

	t.Run("should handle incomplete single underscore with trailing newlines", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text with _italic_\n", Mend("Text with _italic\n"))
		must.Eq(t, "_incomplete_\n\n", Mend("_incomplete\n\n"))
		must.Eq(t, "Start _text_\n", Mend("Start _text\n"))
	})
}

func TestBoldItalicFormatting(t *testing.T) {
	t.Parallel()

	t.Run("should complete incomplete bold-italic formatting", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text with ***bold-italic***", Mend("Text with ***bold-italic"))
		must.Eq(t, "***incomplete***", Mend("***incomplete"))
	})

	t.Run("should keep complete bold-italic formatting unchanged", func(t *testing.T) {
		t.Parallel()
		text := "Text with ***bold and italic text***"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle multiple bold-italic sections", func(t *testing.T) {
		t.Parallel()
		text := "***first*** and ***second***"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should complete odd number of triple asterisk markers", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "***first*** and ***second***", Mend("***first*** and ***second"))
	})

	t.Run("should not confuse triple asterisks with single or double", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "*italic* **bold** ***both***", Mend("*italic* **bold** ***both"))
	})

	t.Run("should handle triple asterisks at start of text", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "***Starting bold-italic***", Mend("***Starting bold-italic"))
	})

	t.Run("should handle nested formatting with triple asterisks", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "***bold-italic with `code***`", Mend("***bold-italic with `code"))
	})

	t.Run("should handle bold-italic chunks", func(t *testing.T) {
		t.Parallel()
		chunks := []string{
			"This is",
			"This is ***very",
			"This is ***very important",
			"This is ***very important***",
			"This is ***very important*** to know",
		}
		must.Eq(t, "This is", Mend(chunks[0]))
		must.Eq(t, "This is ***very***", Mend(chunks[1]))
		must.Eq(t, "This is ***very important***", Mend(chunks[2]))
		must.Eq(t, chunks[3], Mend(chunks[3]))
		must.Eq(t, chunks[4], Mend(chunks[4]))
	})

	t.Run("should handle text ending with multiple consecutive asterisks", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "text ***", Mend("text ***"))
		must.Eq(t, "text ****", Mend("text ****"))
		must.Eq(t, "text *****", Mend("text *****"))
		must.Eq(t, "text ******", Mend("text ******"))
		must.Eq(t, "text***", Mend("text***"))
		must.Eq(t, "word****", Mend("word****"))
		must.Eq(t, "end******", Mend("end******"))
		must.Eq(t, "***start***end***", Mend("***start***end***"))
		must.Eq(t, "***text***", Mend("***text***"))
		must.Eq(t, "***incomplete***", Mend("***incomplete"))
		must.Eq(t, "***word text***", Mend("***word text***"))
	})

	t.Run("should not add closing markers to overlapping bold and italic (#302)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Combined **bold and *italic*** text", Mend("Combined **bold and *italic*** text"))
		must.Eq(t, "**bold and *italic*** more text", Mend("**bold and *italic*** more text"))
		must.Eq(t, "test **bold and *italic*** end", Mend("test **bold and *italic*** end"))
		must.Eq(t, "- Combined **bold and *italic*** text", Mend("- Combined **bold and *italic*** text"))
	})
}

func TestIntrawordAsteriskEmphasis(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"*foo*bar",
		"this is *real*ly good",
		"이것은 *기울임*으로 표시",
		"5*6*78",
		"*foo*bar*",
		"a *b*c*",
		"a *b*c* d",
		"*b*c*",
		"*a* b*",
		"a *b* c*",
		"*a*b*c*d",
		"x *y*z w",
		"before *a*b after *c*",
	} {
		t.Run("keeps complete emphasis unchanged: "+text, func(t *testing.T) {
			t.Parallel()
			must.Eq(t, text, Mend(text))
		})
	}

	t.Run("leaves a lone intraword asterisk literal", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "foo*bar", Mend("foo*bar"))
		must.Eq(t, "hello*world", Mend("hello*world"))
		must.Eq(t, "test*123*test", Mend("test*123*test"))
	})

	t.Run("continues an active intraword emphasis chain", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "*foo*bar*baz*", Mend("*foo*bar*baz"))
		must.Eq(t, "*file*name*ext*", Mend("*file*name*ext"))
	})

	t.Run("completes a later incomplete run after a closed intraword pair", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "before *a*b after *c*", Mend("before *a*b after *c"))
		must.Eq(t, "*기울임*으로 *another*", Mend("*기울임*으로 *another"))
	})

	t.Run("does not reopen after whitespace following a closed intraword pair", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "*foo* bar*baz", Mend("*foo* bar*baz"))
	})
}

func TestWordInternalUnderscores(t *testing.T) {
	t.Parallel()

	t.Run("underscores as word separators", func(t *testing.T) {
		t.Parallel()
		t.Run("should handle single underscore between words", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "hello_world", Mend("hello_world"))
		})
		t.Run("should handle multiple underscores between words", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "hello_world_test", Mend("hello_world_test"))
		})
		t.Run("should handle CONSTANT_CASE", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "MAX_VALUE", Mend("MAX_VALUE"))
		})
		t.Run("should handle multiple snake_case words in text", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "The user_name and user_email are required", Mend("The user_name and user_email are required"))
		})
		t.Run("should handle underscore in URLs", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "Visit https://example.com/path_with_underscore", Mend("Visit https://example.com/path_with_underscore"))
		})
		t.Run("should handle numbers with underscores", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "The value is 1_000_000", Mend("The value is 1_000_000"))
		})
	})

	t.Run("incomplete italic formatting", func(t *testing.T) {
		t.Parallel()
		t.Run("should complete italic at word boundary", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "_italic text_", Mend("_italic text"))
		})
		t.Run("should complete italic with punctuation", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "This is _italic_", Mend("This is _italic"))
		})
		t.Run("should complete italic before newline", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "_italic_\n", Mend("_italic\n"))
		})
	})

	t.Run("edge cases", func(t *testing.T) {
		t.Parallel()
		t.Run("should handle underscore at end of word (ambiguous case)", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "word_", Mend("word_"))
		})
		t.Run("should handle leading underscore in identifier", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "_privateVariable_", Mend("_privateVariable"))
		})
		t.Run("should handle code with underscores in markdown", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "Use `variable_name` in your code", Mend("Use `variable_name` in your code"))
		})
		t.Run("should handle mixed snake_case and italic", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "The variable_name is _important_", Mend("The variable_name is _important"))
		})
		t.Run("should not modify complete italic pairs", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "_complete italic_ and some_other_text", Mend("_complete italic_ and some_other_text"))
		})
		t.Run("should handle underscore in code blocks", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "```\nfunction_name()\n```", Mend("```\nfunction_name()\n```"))
		})
		t.Run("should handle HTML attributes with underscores", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, `<div data_attribute="value">`, Mend(`<div data_attribute="value">`))
		})
	})

	t.Run("real-world scenarios", func(t *testing.T) {
		t.Parallel()
		t.Run("should handle Python-style names", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "__init__ and __main__ are special", Mend("__init__ and __main__ are special"))
		})
		t.Run("should handle markdown in sentences with snake_case", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "The user_id field stores the _unique identifier_", Mend("The user_id field stores the _unique identifier"))
		})
		t.Run("should handle the original bug report case", func(t *testing.T) {
			t.Parallel()
			input := "hello_world\n\n<a href=\"example_link\"/>"
			result := Mend(input)
			must.Eq(t, input, result)
			must.False(t, regexp.MustCompile(`hello_world_`).MatchString(result))
			must.False(t, regexp.MustCompile(`_$`).MatchString(result))
		})
	})
}

func TestListHandling(t *testing.T) {
	t.Parallel()

	t.Run("should not add asterisk to lists using asterisk markers", func(t *testing.T) {
		t.Parallel()
		text := "* Item 1\n* Item 2\n* Item 3"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should not add asterisk to single list item", func(t *testing.T) {
		t.Parallel()
		text := "* Single item"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should not add asterisk to nested lists", func(t *testing.T) {
		t.Parallel()
		text := "* Parent item\n  * Nested item 1\n  * Nested item 2"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle lists with italic text correctly", func(t *testing.T) {
		t.Parallel()
		text := "* Item with *italic* text\n* Another item"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should complete incomplete italic even in list items", func(t *testing.T) {
		t.Parallel()
		text := "* Item with *incomplete italic\n* Another item"
		must.Eq(t, "* Item with *incomplete italic\n* Another item*", Mend(text))
	})

	t.Run("should handle mixed list markers and italic formatting", func(t *testing.T) {
		t.Parallel()
		text := "* First item\n* Second *italic* item\n* Third item"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle lists with tabs for indentation", func(t *testing.T) {
		t.Parallel()
		text := "*\tItem with tab\n*\tAnother item"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should not interfere with dash lists", func(t *testing.T) {
		t.Parallel()
		text := "- Item 1\n- Item 2 with *italic*\n- Item 3"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle the Gemini response example from issue", func(t *testing.T) {
		t.Parallel()
		geminiResponse := "* user123\n* user456\n* user789"
		must.Eq(t, geminiResponse, Mend(geminiResponse))
	})

	t.Run("should handle lists with incomplete formatting", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "- Item 1\n- Item 2 with **bol**", Mend("- Item 1\n- Item 2 with **bol"))
	})

	t.Run("should handle lists with emphasis character blocks (#97)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "- __", Mend("- __"))
		must.Eq(t, "- **", Mend("- **"))
		must.Eq(t, "- __\n- **", Mend("- __\n- **"))
		must.Eq(t, "\n- __\n- **", Mend("\n- __\n- **"))
		must.Eq(t, "* __\n* **", Mend("* __\n* **"))
		must.Eq(t, "+ __\n+ **", Mend("+ __\n+ **"))
		must.Eq(t, "- __ text after__", Mend("- __ text after"))
		must.Eq(t, "- ** text after**", Mend("- ** text after"))
		must.Eq(t, "- __\n- Normal item\n- **", Mend("- __\n- Normal item\n- **"))
		must.Eq(t, "- ***", Mend("- ***"))
		must.Eq(t, "- *", Mend("- *"))
		must.Eq(t, "- _", Mend("- _"))
		must.Eq(t, "- ~~", Mend("- ~~"))
		must.Eq(t, "- `", Mend("- `"))
	})

	t.Run("should not complete list items with emphasis markers spanning multiple lines", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "- **text\nmore text", Mend("- **text\nmore text"))
		must.Eq(t, "* **content\n* Another item", Mend("* **content\n* Another item"))
	})
}

func TestMixedFormatting(t *testing.T) {
	t.Parallel()

	t.Run("should handle multiple formatting types", func(t *testing.T) {
		t.Parallel()
		text := "**bold** and *italic* and `code` and ~~strike~~"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should complete multiple incomplete formats", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "**bold and *italic*", Mend("**bold and *italic"))
	})

	t.Run("should handle nested formatting", func(t *testing.T) {
		t.Parallel()
		text := "**bold with *italic* inside**"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should prioritize link/image preservation over formatting completion", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text with [link and **bold](streamdown:incomplete-link)", Mend("Text with [link and **bold"))
	})

	t.Run("should handle complex real-world markdown", func(t *testing.T) {
		t.Parallel()
		text := "# Heading\n\n**Bold text** with *italic* and `code`.\n\n- List item\n- Another item with ~~strike~~"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle bold inside italic", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "*italic with **bold***", Mend("*italic with **bold"))
	})

	t.Run("should handle code inside bold", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "**bold with `code**`", Mend("**bold with `code"))
	})

	t.Run("should handle strikethrough with other formatting", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "~~strike with **bold**~~", Mend("~~strike with **bold"))
	})

	t.Run("should handle dollar sign inside other formatting", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "**bold with $x^2**", Mend("**bold with $x^2"))
	})

	t.Run("should handle deeply nested incomplete formatting", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "**bold *italic `code ~~strike*`", Mend("**bold *italic `code ~~strike"))
	})

	t.Run("should preserve complete nested formatting", func(t *testing.T) {
		t.Parallel()
		text := "**bold *italic* text** and `code`"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle mixed bold-italic formatting (#265)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "**bold and *bold-italic***", Mend("**bold and *bold-italic***"))
	})

	t.Run("should close nested underscore italic before bold (#302)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "combined **_bold and italic_**", Mend("combined **_bold and italic"))
		must.Eq(t, "**_text_**", Mend("**_text"))
		must.Eq(t, "_italic and **bold**_", Mend("_italic and **bold"))
	})
}

func TestEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("should handle text ending with formatting characters", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text ending with *", Mend("Text ending with *"))
		must.Eq(t, "Text ending with **", Mend("Text ending with **"))
	})

	t.Run("should handle empty formatting markers", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "****", Mend("****"))
		must.Eq(t, "``", Mend("``"))
	})

	t.Run("should handle standalone emphasis characters (#90)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "**", Mend("**"))
		must.Eq(t, "__", Mend("__"))
		must.Eq(t, "***", Mend("***"))
		must.Eq(t, "*", Mend("*"))
		must.Eq(t, "_", Mend("_"))
		must.Eq(t, "~~", Mend("~~"))
		must.Eq(t, "`", Mend("`"))
		must.Eq(t, "** __", Mend("** __"))
		must.Eq(t, "\n** __\n", Mend("\n** __\n"))
		must.Eq(t, "* _ ~~ `", Mend("* _ ~~ `"))
		must.Eq(t, "**", Mend("** "))
		must.Eq(t, " **", Mend(" **"))
		must.Eq(t, "  **  ", Mend("  **  "))
		must.Eq(t, "**text**", Mend("**text"))
		must.Eq(t, "__text__", Mend("__text"))
		must.Eq(t, "*text*", Mend("*text"))
		must.Eq(t, "_text_", Mend("_text"))
		must.Eq(t, "~~text~~", Mend("~~text"))
		must.Eq(t, "`text`", Mend("`text"))
	})

	t.Run("should handle very long text", func(t *testing.T) {
		t.Parallel()
		longText := strings.Repeat("a", 10_000) + " **bold"
		expected := strings.Repeat("a", 10_000) + " **bold**"
		must.Eq(t, expected, Mend(longText))
	})

	t.Run("should handle text with only formatting characters", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "*", Mend("*"))
		must.Eq(t, "**", Mend("**"))
		must.Eq(t, "`", Mend("`"))
	})

	t.Run("should handle escaped characters", func(t *testing.T) {
		t.Parallel()
		text := "Text with \\* escaped asterisk"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle markdown at very end of string", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "text**", Mend("text**"))
		must.Eq(t, "text*", Mend("text*"))
		must.Eq(t, "text`", Mend("text`"))
		must.Eq(t, "text$", Mend("text$"))
		must.Eq(t, "text~~", Mend("text~~"))
	})

	t.Run("should handle whitespace before incomplete markdown", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "text **bold**", Mend("text **bold"))
		must.Eq(t, "text\n**bold**", Mend("text\n**bold"))
		must.Eq(t, "text\t`code`", Mend("text\t`code"))
	})

	t.Run("should handle unicode characters in incomplete markdown", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "**émoji 🎉**", Mend("**émoji 🎉"))
		must.Eq(t, "`código`", Mend("`código"))
	})

	t.Run("should handle HTML entities in incomplete markdown", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "**&lt;tag&gt;**", Mend("**&lt;tag&gt;"))
		must.Eq(t, "`&amp;`", Mend("`&amp;"))
	})

	t.Run("should not treat asterisks flanked by whitespace as emphasis markers (#370)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "3 + 2 - 5 * 0 = ?", Mend("3 + 2 - 5 * 0 = ?"))
		must.Eq(t, "5 * 0", Mend("5 * 0"))
		must.Eq(t, "x * y", Mend("x * y"))
		must.Eq(t, "a * b = c", Mend("a * b = c"))
		must.Eq(t, "2 * 3 * 4", Mend("2 * 3 * 4"))
		must.Eq(t, "5 * 0 and *italic*", Mend("5 * 0 and *italic"))
	})
}

func TestHorizontalRuleHandling(t *testing.T) {
	t.Parallel()

	t.Run("should preserve complete horizontal rules with hyphens", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "---", Mend("---"))
		must.Eq(t, "----", Mend("----"))
		must.Eq(t, "-----", Mend("-----"))
	})

	t.Run("should preserve complete horizontal rules with asterisks", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "***", Mend("***"))
		must.Eq(t, "****", Mend("****"))
		must.Eq(t, "*****", Mend("*****"))
	})

	t.Run("should preserve complete horizontal rules with underscores", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "___", Mend("___"))
		must.Eq(t, "____", Mend("____"))
		must.Eq(t, "_____", Mend("_____"))
	})

	t.Run("should preserve horizontal rules with spaces", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "- - -", Mend("- - -"))
		must.Eq(t, "* * *", Mend("* * *"))
		must.Eq(t, "_ _ _", Mend("_ _ _"))
	})

	t.Run("should preserve horizontal rules with mixed spacing", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "-  -  -", Mend("-  -  -"))
		must.Eq(t, "*   *   *", Mend("*   *   *"))
		must.Eq(t, "_    _    _", Mend("_    _    _"))
	})

	t.Run("should not confuse horizontal rules with emphasis", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text before\n***\nText after", Mend("Text before\n***\nText after"))
		must.Eq(t, "Text before\n___\nText after", Mend("Text before\n___\nText after"))
	})

	t.Run("should handle horizontal rules at the end of text", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Some text\n\n---", Mend("Some text\n\n---"))
		must.Eq(t, "Some text\n\n***", Mend("Some text\n\n***"))
		must.Eq(t, "Some text\n\n___", Mend("Some text\n\n___"))
	})

	t.Run("should handle horizontal rules at the start of text", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "---\n\nSome text", Mend("---\n\nSome text"))
		must.Eq(t, "***\n\nSome text", Mend("***\n\nSome text"))
		must.Eq(t, "___\n\nSome text", Mend("___\n\nSome text"))
	})

	t.Run("should handle multiple horizontal rules", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Section 1\n\n---\n\nSection 2\n\n---\n\nSection 3", Mend("Section 1\n\n---\n\nSection 2\n\n---\n\nSection 3"))
	})

	t.Run("should not confuse two asterisks with horizontal rule start", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text with **bold**", Mend("Text with **bold"))
	})

	t.Run("should not confuse two hyphens with horizontal rule", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text with --", Mend("Text with --"))
	})

	t.Run("should handle horizontal rules after lists", func(t *testing.T) {
		t.Parallel()
		text := "- Item 1\n- Item 2\n\n---\n\nNew section"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle horizontal rules before headings", func(t *testing.T) {
		t.Parallel()
		text := "---\n\n# Heading"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle partial horizontal rules during streaming", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "--", Mend("--"))
		must.Eq(t, "**", Mend("**"))
		must.Eq(t, "__", Mend("__"))
		must.Eq(t, "Text\n\n--", Mend("Text\n\n--"))
	})

	t.Run("should not add closing markers to standalone asterisk sequences that could be rules", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "****", Mend("****"))
		must.Eq(t, "*****", Mend("*****"))
	})

	t.Run("should handle horizontal rules with leading whitespace", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "   ---", Mend("   ---"))
		must.Eq(t, "  ***", Mend("  ***"))
		must.Eq(t, " ___", Mend(" ___"))
	})

	t.Run("should handle horizontal rule-like patterns in text", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "This is not a --- horizontal rule", Mend("This is not a --- horizontal rule"))
	})

	t.Run("should not complete emphasis when asterisks form potential horizontal rule", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text\n***", Mend("Text\n***"))
	})

	t.Run("should handle horizontal rules in complex markdown", func(t *testing.T) {
		t.Parallel()
		text := "# Title\n\nSome content with **bold** text.\n\n---\n\n## Section 2\n\nMore content."
		must.Eq(t, text, Mend(text))
	})
}

func TestChunkedStreamingScenarios(t *testing.T) {
	t.Parallel()

	t.Run("should handle nested formatting cut mid-stream", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "This is **bold with *ital*", Mend("This is **bold with *ital"))
		must.Eq(t, "**bold _und_**", Mend("**bold _und"))
	})

	t.Run("should handle headings with incomplete formatting", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "# Main Title\n## Subtitle with **emph**", Mend("# Main Title\n## Subtitle with **emph"))
	})

	t.Run("should handle blockquotes with incomplete formatting", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "> Quote with **bold**", Mend("> Quote with **bold"))
	})

	t.Run("should handle tables with incomplete formatting", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "| Col1 | Col2 |\n|------|------|\n| **dat**", Mend("| Col1 | Col2 |\n|------|------|\n| **dat"))
	})

	t.Run("should handle complex nested structures from chunks", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "1. First item\n   - Nested with `code\n2. Second`", Mend("1. First item\n   - Nested with `code\n2. Second"))
	})

	t.Run("should handle multiple incomplete formats in one chunk", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text **bold `code**`", Mend("Text **bold `code"))
	})
}

func TestRealWorldStreamingChunks(t *testing.T) {
	t.Parallel()

	t.Run("should handle typical GPT response chunks", func(t *testing.T) {
		t.Parallel()
		chunks := []string{
			"Here is",
			"Here is a **bold",
			"Here is a **bold statement",
			"Here is a **bold statement** about",
			"Here is a **bold statement** about `code",
			"Here is a **bold statement** about `code`.",
		}
		must.Eq(t, "Here is", Mend(chunks[0]))
		must.Eq(t, "Here is a **bold**", Mend(chunks[1]))
		must.Eq(t, "Here is a **bold statement**", Mend(chunks[2]))
		must.Eq(t, "Here is a **bold statement** about", Mend(chunks[3]))
		must.Eq(t, "Here is a **bold statement** about `code`", Mend(chunks[4]))
		must.Eq(t, chunks[5], Mend(chunks[5]))
	})

	t.Run("should handle code explanation chunks", func(t *testing.T) {
		t.Parallel()
		chunks := []string{
			"To use this function",
			"To use this function, call `getData(",
			"To use this function, call `getData()` with",
		}
		must.Eq(t, chunks[0], Mend(chunks[0]))
		must.Eq(t, "To use this function, call `getData(`", Mend(chunks[1]))
		must.Eq(t, chunks[2], Mend(chunks[2]))
	})
}

func TestCoverageGapsEmphasis(t *testing.T) {
	t.Parallel()

	t.Run("half-complete double underscore closing", func(t *testing.T) {
		t.Parallel()
		t.Run("should complete __content_ to __content__", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "__content__", Mend("__content_"))
		})
	})

	t.Run("underscore with trailing double asterisks", func(t *testing.T) {
		t.Parallel()
		t.Run("should close underscore when trailing ** is unrelated", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "_text**_", Mend("_text**"))
		})
	})

	t.Run("bold-italic inside code block", func(t *testing.T) {
		t.Parallel()
		t.Run("should not complete *** markers inside code blocks", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "```\n***bold", Mend("```\n***bold"))
		})
		t.Run("should complete *** outside code block with *** inside", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "```\n***\n```\n***text***", Mend("```\n***\n```\n***text"))
		})
	})

	t.Run("countTripleAsterisks", func(t *testing.T) {
		t.Parallel()
		t.Run("should count trailing *** at end of text", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, 1, countTripleAsterisks("text***"))
		})
		t.Run("should skip *** inside code blocks", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, 0, countTripleAsterisks("```\n***\n```"))
		})
		t.Run("should count *** outside but not inside code blocks", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, 1, countTripleAsterisks("```\n***\n```\n***"))
		})
		t.Run("should flush pending asterisks before code block toggle", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, 1, countTripleAsterisks("***```code```"))
		})
	})

	t.Run("single underscore counting with code blocks", func(t *testing.T) {
		t.Parallel()
		t.Run("should skip _ inside fenced code blocks", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "```\n_code\n```\n_text_", Mend("```\n_code\n```\n_text"))
		})
	})

	t.Run("double underscore counting with code blocks", func(t *testing.T) {
		t.Parallel()
		t.Run("should skip __ inside fenced code blocks", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "```\n__code\n```\n__text__", Mend("```\n__code\n```\n__text"))
		})
	})

	t.Run("isWithinLinkOrImageUrl — ) found before (", func(t *testing.T) {
		t.Parallel()
		t.Run("should handle underscore after complete link", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "[link](url) _word_", Mend("[link](url) _word"))
		})
	})

	t.Run("isWithinLinkOrImageUrl edge cases", func(t *testing.T) {
		t.Parallel()
		t.Run("should handle underscore after bare parenthesis", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "func(_arg_", Mend("func(_arg"))
		})
	})

	t.Run("isWithinHtmlTag edge cases", func(t *testing.T) {
		t.Parallel()
		t.Run("should handle underscore after > character", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "div> _text_", Mend("div> _text"))
		})
		t.Run("should handle underscore near < with invalid tag start", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "3<5 _text_", Mend("3<5 _text"))
		})
		t.Run("should handle underscore on new line after HTML element", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "<div>\n_text_", Mend("<div>\n_text"))
		})
	})

	t.Run("underscore inside link URL", func(t *testing.T) {
		t.Parallel()
		t.Run("should not close underscore that is part of a link URL", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "[link](a_b) _word_", Mend("[link](a_b) _word"))
		})
	})

	t.Run("double underscore half-complete in code block", func(t *testing.T) {
		t.Parallel()
		t.Run("should not complete __content_ inside code block", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "```\n__content_", Mend("```\n__content_"))
		})
	})

	t.Run("double underscore half-complete with even pairs", func(t *testing.T) {
		t.Parallel()
		t.Run("should not complete when __ pairs are balanced", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "__a__ __b__content_", Mend("__a__ __b__content_"))
		})
	})
}

func TestBrokenMarkdownEmphasis(t *testing.T) {
	t.Parallel()

	t.Run("rapid successive formatting switches", func(t *testing.T) {
		t.Parallel()
		t.Run("should close italic and strikethrough but not bold when asterisk in content", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "**bold then *italic then ~~strike*~~", Mend("**bold then *italic then ~~strike"))
		})
		t.Run("should close italic and strikethrough when bold pattern blocked", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "~~strike **bold *italic*~~", Mend("~~strike **bold *italic"))
		})
		t.Run("should close handlers in priority order (bold before strikethrough before code)", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "*italic **bold ~~strike `code***`~~", Mend("*italic **bold ~~strike `code"))
		})
		t.Run("should close bold before strikethrough (priority order)", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "**bold ~~strike**~~", Mend("**bold ~~strike"))
		})
		t.Run("should close italic then bold via bold-italic handler", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "*italic **bold***", Mend("*italic **bold"))
		})
	})

	t.Run("formatting cut mid-marker", func(t *testing.T) {
		t.Parallel()
		t.Run("should not close single asterisk at end (ambiguous)", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "text*", Mend("text*"))
		})
		t.Run("should handle opening marker + single char of closing", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "**bold**", Mend("**bold*"))
			must.Eq(t, "~~strike~~", Mend("~~strike~"))
		})
	})

	t.Run("backslash escapes with incomplete formatting", func(t *testing.T) {
		t.Parallel()
		t.Run("should not close escaped asterisks", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "\\*not italic", Mend("\\*not italic"))
		})
		t.Run("should not close double-escaped backslash before asterisk", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "\\\\*actually italic", Mend("\\\\*actually italic"))
		})
		t.Run("should close escaped double asterisks (remend does not track escape depth)", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "\\**not bold**", Mend("\\**not bold"))
		})
		t.Run("should handle mixed escaped and real formatting", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "\\*escaped\\* but *real italic*", Mend("\\*escaped\\* but *real italic"))
		})
	})

	t.Run("nested blockquotes with formatting", func(t *testing.T) {
		t.Parallel()
		t.Run("should close bold in deeply nested blockquote", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "> > **deeply nested bold**", Mend("> > **deeply nested bold"))
		})
		t.Run("should close bold in blockquote with list", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "> * list with **bold**", Mend("> * list with **bold"))
		})
		t.Run("should close italic in triple nested blockquote", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "> > > triple nested *italic*", Mend("> > > triple nested *italic"))
		})
		t.Run("should close strikethrough in blockquote", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "> ~~struck text~~", Mend("> ~~struck text"))
		})
	})

	t.Run("task lists with formatting", func(t *testing.T) {
		t.Parallel()
		t.Run("should close bold in unchecked task", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "- [ ] **bold task**", Mend("- [ ] **bold task"))
		})
		t.Run("should keep complete strikethrough in checked task", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "- [x] completed ~~struck~~", Mend("- [x] completed ~~struck~~"))
		})
		t.Run("should close italic in unchecked task", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "- [ ] *italic task*", Mend("- [ ] *italic task"))
		})
		t.Run("should close inline code in task", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "- [ ] `code task`", Mend("- [ ] `code task"))
		})
	})

	t.Run("formatting inside table cells", func(t *testing.T) {
		t.Parallel()
		t.Run("should close bold that appears to span cell boundary", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "| **bold | next |**", Mend("| **bold | next |"))
		})
		t.Run("should handle complete formatting in table cell", func(t *testing.T) {
			t.Parallel()
			text := "| **bold** | next |"
			must.Eq(t, text, Mend(text))
		})
	})

	t.Run("consecutive completed + incomplete formatting", func(t *testing.T) {
		t.Parallel()
		t.Run("should close second bold after complete bold", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "**bold** then **more**", Mend("**bold** then **more"))
		})
		t.Run("should close second italic after complete italic", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "*first* and *second*", Mend("*first* and *second"))
		})
		t.Run("should close second bold-italic after complete bold-italic", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "***first*** and ***second***", Mend("***first*** and ***second"))
		})
	})

	t.Run("formatting at paragraph boundaries", func(t *testing.T) {
		t.Parallel()
		t.Run("should close bold after paragraph break", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "paragraph1\n\n**bold**", Mend("paragraph1\n\n**bold"))
		})
		t.Run("should close italic after paragraph break", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "line1\n\n*italic text*", Mend("line1\n\n*italic text"))
		})
		t.Run("should close formatting after multiple newlines", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "text\n\n\n**bold**", Mend("text\n\n\n**bold"))
		})
	})

	t.Run("deeply nested formatting", func(t *testing.T) {
		t.Parallel()
		t.Run("should close handlers in priority order with deeply nested formatting", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "**bold *italic ~~strike `code*`~~", Mend("**bold *italic ~~strike `code"))
		})
		t.Run("should close bold-italic then code then strikethrough", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "***bold-italic ~~strike `code***`~~", Mend("***bold-italic ~~strike `code"))
		})
		t.Run("should close italic but not bold when asterisk blocks bold pattern", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "**bold and *italic*", Mend("**bold and *italic"))
		})
	})

	t.Run("CJK and Unicode with formatting", func(t *testing.T) {
		t.Parallel()
		t.Run("should close bold with Chinese text", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "**中文粗体**", Mend("**中文粗体"))
		})
		t.Run("should close italic with Japanese text", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "*日本語*", Mend("*日本語"))
		})
		t.Run("should close bold with mixed CJK and Latin", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "**Hello 世界**", Mend("**Hello 世界"))
		})
	})

	t.Run("formatting after structural elements", func(t *testing.T) {
		t.Parallel()
		t.Run("should close bold after horizontal rule", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "---\n**bold after rule**", Mend("---\n**bold after rule"))
		})
		t.Run("should close bold after heading", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "# Heading\n**bold**", Mend("# Heading\n**bold"))
		})
		t.Run("should close bold after blockquote", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "> quote\n**bold**", Mend("> quote\n**bold"))
		})
		t.Run("should close italic after code block", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "```\ncode\n```\n*italic*", Mend("```\ncode\n```\n*italic"))
		})
	})

	t.Run("indented code blocks", func(t *testing.T) {
		t.Parallel()
		t.Run("should still close asterisks in indented text (not fenced)", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "    *asterisks in indented*", Mend("    *asterisks in indented"))
		})
		t.Run("should close bold in indented text", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "    **bold in indented**", Mend("    **bold in indented"))
		})
	})

	t.Run("back-to-back code blocks", func(t *testing.T) {
		t.Parallel()
		t.Run("should handle formatting after closed code block", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "```\ncode\n```\n**bold**", Mend("```\ncode\n```\n**bold"))
		})
	})

	t.Run("confusing asterisk sequences", func(t *testing.T) {
		t.Parallel()
		t.Run("should handle four asterisks (bold-italic handler appends ***)", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "****text***", Mend("****text"))
		})
		t.Run("should handle five asterisks (bold-italic handler appends ***)", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "*****text***", Mend("*****text"))
		})
		t.Run("should handle mixed asterisk counts", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "*a**b***", Mend("*a**b"))
		})
	})

	t.Run("whitespace edge cases", func(t *testing.T) {
		t.Parallel()
		t.Run("should close bold with tabs in content", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "**bold\twith\ttabs**", Mend("**bold\twith\ttabs"))
		})
		t.Run("should close bold with CRLF", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "**bold\r\nwith CRLF**", Mend("**bold\r\nwith CRLF"))
		})
		t.Run("should close bold after many leading newlines", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "\n\n\n**bold**", Mend("\n\n\n**bold"))
		})
		t.Run("should close bold and trim trailing space", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "**bold**", Mend("**bold "))
		})
	})

	t.Run("disabled handlers via options", func(t *testing.T) {
		t.Parallel()
		t.Run("should not close bold when bold is disabled", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "**bold text", remend("**bold text", remendOptions{bold: boolPtr(false)}))
		})
		t.Run("should close italic even when bold is disabled", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "**bold *italic*", remend("**bold *italic", remendOptions{bold: boolPtr(false)}))
		})
		t.Run("should not close anything when all are disabled", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "**bold *italic `code ~~strike", remend("**bold *italic `code ~~strike", remendOptions{
				bold:          boolPtr(false),
				italic:        boolPtr(false),
				inlineCode:    boolPtr(false),
				strikethrough: boolPtr(false),
				boldItalic:    boolPtr(false),
			}))
		})
		t.Run("should not close bold when asterisk in content blocks pattern (italic disabled)", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "**bold *italic", remend("**bold *italic", remendOptions{italic: boolPtr(false)}))
		})
		t.Run("should close strikethrough but not bold when bold is disabled", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "**bold ~~strike~~", remend("**bold ~~strike", remendOptions{bold: boolPtr(false)}))
		})
	})

	t.Run("real-world AI streaming patterns", func(t *testing.T) {
		t.Parallel()
		t.Run("should handle markdown list being built with bold", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "1. First\n2. **Second item with bold**", Mend("1. First\n2. **Second item with bold"))
		})
		t.Run("should handle mixed inline code and bold", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "The function `getData` returns a **Promise**", Mend("The function `getData` returns a **Promise"))
		})
		t.Run("should handle heading with incomplete italic", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "## Important *note*", Mend("## Important *note"))
		})
		t.Run("should handle link with incomplete formatting after it", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "[click here](https://example.com) for **more**", Mend("[click here](https://example.com) for **more"))
		})
	})
}
