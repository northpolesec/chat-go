// Ported from packages/chat/src/markdown.ts @ 6adca36 (chat v4.40.0).
// Divergences: remark/mdast → goldmark AST (extension.GFM + extension.Strikethrough);
// helpers that read node text take source []byte (goldmark segments);
// goldmark Emphasis.Level>=2 is strong / Level==1 is emphasis;
// constructed text is *ast.String (owned bytes, not a source segment);
// constructed FencedCodeBlock language is a node attribute (Language() needs source);
// GetNodeChildren hides CodeSpan/FencedCodeBlock/CodeBlock children to match mdast
// value-nodes (adapters use GetNodeValue); IsTableRowNode accepts TableHeader;
// ToPlainText trims the trailing newline goldmark stores on fenced code lines;
// GetNodeValue/ToPlainText unescape CommonMark backslash-punctuations on non-raw
// Text (mdast stores unescaped values); StringifyMarkdown still emits the raw
// segment so escapes survive; also emits strikethrough, GFM tables, and autolinks
// (Task 16).
package chat

import (
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	gmutil "github.com/yuin/goldmark/util"
)

const constructedLangAttr = "chat-lang"

var md = goldmark.New(goldmark.WithExtensions(extension.GFM, extension.Strikethrough))

// ParseMarkdown parses src into a goldmark document (GFM + strikethrough).
func ParseMarkdown(src string) ast.Node {
	return md.Parser().Parse(text.NewReader([]byte(src)))
}

func IsTextNode(node ast.Node) bool {
	switch node.(type) {
	case *ast.Text, *ast.String:
		return true
	default:
		return false
	}
}

func IsParagraphNode(node ast.Node) bool {
	_, ok := node.(*ast.Paragraph)
	return ok
}

func IsStrongNode(node ast.Node) bool {
	e, ok := node.(*ast.Emphasis)
	return ok && e.Level >= 2
}

func IsEmphasisNode(node ast.Node) bool {
	e, ok := node.(*ast.Emphasis)
	return ok && e.Level == 1
}

func IsDeleteNode(node ast.Node) bool {
	_, ok := node.(*east.Strikethrough)
	return ok
}

func IsInlineCodeNode(node ast.Node) bool {
	_, ok := node.(*ast.CodeSpan)
	return ok
}

func IsCodeNode(node ast.Node) bool {
	switch node.(type) {
	case *ast.FencedCodeBlock, *ast.CodeBlock:
		return true
	default:
		return false
	}
}

func IsLinkNode(node ast.Node) bool {
	_, ok := node.(*ast.Link)
	return ok
}

func IsBlockquoteNode(node ast.Node) bool {
	_, ok := node.(*ast.Blockquote)
	return ok
}

func IsListNode(node ast.Node) bool {
	_, ok := node.(*ast.List)
	return ok
}

func IsListItemNode(node ast.Node) bool {
	_, ok := node.(*ast.ListItem)
	return ok
}

func IsTableNode(node ast.Node) bool {
	_, ok := node.(*east.Table)
	return ok
}

func IsTableRowNode(node ast.Node) bool {
	switch node.(type) {
	case *east.TableRow, *east.TableHeader:
		return true
	default:
		return false
	}
}

func IsTableCellNode(node ast.Node) bool {
	_, ok := node.(*east.TableCell)
	return ok
}

func isValueNode(node ast.Node) bool {
	switch node.(type) {
	case *ast.Text, *ast.String, *ast.CodeSpan, *ast.FencedCodeBlock, *ast.CodeBlock:
		return true
	default:
		return false
	}
}

func collectChildren(node ast.Node) []ast.Node {
	if node == nil {
		return nil
	}
	out := make([]ast.Node, 0, node.ChildCount())
	for c := node.FirstChild(); c != nil; c = c.NextSibling() {
		out = append(out, c)
	}
	return out
}

