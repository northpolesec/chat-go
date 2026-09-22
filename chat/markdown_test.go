package chat

import (
	"regexp"
	"strings"
	"testing"

	"github.com/shoenig/test/must"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
)

var (
	botMentionWithWhitespace = regexp.MustCompile(`@test-bot\s+hi there`)
	listItemsWithWhitespace  = regexp.MustCompile(`one\s+two`)
)

func TestParseMarkdown(t *testing.T) {
	t.Parallel()

	t.Run("parses plain text", func(t *testing.T) {
		t.Parallel()
		doc := ParseMarkdown("Hello, world!")
		must.True(t, isDocument(doc))
		must.Eq(t, 1, doc.ChildCount())
		must.True(t, IsParagraphNode(doc.FirstChild()))
	})

	t.Run("parses bold text", func(t *testing.T) {
		t.Parallel()
		para := ParseMarkdown("**bold**").FirstChild()
		must.True(t, IsStrongNode(para.FirstChild()))
	})

	t.Run("parses italic text", func(t *testing.T) {
		t.Parallel()
		para := ParseMarkdown("_italic_").FirstChild()
		must.True(t, IsEmphasisNode(para.FirstChild()))
	})

	t.Run("parses strikethrough (GFM)", func(t *testing.T) {
		t.Parallel()
		para := ParseMarkdown("~~deleted~~").FirstChild()
		must.True(t, IsDeleteNode(para.FirstChild()))
	})

	t.Run("parses inline code", func(t *testing.T) {
		t.Parallel()
		para := ParseMarkdown("`code`").FirstChild()
		must.True(t, IsInlineCodeNode(para.FirstChild()))
	})

	t.Run("parses code blocks", func(t *testing.T) {
		t.Parallel()
		src := "```javascript\nconst x = 1;\n```"
		node := ParseMarkdown(src).FirstChild()
		must.True(t, IsCodeNode(node))
		must.Eq(t, "javascript", CodeLanguage(node, []byte(src)))
		must.Eq(t, "const x = 1;", GetNodeValue(node, []byte(src)))
	})

	t.Run("parses links", func(t *testing.T) {
		t.Parallel()
		para := ParseMarkdown("[text](https://example.com)").FirstChild()
		link, ok := para.FirstChild().(*ast.Link)
		must.True(t, ok)
		must.True(t, IsLinkNode(link))
		must.Eq(t, "https://example.com", string(link.Destination))
	})

	t.Run("parses blockquotes", func(t *testing.T) {
		t.Parallel()
		must.True(t, IsBlockquoteNode(ParseMarkdown("> quoted text").FirstChild()))
	})

	t.Run("parses unordered lists", func(t *testing.T) {
		t.Parallel()
		list, ok := ParseMarkdown("- item 1\n- item 2").FirstChild().(*ast.List)
		must.True(t, ok)
		must.True(t, IsListNode(list))
		must.False(t, list.IsOrdered())
	})

	t.Run("parses ordered lists", func(t *testing.T) {
		t.Parallel()
		list, ok := ParseMarkdown("1. first\n2. second").FirstChild().(*ast.List)
		must.True(t, ok)
		must.True(t, IsListNode(list))
		must.True(t, list.IsOrdered())
	})

	t.Run("handles nested formatting", func(t *testing.T) {
		t.Parallel()
		strong := ParseMarkdown("**_bold italic_**").FirstChild().FirstChild()
		must.True(t, IsStrongNode(strong))
		must.True(t, IsEmphasisNode(strong.FirstChild()))
	})

	t.Run("handles empty string", func(t *testing.T) {
		t.Parallel()
		doc := ParseMarkdown("")
		must.True(t, isDocument(doc))
		must.Eq(t, 0, doc.ChildCount())
	})

	t.Run("handles multiple paragraphs", func(t *testing.T) {
		t.Parallel()
		doc := ParseMarkdown("First paragraph.\n\nSecond paragraph.")
		must.Eq(t, 2, doc.ChildCount())
		must.True(t, IsParagraphNode(doc.FirstChild()))
		must.True(t, IsParagraphNode(doc.LastChild()))
	})
}

