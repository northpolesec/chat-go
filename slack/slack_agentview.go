// Ported from packages/adapter-slack/src/index.ts @ 6adca36 (chat v4.40.0)
// (partial: publishHomeView, setSuggestedPrompts, setAssistantStatus,
// setSessionStatus, setAssistantTitle, applyConfiguredSuggestedPrompts,
// applyConfiguredSessionTitle, handleAppContextChanged, app_home entities).
// Divergences: AgentViewPublisher methods take encoded thread IDs; Slack
// channel+thread_ts helpers are unexported (same-package tests call them).
// StartTyping "" remains unset → processing (JS explicit "" → active cannot
// be expressed on a string status). views.publish / setSuggestedPrompts use
// EncodingJSON; setStatus / setTitle / rename use slackCall (form).
package slack

import (
	"context"
	"strings"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/slack/api"
)

var _ chat.AgentViewPublisher = (*SlackAdapter)(nil)

const (
	maxSuggestedPrompts = 4
	maxSessionTitleLen  = 80
	methodSetPrompts    = "assistant.threads.setSuggestedPrompts"
	methodSetTitle      = "assistant.threads.setTitle"
	methodViewsPublish  = "views.publish"
	methodSessionRename = "agents.sessions.rename"
)

// SuggestedPromptsContext is upstream SlackSuggestedPromptsContext.
type SuggestedPromptsContext struct {
	ChannelID    string
	EnterpriseID string
	Entities     []chat.AppContextEntity
	TeamID       string
	ThreadTS     string
	UserID       string
}

// SuggestedPromptsResolver is the function form of SlackSuggestedPrompts.
// Returning (nil, nil) skips setting prompts.
type SuggestedPromptsResolver func(ctx context.Context, in SuggestedPromptsContext) (*chat.SuggestedPrompts, error)

// PublishHomeView implements chat.AgentViewPublisher.
func (a *SlackAdapter) PublishHomeView(ctx context.Context, userID string, card any) error {
	_, err := a.apiClient().Call(ctx, methodViewsPublish, map[string]any{
		"user_id": userID,
		"view":    homeViewFromCard(card),
	}, api.EncodingJSON)
	return err
}

func homeViewFromCard(card any) any {
	switch v := card.(type) {
	case map[string]any:
		if anyString(v["type"]) != "" {
			return v
		}
	case chat.Card:
		return map[string]any{"type": "home", "blocks": CardToBlockKit(v)}
	case *chat.Card:
		if v != nil {
			return map[string]any{"type": "home", "blocks": CardToBlockKit(*v)}
		}
	}
	return map[string]any{"type": "home", "blocks": []SlackBlock{}}
}

// SetSuggestedPrompts implements chat.AgentViewPublisher.
func (a *SlackAdapter) SetSuggestedPrompts(ctx context.Context, threadID string, prompts chat.SuggestedPrompts) error {
	id, err := a.decodeThreadID(threadID)
	if err != nil {
		return err
	}
	return a.setSuggestedPrompts(ctx, id.Channel, id.ThreadTS, prompts.Prompts, prompts.Title)
}

func (a *SlackAdapter) setSuggestedPrompts(ctx context.Context, channel, threadTS string, prompts []chat.SuggestedPrompt, title string) error {
	if _, err := a.getToken(ctx); err != nil {
		return err
	}
	body := map[string]any{"channel_id": channel}
	if threadTS != "" {
		body["thread_ts"] = threadTS
	}
	if title != "" {
		body["title"] = title
	}
	rows := make([]map[string]string, len(prompts))
	for i, p := range prompts {
		rows[i] = map[string]string{"title": p.Title, "message": p.Message}
	}
	body["prompts"] = rows
	_, err := a.apiClient().Call(ctx, methodSetPrompts, body, api.EncodingJSON)
	return err
}

// SetAssistantStatus implements chat.AgentViewPublisher.
func (a *SlackAdapter) SetAssistantStatus(ctx context.Context, threadID, status string) error {
	id, err := a.decodeThreadID(threadID)
	if err != nil {
		return err
	}
	return a.setAssistantStatus(ctx, id.Channel, id.ThreadTS, status, nil)
}

func (a *SlackAdapter) setAssistantStatus(ctx context.Context, channel, threadTS, status string, loadingMessages []string) error {
	if a.agentView && strings.TrimSpace(status) == "" {
		return a.setSessionStatus(ctx, channel, threadTS, string(chat.AgentSessionActive), "")
	}
	body := map[string]any{
		"channel_id": channel,
		"thread_ts":  threadTS,
		"status":     status,
	}
	switch {
	case loadingMessages != nil:
		body["loading_messages"] = loadingMessages
	case len(a.loadingMessages) > 0:
		body["loading_messages"] = a.loadingMessages
	case a.agentView:
		body["loading_messages"] = []string{status}
	}
	_, err := a.slackCall(ctx, methodSetStatus, body)
	return err
}

