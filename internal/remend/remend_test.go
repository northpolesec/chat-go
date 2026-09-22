package remend

import (
	"regexp"
	"strings"
	"testing"

	"github.com/shoenig/test/must"
)

func TestBasicInputHandling(t *testing.T) {
	t.Parallel()

	t.Run("should return empty string unchanged", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "", Mend(""))
	})

	t.Run("should return regular text unchanged", func(t *testing.T) {
		t.Parallel()
		text := "This is plain text without any markdown"
		must.Eq(t, text, Mend(text))
	})
}

func TestEmptyStringThroughHandlerPipeline(t *testing.T) {
	t.Parallel()

	t.Run("should handle single space input", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "", Mend(" "))
	})
}

func TestWhitespaceEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("should trim trailing single space", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "text", Mend("text "))
	})

	t.Run("should preserve trailing double space", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "text  ", Mend("text  "))
	})
}

func TestCustomHandlers(t *testing.T) {
	t.Parallel()

	t.Run("should execute custom handlers", func(t *testing.T) {
		t.Parallel()
		fooBar := regexp.MustCompile("foo")
		handler := customHandler{
			name: "test",
			handle: func(text string) string {
				return fooBar.ReplaceAllString(text, "bar")
			},
		}
		must.Eq(t, "bar", remend("foo", remendOptions{handlers: []customHandler{handler}}))
	})

	t.Run("should execute custom handlers after built-in handlers by default", func(t *testing.T) {
		t.Parallel()
		handler := customHandler{
			name: "test",
			handle: func(text string) string {
				return text + "!"
			},
		}
		must.Eq(t, "**bold**!", remend("**bold", remendOptions{handlers: []customHandler{handler}}))
	})

	t.Run("should handle empty handlers array", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "**bold**", remend("**bold", remendOptions{handlers: []customHandler{}}))
	})

	t.Run("should respect custom handler priority", func(t *testing.T) {
		t.Parallel()
		var results []string
		lowPriority := customHandler{
			name:     "low",
			priority: intPtr(200),
			handle: func(text string) string {
				results = append(results, "low")
				return text
			},
		}
		highPriority := customHandler{
			name:     "high",
			priority: intPtr(5),
			handle: func(text string) string {
				results = append(results, "high")
				return text
			},
		}
		remend("test", remendOptions{handlers: []customHandler{lowPriority, highPriority}})
		must.Eq(t, []string{"high", "low"}, results)
	})

	t.Run("should allow custom handlers to run before built-ins", func(t *testing.T) {
		t.Parallel()
		var results []string
		beforeSetext := customHandler{
			name:     "beforeSetext",
			priority: intPtr(-1),
			handle: func(text string) string {
				results = append(results, "custom")
				return text
			},
		}
		remend("test\n-", remendOptions{handlers: []customHandler{beforeSetext}})
		must.Eq(t, "custom", results[0])
	})

	t.Run("should handle multiple custom handlers", func(t *testing.T) {
		t.Parallel()
		handler1 := customHandler{
			name: "replace-a",
			handle: func(text string) string {
				return regexp.MustCompile("a").ReplaceAllString(text, "b")
			},
		}
		handler2 := customHandler{
			name: "replace-b",
			handle: func(text string) string {
				return regexp.MustCompile("b").ReplaceAllString(text, "c")
			},
		}
		must.Eq(t, "ccc", remend("aaa", remendOptions{handlers: []customHandler{handler1, handler2}}))
	})

	t.Run("should handle custom handlers with same priority in order", func(t *testing.T) {
		t.Parallel()
		var results []string
		first := customHandler{
			name:     "first",
			priority: intPtr(100),
			handle: func(text string) string {
				results = append(results, "first")
				return text
			},
		}
		second := customHandler{
			name:     "second",
			priority: intPtr(100),
			handle: func(text string) string {
				results = append(results, "second")
				return text
			},
		}
		remend("test", remendOptions{handlers: []customHandler{first, second}})
		must.Eq(t, []string{"first", "second"}, results)
	})

	t.Run("should work with disabled built-in handlers", func(t *testing.T) {
		t.Parallel()
		handler := customHandler{
			name: "test",
			handle: func(text string) string {
				return text + "!"
			},
		}
		must.Eq(t, "**bold!", remend("**bold", remendOptions{bold: boolPtr(false), handlers: []customHandler{handler}}))
	})

	t.Run("should work with no built-in handlers enabled", func(t *testing.T) {
		t.Parallel()
		handler := customHandler{
			name: "uppercase",
			handle: func(text string) string {
				return strings.ToUpper(text)
			},
		}
		must.Eq(t, "HELLO", remend("hello", remendOptions{
			bold:           boolPtr(false),
			italic:         boolPtr(false),
			boldItalic:     boolPtr(false),
			inlineCode:     boolPtr(false),
			strikethrough:  boolPtr(false),
			katex:          boolPtr(false),
			links:          boolPtr(false),
			images:         boolPtr(false),
			setextHeadings: boolPtr(false),
			handlers:       []customHandler{handler},
		}))
	})
}