func TestStringifyMarkdown(t *testing.T) {
	t.Parallel()

	t.Run("stringifies a simple AST", func(t *testing.T) {
		t.Parallel()
		node := Root([]ast.Node{Paragraph([]ast.Node{Text("Hello")})})
		must.Eq(t, "Hello", strings.TrimSpace(StringifyMarkdown(node, nil, StringifyOptions{})))
	})

	t.Run("stringifies bold text", func(t *testing.T) {
		t.Parallel()
		node := Root([]ast.Node{Paragraph([]ast.Node{Strong([]ast.Node{Text("bold")})})})
		must.Eq(t, "**bold**", strings.TrimSpace(StringifyMarkdown(node, nil, StringifyOptions{})))
	})

	t.Run("stringifies italic text", func(t *testing.T) {
		t.Parallel()
		node := Root([]ast.Node{Paragraph([]ast.Node{Emphasis([]ast.Node{Text("italic")})})})
		must.Eq(t, "*italic*", strings.TrimSpace(StringifyMarkdown(node, nil, StringifyOptions{})))
	})

	t.Run("stringifies inline code", func(t *testing.T) {
		t.Parallel()
		node := Root([]ast.Node{Paragraph([]ast.Node{InlineCode("code")})})
		must.Eq(t, "`code`", strings.TrimSpace(StringifyMarkdown(node, nil, StringifyOptions{})))
	})

	t.Run("stringifies links", func(t *testing.T) {
		t.Parallel()
		node := Root([]ast.Node{Paragraph([]ast.Node{Link("https://example.com", []ast.Node{Text("link")})})})
		must.Eq(t, "[link](https://example.com)", strings.TrimSpace(StringifyMarkdown(node, nil, StringifyOptions{})))
	})

	t.Run("round-trips markdown correctly", func(t *testing.T) {
		t.Parallel()
		original := "**bold** and _italic_ and `code`"
		src := []byte(original)
		doc := ParseMarkdown(original)
		result := StringifyMarkdown(doc, src, StringifyOptions{})
		reparsed := ParseMarkdown(result)
		must.Eq(t, doc.ChildCount(), reparsed.ChildCount())
	})
}

func TestToPlainText(t *testing.T) {
	t.Parallel()

	t.Run("extracts plain text from AST", func(t *testing.T) {
		t.Parallel()
		src := "**bold** and _italic_"
		must.Eq(t, "bold and italic", ToPlainText(ParseMarkdown(src), []byte(src)))
	})

	t.Run("extracts text from code blocks", func(t *testing.T) {
		t.Parallel()
		src := "```\ncode block\n```"
		must.Eq(t, "code block", ToPlainText(ParseMarkdown(src), []byte(src)))
	})

	t.Run("extracts text from links", func(t *testing.T) {
		t.Parallel()
		src := "[link text](https://example.com)"
		must.Eq(t, "link text", ToPlainText(ParseMarkdown(src), []byte(src)))
	})

	t.Run("preserves soft line breaks as whitespace", func(t *testing.T) {
		t.Parallel()
		src := "@test-bot\nhi there"
		must.RegexMatch(t, botMentionWithWhitespace, ToPlainText(ParseMarkdown(src), []byte(src)))
	})

	t.Run("preserves paragraph boundaries as whitespace", func(t *testing.T) {
		t.Parallel()
		src := "@test-bot\n\nhi there"
		must.RegexMatch(t, botMentionWithWhitespace, ToPlainText(ParseMarkdown(src), []byte(src)))
	})

	t.Run("separates list items with whitespace", func(t *testing.T) {
		t.Parallel()
		src := "- one\n- two"
		must.RegexMatch(t, listItemsWithWhitespace, ToPlainText(ParseMarkdown(src), []byte(src)))
	})

	t.Run("separates table cells and rows with structural whitespace", func(t *testing.T) {
		t.Parallel()
		src := "| Name | Role |\n| --- | --- |\n| **Ada** Lovelace | Engineer |"
		must.Eq(t, "Name\tRole\nAda Lovelace\tEngineer", ToPlainText(ParseMarkdown(src), []byte(src)))
	})

	t.Run("keeps empty table cells so columns stay aligned", func(t *testing.T) {
		t.Parallel()
		src := "| Name | Middle | Units |\n| --- | --- | --- |\n| Samsung |  | 3 |"
		must.Eq(t, "Name\tMiddle\tUnits\nSamsung\t\t3", ToPlainText(ParseMarkdown(src), []byte(src)))
	})

	t.Run("drops table rows with no content", func(t *testing.T) {
		t.Parallel()
		src := "|  |  |\n| --- | --- |\n| Samsung | 3 |"
		must.Eq(t, "Samsung\t3", ToPlainText(ParseMarkdown(src), []byte(src)))
	})

	t.Run("handles empty AST", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "", ToPlainText(Root(nil), nil))
	})
}

func TestMarkdownToPlainText(t *testing.T) {
	t.Parallel()

	t.Run("converts markdown to plain text directly", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "bold and italic", MarkdownToPlainText("**bold** and _italic_"))
	})

	t.Run("handles complex markdown", func(t *testing.T) {
		t.Parallel()
		result := MarkdownToPlainText("# Heading\n\nParagraph with `code`.")
		must.StrContains(t, result, "Heading")
		must.StrContains(t, result, "Paragraph with code")
	})

	t.Run("preserves whitespace after a newline-separated mention", func(t *testing.T) {
		t.Parallel()
		must.RegexMatch(t, botMentionWithWhitespace, MarkdownToPlainText("@test-bot\nhi there"))
	})

	t.Run("preserves whitespace after a paragraph-separated mention", func(t *testing.T) {
		t.Parallel()
		must.RegexMatch(t, botMentionWithWhitespace, MarkdownToPlainText("@test-bot\n\nhi there"))
	})
}

