// Ported from packages/adapter-slack/src/index.ts @ 6adca36 (chat v4.40.0)
// (partial: scheduleMessage, fetchChannelInfo, fetchChannelMessages,
// listThreads, postChannelMessage, postObject/editObject).
// Divergences: ScheduleMessage returns *chat.RawMessage (no cancel closure;
// MessageScheduler has no ScheduledMessage); channel ID is split(":")[1];
// conversations.history / chat.scheduleMessage via slackCall (form);
// plan rendering is Block Kit `plan` / `task_card` (not cards.Card);
// pins/usergroups have no index.test.ts thin-client coverage (WebClient
// escape hatch only — not ported).
package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/northpolesec/chat-go/slack/api"
	"github.com/yuin/goldmark/ast"
)

var (
	_ chat.MessageScheduler = (*SlackAdapter)(nil)
	_ chat.ChannelReader    = (*SlackAdapter)(nil)
	_ chat.ObjectPoster     = (*SlackAdapter)(nil)
)

const (
	invalidChannelIDPrefix     = "Invalid Slack channel ID: "
	schedulePastMessage        = "postAt must be in the future"
	scheduleFilesMessage       = "File uploads are not supported in scheduled messages"
	methodScheduleMessage      = "chat.scheduleMessage"
	methodConversationsHistory = "conversations.history"
	defaultChannelLimit        = 100
	defaultThreadLimit         = 50
	maxHistoryFetch            = 200
)

func (a *SlackAdapter) decodeChannelID(channelID string) (string, error) {
	parts := strings.Split(channelID, ":")
	if len(parts) < 2 || parts[1] == "" {
		return "", shared.NewValidationError(adapterName, invalidChannelIDPrefix+channelID)
	}
	return parts[1], nil
}

// ScheduleMessage implements chat.MessageScheduler (chat.scheduleMessage).
func (a *SlackAdapter) ScheduleMessage(ctx context.Context, threadID string, msg chat.AdapterPostableMessage, postAt time.Time) (*chat.RawMessage, error) {
	message, err := a.resolveMessageMentions(ctx, msg, threadID)
	if err != nil {
		return nil, err
	}
	id, err := a.decodeThreadID(threadID)
	if err != nil {
		return nil, err
	}
	if postAt.Unix() <= a.clock().Unix() {
		return nil, shared.NewValidationError(adapterName, schedulePastMessage)
	}
	if len(shared.ExtractFiles(message)) > 0 {
		return nil, shared.NewValidationError(adapterName, scheduleFilesMessage)
	}
	text, markdown, blocks := a.renderPostable(message)
	body := map[string]any{
		"channel":      id.Channel,
		"post_at":      postAt.Unix(),
		"unfurl_links": false,
		"unfurl_media": false,
	}
	if id.ThreadTS != "" {
		body["thread_ts"] = id.ThreadTS
	}
	switch {
	case len(blocks) > 0:
		body["text"] = text
		body["blocks"] = blocks
	case markdown != "":
		body["markdown_text"] = markdown
	case text != "":
		body["text"] = text
	}
	resp, err := a.slackCall(ctx, methodScheduleMessage, body)
	if err != nil {
		return nil, err
	}
	var payload struct {
		ScheduledMessageID string `json:"scheduled_message_id"`
	}
	_ = json.Unmarshal(resp.Raw, &payload)
	var raw any
	if len(resp.Raw) > 0 {
		_ = json.Unmarshal(resp.Raw, &raw)
	}
	return &chat.RawMessage{ID: payload.ScheduledMessageID, Channel: threadID, Raw: raw}, nil
}

