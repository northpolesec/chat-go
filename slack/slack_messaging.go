// Ported from packages/adapter-slack/src/index.ts @ 6adca36 (chat v4.40.0)
// (partial: PostMessage, EditMessage, DeleteMessage, PostEphemeral, reactions,
// typing, OpenDM, Fetch*, OpenModal, renderFormatted).
package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/northpolesec/chat-go/slack/api"
	"github.com/yuin/goldmark/ast"
)

var (
	_ chat.Adapter         = (*SlackAdapter)(nil)
	_ chat.EphemeralPoster = (*SlackAdapter)(nil)
	_ chat.TypingNotifier  = (*SlackAdapter)(nil)
	_ chat.DMOpener        = (*SlackAdapter)(nil)
	_ chat.MessageFetcher  = (*SlackAdapter)(nil)
	_ chat.ModalOpener     = (*SlackAdapter)(nil)
)

const (
	fileOnlyIDPrefix   = "file-"
	openDMFailed       = "Failed to open DM - no channel returned"
	defaultTyping      = "Typing..."
	methodReactionsAdd = "reactions.add"
	methodReactionsRm  = "reactions.remove"
	methodViewsUpdate  = "views.update"
	methodConvOpen     = "conversations.open"
	methodConvInfo     = "conversations.info"
	methodSetStatus    = "assistant.threads.setStatus"
	methodSessionStat  = "agents.sessions.setStatus"
)

// ThreadInfo is chat.ThreadInfo; the alias keeps the Slack package's exported name.
type ThreadInfo = chat.ThreadInfo

func falsePtr() *bool { v := false; return &v }

func (a *SlackAdapter) handleSlackError(err error) error {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) && apiErr.Code == "ratelimited" {
		return shared.NewAdapterRateLimitError(adapterName, 0)
	}
	return err
}

// slackCall is the form-encoded helper. Grid extras and the token resolve
// once inside api.Client.Call (apiClient Extra + getToken TokenSource).
func (a *SlackAdapter) slackCall(ctx context.Context, method string, body map[string]any) (api.Response, error) {
	resp, err := a.apiClient().Call(ctx, method, body, api.EncodingForm)
	if err != nil {
		return resp, a.handleSlackError(err)
	}
	if !resp.OK {
		code := resp.Error
		if code == "" {
			code = "unknown_error"
		}
		return resp, a.handleSlackError(&api.APIError{Code: code})
	}
	return resp, nil
}

func (a *SlackAdapter) renderPostable(message chat.AdapterPostableMessage) (text, markdown string, blocks []any) {
	if card := shared.ExtractCard(message); card != nil {
		return CardToFallbackText(*card), "", blocksAsAny(CardToBlockKit(*card))
	}
	payload := a.format.ToSlackPayload(message)
	return payload.Text, payload.MarkdownText, nil
}

// RenderFormatted is adapter.renderFormatted (goldmark AST → markdown).
func (a *SlackAdapter) RenderFormatted(content chat.FormattedContent) string {
	node, _ := content.(ast.Node)
	return a.format.FromAst(node, nil)
}

// PostMessage implements chat.Adapter.
func (a *SlackAdapter) PostMessage(ctx context.Context, threadID string, msg chat.AdapterPostableMessage) (*chat.RawMessage, error) {
	message, err := a.resolveMessageMentions(ctx, msg, threadID)
	if err != nil {
		return nil, err
	}
	id, err := a.decodeThreadID(threadID)
	if err != nil {
		return nil, err
	}
	threadTS := id.ThreadTS
	uploaded, err := a.uploadPostableFiles(ctx, message, id.Channel, threadTS)
	if err != nil {
		return nil, a.handleSlackError(err)
	}
	if uploaded != nil && !postableHasTextOrCard(message) {
		return &chat.RawMessage{
			ID:      fmt.Sprintf("%s%d", fileOnlyIDPrefix, time.Now().UnixMilli()),
			Channel: threadID,
			Raw:     map[string]any{"files": shared.ExtractFiles(message), "uploadedFileIds": uploaded},
		}, nil
	}
	text, markdown, blocks := a.renderPostable(message)
	opts := api.MessageOptions{
		Channel:      id.Channel,
		Text:         text,
		MarkdownText: markdown,
		Blocks:       blocks,
		ThreadTS:     threadTS,
		UnfurlLinks:  falsePtr(),
		UnfurlMedia:  falsePtr(),
	}
	posted, err := a.apiClient().PostMessage(ctx, opts)
	if err != nil {
		return nil, a.handleSlackError(err)
	}
	return postedRaw(posted, threadID, uploaded), nil
}