func TestWalkAST(t *testing.T) {
	t.Parallel()

	t.Run("visits all nodes", func(t *testing.T) {
		t.Parallel()
		src := "**bold** and _italic_"
		visited := []string{}
		WalkAST(ParseMarkdown(src), func(node ast.Node) ast.Node {
			visited = append(visited, nodeTypeName(node))
			return node
		})
		must.SliceContains[string](t, visited, "paragraph")
		must.SliceContains[string](t, visited, "strong")
		must.SliceContains[string](t, visited, "emphasis")
		must.SliceContains[string](t, visited, "text")
	})

	t.Run("allows filtering nodes by returning null", func(t *testing.T) {
		t.Parallel()
		src := "**bold** and _italic_"
		filtered := WalkAST(ParseMarkdown(src), func(node ast.Node) ast.Node {
			if IsStrongNode(node) {
				return nil
			}
			return node
		})
		plain := ToPlainText(filtered, []byte(src))
		must.False(t, strings.Contains(plain, "bold"))
		must.StrContains(t, plain, "italic")
	})

	t.Run("allows transforming nodes", func(t *testing.T) {
		t.Parallel()
		transformed := WalkAST(Root([]ast.Node{Paragraph([]ast.Node{Text("hello")})}), func(node ast.Node) ast.Node {
			if IsTextNode(node) {
				return Text(strings.ToUpper(GetNodeValue(node, nil)))
			}
			return node
		})
		must.Eq(t, "HELLO", ToPlainText(transformed, nil))
	})

	t.Run("handles deeply nested structures", func(t *testing.T) {
		t.Parallel()
		types := []string{}
		WalkAST(ParseMarkdown("> **_nested_ text**"), func(node ast.Node) ast.Node {
			types = append(types, nodeTypeName(node))
			return node
		})
		must.SliceContains[string](t, types, "blockquote")
		must.SliceContains[string](t, types, "strong")
		must.SliceContains[string](t, types, "emphasis")
	})

	t.Run("handles empty AST", func(t *testing.T) {
		t.Parallel()
		visited := []string{}
		WalkAST(Root(nil), func(node ast.Node) ast.Node {
			visited = append(visited, nodeTypeName(node))
			return node
		})
		must.Eq(t, 0, len(visited))
	})
}

func TestASTBuilders(t *testing.T) {
	t.Parallel()

	t.Run("text", func(t *testing.T) {
		t.Parallel()
		t.Run("creates a text node", func(t *testing.T) {
			t.Parallel()
			node := Text("hello")
			must.True(t, IsTextNode(node))
			must.Eq(t, "hello", GetNodeValue(node, nil))
		})
		t.Run("handles empty string", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "", GetNodeValue(Text(""), nil))
		})
		t.Run("handles special characters", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, `hello & world < > "`, GetNodeValue(Text(`hello & world < > "`), nil))
		})
	})

	t.Run("strong", func(t *testing.T) {
		t.Parallel()
		t.Run("creates a strong node", func(t *testing.T) {
			t.Parallel()
			node := Strong([]ast.Node{Text("bold")})
			must.True(t, IsStrongNode(node))
			must.Eq(t, 1, node.ChildCount())
		})
		t.Run("handles nested content", func(t *testing.T) {
			t.Parallel()
			node := Strong([]ast.Node{Emphasis([]ast.Node{Text("bold italic")})})
			must.True(t, IsEmphasisNode(node.FirstChild()))
		})
	})

	t.Run("emphasis", func(t *testing.T) {
		t.Parallel()
		t.Run("creates an emphasis node", func(t *testing.T) {
			t.Parallel()
			node := Emphasis([]ast.Node{Text("italic")})
			must.True(t, IsEmphasisNode(node))
			must.Eq(t, 1, node.ChildCount())
		})
	})

	t.Run("strikethrough", func(t *testing.T) {
		t.Parallel()
		t.Run("creates a delete node", func(t *testing.T) {
			t.Parallel()
			node := Strikethrough([]ast.Node{Text("deleted")})
			must.True(t, IsDeleteNode(node))
			must.Eq(t, 1, node.ChildCount())
		})
	})

	t.Run("inlineCode", func(t *testing.T) {
		t.Parallel()
		t.Run("creates an inline code node", func(t *testing.T) {
			t.Parallel()
			node := InlineCode("const x = 1")
			must.True(t, IsInlineCodeNode(node))
			must.Eq(t, "const x = 1", GetNodeValue(node, nil))
		})
	})

	t.Run("codeBlock", func(t *testing.T) {
		t.Parallel()
		t.Run("creates a code block node", func(t *testing.T) {
			t.Parallel()
			node := CodeBlock("function() {}", "javascript")
			must.True(t, IsCodeNode(node))
			must.Eq(t, "function() {}", GetNodeValue(node, nil))
			must.Eq(t, "javascript", CodeLanguage(node, nil))
		})
		t.Run("handles missing language", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "", CodeLanguage(CodeBlock("plain code"), nil))
		})
	})

	t.Run("link", func(t *testing.T) {
		t.Parallel()
		t.Run("creates a link node", func(t *testing.T) {
			t.Parallel()
			node := Link("https://example.com", []ast.Node{Text("Example")})
			must.True(t, IsLinkNode(node))
			must.Eq(t, "https://example.com", string(node.Destination))
			must.Eq(t, 1, node.ChildCount())
		})
		t.Run("handles title", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "Title", string(Link("https://example.com", []ast.Node{Text("Example")}, "Title").Title))
		})
	})

	t.Run("blockquote", func(t *testing.T) {
		t.Parallel()
		t.Run("creates a blockquote node", func(t *testing.T) {
			t.Parallel()
			node := Blockquote([]ast.Node{Paragraph([]ast.Node{Text("quoted")})})
			must.True(t, IsBlockquoteNode(node))
			must.Eq(t, 1, node.ChildCount())
		})
	})

	t.Run("paragraph", func(t *testing.T) {
		t.Parallel()
		t.Run("creates a paragraph node", func(t *testing.T) {
			t.Parallel()
			node := Paragraph([]ast.Node{Text("content")})
			must.True(t, IsParagraphNode(node))
			must.Eq(t, 1, node.ChildCount())
		})
	})

	t.Run("root", func(t *testing.T) {
		t.Parallel()
		t.Run("creates a root node", func(t *testing.T) {
			t.Parallel()
			node := Root([]ast.Node{Paragraph([]ast.Node{Text("content")})})
			must.True(t, isDocument(node))
			must.Eq(t, 1, node.ChildCount())
		})
		t.Run("handles empty children", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, 0, Root(nil).ChildCount())
		})
	})
}

