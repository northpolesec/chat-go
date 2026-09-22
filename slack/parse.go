// Ported from packages/adapter-slack/src/index.ts (parseMessage/parseSlackMessage region) @ 6adca36 (chat v4.40.0).
// Divergences: Message.Raw is the inner Slack event; Formatted is goldmark;
// FetchData snapshots the request-ctx token at parse (ALS → ctx via
// parseSlackMessageSync + WithBotToken).
package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/northpolesec/chat-go/chat"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

var (
	slackMessageURLPattern = regexp.MustCompile(`^https?://[^/]+\.slack\.com/archives/([A-Z0-9]+)/p(\d+)(?:\?.*)?$`)
	bracketedURLPattern    = regexp.MustCompile(`<(https?://[^>]{1,1000})>`)
	trailingSlashPattern   = regexp.MustCompile(`/$`)
	httpURLPrefix          = regexp.MustCompile(`^https?://`)
	tableBlockTypes        = map[string]struct{}{"table": {}, "data_table": {}}
)

// SlackEvent is the inner Slack message event (upstream SlackEvent).
type SlackEvent struct {
	Attachments     []SlackAttachment `json:"attachments,omitempty"`
	Blocks          []map[string]any  `json:"blocks,omitempty"`
	BotID           string            `json:"bot_id,omitempty"`
	BotProfile      *SlackBotProfile  `json:"bot_profile,omitempty"`
	Channel         string            `json:"channel,omitempty"`
	ChannelType     string            `json:"channel_type,omitempty"`
	DeletedTs       string            `json:"deleted_ts,omitempty"`
	Edited          *SlackEdited      `json:"edited,omitempty"`
	EventTs         string            `json:"event_ts,omitempty"`
	Files           []SlackFile       `json:"files,omitempty"`
	Hidden          bool              `json:"hidden,omitempty"`
	LatestReply     string            `json:"latest_reply,omitempty"`
	ReplyCount      int               `json:"reply_count,omitempty"`
	Message         *SlackEvent       `json:"message,omitempty"`
	PreviousMessage *SlackEvent       `json:"previous_message,omitempty"`
	Subtype         string            `json:"subtype,omitempty"`
	Team            string            `json:"team,omitempty"`
	TeamID          string            `json:"team_id,omitempty"`
	Text            string            `json:"text,omitempty"`
	ThreadTs        string            `json:"thread_ts,omitempty"`
	Ts              string            `json:"ts,omitempty"`
	Type            string            `json:"type"`
	User            string            `json:"user,omitempty"`
	Username        string            `json:"username,omitempty"`
}

// SlackBotProfile is event.bot_profile.
type SlackBotProfile struct {
	UserID string `json:"user_id,omitempty"`
}

// SlackEdited is event.edited.
type SlackEdited struct {
	Ts string `json:"ts"`
}

// SlackFile is one event.files entry.
type SlackFile struct {
	ID         string `json:"id,omitempty"`
	Mimetype   string `json:"mimetype,omitempty"`
	URLPrivate string `json:"url_private,omitempty"`
	Name       string `json:"name,omitempty"`
	Size       int    `json:"size,omitempty"`
	OriginalW  int    `json:"original_w,omitempty"`
	OriginalH  int    `json:"original_h,omitempty"`
}

// SlackAttachment is a legacy attachment on a Slack event.
type SlackAttachment struct {
	Blocks      []map[string]any       `json:"blocks,omitempty"`
	Fallback    string                 `json:"fallback,omitempty"`
	Fields      []SlackAttachmentField `json:"fields,omitempty"`
	FromURL     string                 `json:"from_url,omitempty"`
	ImageURL    string                 `json:"image_url,omitempty"`
	IsAppUnfurl bool                   `json:"is_app_unfurl,omitempty"`
	IsMsgUnfurl bool                   `json:"is_msg_unfurl,omitempty"`
	MrkdwnIn    []string               `json:"mrkdwn_in,omitempty"`
	OriginalURL string                 `json:"original_url,omitempty"`
	Pretext     string                 `json:"pretext,omitempty"`
	ServiceName string                 `json:"service_name,omitempty"`
	Text        string                 `json:"text,omitempty"`
	ThumbURL    string                 `json:"thumb_url,omitempty"`
	Title       string                 `json:"title,omitempty"`
	TitleLink   string                 `json:"title_link,omitempty"`
}

// SlackAttachmentField is attachment.fields[].
type SlackAttachmentField struct {
	Title string `json:"title,omitempty"`
	Value string `json:"value,omitempty"`
	Short bool   `json:"short,omitempty"`
}

