package remend

import (
	"strings"
	"testing"

	"github.com/shoenig/test/must"
)

func TestInlineCodeFormatting(t *testing.T) {
	t.Parallel()

	t.Run("should complete incomplete inline code", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Text with `code`", Mend("Text with `code"))
		must.Eq(t, "`incomplete`", Mend("`incomplete"))
	})

	t.Run("should keep complete inline code unchanged", func(t *testing.T) {
		t.Parallel()
		text := "Text with `inline code`"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle multiple inline code sections", func(t *testing.T) {
		t.Parallel()
		text := "`code1` and `code2`"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should not complete backticks inside code blocks", func(t *testing.T) {
		t.Parallel()
		text := "```\ncode block with `backtick\n```"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle incomplete code blocks correctly", func(t *testing.T) {
		t.Parallel()
		text := "```javascript\nconst x = `template"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle inline triple backticks correctly", func(t *testing.T) {
		t.Parallel()
		text := "```python print(\"Hello, Sunnyvale!\")```"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle incomplete inline triple backticks", func(t *testing.T) {
		t.Parallel()
		text := "```python print(\"Hello, Sunnyvale!\")``"
		must.Eq(t, "```python print(\"Hello, Sunnyvale!\")```", Mend(text))
	})

	t.Run("should not modify text with complete triple backticks at the end", func(t *testing.T) {
		t.Parallel()
		text := "```code```"
		must.Eq(t, text, Mend(text))

		text2 := "```code```\n"
		must.Eq(t, text2, Mend(text2))

		text3 := "```\ncode\n```"
		must.Eq(t, text3, Mend(text3))

		text4 := "``````"
		must.Eq(t, text4, Mend(text4))

		text5 := "text``````"
		must.Eq(t, text5, Mend(text5))
	})

	t.Run("should handle code block with incomplete inline code after (#302)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "```\nblock\n```\n`inline`", Mend("```\nblock\n```\n`inline"))
	})
}

func TestEmphasisMarkersInsideInlineCodeSpansShouldNotLeak(t *testing.T) {
	t.Parallel()

	t.Run("should not complete bold/italic/strikethrough if they are inside inline code", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "`**bold`", Mend("`**bold`"))
		must.Eq(t, "`*italic`", Mend("`*italic`"))
		must.Eq(t, "`~~strikethrough`", Mend("`~~strikethrough`"))
	})

	t.Run("should still complete emphasis markers outside inline code", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "**bold**", Mend("**bold"))
		must.Eq(t, "*italic*", Mend("*italic"))
		must.Eq(t, "~~strike~~", Mend("~~strike"))
	})

	t.Run("should complete emphasis after a closed inline code span", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "`code` **bold**", Mend("`code` **bold"))
	})
}

func TestEscapedBackticksInInlineCode(t *testing.T) {
	t.Parallel()

	t.Run("should not treat escaped backticks as code delimiters", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "\\`not code\\` **bold**", Mend("\\`not code\\` **bold"))
	})

	t.Run("should complete emphasis when only escaped backticks are present", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "\\` *italic*", Mend("\\` *italic"))
	})
}

func TestInlineCodeBrokenMarkdownRemainder(t *testing.T) {
	t.Parallel()

	t.Run("should close inline code that spans cell boundary", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "| `code | next |`", Mend("| `code | next |"))
	})

	t.Run("should close second inline code after complete code", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "`code` then `more`", Mend("`code` then `more"))
	})

	t.Run("should close inline code across paragraph", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "text\n\n`code`", Mend("text\n\n`code"))
	})

	t.Run("should close inline code with Korean text", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "`한국어 코드`", Mend("`한국어 코드"))
	})

	t.Run("should not close formatting inside open code block", func(t *testing.T) {
		t.Parallel()
		text := "```\ncode\n```\n```\nmore"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle inline code after code block", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "```\nblock\n```\n`inline`", Mend("```\nblock\n```\n`inline"))
	})

	t.Run("should handle code explanation with incomplete code block", func(t *testing.T) {
		t.Parallel()
		text := "Here's how to use it:\n\n```typescript\nconst x = 1"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle code block followed by explanation", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "```js\nconst x = 1;\n```\n\nThis creates a **variable**", Mend("```js\nconst x = 1;\n```\n\nThis creates a **variable"))
	})

	t.Run("should handle bullet list with inline code", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "- Use `map` to transform\n- Use `filter`", Mend("- Use `map` to transform\n- Use `filter"))
	})
}