func TestParseMarkdownTables(t *testing.T) {
	t.Parallel()

	t.Run("parses GFM tables", func(t *testing.T) {
		t.Parallel()
		must.True(t, IsTableNode(ParseMarkdown("| A | B |\n|---|---|\n| 1 | 2 |").FirstChild()))
	})

	t.Run("parses table with multiple rows", func(t *testing.T) {
		t.Parallel()
		md := "| Name | Age |\n|------|-----|\n| Alice | 30 |\n| Bob | 25 |"
		table := ParseMarkdown(md).FirstChild()
		must.True(t, IsTableNode(table))
		must.Eq(t, 3, table.ChildCount()) // header + 2 data rows
	})
}

func TestTableTypeGuards(t *testing.T) {
	t.Parallel()

	t.Run("isTableNode identifies table nodes", func(t *testing.T) {
		t.Parallel()
		table := ParseMarkdown("| A | B |\n|---|---|\n| 1 | 2 |").FirstChild()
		must.True(t, IsTableNode(table))
		must.False(t, IsTableNode(Paragraph(nil)))
	})

	t.Run("isTableRowNode identifies table row nodes", func(t *testing.T) {
		t.Parallel()
		table := ParseMarkdown("| A | B |\n|---|---|\n| 1 | 2 |").FirstChild()
		must.True(t, IsTableRowNode(table.FirstChild()))
	})

	t.Run("isTableCellNode identifies table cell nodes", func(t *testing.T) {
		t.Parallel()
		table := ParseMarkdown("| A | B |\n|---|---|\n| 1 | 2 |").FirstChild()
		must.True(t, IsTableCellNode(table.FirstChild().FirstChild()))
	})
}

func TestTableToASCII(t *testing.T) {
	t.Parallel()

	t.Run("renders a simple table", func(t *testing.T) {
		t.Parallel()
		src := "| A | B |\n|---|---|\n| 1 | 2 |"
		result := TableToASCII(ParseMarkdown(src).FirstChild(), []byte(src))
		must.StrContains(t, result, "A")
		must.StrContains(t, result, "B")
		must.StrContains(t, result, "1")
		must.StrContains(t, result, "2")
		must.StrContains(t, result, "-|")
	})

	t.Run("pads columns to equal width", func(t *testing.T) {
		t.Parallel()
		src := "| Name | Age |\n|------|-----|\n| Alice | 30 |\n| Bob | 25 |"
		lines := strings.Split(TableToASCII(ParseMarkdown(src).FirstChild(), []byte(src)), "\n")
		must.Eq(t, "Name  | Age", lines[0])
		must.Eq(t, "------|----", lines[1])
		must.Eq(t, "Alice | 30", lines[2])
		must.Eq(t, "Bob   | 25", lines[3])
	})

	t.Run("handles empty table", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "", TableToASCII(east.NewTable(), nil))
	})
}