type slackTableData struct {
	headerless bool
	rows       [][]string
}

type slackEventTables struct {
	leading  []slackTableData
	trailing []slackTableData
}

type slackAttachmentPart struct {
	literal  string
	mrkdwn   string
	isMrkdwn bool
}

type slackAttachmentContent struct {
	parts  []slackAttachmentPart
	tables []slackTableData
}

// ParseMessage is adapter.parseMessage (inner event → chat.Message).
func (a *SlackAdapter) ParseMessage(raw any) (*chat.Message, error) {
	event := asSlackEvent(raw)
	threadTs := event.ThreadTs
	if threadTs == "" {
		threadTs = event.Ts
	}
	threadID := a.encodeThreadID(ThreadID{Channel: event.Channel, ThreadTS: threadTs})
	return a.parseSlackMessageSync(context.Background(), event, threadID), nil
}

func (a *SlackAdapter) parseSlackMessageSync(ctx context.Context, event SlackEvent, threadID string) *chat.Message {
	isMe := a.isMessageFromSelf(ctx, event)
	text := event.Text
	formatted := a.content(event, text)
	userName := event.Username
	if userName == "" {
		userName = event.User
	}
	if userName == "" {
		userName = "unknown"
	}
	fullName := userName
	authorID := event.User
	if authorID == "" && event.BotProfile != nil {
		authorID = event.BotProfile.UserID
	}
	if authorID == "" {
		authorID = event.BotID
	}
	if authorID == "" {
		authorID = "unknown"
	}
	var editedAt *time.Time
	if event.Edited != nil {
		t := parseSlackTimestamp(event.Edited.Ts)
		editedAt = &t
	}
	atts := make([]chat.Attachment, 0, len(event.Files))
	teamID := event.TeamID
	if teamID == "" {
		teamID = event.Team
	}
	for _, f := range event.Files {
		atts = append(atts, a.createAttachment(ctx, f, teamID))
	}
	return chat.NewMessage(chat.MessageData{
		ID:        event.Ts,
		ThreadID:  threadID,
		Text:      chat.ToPlainText(formatted, nil),
		Formatted: formatted,
		Raw:       event,
		Author: chat.Author{
			UserID:   authorID,
			UserName: userName,
			FullName: fullName,
			IsBot:    boolPtr(event.BotID != ""),
			IsSystem: event.User == slackSystemUserID,
			IsMe:     isMe,
		},
		Metadata: chat.MessageMetadata{
			DateSent: parseSlackTimestamp(event.Ts),
			Edited:   event.Edited != nil,
			EditedAt: editedAt,
		},
		Attachments: atts,
		Links:       a.extractLinks(event),
	})
}

func (a *SlackAdapter) parseSlackMessage(ctx context.Context, event SlackEvent, threadID string) (*chat.Message, error) {
	msg := a.parseSlackMessageSync(ctx, event, threadID)
	if event.User != "" && event.Username == "" {
		if info := a.lookupUser(ctx, event.User); info != nil {
			msg.Author.UserName = info.DisplayName
			msg.Author.FullName = info.RealName
			msg.Author.Email = info.Email
		} else {
			msg.Author.UserName = event.User
			msg.Author.FullName = event.User
		}
	}
	a.trackThreadParticipant(ctx, threadID, event.User)
	formatted, err := a.resolvedContent(ctx, event, event.Text)
	if err != nil {
		return nil, err
	}
	msg.Formatted = formatted
	msg.Text = chat.ToPlainText(formatted, nil)
	if a.isSelfMentioned(ctx, event) {
		msg.IsMention = boolPtr(true)
	}
	return msg, nil
}