// SetSessionStatus implements chat.AgentViewPublisher.
func (a *SlackAdapter) SetSessionStatus(ctx context.Context, threadID string, status chat.AgentSessionStatus) error {
	id, err := a.decodeThreadID(threadID)
	if err != nil {
		return err
	}
	return a.setSessionStatus(ctx, id.Channel, id.ThreadTS, string(status), "")
}

// SetAssistantTitle implements chat.AgentViewPublisher.
func (a *SlackAdapter) SetAssistantTitle(ctx context.Context, threadID, title string) error {
	id, err := a.decodeThreadID(threadID)
	if err != nil {
		return err
	}
	return a.setAssistantTitle(ctx, id.Channel, id.ThreadTS, title)
}

func (a *SlackAdapter) setAssistantTitle(ctx context.Context, channel, threadTS, title string) error {
	if a.agentView {
		return a.renameAgentSession(ctx, channel, threadTS, title)
	}
	_, err := a.slackCall(ctx, methodSetTitle, map[string]any{
		"channel_id": channel,
		"thread_ts":  threadTS,
		"title":      title,
	})
	return err
}

func (a *SlackAdapter) renameAgentSession(ctx context.Context, channel, threadTS, title string) error {
	_, err := a.slackCall(ctx, methodSessionRename, map[string]any{
		"channel_id": channel,
		"thread_ts":  threadTS,
		"title":      title,
	})
	return err
}

func (a *SlackAdapter) applyConfiguredSuggestedPrompts(ctx context.Context, in SuggestedPromptsContext) {
	if a.suggestedPromptsResolver == nil && a.suggestedPrompts == nil {
		return
	}
	resolved, err := a.resolveSuggestedPrompts(ctx, in)
	if err != nil {
		a.logger.Warn("Failed to apply configured suggested prompts", "channelId", in.ChannelID, "error", err)
		return
	}
	if resolved == nil || len(resolved.Prompts) == 0 {
		return
	}
	prompts := resolved.Prompts
	if len(prompts) > maxSuggestedPrompts {
		a.logger.Warn("Slack shows at most 4 suggested prompts; dropping the rest", "configured", len(prompts))
		prompts = prompts[:maxSuggestedPrompts]
	}
	if err := a.setSuggestedPrompts(ctx, in.ChannelID, in.ThreadTS, prompts, resolved.Title); err != nil {
		a.logger.Warn("Failed to apply configured suggested prompts", "channelId", in.ChannelID, "error", err)
	}
}

func (a *SlackAdapter) resolveSuggestedPrompts(ctx context.Context, in SuggestedPromptsContext) (*chat.SuggestedPrompts, error) {
	if a.suggestedPromptsResolver != nil {
		return a.suggestedPromptsResolver(ctx, in)
	}
	return a.suggestedPrompts, nil
}

func (a *SlackAdapter) applyConfiguredSessionTitle(ctx context.Context, event SlackEvent) {
	if !a.agentView || !a.sessionTitleEnabled() || event.Channel == "" || event.Ts == "" ||
		event.User == "" || event.Text == "" || event.BotID != "" || event.Subtype != "" || event.ThreadTs != "" {
		return
	}
	title, err := a.resolveSessionTitle(ctx, SessionTitleContext{
		ChannelID: event.Channel,
		Text:      event.Text,
		ThreadTS:  event.Ts,
		UserID:    event.User,
	})
	if err != nil {
		a.logger.Warn("Failed to set Slack agent session title", "channelId", event.Channel, "error", err, "threadTs", event.Ts)
		return
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return
	}
	if len(title) > maxSessionTitleLen {
		title = title[:maxSessionTitleLen]
	}
	if err := a.renameAgentSession(ctx, event.Channel, event.Ts, title); err != nil {
		a.logger.Warn("Failed to set Slack agent session title", "channelId", event.Channel, "error", err, "threadTs", event.Ts)
	}
}

func (a *SlackAdapter) sessionTitleEnabled() bool {
	return a.sessionTitleResolver != nil || a.sessionTitle
}

func (a *SlackAdapter) resolveSessionTitle(ctx context.Context, in SessionTitleContext) (string, error) {
	if a.sessionTitleResolver != nil {
		return a.sessionTitleResolver(ctx, in)
	}
	line, _, _ := strings.Cut(in.Text, "\n")
	return strings.TrimSpace(line), nil
}

func (a *SlackAdapter) handleAppContextChanged(ctx context.Context, raw map[string]any) {
	if a.chat == nil {
		a.logger.Warn("Chat instance not initialized, ignoring app_context_changed")
		return
	}
	p, ok := a.chat.(appContextProcessor)
	if !ok {
		return
	}
	_ = p.ProcessAppContextChanged(ctx, processAppContextInput{
		Adapter:   a,
		ChannelID: anyString(raw["channel"]),
		Entities:  NormalizeAppContextEntities(slackAppContextFromAny(raw["context"])),
		Raw:       raw,
		UserID:    anyString(raw["user"]),
	})
}

func appHomeEntities(raw map[string]any) []chat.AppContextEntity {
	if raw["context"] == nil {
		return nil
	}
	return NormalizeAppContextEntities(slackAppContextFromAny(raw["context"]))
}