func TestTableElementToASCII(t *testing.T) {
	t.Parallel()

	t.Run("renders headers and rows", func(t *testing.T) {
		t.Parallel()
		result := TableElementToASCII(
			[]string{"Name", "Age"},
			[][]string{{"Alice", "30"}, {"Bob", "25"}},
		)
		lines := strings.Split(result, "\n")
		must.Eq(t, 4, len(lines))
		must.StrContains(t, lines[0], "Name")
		must.StrContains(t, lines[0], "Age")
		must.StrContains(t, lines[1], "---")
		must.StrContains(t, lines[2], "Alice")
		must.StrContains(t, lines[3], "Bob")
	})

	t.Run("pads columns correctly", func(t *testing.T) {
		t.Parallel()
		lines := strings.Split(TableElementToASCII(
			[]string{"Name", "Age", "Role"},
			[][]string{{"Alice", "30", "Engineer"}, {"Bob", "25", "Designer"}},
		), "\n")
		must.Eq(t, "Name  | Age | Role", lines[0])
		must.Eq(t, "Alice | 30  | Engineer", lines[2])
		must.Eq(t, "Bob   | 25  | Designer", lines[3])
	})
}

func TestTypeGuards(t *testing.T) {
	t.Parallel()

	t.Run("isTextNode", func(t *testing.T) {
		t.Parallel()
		t.Run("returns true for text nodes", func(t *testing.T) {
			t.Parallel()
			must.True(t, IsTextNode(Text("hello")))
		})
		t.Run("returns false for non-text nodes", func(t *testing.T) {
			t.Parallel()
			must.False(t, IsTextNode(Paragraph(nil)))
		})
	})

	t.Run("isParagraphNode", func(t *testing.T) {
		t.Parallel()
		t.Run("returns true for paragraph nodes", func(t *testing.T) {
			t.Parallel()
			must.True(t, IsParagraphNode(Paragraph(nil)))
		})
		t.Run("returns false for non-paragraph nodes", func(t *testing.T) {
			t.Parallel()
			must.False(t, IsParagraphNode(Text("hello")))
		})
	})

	t.Run("isStrongNode", func(t *testing.T) {
		t.Parallel()
		t.Run("returns true for strong nodes", func(t *testing.T) {
			t.Parallel()
			must.True(t, IsStrongNode(Strong([]ast.Node{Text("bold")})))
		})
		t.Run("returns false for non-strong nodes", func(t *testing.T) {
			t.Parallel()
			must.False(t, IsStrongNode(Emphasis([]ast.Node{Text("italic")})))
		})
	})

	t.Run("isEmphasisNode", func(t *testing.T) {
		t.Parallel()
		t.Run("returns true for emphasis nodes", func(t *testing.T) {
			t.Parallel()
			must.True(t, IsEmphasisNode(Emphasis([]ast.Node{Text("italic")})))
		})
		t.Run("returns false for non-emphasis nodes", func(t *testing.T) {
			t.Parallel()
			must.False(t, IsEmphasisNode(Text("hello")))
		})
	})

	t.Run("isDeleteNode", func(t *testing.T) {
		t.Parallel()
		t.Run("returns true for delete (strikethrough) nodes", func(t *testing.T) {
			t.Parallel()
			must.True(t, IsDeleteNode(Strikethrough([]ast.Node{Text("deleted")})))
		})
		t.Run("returns false for non-delete nodes", func(t *testing.T) {
			t.Parallel()
			must.False(t, IsDeleteNode(Text("hello")))
		})
	})

	t.Run("isInlineCodeNode", func(t *testing.T) {
		t.Parallel()
		t.Run("returns true for inline code nodes", func(t *testing.T) {
			t.Parallel()
			must.True(t, IsInlineCodeNode(InlineCode("code")))
		})
		t.Run("returns false for non-inline-code nodes", func(t *testing.T) {
			t.Parallel()
			must.False(t, IsInlineCodeNode(CodeBlock("block code")))
		})
	})

	t.Run("isCodeNode", func(t *testing.T) {
		t.Parallel()
		t.Run("returns true for code block nodes", func(t *testing.T) {
			t.Parallel()
			must.True(t, IsCodeNode(CodeBlock("const x = 1")))
		})
		t.Run("returns false for inline code nodes", func(t *testing.T) {
			t.Parallel()
			must.False(t, IsCodeNode(InlineCode("code")))
		})
	})

	t.Run("isLinkNode", func(t *testing.T) {
		t.Parallel()
		t.Run("returns true for link nodes", func(t *testing.T) {
			t.Parallel()
			must.True(t, IsLinkNode(Link("https://example.com", []ast.Node{Text("link")})))
		})
		t.Run("returns false for non-link nodes", func(t *testing.T) {
			t.Parallel()
			must.False(t, IsLinkNode(Text("hello")))
		})
	})

	t.Run("isBlockquoteNode", func(t *testing.T) {
		t.Parallel()
		t.Run("returns true for blockquote nodes", func(t *testing.T) {
			t.Parallel()
			must.True(t, IsBlockquoteNode(Blockquote([]ast.Node{Paragraph([]ast.Node{Text("quoted")})})))
		})
		t.Run("returns false for non-blockquote nodes", func(t *testing.T) {
			t.Parallel()
			must.False(t, IsBlockquoteNode(Text("hello")))
		})
	})

	t.Run("isListNode", func(t *testing.T) {
		t.Parallel()
		t.Run("returns true for list nodes", func(t *testing.T) {
			t.Parallel()
			must.True(t, IsListNode(ParseMarkdown("- item 1\n- item 2").FirstChild()))
		})
		t.Run("returns false for non-list nodes", func(t *testing.T) {
			t.Parallel()
			must.False(t, IsListNode(Text("hello")))
		})
	})

	t.Run("isListItemNode", func(t *testing.T) {
		t.Parallel()
		t.Run("returns true for list item nodes", func(t *testing.T) {
			t.Parallel()
			must.True(t, IsListItemNode(ParseMarkdown("- item 1").FirstChild().FirstChild()))
		})
		t.Run("returns false for non-list-item nodes", func(t *testing.T) {
			t.Parallel()
			must.False(t, IsListItemNode(Text("hello")))
		})
	})
}