func parseSlackTimestamp(ts string) time.Time {
	if ts == "" {
		return time.UnixMilli(0).UTC()
	}
	f, err := strconv.ParseFloat(ts, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMilli(int64(f * 1000)).UTC()
}

func (a *SlackAdapter) content(event SlackEvent, text string) ast.Node {
	return a.assembleContent(text, eventTables(event), flattenAttachmentNodes(a, authorAttachments(event)))
}

func flattenAttachmentNodes(a *SlackAdapter, atts []SlackAttachment) []ast.Node {
	var nodes []ast.Node
	for _, att := range atts {
		nodes = append(nodes, a.attachmentNodes(attachmentContent(att))...)
	}
	return nodes
}

func (a *SlackAdapter) resolvedContent(ctx context.Context, event SlackEvent, text string) (ast.Node, error) {
	tables := eventTables(event)
	atts := authorAttachments(event)
	contents := make([]slackAttachmentContent, len(atts))
	for i, att := range atts {
		contents[i] = attachmentContent(att)
	}
	userIDs, channelIDs := mentionIDs(tables, contents)
	users := map[string]struct{}{}
	channels := map[string]struct{}{}
	for _, id := range userIDs {
		users[id] = struct{}{}
	}
	for _, id := range channelIDs {
		channels[id] = struct{}{}
	}
	collectMentionIDs(text, users, channels)
	userIDs = userIDs[:0]
	channelIDs = channelIDs[:0]
	for id := range users {
		userIDs = append(userIDs, id)
	}
	for id := range channels {
		channelIDs = append(channelIDs, id)
	}
	names, err := a.lookupMentionNames(ctx, userIDs, channelIDs)
	if err != nil {
		return nil, err
	}
	resolveTable := func(data slackTableData) slackTableData {
		rows := make([][]string, len(data.rows))
		for i, row := range data.rows {
			rows[i] = make([]string, len(row))
			for j, cell := range row {
				rows[i][j] = applyMentionNames(cell, names)
			}
		}
		return slackTableData{headerless: data.headerless, rows: rows}
	}
	leading := make([]slackTableData, len(tables.leading))
	for i, t := range tables.leading {
		leading[i] = resolveTable(t)
	}
	trailing := make([]slackTableData, len(tables.trailing))
	for i, t := range tables.trailing {
		trailing[i] = resolveTable(t)
	}
	var nodes []ast.Node
	for _, content := range contents {
		resolved := slackAttachmentContent{tables: make([]slackTableData, len(content.tables))}
		for i, t := range content.tables {
			resolved.tables[i] = resolveTable(t)
		}
		for _, part := range content.parts {
			if part.isMrkdwn {
				resolved.parts = append(resolved.parts, slackAttachmentPart{isMrkdwn: true, mrkdwn: applyMentionNames(part.mrkdwn, names)})
			} else {
				resolved.parts = append(resolved.parts, slackAttachmentPart{literal: applyMentionNames(part.literal, names)})
			}
		}
		nodes = append(nodes, a.attachmentNodes(resolved)...)
	}
	return a.assembleContent(applyMentionNames(text, names), slackEventTables{leading: leading, trailing: trailing}, nodes), nil
}

type mentionNames struct {
	users    map[string]string
	channels map[string]string
}

func (a *SlackAdapter) lookupMentionNames(ctx context.Context, userIDs, channelIDs []string) (mentionNames, error) {
	names := mentionNames{users: map[string]string{}, channels: map[string]string{}}
	seenUsers := map[string]struct{}{}
	for _, id := range userIDs {
		if _, ok := seenUsers[id]; ok {
			continue
		}
		seenUsers[id] = struct{}{}
		info := a.lookupUser(ctx, id)
		if info != nil && info.DisplayName != "" {
			names.users[id] = info.DisplayName
		} else {
			names.users[id] = id
		}
	}
	seenChans := map[string]struct{}{}
	for _, id := range channelIDs {
		if _, ok := seenChans[id]; ok {
			continue
		}
		seenChans[id] = struct{}{}
		names.channels[id] = a.lookupChannel(ctx, id)
	}
	return names, nil
}

func applyMentionNames(text string, names mentionNames) string {
	if len(names.users) == 0 && len(names.channels) == 0 {
		return text
	}
	var b strings.Builder
	remaining := text
	for {
		start := findNextMention(remaining)
		if start == -1 {
			b.WriteString(remaining)
			return b.String()
		}
		b.WriteString(remaining[:start])
		remaining = remaining[start:]
		end := strings.IndexByte(remaining, '>')
		if end == -1 {
			b.WriteString(remaining)
			return b.String()
		}
		prefix := remaining[1]
		inner := remaining[2:end]
		id, _, hadPipe := strings.Cut(inner, "|")
		switch {
		case prefix == '@' && slackIDPattern.MatchString(id):
			if name, ok := names.users[id]; ok {
				b.WriteString("<@")
				b.WriteString(id)
				b.WriteByte('|')
				b.WriteString(name)
				b.WriteByte('>')
			} else {
				b.WriteString(remaining[:end+1])
			}
		case prefix == '#' && !hadPipe:
			if name, ok := names.channels[id]; ok {
				b.WriteString("<#")
				b.WriteString(id)
				b.WriteByte('|')
				b.WriteString(name)
				b.WriteByte('>')
			} else {
				b.WriteString(remaining[:end+1])
			}
		default:
			b.WriteString(remaining[:end+1])
		}
		remaining = remaining[end+1:]
	}
}

func findNextMention(text string) int {
	at := strings.Index(text, "<@")
	hash := strings.Index(text, "<#")
	switch {
	case at == -1:
		return hash
	case hash == -1:
		return at
	case at < hash:
		return at
	default:
		return hash
	}
}

func mentionIDs(tables slackEventTables, atts []slackAttachmentContent) (userIDs, channelIDs []string) {
	users := map[string]struct{}{}
	channels := map[string]struct{}{}
	scan := func(s string) {
		collectMentionIDs(s, users, channels)
	}
	for _, t := range append(append([]slackTableData{}, tables.leading...), tables.trailing...) {
		for _, row := range t.rows {
			for _, cell := range row {
				scan(cell)
			}
		}
	}
	for _, att := range atts {
		for _, t := range att.tables {
			for _, row := range t.rows {
				for _, cell := range row {
					scan(cell)
				}
			}
		}
		for _, p := range att.parts {
			if p.isMrkdwn {
				scan(p.mrkdwn)
			} else {
				scan(p.literal)
			}
		}
	}
	for id := range users {
		userIDs = append(userIDs, id)
	}
	for id := range channels {
		channelIDs = append(channelIDs, id)
	}
	return userIDs, channelIDs
}

func collectMentionIDs(text string, userIDs, channelIDs map[string]struct{}) {
	for segment := range strings.SplitSeq(text, "<") {
		inner, _, ok := strings.Cut(segment, ">")
		if !ok {
			continue
		}
		switch {
		case strings.HasPrefix(inner, "@"):
			id, _, _ := strings.Cut(inner[1:], "|")
			if slackIDPattern.MatchString(id) {
				userIDs[id] = struct{}{}
			}
		case strings.HasPrefix(inner, "#"):
			rest := inner[1:]
			if !strings.Contains(rest, "|") && slackIDPattern.MatchString(rest) {
				channelIDs[rest] = struct{}{}
			}
		}
	}
}

func (a *SlackAdapter) assembleContent(text string, tables slackEventTables, attachments []ast.Node) ast.Node {
	var doc ast.Node
	if text == "" {
		doc = ast.NewDocument()
	} else {
		md := slackMrkdwnToMarkdown(text)
		doc = a.format.ToAst(text)
		materializeSource(doc, []byte(md))
	}
	var first ast.Node
	if doc.FirstChild() != nil {
		first = doc.FirstChild()
	}
	for _, t := range slices.Backward(tables.leading) {
		n := a.tableNode(t)
		if first != nil {
			doc.InsertBefore(doc, first, n)
		} else {
			doc.AppendChild(doc, n)
		}
		first = n
	}
	for _, t := range tables.trailing {
		doc.AppendChild(doc, a.tableNode(t))
	}
	for _, n := range attachments {
		doc.AppendChild(doc, n)
	}
	return doc
}

func (a *SlackAdapter) attachmentNodes(content slackAttachmentContent) []ast.Node {
	var nodes []ast.Node
	var lines [][]ast.Node
	flush := func() {
		if len(lines) == 0 {
			return
		}
		p := ast.NewParagraph()
		for i, line := range lines {
			if i > 0 {
				p.AppendChild(p, ast.NewString([]byte("\n")))
			}
			for _, n := range line {
				p.AppendChild(p, n)
			}
		}
		nodes = append(nodes, p)
		lines = nil
	}
	for _, part := range content.parts {
		if part.isMrkdwn {
			flush()
			md := slackMrkdwnToMarkdown(part.mrkdwn)
			root := a.format.ToAst(part.mrkdwn)
			materializeSource(root, []byte(md))
			for c := root.FirstChild(); c != nil; {
				next := c.NextSibling()
				root.RemoveChild(root, c)
				nodes = append(nodes, c)
				c = next
			}
		} else {
			for line := range strings.SplitSeq(part.literal, "\n") {
				if strings.TrimSpace(line) != "" {
					lines = append(lines, literalPhrasing(line))
				} else {
					flush()
				}
			}
		}
	}
	flush()
	for _, t := range content.tables {
		nodes = append(nodes, a.tableNode(t))
	}
	return nodes
}

func literalPhrasing(line string) []ast.Node {
	var children []ast.Node
	var plain strings.Builder
	flushPlain := func() {
		if plain.Len() == 0 {
			return
		}
		children = append(children, ast.NewString([]byte(unescapeSlackText(plain.String()))))
		plain.Reset()
	}
	remaining := line
	for remaining != "" {
		start := strings.IndexByte(remaining, '<')
		end := -1
		if start != -1 {
			end = strings.IndexByte(remaining[start+1:], '>')
			if end != -1 {
				end += start + 1
			}
		}
		if end == -1 {
			plain.WriteString(remaining)
			break
		}
		plain.WriteString(remaining[:start])
		token := remaining[start : end+1]
		inner := remaining[start+1 : end]
		remaining = remaining[end+1:]
		target, label, hasLabel := strings.Cut(inner, "|")
		id := ""
		if len(target) > 0 {
			id = target[1:]
		}
		switch {
		case strings.HasPrefix(target, "@") && slackIDPattern.MatchString(id):
			if hasLabel {
				plain.WriteByte('@')
				plain.WriteString(label)
			} else {
				plain.WriteByte('@')
				plain.WriteString(id)
			}
		case strings.HasPrefix(target, "#") && slackIDPattern.MatchString(id):
			if hasLabel {
				plain.WriteByte('#')
				plain.WriteString(label)
				plain.WriteString(" (")
				plain.WriteString(id)
				plain.WriteByte(')')
			} else {
				plain.WriteByte('#')
				plain.WriteString(id)
			}
		case httpURLPrefix.MatchString(target):
			flushPlain()
			link := ast.NewLink()
			link.Destination = []byte(target)
			text := target
			if hasLabel {
				text = unescapeSlackText(label)
			}
			link.AppendChild(link, ast.NewString([]byte(text)))
			children = append(children, link)
		default:
			plain.WriteString(token)
		}
	}
	flushPlain()
	return children
}

func (a *SlackAdapter) tableNode(data slackTableData) ast.Node {
	table := east.NewTable()
	rows := data.rows
	if data.headerless {
		width := 0
		for _, r := range rows {
			if len(r) > width {
				width = len(r)
			}
		}
		empty := make([]string, width)
		rows = append([][]string{empty}, rows...)
	}
	for i, row := range rows {
		rowNode := east.NewTableRow(nil)
		for _, cell := range row {
			cellNode := east.NewTableCell()
			for _, child := range a.cellChildren(cell) {
				cellNode.AppendChild(cellNode, child)
			}
			rowNode.AppendChild(rowNode, cellNode)
		}
		var n ast.Node = rowNode
		if i == 0 {
			n = east.NewTableHeader(rowNode)
		}
		table.AppendChild(table, n)
	}
	return table
}

func (a *SlackAdapter) cellChildren(cell string) []ast.Node {
	if cell == "" {
		return nil
	}
	md := slackMrkdwnToMarkdown(cell)
	root := a.format.ToAst(cell)
	materializeSource(root, []byte(md))
	var children []ast.Node
	for n := root.FirstChild(); n != nil; n = n.NextSibling() {
		if len(children) > 0 {
			children = append(children, ast.NewString([]byte("\n")))
		}
		if n.Kind() == ast.KindParagraph {
			for c := n.FirstChild(); c != nil; {
				next := c.NextSibling()
				n.RemoveChild(n, c)
				children = append(children, c)
				c = next
			}
		} else {
			children = append(children, ast.NewString([]byte(chat.ToPlainText(n, nil))))
		}
	}
	return children
}

func materializeSource(node ast.Node, src []byte) {
	if node == nil {
		return
	}
	var replace []struct {
		parent, old, neu ast.Node
	}
	_ = ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch t := n.(type) {
		case *ast.Text:
			v := t.Value(src)
			if t.SoftLineBreak() || t.HardLineBreak() {
				v = append(append([]byte{}, v...), '\n')
			}
			if p := t.Parent(); p != nil {
				replace = append(replace, struct{ parent, old, neu ast.Node }{p, t, ast.NewString(v)})
			}
		case *ast.CodeSpan:
			var b strings.Builder
			for c := t.FirstChild(); c != nil; c = c.NextSibling() {
				if tx, ok := c.(*ast.Text); ok {
					b.Write(tx.Value(src))
				}
			}
			if p := t.Parent(); p != nil {
				replace = append(replace, struct{ parent, old, neu ast.Node }{p, t, ast.NewString([]byte(b.String()))})
			}
		case *ast.AutoLink:
			label := t.Label(src)
			if p := t.Parent(); p != nil {
				link := ast.NewLink()
				link.Destination = append([]byte{}, t.URL(src)...)
				link.AppendChild(link, ast.NewString(label))
				replace = append(replace, struct{ parent, old, neu ast.Node }{p, t, link})
			}
		}
		return ast.WalkContinue, nil
	})
	for _, r := range replace {
		r.parent.ReplaceChild(r.parent, r.old, r.neu)
	}
	// Fenced code still needs source; copy lines onto String children via a dummy reader.
	_ = ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if fb, ok := n.(*ast.FencedCodeBlock); ok {
			var b strings.Builder
			lines := fb.Lines()
			for i := 0; i < lines.Len(); i++ {
				seg := lines.At(i)
				b.Write(seg.Value(src))
			}
			fb.SetLines(&text.Segments{})
			fb.AppendChild(fb, ast.NewString([]byte(strings.TrimRight(b.String(), "\n"))))
		}
		return ast.WalkContinue, nil
	})
}