func TestCodeBlockHandling(t *testing.T) {
	t.Parallel()

	t.Run("should handle incomplete multiline code blocks", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "```javascript\nconst x = 5;", Mend("```javascript\nconst x = 5;"))
		must.Eq(t, "```\ncode here", Mend("```\ncode here"))
	})

	t.Run("should handle complete multiline code blocks", func(t *testing.T) {
		t.Parallel()
		text := "```javascript\nconst x = 5;\n```"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle code blocks with language and incomplete content", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "```python\ndef hello():", Mend("```python\ndef hello():"))
	})

	t.Run("should handle nested backticks inside code blocks", func(t *testing.T) {
		t.Parallel()
		text := "```\nconst str = `template`;\n```"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle incomplete code blocks at end of chunked response", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "Some text\n```js\nconsole.log", Mend("Some text\n```js\nconsole.log"))
	})

	t.Run("should handle code blocks with trailing content", func(t *testing.T) {
		t.Parallel()
		text := "```\ncode\n```\nMore text"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle complete code blocks ending with triple backticks on newline", func(t *testing.T) {
		t.Parallel()
		text := "```python\ndef greet(name):\n    return f\"Hello, {name}!\"\n```"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should handle complete code blocks with trailing newline after closing backticks", func(t *testing.T) {
		t.Parallel()
		text := "```python\ndef greet(name):\n    return f\"Hello, {name}!\"\n```\n"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should not add extra characters to complete simple code block", func(t *testing.T) {
		t.Parallel()
		text := "```\nSimple code block\nwith multiple lines\nand some special characters: !@#$%^&*()\n```"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should not add extra characters to complete Python code block with underscores and asterisks", func(t *testing.T) {
		t.Parallel()
		text := "```python\ndef hello_world():\n    \"\"\"A simple function\"\"\"\n    name = \"World\"\n    print(f\"Hello, {name}!\")\n    \n    # List comprehension\n    numbers = [x**2 for x in range(10) if x % 2 == 0]\n    return numbers\n\nclass TestClass:\n    def __init__(self, value):\n        self.value = value\n```"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should not add backticks when code block ends properly", func(t *testing.T) {
		t.Parallel()
		grokOutput := "```python def greet(name): return f\"Hello, {name}!\"\n```"
		must.Eq(t, grokOutput, Mend(grokOutput))
	})

	t.Run("should handle multiple complete code blocks with newlines", func(t *testing.T) {
		t.Parallel()
		text := "```js\ncode1\n```\n\n```python\ncode2\n```"
		must.Eq(t, text, Mend(text))
	})

	t.Run("should correctly handle code on same line as opening backticks with closing on newline", func(t *testing.T) {
		t.Parallel()
		text := "```python def greet(name): return f\"Hello, {name}!\"\n```"
		must.Eq(t, text, Mend(text))
		must.False(t, strings.Contains(Mend(text), "````"))
	})

	t.Run("should only treat truly inline triple backticks as inline", func(t *testing.T) {
		t.Parallel()
		inline := "```python code```"
		must.Eq(t, inline, Mend(inline))
		multiline := "```python code\n```"
		must.Eq(t, multiline, Mend(multiline))
	})

	t.Run("should not treat brackets inside complete code blocks as incomplete links", func(t *testing.T) {
		t.Parallel()
		text := "Here's some code:\n\n```javascript\nconst arr = [1, 2, 3];\nconsole.log(arr[0]);\n```\nDone with code block."
		must.False(t, strings.Contains(Mend(text), "streamdown:incomplete-link"))
		must.Eq(t, text, Mend(text))
	})

	t.Run("should still detect actual incomplete links outside of code blocks", func(t *testing.T) {
		t.Parallel()
		text := "Here's a code block:\n```bash\necho \"test\"\n```\nAnd here's an [incomplete link"
		must.True(t, strings.Contains(Mend(text), "streamdown:incomplete-link"))
		must.Eq(t, "Here's a code block:\n```bash\necho \"test\"\n```\nAnd here's an [incomplete link](streamdown:incomplete-link)", Mend(text))
	})

	t.Run("should not add incomplete-link marker after complete code blocks - #227", func(t *testing.T) {
		t.Parallel()
		textContent := "Precisely.\n\nWhen full-screen TUI applications like **Vim**, **less**, or **htop** start, they switch the terminal into what's called the **alternate screen buffer**—a second, temporary display area separate from the main scrollback buffer.\n\n### How it works\nThey send ANSI escape sequences such as:\n```bash\n# Enter alternate screen buffer\necho -e \"\\\\e[?1049h\"\n\n# Exit (back to normal buffer)\necho -e \"\\\\e[?1049l\"\n```\n\n- `\\\\e[?1049h` — activates the alternate screen.\n- `\\\\e[?1049l` — deactivates it and restores the previous view.\n\nWhile in this mode:\n- The \"scrollback\" (your regular terminal history) is hidden.\n- The program gets a fresh, empty screen to draw on.\n- When the program exits, the screen restores exactly as it was before.\n\n### tmux behavior\n`tmux` respects these escape sequences by default. When apps use the alternate buffer, tmux holds that screen separately from the main one. That's why, when you scroll in tmux during Vim, you don't see your shell history—you have to leave Vim first.\n\nIf someone wants to **disable** this behavior (so the app draws on the main screen and you can scroll back freely), they can set:\n```bash\nset -g terminal-overrides 'xterm*:smcup@:rmcup@'\n```\nin their `~/.tmux.conf`, which disables use of the alternate buffer entirely.\n\nWould you like me to show how to conditionally toggle that behavior per app or session?"
		must.False(t, strings.Contains(Mend(textContent), "streamdown:incomplete-link"))
		must.Eq(t, textContent, Mend(textContent))
	})

	t.Run("should not add extra __ after code block with underscores followed by bullet list (#300)", func(t *testing.T) {
		t.Parallel()
		input := "```css\n/* Commentary */\n\n[class*=\"WidgetTitle__Header\"] {\n  font-size: 18px !important;\n}\n```\n\nNotes and tips:\n* Use !important only where necessary in CSS."
		must.Eq(t, input, Mend(input))
		must.False(t, strings.HasSuffix(Mend(input), "__"))
	})

	t.Run("should handle complete code blocks with underscores followed by asterisk list (#300)", func(t *testing.T) {
		t.Parallel()
		input := "```python\ndef __init__(self):\n    pass\n```\n\n* List item"
		must.Eq(t, input, Mend(input))
		must.False(t, strings.HasSuffix(Mend(input), "__"))
	})

	t.Run("should handle code blocks with underscores and following text with asterisks (#300)", func(t *testing.T) {
		t.Parallel()
		input := "Here's some code:\n```javascript\nconst my__variable = \"test\";\nconst another_var = 5;\n```\n\nSome notes:\n* First note\n* Second note"
		must.Eq(t, input, Mend(input))
		must.False(t, strings.HasSuffix(Mend(input), "__"))
	})

	t.Run("should not add stray * from [*] in mermaid code blocks", func(t *testing.T) {
		t.Parallel()
		input := "Here's a state diagram:\n\n```mermaid\nstateDiagram-v2\n    [*] --> Idle\n    Idle --> Loading: fetch()\n    Loading --> Success: 200 OK\n    Loading --> Error: 4xx/5xx\n    Error --> Loading: retry()\n    Success --> Idle: reset()\n```"
		must.Eq(t, input, Mend(input))
	})

	t.Run("should not add stray * from [*] in incomplete mermaid code blocks (streaming)", func(t *testing.T) {
		t.Parallel()
		input := "Here's a state diagram:\n\n```mermaid\nstateDiagram-v2\n    [*] --> Idle\n    Idle --> Loading: fetch()"
		must.Eq(t, input, Mend(input))
	})

	t.Run("should not add stray * when emphasis exists outside code block with [*] inside", func(t *testing.T) {
		t.Parallel()
		input := "*Note:* Here's a state diagram:\n\n```mermaid\nstateDiagram-v2\n    [*] --> Idle\n```"
		must.Eq(t, input, Mend(input))
	})

	t.Run("should still complete emphasis when * is only outside code blocks", func(t *testing.T) {
		t.Parallel()
		input := "```mermaid\nstateDiagram-v2\n    [*] --> Idle\n```\n\nHere is *incomplete italic"
		must.Eq(t, "```mermaid\nstateDiagram-v2\n    [*] --> Idle\n```\n\nHere is *incomplete italic*", Mend(input))
	})

	t.Run("should handle incomplete markdown after code block (#302)", func(t *testing.T) {
		t.Parallel()
		text := "```css\ncode here\n```\n\n**incomplete bold"
		must.Eq(t, "```css\ncode here\n```\n\n**incomplete bold**", Mend(text))
	})
}