func TestGetNodeChildren(t *testing.T) {
	t.Parallel()

	t.Run("returns children for paragraph node", func(t *testing.T) {
		t.Parallel()
		children := GetNodeChildren(Paragraph([]ast.Node{Text("hello"), Text(" world")}))
		must.Eq(t, 2, len(children))
		must.Eq(t, "hello", GetNodeValue(children[0], nil))
	})

	t.Run("returns children for strong node", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, 1, len(GetNodeChildren(Strong([]ast.Node{Text("bold")}))))
	})

	t.Run("returns empty array for text node (no children)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, 0, len(GetNodeChildren(Text("hello"))))
	})

	t.Run("returns empty array for inline code node (no children)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, 0, len(GetNodeChildren(InlineCode("code"))))
	})

	t.Run("returns empty array for code block node (no children)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, 0, len(GetNodeChildren(CodeBlock("code", "js"))))
	})

	t.Run("returns children for blockquote node", func(t *testing.T) {
		t.Parallel()
		children := GetNodeChildren(Blockquote([]ast.Node{Paragraph([]ast.Node{Text("quoted")})}))
		must.Eq(t, 1, len(children))
		must.True(t, IsParagraphNode(children[0]))
	})

	t.Run("returns children for emphasis node", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, 1, len(GetNodeChildren(Emphasis([]ast.Node{Text("italic")}))))
	})

	t.Run("returns children for link node", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, 1, len(GetNodeChildren(Link("https://example.com", []ast.Node{Text("link")}))))
	})
}

func TestGetNodeValue(t *testing.T) {
	t.Parallel()

	t.Run("returns value for text node", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "hello", GetNodeValue(Text("hello"), nil))
	})

	t.Run("returns value for inline code node", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "const x = 1", GetNodeValue(InlineCode("const x = 1"), nil))
	})

	t.Run("returns value for code block node", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "function() {}", GetNodeValue(CodeBlock("function() {}"), nil))
	})

	t.Run("returns empty string for paragraph node (no value)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "", GetNodeValue(Paragraph([]ast.Node{Text("hello")}), nil))
	})

	t.Run("returns empty string for strong node (no value)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "", GetNodeValue(Strong([]ast.Node{Text("bold")}), nil))
	})

	t.Run("returns empty string for emphasis node (no value)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "", GetNodeValue(Emphasis([]ast.Node{Text("italic")}), nil))
	})

	t.Run("returns empty string for blockquote (no value)", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "", GetNodeValue(Blockquote([]ast.Node{Paragraph([]ast.Node{Text("quoted")})}), nil))
	})

	t.Run("returns value for text with empty string", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "", GetNodeValue(Text(""), nil))
	})
}

func TestParseMarkdownEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("handles markdown with only whitespace", func(t *testing.T) {
		t.Parallel()
		doc := ParseMarkdown("   ")
		must.True(t, isDocument(doc))
		must.True(t, doc.ChildCount() >= 0)
	})

	t.Run("handles markdown with special characters", func(t *testing.T) {
		t.Parallel()
		src := `Hello <world> & "quotes"`
		doc := ParseMarkdown(src)
		must.True(t, isDocument(doc))
		must.StrContains(t, ToPlainText(doc, []byte(src)), "Hello")
	})

	t.Run("handles very long markdown input", func(t *testing.T) {
		t.Parallel()
		doc := ParseMarkdown(strings.Repeat("word ", 1000))
		must.True(t, isDocument(doc))
		must.True(t, doc.ChildCount() > 0)
	})

	t.Run("handles markdown with mixed heading levels", func(t *testing.T) {
		t.Parallel()
		doc := ParseMarkdown("# H1\n## H2\n### H3")
		must.Eq(t, 3, doc.ChildCount())
		must.Eq(t, ast.KindHeading, doc.FirstChild().Kind())
		must.Eq(t, ast.KindHeading, doc.FirstChild().NextSibling().Kind())
		must.Eq(t, ast.KindHeading, doc.LastChild().Kind())
	})

	t.Run("handles markdown with thematic break (hr)", func(t *testing.T) {
		t.Parallel()
		doc := ParseMarkdown("before\n\n---\n\nafter")
		must.True(t, doc.ChildCount() >= 3)
		found := false
		for c := doc.FirstChild(); c != nil; c = c.NextSibling() {
			if c.Kind() == ast.KindThematicBreak {
				found = true
				break
			}
		}
		must.True(t, found)
	})
}