func isForeignAttachment(att SlackAttachment) bool {
	return att.IsMsgUnfurl || att.IsAppUnfurl || att.FromURL != "" || att.OriginalURL != ""
}

func eventTables(event SlackEvent) slackEventTables {
	blocks := event.Blocks
	splitIdx := len(blocks)
	for i, block := range blocks {
		typ, _ := block["type"].(string)
		if _, ok := tableBlockTypes[typ]; !ok {
			splitIdx = i
			break
		}
	}
	parse := func(list []map[string]any) []slackTableData {
		var out []slackTableData
		for _, block := range list {
			if t, ok := tableData(block); ok {
				out = append(out, t)
			}
		}
		return out
	}
	return slackEventTables{
		leading:  parse(blocks[:splitIdx]),
		trailing: parse(blocks[splitIdx:]),
	}
}

func authorAttachments(event SlackEvent) []SlackAttachment {
	var out []SlackAttachment
	for _, att := range event.Attachments {
		if !isForeignAttachment(att) {
			out = append(out, att)
		}
	}
	return out
}

func attachmentContent(att SlackAttachment) slackAttachmentContent {
	var tables []slackTableData
	for _, block := range att.Blocks {
		if t, ok := tableData(block); ok {
			tables = append(tables, t)
		}
	}
	var parts []slackAttachmentPart
	if len(tables) == 0 {
		mrkdwnIn := map[string]struct{}{}
		for _, name := range att.MrkdwnIn {
			mrkdwnIn[name] = struct{}{}
		}
		push := func(value string, mrkdwn bool) {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				return
			}
			if mrkdwn {
				parts = append(parts, slackAttachmentPart{isMrkdwn: true, mrkdwn: trimmed})
			} else {
				parts = append(parts, slackAttachmentPart{literal: trimmed})
			}
		}
		_, pretextMD := mrkdwnIn["pretext"]
		push(att.Pretext, pretextMD)
		title := strings.TrimSpace(att.Title)
		if att.TitleLink != "" {
			if title != "" {
				push("<"+att.TitleLink+"|"+escapeSlackText(title)+">", false)
			} else {
				push("<"+att.TitleLink+">", false)
			}
		} else {
			push(title, false)
		}
		_, textMD := mrkdwnIn["text"]
		push(att.Text, textMD)
		_, fieldsMD := mrkdwnIn["fields"]
		for _, field := range att.Fields {
			ft := strings.TrimSpace(field.Title)
			fv := strings.TrimSpace(field.Value)
			switch {
			case ft != "" && fv != "":
				push(ft+": "+fv, fieldsMD)
			case ft != "":
				push(ft, fieldsMD)
			default:
				push(fv, fieldsMD)
			}
		}
		if len(parts) == 0 {
			push(att.Fallback, false)
		}
	}
	return slackAttachmentContent{parts: parts, tables: tables}
}