func TestExportedUtilities(t *testing.T) {
	t.Parallel()

	t.Run("isWithinCodeBlock", func(t *testing.T) {
		t.Parallel()
		t.Run("should detect position inside code block", func(t *testing.T) {
			t.Parallel()
			text := "```\ncode\n```"
			must.True(t, isWithinCodeBlock(text, 5))
		})
		t.Run("should detect position outside code block", func(t *testing.T) {
			t.Parallel()
			text := "before ```code``` after"
			must.False(t, isWithinCodeBlock(text, 2))
		})
	})

	t.Run("isWithinMathBlock", func(t *testing.T) {
		t.Parallel()
		t.Run("should detect position inside block math", func(t *testing.T) {
			t.Parallel()
			text := "$$x^2$$"
			must.True(t, isWithinMathBlock(text, 3))
		})
		t.Run("should detect position outside math", func(t *testing.T) {
			t.Parallel()
			text := "before $x$ after"
			must.False(t, isWithinMathBlock(text, 14))
		})
	})

	t.Run("isWithinLinkOrImageUrl", func(t *testing.T) {
		t.Parallel()
		t.Run("should detect position inside link URL", func(t *testing.T) {
			t.Parallel()
			text := "[text](http://example.com)"
			must.True(t, isWithinLinkOrImageURL(text, 10))
		})
		t.Run("should detect position outside link", func(t *testing.T) {
			t.Parallel()
			text := "before [text](url) after"
			must.False(t, isWithinLinkOrImageURL(text, 2))
		})
	})

	t.Run("isWordChar", func(t *testing.T) {
		t.Parallel()
		t.Run("should identify word characters", func(t *testing.T) {
			t.Parallel()
			must.True(t, isWordChar("a"))
			must.True(t, isWordChar("Z"))
			must.True(t, isWordChar("5"))
			must.True(t, isWordChar("_"))
		})
		t.Run("should identify non-word characters", func(t *testing.T) {
			t.Parallel()
			must.False(t, isWordChar(" "))
			must.False(t, isWordChar("*"))
			must.False(t, isWordChar(""))
		})
	})
}

func TestCustomHandlerExampleJokeMarker(t *testing.T) {
	t.Parallel()
	jokeMarkerPattern := regexp.MustCompile(`<<<JOKE>>>([^<]*)$`)

	t.Run("should complete joke markers", func(t *testing.T) {
		t.Parallel()
		jokeHandler := customHandler{
			name:     "joke",
			priority: intPtr(80),
			handle: func(text string) string {
				if jokeMarkerPattern.MatchString(text) && !strings.HasSuffix(text, "<<</JOKE>>>") {
					return text + "<<</JOKE>>>"
				}
				return text
			},
		}
		must.Eq(t, "<<<JOKE>>>Why did the chicken<<</JOKE>>>", remend("<<<JOKE>>>Why did the chicken", remendOptions{handlers: []customHandler{jokeHandler}}))
	})

	t.Run("should not double-complete joke markers", func(t *testing.T) {
		t.Parallel()
		jokeHandler := customHandler{
			name:     "joke",
			priority: intPtr(80),
			handle: func(text string) string {
				if jokeMarkerPattern.MatchString(text) && !strings.HasSuffix(text, "<<</JOKE>>>") {
					return text + "<<</JOKE>>>"
				}
				return text
			},
		}
		must.Eq(t, "<<<JOKE>>>complete<<</JOKE>>>", remend("<<<JOKE>>>complete<<</JOKE>>>", remendOptions{handlers: []customHandler{jokeHandler}}))
	})
}

// Permanently deferred: basic-input.test.ts / should return non-string inputs
// unchanged — Go Mend takes string; remend(null)/undefined/123 cannot be expressed.