// FetchChannelInfo implements chat.ChannelReader.
func (a *SlackAdapter) FetchChannelInfo(ctx context.Context, channelID string) (chat.ChannelInfo, error) {
	channel, err := a.decodeChannelID(channelID)
	if err != nil {
		return chat.ChannelInfo{}, err
	}
	resp, err := a.slackCall(ctx, methodConvInfo, map[string]any{"channel": channel})
	if err != nil {
		return chat.ChannelInfo{}, err
	}
	var payload struct {
		Channel struct {
			IsExtShared bool   `json:"is_ext_shared"`
			IsIM        bool   `json:"is_im"`
			IsMPIM      bool   `json:"is_mpim"`
			IsPrivate   bool   `json:"is_private"`
			Name        string `json:"name"`
			NumMembers  int    `json:"num_members"`
			Purpose     struct {
				Value string `json:"value"`
			} `json:"purpose"`
			Topic struct {
				Value string `json:"value"`
			} `json:"topic"`
		} `json:"channel"`
	}
	if err := json.Unmarshal(resp.Raw, &payload); err != nil {
		return chat.ChannelInfo{}, err
	}
	info := payload.Channel
	if info.IsExtShared {
		a.markExternalChannel(channel)
	}
	vis := chat.ChannelUnknown
	switch {
	case info.IsExtShared:
		vis = chat.ChannelExternal
	case info.IsIM || info.IsMPIM || info.IsPrivate || strings.HasPrefix(channel, "D"):
		vis = chat.ChannelPrivate
	case strings.HasPrefix(channel, "C"):
		vis = chat.ChannelWorkspace
	}
	name := ""
	if info.Name != "" {
		name = "#" + info.Name
	}
	return chat.ChannelInfo{
		ID:                channelID,
		Name:              name,
		IsDM:              info.IsIM || info.IsMPIM,
		MemberCount:       info.NumMembers,
		ChannelVisibility: vis,
		Metadata: map[string]any{
			"purpose": info.Purpose.Value,
			"topic":   info.Topic.Value,
		},
	}, nil
}

// FetchChannelMessages implements chat.ChannelReader (conversations.history).
func (a *SlackAdapter) FetchChannelMessages(ctx context.Context, channelID string, opts chat.FetchOptions) (chat.FetchResult, error) {
	channel, err := a.decodeChannelID(channelID)
	if err != nil {
		return chat.FetchResult{}, err
	}
	limit := opts.Limit
	if limit == 0 {
		limit = defaultChannelLimit
	}
	if opts.Direction == chat.FetchForward {
		return a.fetchChannelMessagesPage(ctx, channel, limit, opts.Cursor, true)
	}
	return a.fetchChannelMessagesPage(ctx, channel, limit, opts.Cursor, false)
}

func (a *SlackAdapter) fetchChannelMessagesPage(ctx context.Context, channel string, limit int, cursor string, forward bool) (chat.FetchResult, error) {
	body := map[string]any{"channel": channel, "limit": limit}
	if cursor != "" {
		if forward {
			body["oldest"] = cursor
		} else {
			body["latest"] = cursor
		}
		body["inclusive"] = false
	}
	resp, err := a.slackCall(ctx, methodConversationsHistory, body)
	if err != nil {
		return chat.FetchResult{}, err
	}
	msgs, hasMore, _, err := historyPayload(resp)
	if err != nil {
		return chat.FetchResult{}, err
	}
	chronological := make([]SlackEvent, len(msgs))
	copy(chronological, msgs)
	for i, j := 0, len(chronological)-1; i < j; i, j = i+1, j-1 {
		chronological[i], chronological[j] = chronological[j], chronological[i]
	}
	parsed := make([]*chat.Message, 0, len(chronological))
	for _, msg := range chronological {
		threadTS := msg.ThreadTs
		if threadTS == "" {
			threadTS = msg.Ts
		}
		threadID := a.encodeThreadID(ThreadID{Channel: channel, ThreadTS: threadTS})
		got, err := a.parseSlackMessage(ctx, msg, threadID)
		if err != nil {
			return chat.FetchResult{}, err
		}
		parsed = append(parsed, got)
	}
	var next string
	if hasMore && len(chronological) > 0 {
		if forward {
			next = chronological[len(chronological)-1].Ts
		} else {
			next = chronological[0].Ts
		}
	}
	return chat.FetchResult{Messages: parsed, NextCursor: next}, nil
}