func tableData(block map[string]any) (slackTableData, bool) {
	typ, _ := block["type"].(string)
	if _, ok := tableBlockTypes[typ]; !ok {
		return slackTableData{}, false
	}
	rawRows, ok := block["rows"].([]any)
	if !ok {
		return slackTableData{}, false
	}
	var sourceRows [][]any
	for _, row := range rawRows {
		cells, ok := row.([]any)
		if !ok || len(cells) == 0 {
			continue
		}
		sourceRows = append(sourceRows, cells)
	}
	if len(sourceRows) == 0 {
		return slackTableData{}, false
	}
	headerless := typ == "table" && !rowHasBold(sourceRows[0])
	rows := make([][]string, len(sourceRows))
	for i, row := range sourceRows {
		rows[i] = make([]string, len(row))
		for j, cell := range row {
			rows[i][j] = blocktext(cell)
		}
	}
	return slackTableData{headerless: headerless, rows: rows}, true
}

func rowHasBold(row []any) bool {
	return slices.ContainsFunc(row, hasBoldText)
}

func hasBoldText(value any) bool {
	m, ok := value.(map[string]any)
	if !ok {
		return false
	}
	if style, ok := m["style"].(map[string]any); ok {
		if bold, _ := style["bold"].(bool); bold {
			return true
		}
	}
	if elems, ok := m["elements"].([]any); ok {
		return slices.ContainsFunc(elems, hasBoldText)
	}
	return false
}