func isDocument(n ast.Node) bool {
	_, ok := n.(*ast.Document)
	return ok
}

func nodeTypeName(n ast.Node) string {
	switch {
	case IsTextNode(n):
		return "text"
	case IsParagraphNode(n):
		return "paragraph"
	case IsStrongNode(n):
		return "strong"
	case IsEmphasisNode(n):
		return "emphasis"
	case IsDeleteNode(n):
		return "delete"
	case IsInlineCodeNode(n):
		return "inlineCode"
	case IsCodeNode(n):
		return "code"
	case IsLinkNode(n):
		return "link"
	case IsBlockquoteNode(n):
		return "blockquote"
	case IsListNode(n):
		return "list"
	case IsListItemNode(n):
		return "listItem"
	case IsTableNode(n):
		return "table"
	case IsTableRowNode(n):
		return "tableRow"
	case IsTableCellNode(n):
		return "tableCell"
	case n.Kind() == ast.KindDocument:
		return "root"
	case n.Kind() == ast.KindHeading:
		return "heading"
	case n.Kind() == ast.KindThematicBreak:
		return "thematicBreak"
	default:
		return n.Kind().String()
	}
}

var (
	_ FormatConverter   = (*BaseFormatConverter)(nil)
	_ MarkdownConverter = (*BaseFormatConverter)(nil)
)

type testFormatHooks struct{}

func (testFormatHooks) FromAst(node ast.Node, src []byte) string {
	return ToPlainText(node, src)
}

func (testFormatHooks) ToAst(platformText string) ast.Node {
	return ParseMarkdown(platformText)
}

func newTestConverter() *BaseFormatConverter {
	return &BaseFormatConverter{Hooks: testFormatHooks{}}
}

type nodeConverterHooks struct {
	base *BaseFormatConverter
}

func (h nodeConverterHooks) FromAst(node ast.Node, src []byte) string {
	return h.base.FromAstWithNodeConverter(node, src, func(n ast.Node) string {
		if IsParagraphNode(n) {
			return "[para:" + ToPlainText(n, src) + "]"
		}
		return ToPlainText(n, src)
	})
}

func (h nodeConverterHooks) ToAst(inputText string) ast.Node {
	return ParseMarkdown(inputText)
}