// GetNodeChildren returns container children. Value nodes (text, code) are empty,
// matching mdast (those types store a value, not children).
func GetNodeChildren(node ast.Node) []ast.Node {
	if node == nil || isValueNode(node) {
		return nil
	}
	return collectChildren(node)
}

func textValue(t *ast.Text, src []byte) string {
	v := t.Value(src)
	if !t.IsRaw() {
		v = gmutil.UnescapePunctuations(v)
	}
	return string(v)
}

func leafText(node ast.Node, src []byte) string {
	var b strings.Builder
	for c := node.FirstChild(); c != nil; c = c.NextSibling() {
		switch t := c.(type) {
		case *ast.Text:
			b.Write(t.Value(src))
		case *ast.String:
			b.Write(t.Value)
		}
	}
	return b.String()
}

func linesValue(node ast.Node, src []byte) string {
	if node.Lines().Len() == 0 {
		return ""
	}
	return strings.TrimRight(string(node.Lines().Value(src)), "\n")
}

// GetNodeValue returns the mdast-style value (text/code). Empty for containers.
func GetNodeValue(node ast.Node, src []byte) string {
	if node == nil {
		return ""
	}
	switch t := node.(type) {
	case *ast.Text:
		return textValue(t, src)
	case *ast.String:
		return string(t.Value)
	case *ast.CodeSpan:
		return leafText(t, src)
	case *ast.FencedCodeBlock:
		if t.Lines().Len() > 0 {
			return linesValue(t, src)
		}
		return leafText(t, src)
	case *ast.CodeBlock:
		if t.Lines().Len() > 0 {
			return linesValue(t, src)
		}
		return leafText(t, src)
	case *ast.RawHTML:
		return string(t.Segments.Value(src))
	default:
		return ""
	}
}

// CodeLanguage returns a fenced code info-string language. Constructed nodes
// store language on an attribute because goldmark Language() indexes source.
func CodeLanguage(node ast.Node, src []byte) string {
	fb, ok := node.(*ast.FencedCodeBlock)
	if !ok {
		return ""
	}
	if v, found := fb.AttributeString(constructedLangAttr); found {
		if s, ok := v.(string); ok {
			return s
		}
	}
	if fb.Info == nil {
		return ""
	}
	return string(fb.Language(src))
}

func nodeValue(node ast.Node, src []byte) (string, bool) {
	switch t := node.(type) {
	case *ast.Text:
		v := textValue(t, src)
		if t.SoftLineBreak() || t.HardLineBreak() {
			return v + "\n", true
		}
		return v, true
	case *ast.String:
		return string(t.Value), true
	case *ast.CodeSpan, *ast.FencedCodeBlock, *ast.CodeBlock, *ast.RawHTML:
		return GetNodeValue(node, src), true
	case *ast.AutoLink:
		return string(t.Label(src)), true
	default:
		return "", false
	}
}

func childPlainText(node ast.Node, src []byte, sep string, keepEmpty bool) string {
	kids := collectChildren(node)
	texts := make([]string, 0, len(kids))
	for _, c := range kids {
		s := plainTextNode(c, src)
		if keepEmpty || s != "" {
			texts = append(texts, s)
		}
	}
	return strings.Join(texts, sep)
}

func plainTextNode(node ast.Node, src []byte) string {
	if node == nil {
		return ""
	}
	if v, ok := nodeValue(node, src); ok {
		return v
	}
	switch node.Kind() {
	case ast.KindDocument:
		return childPlainText(node, src, "\n\n", false)
	case ast.KindList:
		return childPlainText(node, src, "\n", false)
	case east.KindTable:
		var rows []string
		for _, c := range collectChildren(node) {
			row := plainTextNode(c, src)
			if strings.TrimSpace(row) != "" {
				rows = append(rows, row)
			}
		}
		return strings.Join(rows, "\n")
	case ast.KindListItem, ast.KindBlockquote:
		return childPlainText(node, src, "\n", false)
	case east.KindTableRow, east.KindTableHeader:
		return childPlainText(node, src, "\t", true)
	case ast.KindThematicBreak:
		return ""
	case east.KindTableCell:
		return childPlainText(node, src, "", false)
	default:
		return childPlainText(node, src, "", false)
	}
}