func blocktext(value any) string {
	m, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	typ, _ := m["type"].(string)
	if typ == "" {
		return ""
	}
	text, _ := m["text"].(string)
	hasText := false
	if _, ok := m["text"]; ok {
		if _, isStr := m["text"].(string); isStr {
			hasText = true
		}
	}
	switch typ {
	case "link":
		u, _ := m["url"].(string)
		if u == "" {
			return text
		}
		if text != "" {
			return "<" + u + "|" + text + ">"
		}
		return "<" + u + ">"
	case "emoji":
		name, _ := m["name"].(string)
		if name == "" {
			return ""
		}
		return ":" + name + ":"
	case "user":
		id, _ := m["user_id"].(string)
		if id == "" {
			return ""
		}
		return "<@" + id + ">"
	case "broadcast":
		rng, _ := m["range"].(string)
		if rng == "" {
			return ""
		}
		return "@" + rng
	case "channel":
		id, _ := m["channel_id"].(string)
		if id == "" {
			return ""
		}
		return "<#" + id + ">"
	case "usergroup":
		id, _ := m["usergroup_id"].(string)
		if id == "" {
			return ""
		}
		return "<!subteam^" + id + ">"
	case "date":
		if fallback, _ := m["fallback"].(string); fallback != "" {
			return fallback
		}
		switch ts := m["timestamp"].(type) {
		case float64:
			return time.Unix(int64(ts), 0).UTC().Format("2006-01-02")
		case int:
			return time.Unix(int64(ts), 0).UTC().Format("2006-01-02")
		case int64:
			return time.Unix(ts, 0).UTC().Format("2006-01-02")
		default:
			return ""
		}
	case "color":
		v, _ := m["value"].(string)
		return v
	case "team":
		v, _ := m["team_id"].(string)
		return v
	}
	if hasText {
		return text
	}
	if v, ok := strField(m, "label"); ok {
		return v
	}
	if v, ok := strField(m, "url"); ok {
		return v
	}
	if v, ok := strField(m, "file_id"); ok {
		return v
	}
	switch v := m["value"].(type) {
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	case json.Number:
		return v.String()
	}
	elems, ok := m["elements"].([]any)
	if !ok {
		return ""
	}
	sep := ""
	if typ == "rich_text" || typ == "rich_text_list" {
		sep = "\n"
	}
	parts := make([]string, len(elems))
	for i, e := range elems {
		parts[i] = blocktext(e)
	}
	return strings.Join(parts, sep)
}