// ListThreads implements chat.ChannelReader.
func (a *SlackAdapter) ListThreads(ctx context.Context, channelID string, opts chat.FetchOptions) (chat.ListThreadsResult, error) {
	channel, err := a.decodeChannelID(channelID)
	if err != nil {
		return chat.ListThreadsResult{}, err
	}
	limit := opts.Limit
	if limit == 0 {
		limit = defaultThreadLimit
	}
	body := map[string]any{"channel": channel, "limit": min(limit*3, maxHistoryFetch)}
	if opts.Cursor != "" {
		body["cursor"] = opts.Cursor
	}
	resp, err := a.slackCall(ctx, methodConversationsHistory, body)
	if err != nil {
		return chat.ListThreadsResult{}, err
	}
	msgs, _, next, err := historyPayload(resp)
	if err != nil {
		return chat.ListThreadsResult{}, err
	}
	threads := make([]chat.ThreadSummary, 0, limit)
	for _, msg := range msgs {
		if msg.ReplyCount <= 0 {
			continue
		}
		threadID := a.encodeThreadID(ThreadID{Channel: channel, ThreadTS: msg.Ts})
		root, err := a.parseSlackMessage(ctx, msg, threadID)
		if err != nil {
			return chat.ListThreadsResult{}, err
		}
		var lastReply time.Time
		if msg.LatestReply != "" {
			lastReply = parseSlackTimestamp(msg.LatestReply)
		}
		threads = append(threads, chat.ThreadSummary{
			ID:          threadID,
			RootMessage: root,
			ReplyCount:  msg.ReplyCount,
			LastReplyAt: lastReply,
		})
		if len(threads) == limit {
			break
		}
	}
	return chat.ListThreadsResult{Threads: threads, NextCursor: next}, nil
}

// PostChannelMessage implements chat.ChannelReader.
func (a *SlackAdapter) PostChannelMessage(ctx context.Context, channelID string, msg chat.AdapterPostableMessage) (*chat.RawMessage, error) {
	channel, err := a.decodeChannelID(channelID)
	if err != nil {
		return nil, err
	}
	synthetic := a.encodeThreadID(ThreadID{Channel: channel, ThreadTS: ""})
	result, err := a.PostMessage(ctx, synthetic, msg)
	if err != nil {
		return nil, err
	}
	ts := rawMessageTS(result)
	if ts == "" {
		return result, nil
	}
	out := *result
	out.Channel = a.encodeThreadID(ThreadID{Channel: channel, ThreadTS: ts})
	return &out, nil
}

// PostObject implements chat.ObjectPoster. Unsupported kinds fall back to
// PostMessage("["+kind+"]"); "plan" posts Block Kit plan/task_card blocks.
func (a *SlackAdapter) PostObject(ctx context.Context, threadID, kind string, data any) (*chat.RawMessage, error) {
	if kind != "plan" {
		return a.PostMessage(ctx, threadID, chat.PostableText("["+kind+"]"))
	}
	id, err := a.decodeThreadID(threadID)
	if err != nil {
		return nil, err
	}
	plan := asPlanModel(data)
	text, markdown, blocks := a.renderPlan(plan)
	posted, err := a.apiClient().PostMessage(ctx, api.MessageOptions{
		Channel:      id.Channel,
		Text:         text,
		MarkdownText: markdown,
		Blocks:       blocks,
		ThreadTS:     id.ThreadTS,
		UnfurlLinks:  falsePtr(),
		UnfurlMedia:  falsePtr(),
	})
	if err != nil {
		return nil, a.handleSlackError(err)
	}
	return postedRaw(posted, threadID, nil), nil
}

// EditObject implements chat.ObjectPoster.
func (a *SlackAdapter) EditObject(ctx context.Context, threadID, messageID, kind string, data any) (*chat.RawMessage, error) {
	if kind != "plan" {
		return a.EditMessage(ctx, threadID, messageID, chat.PostableText("["+kind+"]"))
	}
	id, err := a.decodeThreadID(threadID)
	if err != nil {
		return nil, err
	}
	plan := asPlanModel(data)
	text, markdown, blocks := a.renderPlan(plan)
	posted, err := a.apiClient().UpdateMessage(ctx, api.UpdateOptions{
		TS: messageID,
		MessageOptions: api.MessageOptions{
			Channel:      id.Channel,
			Text:         text,
			MarkdownText: markdown,
			Blocks:       blocks,
		},
	})
	if err != nil {
		return nil, a.handleSlackError(err)
	}
	return postedRaw(posted, threadID, nil), nil
}

type planModel struct {
	Tasks []planTask `json:"tasks"`
	Title string     `json:"title"`
}

type planTask struct {
	Details any    `json:"details"`
	ID      string `json:"id"`
	Output  any    `json:"output"`
	Status  string `json:"status"`
	Title   string `json:"title"`
}