// ToPlainText extracts plain text from a goldmark tree (mdast toPlainText).
func ToPlainText(node ast.Node, src []byte) string {
	return plainTextNode(node, src)
}

// MarkdownToPlainText parses src and extracts plain text.
func MarkdownToPlainText(src string) string {
	return ToPlainText(ParseMarkdown(src), []byte(src))
}

// TableToASCII renders a goldmark table as a padded ASCII table.
func TableToASCII(node ast.Node, src []byte) string {
	if !IsTableNode(node) {
		return ""
	}
	var rows [][]string
	for _, row := range collectChildren(node) {
		var cells []string
		for _, cell := range collectChildren(row) {
			cells = append(cells, plainTextNode(cell, src))
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 {
		return ""
	}
	return TableElementToASCII(rows[0], rows[1:])
}

// TableElementToASCII renders headers and rows as a padded ASCII table.
func TableElementToASCII(headers []string, rows [][]string) string {
	allRows := make([][]string, 0, 1+len(rows))
	allRows = append(allRows, headers)
	allRows = append(allRows, rows...)
	colCount := 0
	for _, r := range allRows {
		if len(r) > colCount {
			colCount = len(r)
		}
	}
	if colCount == 0 {
		return ""
	}
	colWidths := make([]int, colCount)
	for _, row := range allRows {
		for i := range colCount {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			if len(cell) > colWidths[i] {
				colWidths[i] = len(cell)
			}
		}
	}
	formatRow := func(cells []string) string {
		parts := make([]string, colCount)
		for i := range colCount {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			parts[i] = padEnd(cell, colWidths[i])
		}
		return strings.TrimRight(strings.Join(parts, " | "), " ")
	}
	lines := make([]string, 0, 2+len(rows))
	lines = append(lines, formatRow(headers))
	seps := make([]string, colCount)
	for i, w := range colWidths {
		seps[i] = strings.Repeat("-", w)
	}
	lines = append(lines, strings.Join(seps, "-|-"))
	for _, row := range rows {
		lines = append(lines, formatRow(row))
	}
	return strings.Join(lines, "\n")
}

func padEnd(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

// StringifyOptions controls emphasis markers.
type StringifyOptions struct {
	Emphasis byte // default '*'
}

func stringifyDefaults(opts StringifyOptions) StringifyOptions {
	if opts.Emphasis == 0 {
		opts.Emphasis = '*'
	}
	return opts
}

// StringifyMarkdown emits markdown for the node types the ported tests exercise.
func StringifyMarkdown(node ast.Node, src []byte, opts StringifyOptions) string {
	return stringifyNode(node, src, stringifyDefaults(opts))
}

func stringifyNode(node ast.Node, src []byte, opts StringifyOptions) string {
	if node == nil {
		return ""
	}
	switch t := node.(type) {
	case *ast.Document:
		return joinBlocks(node, src, opts)
	case *ast.Paragraph:
		return stringifyInlines(node, src, opts)
	case *ast.Emphasis:
		inner := stringifyInlines(node, src, opts)
		if t.Level >= 2 {
			return "**" + inner + "**"
		}
		m := string([]byte{opts.Emphasis})
		return m + inner + m
	case *ast.CodeSpan:
		return "`" + GetNodeValue(node, src) + "`"
	case *ast.Link:
		return "[" + stringifyInlines(node, src, opts) + "](" + string(t.Destination) + ")"
	case *east.Strikethrough:
		return "~~" + stringifyInlines(node, src, opts) + "~~"
	case *east.Table:
		return stringifyTable(node, src, opts)
	case *ast.AutoLink:
		return string(t.URL(src))
	case *ast.Text:
		return string(t.Value(src))
	case *ast.String:
		return string(t.Value)
	default:
		if node.HasChildren() {
			return stringifyInlines(node, src, opts)
		}
		return GetNodeValue(node, src)
	}
}

func joinBlocks(node ast.Node, src []byte, opts StringifyOptions) string {
	var parts []string
	for _, c := range collectChildren(node) {
		parts = append(parts, stringifyNode(c, src, opts))
	}
	return strings.Join(parts, "\n\n")
}

func stringifyInlines(node ast.Node, src []byte, opts StringifyOptions) string {
	var b strings.Builder
	for _, c := range collectChildren(node) {
		b.WriteString(stringifyNode(c, src, opts))
	}
	return b.String()
}

func stringifyTable(node ast.Node, src []byte, opts StringifyOptions) string {
	var lines []string
	for _, row := range collectChildren(node) {
		var cells []string
		for _, cell := range collectChildren(row) {
			cells = append(cells, stringifyInlines(cell, src, opts))
		}
		lines = append(lines, "| "+strings.Join(cells, " | ")+" |")
		if _, ok := row.(*east.TableHeader); ok {
			seps := make([]string, len(cells))
			for i := range seps {
				seps[i] = "---"
			}
			lines = append(lines, "| "+strings.Join(seps, " | ")+" |")
		}
	}
	return strings.Join(lines, "\n")
}

// WalkAST visits children (not the root), replacing with visitor's return
// or dropping the child when visitor returns nil. Recurses into the result.
func WalkAST(node ast.Node, visitor func(ast.Node) ast.Node) ast.Node {
	if node == nil {
		return nil
	}
	for _, child := range GetNodeChildren(node) {
		result := visitor(child)
		if result == nil {
			node.RemoveChild(node, child)
			continue
		}
		if result != child {
			node.ReplaceChild(node, child, result)
		}
		WalkAST(result, visitor)
	}
	return node
}

func appendChildren(parent ast.Node, children []ast.Node) {
	for _, c := range children {
		if c != nil {
			parent.AppendChild(parent, c)
		}
	}
}

func Text(value string) *ast.String {
	return ast.NewString([]byte(value))
}

func Strong(children []ast.Node) *ast.Emphasis {
	n := ast.NewEmphasis(2)
	appendChildren(n, children)
	return n
}

func Emphasis(children []ast.Node) *ast.Emphasis {
	n := ast.NewEmphasis(1)
	appendChildren(n, children)
	return n
}

func Strikethrough(children []ast.Node) *east.Strikethrough {
	n := east.NewStrikethrough()
	appendChildren(n, children)
	return n
}

func InlineCode(value string) *ast.CodeSpan {
	n := ast.NewCodeSpan()
	n.AppendChild(n, ast.NewString([]byte(value)))
	return n
}

func CodeBlock(value string, lang ...string) *ast.FencedCodeBlock {
	n := ast.NewFencedCodeBlock(nil)
	n.AppendChild(n, ast.NewString([]byte(value)))
	if len(lang) > 0 && lang[0] != "" {
		n.SetAttributeString(constructedLangAttr, lang[0])
	}
	return n
}

func Link(url string, children []ast.Node, title ...string) *ast.Link {
	n := ast.NewLink()
	n.Destination = []byte(url)
	if len(title) > 0 {
		n.Title = []byte(title[0])
	}
	appendChildren(n, children)
	return n
}

func Blockquote(children []ast.Node) *ast.Blockquote {
	n := ast.NewBlockquote()
	appendChildren(n, children)
	return n
}

func Paragraph(children []ast.Node) *ast.Paragraph {
	n := ast.NewParagraph()
	appendChildren(n, children)
	return n
}

func Root(children []ast.Node) *ast.Document {
	n := ast.NewDocument()
	appendChildren(n, children)
	return n
}