func strField(m map[string]any, key string) (string, bool) {
	v, ok := m[key].(string)
	return v, ok
}

func (a *SlackAdapter) extractLinks(event SlackEvent) []chat.LinkPreview {
	urls := orderedSet{}
	for _, block := range event.Blocks {
		if typ, _ := block["type"].(string); typ != "rich_text" {
			continue
		}
		sections, _ := block["elements"].([]any)
		for _, section := range sections {
			sm, ok := section.(map[string]any)
			if !ok {
				continue
			}
			els, _ := sm["elements"].([]any)
			for _, el := range els {
				em, ok := el.(map[string]any)
				if !ok {
					continue
				}
				if t, _ := em["type"].(string); t == "link" {
					if u, _ := em["url"].(string); u != "" {
						urls.add(u)
					}
				}
			}
		}
	}
	if urls.len() == 0 && event.Text != "" {
		for _, m := range bracketedURLPattern.FindAllStringSubmatch(event.Text, -1) {
			raw := m[1]
			if i := strings.IndexByte(raw, '|'); i >= 0 {
				raw = raw[:i]
			}
			urls.add(raw)
		}
	}
	type unfurlMeta struct {
		title, description, imageURL, siteName string
	}
	unfurls := map[string]unfurlMeta{}
	for _, att := range event.Attachments {
		attURL := att.FromURL
		if attURL == "" {
			attURL = att.OriginalURL
		}
		if attURL != "" && (att.Title != "" || att.Text != "") {
			unfurls[attURL] = unfurlMeta{
				title:       att.Title,
				description: att.Text,
				imageURL:    firstNonEmpty(att.ImageURL, att.ThumbURL),
				siteName:    att.ServiceName,
			}
			urls.add(attURL)
		}
		if att.TitleLink != "" && !isForeignAttachment(att) {
			urls.add(att.TitleLink)
		}
	}
	out := make([]chat.LinkPreview, 0, urls.len())
	for _, u := range urls.items {
		preview := a.createLinkPreview(u)
		meta, ok := unfurls[u]
		if !ok {
			meta, ok = unfurls[trailingSlashPattern.ReplaceAllString(u, "")]
		}
		if !ok {
			meta, ok = unfurls[u+"/"]
		}
		if ok {
			preview.Title = meta.title
			preview.Description = meta.description
			preview.ImageURL = meta.imageURL
			preview.SiteName = meta.siteName
		}
		out = append(out, preview)
	}
	return out
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

type orderedSet struct{ items []string }

func (s *orderedSet) add(v string) {
	if slices.Contains(s.items, v) {
		return
	}
	s.items = append(s.items, v)
}

func (s *orderedSet) len() int { return len(s.items) }

func (a *SlackAdapter) createLinkPreview(rawURL string) chat.LinkPreview {
	m := slackMessageURLPattern.FindStringSubmatch(rawURL)
	if m == nil {
		return chat.LinkPreview{URL: rawURL}
	}
	channel := m[1]
	rawTs := m[2]
	if len(rawTs) < 6 {
		return chat.LinkPreview{URL: rawURL}
	}
	ts := rawTs[:len(rawTs)-6] + "." + rawTs[len(rawTs)-6:]
	threadID := a.encodeThreadID(ThreadID{Channel: channel, ThreadTS: ts})
	return chat.LinkPreview{
		URL: rawURL,
		FetchMessage: func() (*chat.Message, error) {
			return nil, fmt.Errorf("message not found: %s (thread %s)", rawURL, threadID)
		},
	}
}

func (a *SlackAdapter) createAttachment(ctx context.Context, file SlackFile, teamID string) chat.Attachment {
	fileURL := file.URLPrivate
	typ := chat.AttachmentFile
	switch {
	case strings.HasPrefix(file.Mimetype, "image/"):
		typ = chat.AttachmentImage
	case strings.HasPrefix(file.Mimetype, "video/"):
		typ = chat.AttachmentVideo
	case strings.HasPrefix(file.Mimetype, "audio/"):
		typ = chat.AttachmentAudio
	}
	meta := map[string]string{}
	if fileURL != "" {
		meta["url"] = fileURL
	}
	if teamID != "" {
		meta["teamId"] = teamID
	}
	if rc := requestContextFrom(ctx); rc != nil {
		if rc.enterpriseID != "" {
			meta["enterpriseId"] = rc.enterpriseID
		}
		if rc.isEnterpriseInstall {
			meta["isEnterpriseInstall"] = "true"
		}
	}
	var ctxToken string
	if rc := requestContextFrom(ctx); rc != nil {
		ctxToken = rc.token
	}
	var fetch func() ([]byte, error)
	if fileURL != "" {
		u := fileURL
		fetch = func() ([]byte, error) {
			return a.fetchSlackFile(context.Background(), u, func(context.Context) (string, error) {
				if ctxToken != "" {
					return ctxToken, nil
				}
				return a.getToken(context.Background())
			})
		}
	}
	if len(meta) == 0 {
		meta = nil
	}
	return chat.Attachment{
		Type:          typ,
		URL:           fileURL,
		Name:          file.Name,
		MIMEType:      file.Mimetype,
		Size:          file.Size,
		Width:         file.OriginalW,
		Height:        file.OriginalH,
		FetchMetadata: meta,
		FetchData:     fetch,
	}
}

func asSlackEvent(raw any) SlackEvent {
	switch v := raw.(type) {
	case SlackEvent:
		return v
	case *SlackEvent:
		if v == nil {
			return SlackEvent{}
		}
		return *v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return SlackEvent{}
		}
		var ev SlackEvent
		_ = json.Unmarshal(b, &ev)
		return ev
	}
}