func TestBaseFormatConverter(t *testing.T) {
	t.Parallel()
	converter := newTestConverter()

	t.Run("extractPlainText", func(t *testing.T) {
		t.Parallel()
		t.Run("extracts plain text from platform format", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "bold text", converter.ExtractPlainText("**bold** text"))
		})
	})

	t.Run("fromMarkdown", func(t *testing.T) {
		t.Parallel()
		t.Run("converts markdown to platform format", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "bold", converter.FromMarkdown("**bold**"))
		})
	})

	t.Run("toMarkdown", func(t *testing.T) {
		t.Parallel()
		t.Run("converts platform format to markdown", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "plain text", strings.TrimSpace(converter.ToMarkdown("plain text")))
		})
	})

	t.Run("renderPostable", func(t *testing.T) {
		t.Parallel()

		t.Run("handles string input", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "plain string", converter.RenderPostable("plain string"))
		})

		t.Run("handles raw message", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "raw text", converter.RenderPostable(PostableRaw{Raw: "raw text"}))
		})

		t.Run("handles markdown message", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "bold", converter.RenderPostable(PostableMarkdown{Markdown: "**bold**"}))
		})

		t.Run("handles AST message", func(t *testing.T) {
			t.Parallel()
			node := Root([]ast.Node{Paragraph([]ast.Node{Text("from ast")})})
			must.Eq(t, "from ast", converter.RenderPostable(PostableAst{AST: node}))
		})

		t.Run("handles card with fallback text", func(t *testing.T) {
			t.Parallel()
			card := Card{Title: "Title", Type: "card", Children: []any{CardText("Content")}}
			must.Eq(t, "Custom fallback", converter.RenderPostable(PostableCard{
				Card:         card,
				FallbackText: "Custom fallback",
			}))
		})

		t.Run("generates fallback text from card", func(t *testing.T) {
			t.Parallel()
			card := Card{
				Title:    "Order Status",
				Subtitle: "Your order details",
				Type:     "card",
				Children: []any{CardText("Processing your order...")},
			}
			result := converter.RenderPostable(PostableCard{Card: card})
			must.StrContains(t, result, "Order Status")
			must.StrContains(t, result, "Your order details")
			must.StrContains(t, result, "Processing your order...")
		})

		t.Run("handles card with actions", func(t *testing.T) {
			t.Parallel()
			card := Card{
				Title: "Confirm",
				Type:  "card",
				Children: []any{
					Actions(Button("yes", "Yes"), Button("no", "No")),
				},
			}
			result := converter.RenderPostable(PostableCard{Card: card})
			must.StrContains(t, result, "Confirm")
			must.False(t, strings.Contains(result, "[Yes]"))
			must.False(t, strings.Contains(result, "[No]"))
		})

		t.Run("handles card with fields", func(t *testing.T) {
			t.Parallel()
			card := Card{
				Type: "card",
				Children: []any{
					Fields(Field("Name", "John"), Field("Email", "john@example.com")),
				},
			}
			result := converter.RenderPostable(PostableCard{Card: card})
			must.StrContains(t, result, "**Name**: John")
			must.StrContains(t, result, "**Email**: john@example.com")
		})

		t.Run("handles direct CardElement", func(t *testing.T) {
			t.Parallel()
			must.StrContains(t, converter.RenderPostable(Card{Title: "Direct Card", Type: "card"}), "Direct Card")
		})

		t.Run("throws on invalid input", func(t *testing.T) {
			t.Parallel()
			must.Panic(t, func() {
				converter.RenderPostable(struct{ invalid bool }{invalid: true})
			})
		})

		t.Run("handles card with table element", func(t *testing.T) {
			t.Parallel()
			card := Card{
				Type: "card",
				Children: []any{
					CardTable([]string{"Name", "Age"}, [][]string{{"Alice", "30"}, {"Bob", "25"}}),
				},
			}
			result := converter.RenderPostable(PostableCard{Card: card})
			must.StrContains(t, result, "Name")
			must.StrContains(t, result, "Age")
			must.StrContains(t, result, "Alice")
			must.StrContains(t, result, "30")
		})
	})

	t.Run("deprecated toPlainText method", func(t *testing.T) {
		t.Parallel()
		t.Run("extracts plain text from platform format", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "bold text", converter.ToPlainText("**bold** text"))
		})
	})

	t.Run("fromAstWithNodeConverter", func(t *testing.T) {
		t.Parallel()
		nodeConverter := &BaseFormatConverter{}
		nodeConverter.Hooks = nodeConverterHooks{base: nodeConverter}

		t.Run("joins multiple paragraphs with double newlines", func(t *testing.T) {
			t.Parallel()
			node := Root([]ast.Node{
				Paragraph([]ast.Node{Text("First")}),
				Paragraph([]ast.Node{Text("Second")}),
			})
			must.Eq(t, "[para:First]\n\n[para:Second]", nodeConverter.FromAst(node, nil))
		})

		t.Run("handles single paragraph", func(t *testing.T) {
			t.Parallel()
			node := Root([]ast.Node{Paragraph([]ast.Node{Text("Only")})})
			must.Eq(t, "[para:Only]", nodeConverter.FromAst(node, nil))
		})

		t.Run("handles empty AST", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "", nodeConverter.FromAst(Root(nil), nil))
		})
	})

	t.Run("cardToFallbackText via renderPostable", func(t *testing.T) {
		t.Parallel()

		t.Run("handles card with section children", func(t *testing.T) {
			t.Parallel()
			card := Card{
				Type: "card",
				Children: []any{
					Section(CardText("Section content"), CardText("More content")),
				},
			}
			result := converter.RenderPostable(PostableCard{Card: card})
			must.StrContains(t, result, "Section content")
			must.StrContains(t, result, "More content")
		})

		t.Run("handles card with only title (no children)", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "**Title Only**", converter.RenderPostable(PostableCard{
				Card: Card{Title: "Title Only", Type: "card"},
			}))
		})

		t.Run("handles card with divider child (returns null for divider)", func(t *testing.T) {
			t.Parallel()
			card := Card{
				Title:    "With Divider",
				Type:     "card",
				Children: []any{Divider()},
			}
			must.Eq(t, "**With Divider**", converter.RenderPostable(PostableCard{Card: card}))
		})

		t.Run("handles card with mixed children including actions (excluded)", func(t *testing.T) {
			t.Parallel()
			card := Card{
				Title: "Mixed",
				Type:  "card",
				Children: []any{
					CardText("Visible text"),
					Actions(Button("ok", "OK")),
					Fields(Field("Key", "Val")),
				},
			}
			result := converter.RenderPostable(PostableCard{Card: card})
			must.StrContains(t, result, "Visible text")
			must.False(t, strings.Contains(result, "OK"))
			must.StrContains(t, result, "**Key**: Val")
		})
	})

	t.Run("fromAstWithNodeConverter", func(t *testing.T) {
		t.Parallel()
		t.Run("joins multiple paragraphs with double newlines", func(t *testing.T) {
			t.Parallel()
			node := Root([]ast.Node{
				Paragraph([]ast.Node{Text("First")}),
				Paragraph([]ast.Node{Text("Second")}),
				Paragraph([]ast.Node{Text("Third")}),
			})
			result := converter.FromAst(node, nil)
			must.StrContains(t, result, "First")
			must.StrContains(t, result, "Second")
			must.StrContains(t, result, "Third")
		})
	})
}