func asPlanModel(data any) planModel {
	switch v := data.(type) {
	case planModel:
		return v
	case *planModel:
		if v == nil {
			return planModel{}
		}
		return *v
	default:
		raw, err := json.Marshal(data)
		if err != nil {
			return planModel{}
		}
		var p planModel
		_ = json.Unmarshal(raw, &p)
		return p
	}
}

func (a *SlackAdapter) renderPlan(plan planModel) (text, markdown string, blocks []any) {
	return a.renderPlanFallbackText(plan), "", a.planToBlockKit(plan)
}

func (a *SlackAdapter) renderPlanFallbackText(plan planModel) string {
	title := plan.Title
	if title == "" {
		title = "Plan"
	}
	lines := []string{title}
	for _, task := range plan.Tasks {
		lines = append(lines, fmt.Sprintf("- (%s) %s", task.Status, task.Title))
	}
	return strings.Join(lines, "\n")
}

func (a *SlackAdapter) planToBlockKit(plan planModel) []any {
	title := plan.Title
	if title == "" {
		title = "Plan"
	}
	tasks := make([]any, 0, len(plan.Tasks))
	for _, task := range plan.Tasks {
		card := map[string]any{
			"type":    "task_card",
			"task_id": task.ID,
			"title":   task.Title,
			"status":  task.Status,
		}
		if details := planContentToRichText(task.Details); details != nil {
			card["details"] = details
		}
		if output := planContentToRichText(task.Output); output != nil {
			card["output"] = output
		}
		tasks = append(tasks, card)
	}
	return []any{map[string]any{"type": "plan", "title": title, "tasks": tasks}}
}

func planContentToRichText(content any) map[string]any {
	if content == nil {
		return nil
	}
	switch v := content.(type) {
	case []string:
		if len(v) == 0 {
			return nil
		}
		items := make([]any, 0, len(v))
		for _, item := range v {
			items = append(items, map[string]any{
				"type":     "rich_text_section",
				"elements": []any{map[string]any{"type": "text", "text": item}},
			})
		}
		return map[string]any{
			"type": "rich_text",
			"elements": []any{map[string]any{
				"type": "rich_text_list", "style": "bullet", "elements": items,
			}},
		}
	case []any:
		if len(v) == 0 {
			return nil
		}
		items := make([]any, 0, len(v))
		for _, item := range v {
			items = append(items, map[string]any{
				"type":     "rich_text_section",
				"elements": []any{map[string]any{"type": "text", "text": fmt.Sprint(item)}},
			})
		}
		return map[string]any{
			"type": "rich_text",
			"elements": []any{map[string]any{
				"type": "rich_text_list", "style": "bullet", "elements": items,
			}},
		}
	}
	text := planContentToPlainText(content)
	if text == "" {
		return nil
	}
	return map[string]any{
		"type": "rich_text",
		"elements": []any{map[string]any{
			"type":     "rich_text_section",
			"elements": []any{map[string]any{"type": "text", "text": text}},
		}},
	}
}

func planContentToPlainText(content any) string {
	if content == nil {
		return ""
	}
	switch v := content.(type) {
	case string:
		return v
	case []string:
		return strings.Join(v, "\n")
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, fmt.Sprint(item))
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		if md, ok := v["markdown"].(string); ok {
			return chat.ToPlainText(chat.ParseMarkdown(md), nil)
		}
		if node, ok := v["ast"].(ast.Node); ok {
			return chat.ToPlainText(node, nil)
		}
	case ast.Node:
		return chat.ToPlainText(v, nil)
	}
	return ""
}

func historyPayload(resp api.Response) (msgs []SlackEvent, hasMore bool, next string, err error) {
	var payload struct {
		HasMore  bool         `json:"has_more"`
		Messages []SlackEvent `json:"messages"`
		Meta     struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	if err := json.Unmarshal(resp.Raw, &payload); err != nil {
		return nil, false, "", err
	}
	return payload.Messages, payload.HasMore, payload.Meta.NextCursor, nil
}

func rawMessageTS(msg *chat.RawMessage) string {
	if msg == nil {
		return ""
	}
	m, ok := msg.Raw.(map[string]any)
	if !ok {
		return ""
	}
	s, _ := m["ts"].(string)
	return s
}