// PostEphemeral implements chat.EphemeralPoster.
func (a *SlackAdapter) PostEphemeral(ctx context.Context, threadID, userID string, msg chat.AdapterPostableMessage) (*chat.RawMessage, error) {
	message, err := a.resolveMessageMentions(ctx, msg, threadID)
	if err != nil {
		return nil, err
	}
	id, err := a.decodeThreadID(threadID)
	if err != nil {
		return nil, err
	}
	text, markdown, blocks := a.renderPostable(message)
	posted, err := a.apiClient().PostEphemeral(ctx, api.EphemeralOptions{
		User: userID,
		MessageOptions: api.MessageOptions{
			Channel:      id.Channel,
			Text:         text,
			MarkdownText: markdown,
			Blocks:       blocks,
			ThreadTS:     id.ThreadTS,
		},
	})
	if err != nil {
		return nil, a.handleSlackError(err)
	}
	return postedRaw(posted, threadID, nil), nil
}

// EditMessage implements chat.Adapter.
func (a *SlackAdapter) EditMessage(ctx context.Context, threadID, messageID string, msg chat.AdapterPostableMessage) (*chat.RawMessage, error) {
	message, err := a.resolveMessageMentions(ctx, msg, threadID)
	if err != nil {
		return nil, err
	}
	if eph := a.decodeEphemeralMessageID(messageID); eph != nil {
		id, err := a.decodeThreadID(threadID)
		if err != nil {
			return nil, err
		}
		raw, err := a.sendToResponseURL(ctx, eph.ResponseURL, "replace", message, id.ThreadTS)
		if err != nil {
			return nil, err
		}
		raw["ephemeral"] = true
		return &chat.RawMessage{ID: eph.MessageTS, Channel: threadID, Raw: raw}, nil
	}
	if strings.HasPrefix(messageID, "ephemeral:") {
		return nil, shared.NewValidationError(adapterName, invalidEphemeral)
	}
	id, err := a.decodeThreadID(threadID)
	if err != nil {
		return nil, err
	}
	text, markdown, blocks := a.renderPostable(message)
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

// DeleteMessage implements chat.Adapter.
func (a *SlackAdapter) DeleteMessage(ctx context.Context, threadID, messageID string) error {
	if eph := a.decodeEphemeralMessageID(messageID); eph != nil {
		_, err := a.sendToResponseURL(ctx, eph.ResponseURL, "delete", nil, "")
		return err
	}
	if strings.HasPrefix(messageID, "ephemeral:") {
		return shared.NewValidationError(adapterName, invalidEphemeral)
	}
	id, err := a.decodeThreadID(threadID)
	if err != nil {
		return err
	}
	if err := a.apiClient().DeleteMessage(ctx, id.Channel, messageID); err != nil {
		return a.handleSlackError(err)
	}
	return nil
}

// AddReaction implements chat.Adapter.
func (a *SlackAdapter) AddReaction(ctx context.Context, threadID, messageID string, emoji chat.EmojiValue) error {
	return a.reactionCall(ctx, methodReactionsAdd, threadID, messageID, emoji)
}

// RemoveReaction implements chat.Adapter.
func (a *SlackAdapter) RemoveReaction(ctx context.Context, threadID, messageID string, emoji chat.EmojiValue) error {
	return a.reactionCall(ctx, methodReactionsRm, threadID, messageID, emoji)
}

func (a *SlackAdapter) reactionCall(ctx context.Context, method, threadID, messageID string, emoji chat.EmojiValue) error {
	id, err := a.decodeThreadID(threadID)
	if err != nil {
		return err
	}
	name := strings.ReplaceAll(chat.DefaultEmojiResolver.ToSlack(emoji.Name), ":", "")
	_, err = a.slackCall(ctx, method, map[string]any{
		"channel":   id.Channel,
		"timestamp": messageID,
		"name":      name,
	})
	return err
}

// StartTyping implements chat.Adapter. Empty status is unset (agent-view → processing).
func (a *SlackAdapter) StartTyping(ctx context.Context, threadID, status string, opts chat.TypingOptions) error {
	id, err := a.decodeThreadID(threadID)
	if err != nil {
		return err
	}
	if id.ThreadTS == "" {
		a.logger.Debug("Slack: startTyping skipped - no thread context")
		return nil
	}
	if a.agentView && status == "" {
		if err := a.setSessionStatus(ctx, id.Channel, id.ThreadTS, string(chat.AgentSessionProcessing), opts.InitiatorUserID); err != nil {
			a.logger.Warn("Slack API: agents.sessions.setStatus failed", "channel", id.Channel, "threadTs", id.ThreadTS, "error", err)
		}
		return nil
	}
	loading := a.loadingMessages
	statusText := status
	if statusText == "" {
		if len(loading) > 0 {
			statusText = loading[0]
		} else {
			statusText = defaultTyping
		}
		if len(loading) == 0 {
			loading = []string{defaultTyping}
		}
	} else {
		loading = []string{status}
	}
	_, err = a.slackCall(ctx, methodSetStatus, map[string]any{
		"channel_id":       id.Channel,
		"thread_ts":        id.ThreadTS,
		"status":           statusText,
		"loading_messages": loading,
	})
	if err != nil {
		a.logger.Warn("Slack API: assistant.threads.setStatus failed", "channel", id.Channel, "threadTs", id.ThreadTS, "error", err)
	}
	return nil
}

// EndTyping implements chat.TypingNotifier.
func (a *SlackAdapter) EndTyping(ctx context.Context, threadID string, status chat.AgentSessionStatus) error {
	if !a.agentView {
		return nil
	}
	if status == "" {
		status = chat.AgentSessionActive
	}
	id, err := a.decodeThreadID(threadID)
	if err != nil || id.ThreadTS == "" {
		return nil
	}
	if err := a.setSessionStatus(ctx, id.Channel, id.ThreadTS, string(status), ""); err != nil {
		a.logger.Warn("Slack API: agents.sessions.setStatus failed", "channel", id.Channel, "threadTs", id.ThreadTS, "error", err)
	}
	return nil
}

func (a *SlackAdapter) setSessionStatus(ctx context.Context, channel, threadTS, status, initiator string) error {
	body := map[string]any{
		"channel_id": channel,
		"thread_ts":  threadTS,
		"status":     status,
	}
	if initiator != "" {
		body["initiator_user_id"] = initiator
	}
	_, err := a.slackCall(ctx, methodSessionStat, body)
	return err
}

// OpenDM implements chat.DMOpener.
func (a *SlackAdapter) OpenDM(ctx context.Context, userID string) (string, error) {
	resp, err := a.slackCall(ctx, methodConvOpen, map[string]any{"users": userID})
	if err != nil {
		return "", err
	}
	var payload struct {
		Channel struct {
			ID string `json:"id"`
		} `json:"channel"`
	}
	if err := json.Unmarshal(resp.Raw, &payload); err != nil || payload.Channel.ID == "" {
		return "", shared.NewNetworkError(adapterName, openDMFailed, err)
	}
	return a.encodeThreadID(ThreadID{Channel: payload.Channel.ID, ThreadTS: ""}), nil
}

// FetchMessages implements chat.Adapter.
func (a *SlackAdapter) FetchMessages(ctx context.Context, threadID string, opts chat.FetchOptions) (chat.FetchResult, error) {
	id, err := a.decodeThreadID(threadID)
	if err != nil {
		return chat.FetchResult{}, err
	}
	limit := opts.Limit
	if limit == 0 {
		limit = 100
	}
	if opts.Direction == chat.FetchForward {
		return a.fetchMessagesForward(ctx, id.Channel, id.ThreadTS, threadID, limit, opts.Cursor)
	}
	return a.fetchMessagesBackward(ctx, id.Channel, id.ThreadTS, threadID, limit, opts.Cursor)
}

func (a *SlackAdapter) fetchMessagesForward(ctx context.Context, channel, threadTS, threadID string, limit int, cursor string) (chat.FetchResult, error) {
	resp, err := a.apiClient().FetchThreadReplies(ctx, api.RepliesOptions{
		Channel: channel,
		TS:      threadTS,
		Limit:   limit,
		Cursor:  cursor,
	})
	if err != nil {
		return chat.FetchResult{}, a.handleSlackError(err)
	}
	msgs, next, _, err := repliesPayload(resp)
	if err != nil {
		return chat.FetchResult{}, err
	}
	parsed, err := a.parseReplyMessages(ctx, msgs, threadID)
	if err != nil {
		return chat.FetchResult{}, err
	}
	return chat.FetchResult{Messages: parsed, NextCursor: next}, nil
}

func (a *SlackAdapter) fetchMessagesBackward(ctx context.Context, channel, threadTS, threadID string, limit int, cursor string) (chat.FetchResult, error) {
	fetchLimit := min(1000, max(limit*2, 200))
	inclusive := false
	resp, err := a.apiClient().FetchThreadReplies(ctx, api.RepliesOptions{
		Channel:   channel,
		TS:        threadTS,
		Limit:     fetchLimit,
		Latest:    cursor,
		Inclusive: &inclusive,
	})
	if err != nil {
		return chat.FetchResult{}, a.handleSlackError(err)
	}
	msgs, _, hasMore, err := repliesPayload(resp)
	if err != nil {
		return chat.FetchResult{}, err
	}
	start := max(0, len(msgs)-limit)
	selected := msgs[start:]
	parsed, err := a.parseReplyMessages(ctx, selected, threadID)
	if err != nil {
		return chat.FetchResult{}, err
	}
	var next string
	if start > 0 || hasMore {
		if len(selected) > 0 {
			next = selected[0].Ts
		}
	}
	return chat.FetchResult{Messages: parsed, NextCursor: next}, nil
}

// FetchThread is adapter.fetchThread (Slack-specific).
func (a *SlackAdapter) FetchThread(ctx context.Context, threadID string) (ThreadInfo, error) {
	id, err := a.decodeThreadID(threadID)
	if err != nil {
		return ThreadInfo{}, err
	}
	resp, err := a.slackCall(ctx, methodConvInfo, map[string]any{"channel": id.Channel})
	if err != nil {
		return ThreadInfo{}, err
	}
	var payload struct {
		Channel map[string]any `json:"channel"`
	}
	if err := json.Unmarshal(resp.Raw, &payload); err != nil {
		return ThreadInfo{}, err
	}
	name, _ := payload.Channel["name"].(string)
	ext, _ := payload.Channel["is_ext_shared"].(bool)
	priv, _ := payload.Channel["is_private"].(bool)
	if ext {
		a.markExternalChannel(id.Channel)
	}
	vis := chat.ChannelUnknown
	switch {
	case ext:
		vis = chat.ChannelExternal
	case priv || strings.HasPrefix(id.Channel, "D"):
		vis = chat.ChannelPrivate
	case strings.HasPrefix(id.Channel, "C"):
		vis = chat.ChannelWorkspace
	}
	return ThreadInfo{
		ID:                threadID,
		ChannelID:         id.Channel,
		ChannelName:       name,
		ChannelVisibility: vis,
		Metadata:          map[string]any{"threadTs": id.ThreadTS, "channel": payload.Channel},
	}, nil
}

// FetchMessage implements chat.MessageFetcher. Missing message is (nil, nil).
func (a *SlackAdapter) FetchMessage(ctx context.Context, threadID, messageID string) (*chat.Message, error) {
	id, err := a.decodeThreadID(threadID)
	if err != nil {
		return nil, err
	}
	inclusive := true
	resp, err := a.apiClient().FetchThreadReplies(ctx, api.RepliesOptions{
		Channel:   id.Channel,
		TS:        id.ThreadTS,
		Oldest:    messageID,
		Inclusive: &inclusive,
		Limit:     1,
	})
	if err != nil {
		return nil, a.handleSlackError(err)
	}
	msgs, _, _, err := repliesPayload(resp)
	if err != nil {
		return nil, err
	}
	for _, msg := range msgs {
		if msg.Ts == messageID {
			return a.parseSlackMessage(ctx, msg, threadID)
		}
	}
	return nil, nil
}

// OpenModal implements chat.ModalOpener.
func (a *SlackAdapter) OpenModal(ctx context.Context, triggerID string, modal *chat.Modal) (string, error) {
	return a.openModal(ctx, triggerID, modal, "")
}

func (a *SlackAdapter) openModal(ctx context.Context, triggerID string, modal *chat.Modal, contextID string) (string, error) {
	if modal == nil {
		return "", shared.NewValidationError(adapterName, "modal is required")
	}
	metadata := EncodeModalMetadata(ModalMetadata{ContextID: contextID, PrivateMetadata: modal.PrivateMetadata})
	view := ModalToSlackView(*modal, metadata)
	raw, err := json.Marshal(view)
	if err != nil {
		return "", err
	}
	resp, err := a.apiClient().OpenView(ctx, triggerID, raw)
	if err != nil {
		return "", a.handleSlackError(err)
	}
	return viewIDFrom(resp), nil
}

// UpdateModal implements chat.ModalOpener.
func (a *SlackAdapter) UpdateModal(ctx context.Context, viewID string, modal *chat.Modal) error {
	_, err := a.updateModal(ctx, viewID, modal)
	return err
}

func (a *SlackAdapter) updateModal(ctx context.Context, viewID string, modal *chat.Modal) (string, error) {
	if modal == nil {
		return "", shared.NewValidationError(adapterName, "modal is required")
	}
	view := ModalToSlackView(*modal, "")
	raw, err := json.Marshal(view)
	if err != nil {
		return "", err
	}
	var viewVal any
	if err := json.Unmarshal(raw, &viewVal); err != nil {
		return "", err
	}
	resp, err := a.slackCall(ctx, methodViewsUpdate, map[string]any{
		"view_id": viewID,
		"view":    viewVal,
	})
	if err != nil {
		return "", err
	}
	return viewIDFrom(resp), nil
}

func (a *SlackAdapter) uploadPostableFiles(ctx context.Context, message chat.AdapterPostableMessage, channel, threadTS string) ([]string, error) {
	files := shared.ExtractFiles(message)
	if len(files) == 0 {
		return nil, nil
	}
	uploads := make([]api.FileUpload, 0, len(files))
	for _, f := range files {
		if len(f.Data) == 0 {
			continue
		}
		uploads = append(uploads, api.FileUpload{Data: f.Data, Filename: f.Filename})
	}
	if len(uploads) == 0 {
		return []string{}, nil
	}
	got, err := a.apiClient().UploadFiles(ctx, api.UploadOptions{
		ChannelID: channel,
		ThreadTS:  threadTS,
		Files:     uploads,
	})
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(got))
	for i, f := range got {
		ids[i] = f.ID
	}
	return ids, nil
}

func (a *SlackAdapter) parseReplyMessages(ctx context.Context, msgs []SlackEvent, threadID string) ([]*chat.Message, error) {
	out := make([]*chat.Message, 0, len(msgs))
	for _, msg := range msgs {
		parsed, err := a.parseSlackMessage(ctx, msg, threadID)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
	}
	return out, nil
}

func repliesPayload(resp api.Response) (msgs []SlackEvent, next string, hasMore bool, err error) {
	var payload struct {
		HasMore  bool         `json:"has_more"`
		Messages []SlackEvent `json:"messages"`
		Meta     struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	if err := json.Unmarshal(resp.Raw, &payload); err != nil {
		return nil, "", false, err
	}
	return payload.Messages, payload.Meta.NextCursor, payload.HasMore, nil
}

func postedRaw(posted api.PostedMessage, threadID string, uploaded []string) *chat.RawMessage {
	var raw any
	if len(posted.Raw.Raw) > 0 {
		_ = json.Unmarshal(posted.Raw.Raw, &raw)
	}
	if uploaded != nil {
		if m, ok := raw.(map[string]any); ok {
			m["uploadedFileIds"] = uploaded
			raw = m
		} else {
			raw = map[string]any{"uploadedFileIds": uploaded}
		}
	}
	return &chat.RawMessage{ID: posted.ID, Channel: threadID, Raw: raw}
}

func viewIDFrom(resp api.Response) string {
	var payload struct {
		View struct {
			ID string `json:"id"`
		} `json:"view"`
	}
	_ = json.Unmarshal(resp.Raw, &payload)
	return payload.View.ID
}

func postableHasTextOrCard(message chat.AdapterPostableMessage) bool {
	if shared.ExtractCard(message) != nil {
		return true
	}
	switch m := message.(type) {
	case chat.PostableText:
		return true
	case chat.PostableRaw:
		return m.Raw != ""
	case chat.PostableMarkdown:
		return m.Markdown != ""
	case chat.PostableAst:
		return m.AST != nil
	default:
		return false
	}
}
