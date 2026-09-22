// Ported from packages/adapter-slack/src/index.ts @ 6adca36 (chat v4.40.0)
// (partial: handleWebhook, processEventPayload, interactive/slash/view
// dispatch, event dedupe). Socket mode and socket-forwarding are dropped.
// Divergences: HandleWebhook(w, r) writes the HTTP response (upstream
// returns Response); process* factories flatten to *Input with the parsed
// Message; waitUntil becomes SlackAdapter.pending + waitPending (tests);
// r.Context() is cancelled when the handler returns, so spawned work and
// leftover options-load use context.WithoutCancel; request context rides
// ctx values (not an adapter field); webhook.VerifyRequest wraps
// Config.WebhookVerifier; the adapter unmarshals the envelope itself —
// webhook.Parse is not on this path.
package slack

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/slack/api"
	"github.com/northpolesec/chat-go/slack/webhook"
)

const (
	eventDedupeTTL       = 24 * time.Hour
	unfurlCacheTTL       = time.Hour
	optionsLoadTimeout   = 2500 * time.Millisecond
	invalidSignatureBody = "Invalid signature"
	payloadTooLargeBody  = "Payload too large"
	invalidJSONBody      = "Invalid JSON"
	missingPayloadBody   = "Missing payload"
	invalidPayloadBody   = "Invalid payload JSON"
)

var ignoredMessageSubtypes = map[string]struct{}{
	"message_replied":   {},
	"channel_join":      {},
	"channel_leave":     {},
	"channel_topic":     {},
	"channel_purpose":   {},
	"channel_name":      {},
	"channel_archive":   {},
	"channel_unarchive": {},
	"group_join":        {},
	"group_leave":       {},
	"group_topic":       {},
	"group_purpose":     {},
	"group_name":        {},
	"group_archive":     {},
	"group_unarchive":   {},
	"ekm_access_denied": {},
	"tombstone":         {},
}

type webhookResult struct {
	status      int
	body        []byte
	contentType string
}

func (r webhookResult) write(w http.ResponseWriter) {
	if r.contentType != "" {
		w.Header().Set("Content-Type", r.contentType)
	}
	status := r.status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	if len(r.body) > 0 {
		_, _ = w.Write(r.body)
	}
}

func textResult(status int, body string) webhookResult {
	return webhookResult{status: status, body: []byte(body)}
}

func jsonResult(status int, v any) webhookResult {
	body, err := json.Marshal(v)
	if err != nil {
		return textResult(http.StatusInternalServerError, "")
	}
	return webhookResult{status: status, body: body, contentType: "application/json"}
}

type appHomeProcessor interface {
	ProcessAppHomeOpened(ctx context.Context, in processAppHomeInput) error
}

type processAppHomeInput struct {
	Adapter   chat.Adapter
	ChannelID string
	Entities  []chat.AppContextEntity
	Tab       string
	UserID    string
}

type appContextProcessor interface {
	ProcessAppContextChanged(ctx context.Context, in processAppContextInput) error
}

type processAppContextInput struct {
	Adapter   chat.Adapter
	ChannelID string
	Entities  []chat.AppContextEntity
	Raw       any
	UserID    string
}

type sessionStoppedProcessor interface {
	ProcessAgentSessionStopped(ctx context.Context, in processSessionStoppedInput) error
}

type processSessionStoppedInput struct {
	Adapter            chat.Adapter
	ChannelID          string
	StreamingMessageTs any
	ThreadID           string
	ThreadTs           string
	UserID             string
}

type titleChangedProcessor interface {
	ProcessAgentSessionTitleChanged(ctx context.Context, in processTitleChangedInput) error
}

type processTitleChangedInput struct {
	Adapter       chat.Adapter
	ChannelID     string
	PreviousTitle string
	ThreadID      string
	ThreadTs      string
	Title         string
	UserID        string
}

type optionsLoadGroup struct {
	Label   string
	Options []chat.SelectOptionElement
}

// HandleWebhook is adapter.handleWebhook. It verifies, acks, and dispatches.
func (a *SlackAdapter) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	a.handleWebhook(r).write(w)
}

func (a *SlackAdapter) handleWebhook(r *http.Request) webhookResult {
	body, err := webhook.VerifyRequest(r, a.verifyReadOptions())
	if err != nil {
		if errors.Is(err, webhook.ErrBodyTooLarge) {
			a.logger.Warn("Webhook body exceeds 25 MiB")
			return textResult(http.StatusRequestEntityTooLarge, payloadTooLargeBody)
		}
		a.logger.Warn("Webhook verifier rejected request", "error", err)
		return textResult(http.StatusUnauthorized, invalidSignatureBody)
	}
	a.logger.Debug("Slack webhook received", "bodyLength", len(body))

	contentType := r.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		params, _ := url.ParseQuery(string(body))
		if params.Has("command") && !params.Has("payload") {
			return a.runSlashCommand(r.Context(), params)
		}
		if a.token == nil {
			info := extractInstallationFromInteractive(string(body))
			if info != nil {
				tok, ok := a.resolveTokenForTeam(r.Context(), info.installationID, info.isEnterpriseInstall)
				if ok {
					rc := requestContext{
						token:               tok.token,
						botUserID:           tok.botUserID,
						enterpriseID:        info.enterpriseID,
						isEnterpriseInstall: info.isEnterpriseInstall,
						installationID:      info.installationID,
						teamID:              info.teamID,
					}
					return a.handleInteractivePayload(withRequestContext(r.Context(), &rc), string(body))
				}
			}
			a.logger.Warn("Could not resolve token for interactive payload")
			return textResult(http.StatusOK, "")
		}
		return a.handleInteractivePayload(r.Context(), string(body))
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return textResult(http.StatusBadRequest, invalidJSONBody)
	}

	if anyString(payload["type"]) == "url_verification" {
		if challenge := anyString(payload["challenge"]); challenge != "" {
			return jsonResult(http.StatusOK, map[string]string{"challenge": challenge})
		}
	}

	retryNum := 0.0
	if raw := r.Header.Get("X-Slack-Retry-Num"); raw != "" {
		if n, err := parseFloat(raw); err == nil {
			retryNum = n
		}
	}
	if a.isDuplicateEventDelivery(r.Context(), payload, retryNum) {
		return textResult(http.StatusOK, "ok")
	}

	resolved, status := a.resolveEventRequestContext(r.Context(), payload)
	if status == "unresolved" {
		return textResult(http.StatusOK, "ok")
	}
	workCtx := context.WithoutCancel(r.Context())
	if status != "not-applicable" {
		rc := resolved
		a.processEventPayload(withRequestContext(workCtx, &rc), payload)
		return textResult(http.StatusOK, "ok")
	}
	a.processEventPayload(workCtx, payload)
	return textResult(http.StatusOK, "ok")
}

func (a *SlackAdapter) verifyReadOptions() webhook.ReadOptions {
	opts := webhook.ReadOptions{SigningSecret: a.signingSecret, Now: a.clock()}
	if a.webhookVerifier != nil {
		opts.Verifier = func(req *http.Request, body []byte) (any, error) {
			if err := a.webhookVerifier(req.Header, body); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	return opts
}

func (a *SlackAdapter) clock() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}

func (a *SlackAdapter) spawn(fn func()) {
	a.pending.Go(fn)
}

func (a *SlackAdapter) waitPending(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		a.pending.Wait()
		close(done)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (a *SlackAdapter) runSlashCommand(ctx context.Context, params url.Values) webhookResult {
	if a.token == nil {
		isEnterprise := params.Get("is_enterprise_install") == "true"
		installationID := params.Get("team_id")
		if isEnterprise {
			installationID = params.Get("enterprise_id")
		}
		if installationID != "" {
			tok, ok := a.resolveTokenForTeam(ctx, installationID, isEnterprise)
			if ok {
				rc := requestContext{
					token:               tok.token,
					botUserID:           tok.botUserID,
					enterpriseID:        params.Get("enterprise_id"),
					isEnterpriseInstall: isEnterprise,
					installationID:      installationID,
					teamID:              params.Get("team_id"),
				}
				return a.handleSlashCommand(withRequestContext(ctx, &rc), params)
			}
			a.logger.Warn("Could not resolve token for slash command",
				"installationId", installationID, "isEnterpriseInstall", isEnterprise)
		}
		return textResult(http.StatusOK, "")
	}
	return a.handleSlashCommand(ctx, params)
}

func (a *SlackAdapter) handleSlashCommand(ctx context.Context, params url.Values) webhookResult {
	if a.chat == nil {
		a.logger.Warn("Chat instance not initialized, ignoring slash command")
		return textResult(http.StatusOK, "")
	}
	userID := params.Get("user_id")
	channelID := params.Get("channel_id")
	info := a.lookupUser(ctx, userID)
	userName, fullName := userID, userID
	if info != nil {
		userName = info.DisplayName
		fullName = info.RealName
	}
	raw := map[string]string{}
	for k, vs := range params {
		if len(vs) > 0 {
			raw[k] = vs[len(vs)-1]
		}
	}
	encodedChannel := ""
	if channelID != "" {
		encodedChannel = adapterName + ":" + channelID
	}
	_ = a.chat.ProcessSlashCommand(ctx, chat.ProcessSlashCommandInput{
		Adapter:   a,
		ChannelID: encodedChannel,
		Command:   params.Get("command"),
		Raw:       raw,
		Text:      params.Get("text"),
		TriggerID: params.Get("trigger_id"),
		User: chat.Author{
			UserID:   userID,
			UserName: userName,
			FullName: fullName,
			IsBot:    boolPtr(false),
			IsMe:     false,
		},
	})
	return textResult(http.StatusOK, "")
}

func (a *SlackAdapter) handleInteractivePayload(ctx context.Context, body string) webhookResult {
	params, _ := url.ParseQuery(body)
	payloadStr := params.Get("payload")
	if payloadStr == "" {
		return textResult(http.StatusBadRequest, missingPayloadBody)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadStr), &payload); err != nil {
		return textResult(http.StatusBadRequest, invalidPayloadBody)
	}
	return a.dispatchInteractivePayload(ctx, payload)
}

func (a *SlackAdapter) dispatchInteractivePayload(ctx context.Context, payload map[string]any) webhookResult {
	switch anyString(payload["type"]) {
	case "block_actions":
		a.handleBlockActions(ctx, payload)
		return textResult(http.StatusOK, "")
	case "block_suggestion":
		return a.handleBlockSuggestion(ctx, payload)
	case "view_submission":
		return a.handleViewSubmission(ctx, payload)
	case "view_closed":
		a.handleViewClosed(ctx, payload)
		return textResult(http.StatusOK, "")
	default:
		return textResult(http.StatusOK, "")
	}
}

func (a *SlackAdapter) handleBlockActions(ctx context.Context, payload map[string]any) {
	if a.chat == nil {
		a.logger.Warn("Chat instance not initialized, ignoring action")
		return
	}
	channelRec := record(payload["channel"])
	container := record(payload["container"])
	message := record(payload["message"])
	user := record(payload["user"])
	channel := firstOf(anyString(channelRec["id"]), anyString(container["channel_id"]))
	messageTs := firstOf(anyString(message["ts"]), anyString(container["message_ts"]))
	threadTs := firstOf(anyString(message["thread_ts"]), anyString(container["thread_ts"]), messageTs)
	isViewAction := anyString(container["type"]) == "view"
	if !isViewAction && channel == "" {
		a.logger.Warn("Missing channel in block_actions", "channel", channel)
		return
	}
	threadID := ""
	if channel != "" && (threadTs != "" || messageTs != "") {
		ts := threadTs
		if ts == "" {
			ts = messageTs
		}
		threadID = a.encodeThreadID(ThreadID{Channel: channel, ThreadTS: ts})
	}
	isEphemeral, _ := container["is_ephemeral"].(bool)
	responseURL := anyString(payload["response_url"])
	userID := anyString(user["id"])
	messageID := messageTs
	if isEphemeral && responseURL != "" && messageTs != "" {
		if id, err := a.encodeEphemeralMessageID(messageTs, responseURL, userID); err == nil {
			messageID = id
		}
	}
	actions, _ := payload["actions"].([]any)
	username := firstOf(anyString(user["username"]), anyString(user["name"]), "unknown")
	fullName := firstOf(anyString(user["name"]), anyString(user["username"]), "unknown")
	for _, raw := range actions {
		action := record(raw)
		selected := record(action["selected_option"])
		value := firstOf(anyString(selected["value"]), anyString(action["value"]))
		_ = a.chat.ProcessAction(ctx, chat.ProcessActionInput{
			ActionID:  anyString(action["action_id"]),
			Adapter:   a,
			MessageID: messageID,
			Raw:       payload,
			ThreadID:  threadID,
			TriggerID: anyString(payload["trigger_id"]),
			User: chat.Author{
				UserID:   userID,
				UserName: username,
				FullName: fullName,
				IsBot:    boolPtr(false),
				IsMe:     false,
			},
			Value: value,
		})
	}
}

func (a *SlackAdapter) handleBlockSuggestion(ctx context.Context, payload map[string]any) webhookResult {
	ctx = context.WithoutCancel(ctx)
	if a.chat == nil {
		a.logger.Warn("Chat instance not initialized, ignoring block suggestion")
		return a.optionsLoadResponse(nil)
	}
	user := record(payload["user"])
	userID := anyString(user["id"])
	timeout := a.optionsLoadTimeout
	if timeout <= 0 {
		timeout = optionsLoadTimeout
	}
	type loadResult struct {
		v   any
		err error
	}
	ch := make(chan loadResult, 1)
	go func() {
		v, err := a.chat.ProcessOptionsLoad(ctx, chat.ProcessOptionsLoadInput{
			ActionID: anyString(payload["action_id"]),
			Adapter:  a,
			Query:    anyString(payload["value"]),
			Raw:      payload,
			User: chat.Author{
				UserID:   userID,
				UserName: firstOf(anyString(user["username"]), anyString(user["name"]), userID),
				FullName: firstOf(anyString(user["name"]), anyString(user["username"]), userID),
				IsBot:    boolPtr(false),
				IsMe:     false,
			},
		})
		ch <- loadResult{v: v, err: err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case res := <-ch:
		if res.err != nil {
			a.logger.Warn("Options load handler error", "error", res.err)
			return a.optionsLoadResponse(nil)
		}
		return a.optionsLoadResponse(res.v)
	case <-timer.C:
		a.logger.Warn("Options load handler timed out",
			"actionId", anyString(payload["action_id"]), "timeoutMs", timeout.Milliseconds())
		a.spawn(func() {
			if res := <-ch; res.err != nil {
				a.logger.Error("Options load handler error after timeout",
					"error", res.err, "actionId", anyString(payload["action_id"]))
			}
		})
		return a.optionsLoadResponse(nil)
	}
}

func (a *SlackAdapter) optionsLoadResponse(result any) webhookResult {
	if groups, ok := asOptionsGroups(result); ok {
		if len(groups) > 100 {
			groups = groups[:100]
		}
		out := make([]map[string]any, 0, len(groups))
		for _, g := range groups {
			label := g.Label
			if len(label) > 75 {
				label = label[:75]
			}
			opts := g.Options
			if len(opts) > 100 {
				opts = opts[:100]
			}
			mapped := make([]map[string]any, 0, len(opts))
			for _, o := range opts {
				mapped = append(mapped, slackOptionJSON(o))
			}
			out = append(out, map[string]any{
				"label":   map[string]any{"type": "plain_text", "text": label},
				"options": mapped,
			})
		}
		return jsonResult(http.StatusOK, map[string]any{"option_groups": out})
	}
	opts := asSelectOptions(result)
	if len(opts) > 100 {
		opts = opts[:100]
	}
	mapped := make([]map[string]any, 0, len(opts))
	for _, o := range opts {
		mapped = append(mapped, slackOptionJSON(o))
	}
	return jsonResult(http.StatusOK, map[string]any{"options": mapped})
}

func slackOptionJSON(o chat.SelectOptionElement) map[string]any {
	return map[string]any{
		"text":  map[string]any{"type": "plain_text", "text": o.Label},
		"value": o.Value,
	}
}

func (a *SlackAdapter) handleViewSubmission(ctx context.Context, payload map[string]any) webhookResult {
	if a.chat == nil {
		a.logger.Warn("Chat instance not initialized, ignoring view submission")
		return textResult(http.StatusOK, "")
	}
	user := record(payload["user"])
	view := record(payload["view"])
	meta := DecodeModalMetadata(anyString(view["private_metadata"]))
	resp, err := a.chat.ProcessModalSubmit(ctx, chat.ProcessModalInput{
		Adapter:         a,
		CallbackID:      anyString(view["callback_id"]),
		ContextID:       meta.ContextID,
		PrivateMetadata: meta.PrivateMetadata,
		Raw:             payload,
		User: chat.Author{
			UserID:   anyString(user["id"]),
			UserName: firstOf(anyString(user["username"]), anyString(user["name"]), "unknown"),
			FullName: firstOf(anyString(user["name"]), anyString(user["username"]), "unknown"),
			IsBot:    boolPtr(false),
			IsMe:     false,
		},
		Values: flattenViewValues(view),
		ViewID: anyString(view["id"]),
	})
	if err != nil || resp == nil {
		return textResult(http.StatusOK, "")
	}
	return jsonResult(http.StatusOK, a.modalResponseToSlack(resp, meta.ContextID))
}

func (a *SlackAdapter) handleViewClosed(ctx context.Context, payload map[string]any) {
	if a.chat == nil {
		a.logger.Warn("Chat instance not initialized, ignoring view closed")
		return
	}
	user := record(payload["user"])
	view := record(payload["view"])
	meta := DecodeModalMetadata(anyString(view["private_metadata"]))
	_ = a.chat.ProcessModalClose(ctx, chat.ProcessModalInput{
		Adapter:         a,
		CallbackID:      anyString(view["callback_id"]),
		ContextID:       meta.ContextID,
		PrivateMetadata: meta.PrivateMetadata,
		Raw:             payload,
		User: chat.Author{
			UserID:   anyString(user["id"]),
			UserName: firstOf(anyString(user["username"]), anyString(user["name"]), "unknown"),
			FullName: firstOf(anyString(user["name"]), anyString(user["username"]), "unknown"),
			IsBot:    boolPtr(false),
			IsMe:     false,
		},
		ViewID: anyString(view["id"]),
	})
}

func (a *SlackAdapter) modalResponseToSlack(response any, contextID string) map[string]any {
	action, errors, modal := splitModalResult(response)
	switch action {
	case "clear":
		return map[string]any{"response_action": "clear"}
	case "errors":
		return map[string]any{"response_action": "errors", "errors": errors}
	case "update", "push":
		if modal == nil {
			return map[string]any{}
		}
		encoded := EncodeModalMetadata(ModalMetadata{
			ContextID:       contextID,
			PrivateMetadata: modal.PrivateMetadata,
		})
		view := ModalToSlackView(*modal, encoded)
		return map[string]any{"response_action": action, "view": view}
	default:
		return map[string]any{}
	}
}

func splitModalResult(response any) (action string, errors map[string]string, modal *chat.Modal) {
	switch r := response.(type) {
	case map[string]any:
		action = anyString(r["action"])
		if e, ok := r["errors"].(map[string]string); ok {
			errors = e
		}
		modal = chat.ToModalElement(r["modal"])
	}
	return action, errors, modal
}

func flattenViewValues(view map[string]any) map[string]string {
	state := record(view["state"])
	values := record(state["values"])
	out := map[string]string{}
	for _, block := range values {
		for actionID, input := range record(block) {
			in := record(input)
			selected := record(in["selected_option"])
			out[actionID] = firstOf(anyString(in["value"]), anyString(in["selected_date"]), anyString(selected["value"]))
		}
	}
	return out
}

func (a *SlackAdapter) processEventPayload(ctx context.Context, payload map[string]any) {
	if anyString(payload["type"]) != "event_callback" {
		return
	}
	event := record(payload["event"])
	if len(event) == 0 {
		return
	}
	a.markEventDelivered(ctx, payload)

	if anyBool(payload["is_ext_shared_channel"]) {
		channelID := anyString(event["channel"])
		if channelID == "" {
			channelID = anyString(record(event["item"])["channel"])
		}
		if channelID != "" {
			a.markExternalChannel(channelID)
		}
	}

	switch anyString(event["type"]) {
	case "message", "app_mention":
		if anyString(event["team"]) == "" && anyString(event["team_id"]) == "" {
			if teamID := anyString(payload["team_id"]); teamID != "" {
				event["team_id"] = teamID
			}
		}
		a.handleMessageEvent(ctx, event)
	case "reaction_added", "reaction_removed":
		a.spawn(func() { a.handleReactionEvent(ctx, event) })
	case "assistant_thread_started":
		a.handleAssistantThreadStarted(ctx, event)
	case "assistant_thread_context_changed":
		a.handleAssistantContextChanged(ctx, event)
	case "agent_session_stopped":
		a.handleAgentSessionStopped(ctx, event)
	case "agent_session_title_changed":
		a.handleAgentSessionTitleChanged(ctx, event)
	case "app_context_changed":
		a.handleAppContextChanged(ctx, event)
	case "app_home_opened":
		if a.agentView || anyString(event["tab"]) == "home" {
			auth := firstAuthorization(payload)
			teamID := firstOf(anyString(auth["team_id"]), anyString(payload["team_id"]))
			a.handleAppHomeOpened(ctx, event, teamID)
		}
	case "member_joined_channel":
		a.handleMemberJoinedChannel(ctx, event)
	case "user_change":
		a.spawn(func() { a.handleUserChange(ctx, event) })
	}
}

func (a *SlackAdapter) handleMessageEvent(ctx context.Context, raw map[string]any) {
	if a.chat == nil {
		a.logger.Warn("Chat instance not initialized, ignoring event")
		return
	}
	event := asSlackEvent(raw)
	switch event.Subtype {
	case "message_changed":
		a.handleMessageChanged(ctx, event)
		return
	case "message_deleted":
		a.handleMessageDeleted(ctx, event)
		return
	}
	if event.Subtype != "" {
		if _, skip := ignoredMessageSubtypes[event.Subtype]; skip {
			a.logger.Debug("Ignoring message subtype", "subtype", event.Subtype)
			return
		}
	}
	if event.Channel == "" || event.Ts == "" {
		a.logger.Debug("Ignoring event without channel or ts", "channel", event.Channel, "ts", event.Ts)
		return
	}
	isDM := event.ChannelType == "im"
	threadID := a.threadIdForMessageEvent(event)
	isMention := event.Type == "app_mention"
	parse := func(id string) *chat.Message {
		msg, err := a.parseSlackMessage(ctx, event, id)
		if err != nil {
			msg = a.parseSlackMessageSync(ctx, event, id)
		}
		if isMention {
			msg.IsMention = boolPtr(true)
		}
		return msg
	}
	if a.agentView && isDM && event.ThreadTs == "" {
		conversationID := a.encodeThreadID(ThreadID{Channel: event.Channel})
		a.spawn(func() {
			routed := threadID
			if st := a.chat.State(); st != nil {
				ok, err := st.IsSubscribed(ctx, conversationID)
				if err != nil {
					a.logger.Warn("agent_view DM subscription check failed; using per-message thread",
						"error", err.Error(), "threadId", threadID)
				} else if ok {
					routed = conversationID
				}
			}
			if err := a.chat.ProcessMessage(ctx, chat.ProcessMessageInput{
				Adapter: a, Message: parse(routed), ThreadID: routed,
			}); err != nil {
				a.logger.Warn("Agent view DM processing failed", "error", err, "threadId", routed)
			}
			a.applyConfiguredSessionTitle(ctx, event)
		})
		return
	}
	_ = a.chat.ProcessMessage(ctx, chat.ProcessMessageInput{
		Adapter: a, Message: parse(threadID), ThreadID: threadID,
	})
}

func (a *SlackAdapter) handleMessageChanged(ctx context.Context, event SlackEvent) {
	inner := event.Message
	if inner == nil || event.Channel == "" {
		return
	}
	normalized := *inner
	if normalized.Channel == "" {
		normalized.Channel = event.Channel
	}
	if normalized.ChannelType == "" {
		normalized.ChannelType = event.ChannelType
	}
	if normalized.Team == "" {
		normalized.Team = event.Team
	}
	if normalized.TeamID == "" {
		normalized.TeamID = event.TeamID
	}
	if normalized.Type == "" {
		normalized.Type = "message"
	}
	if inner.Subtype == "tombstone" {
		a.logger.Debug("Ignoring tombstone message_changed")
		return
	}
	hasUnfurl := false
	for _, att := range inner.Attachments {
		if att.FromURL != "" || att.OriginalURL != "" {
			hasUnfurl = true
			break
		}
	}
	if hasUnfurl && a.chat != nil && inner.Ts != "" && len(inner.Attachments) > 0 {
		unfurls := map[string]map[string]string{}
		for _, att := range inner.Attachments {
			attURL := firstOf(att.FromURL, att.OriginalURL)
			if attURL != "" && (att.Title != "" || att.Text != "") {
				unfurls[attURL] = map[string]string{
					"title":       att.Title,
					"description": att.Text,
					"imageUrl":    firstOf(att.ImageURL, att.ThumbURL),
					"siteName":    att.ServiceName,
				}
			}
		}
		if len(unfurls) > 0 {
			if st := a.chat.State(); st != nil {
				if err := chat.StateSet(ctx, st, a.unfurlCacheKey(ctx, event.Channel, inner.Ts), unfurls, unfurlCacheTTL); err != nil {
					a.logger.Error("Failed to cache unfurl metadata", "error", err)
				}
			}
		}
	}
	previous := event.PreviousMessage
	isHiddenEdit := previous != nil && (editedTs(inner) != editedTs(previous) || inner.Text != previous.Text)
	if event.Hidden && !isHiddenEdit {
		return
	}
	if previous != nil && !isHiddenEdit {
		a.logger.Debug("Ignoring message_changed with no content change")
		return
	}
	if a.chat == nil || normalized.Channel == "" || normalized.Ts == "" {
		return
	}
	threadID := a.threadIdForMessageEvent(normalized)
	var prevMsg *chat.Message
	if previous != nil {
		snapshot := *previous
		if snapshot.Channel == "" {
			snapshot.Channel = normalized.Channel
		}
		if snapshot.ChannelType == "" {
			snapshot.ChannelType = normalized.ChannelType
		}
		if snapshot.Type == "" {
			snapshot.Type = "message"
		}
		msg, err := a.parseSlackMessage(ctx, snapshot, threadID)
		if err != nil {
			a.logger.Warn("Falling back to sync parse for pre-edit message", "error", err, "threadId", threadID)
			msg = a.parseSlackMessageSync(ctx, snapshot, threadID)
		}
		prevMsg = msg
	}
	newMsg, err := a.parseSlackMessage(ctx, normalized, threadID)
	if err != nil {
		newMsg = a.parseSlackMessageSync(ctx, normalized, threadID)
	}
	_ = a.chat.ProcessMessageUpdated(ctx, chat.ProcessMessageInput{
		Adapter: a, Message: newMsg, PreviousMessage: prevMsg, ThreadID: threadID,
	})
}

func editedTs(ev *SlackEvent) string {
	if ev == nil || ev.Edited == nil {
		return ""
	}
	return ev.Edited.Ts
}

func (a *SlackAdapter) handleMessageDeleted(ctx context.Context, event SlackEvent) {
	deletedTs := event.DeletedTs
	if deletedTs == "" && event.Message != nil {
		deletedTs = event.Message.Ts
	}
	if deletedTs == "" && event.PreviousMessage != nil {
		deletedTs = event.PreviousMessage.Ts
	}
	if a.chat == nil || event.Channel == "" || deletedTs == "" {
		return
	}
	channelType := event.ChannelType
	threadTs := ""
	ts := deletedTs
	if prev := event.PreviousMessage; prev != nil {
		if prev.ChannelType != "" {
			channelType = prev.ChannelType
		}
		threadTs = prev.ThreadTs
		if prev.Ts != "" {
			ts = prev.Ts
		}
	}
	threadID := a.threadIdForMessageEvent(SlackEvent{
		Channel: event.Channel, ChannelType: channelType, ThreadTs: threadTs, Ts: ts,
	})
	_ = a.chat.ProcessMessageDeleted(ctx, threadID, deletedTs)
}

func (a *SlackAdapter) handleReactionEvent(ctx context.Context, raw map[string]any) {
	if a.chat == nil {
		a.logger.Warn("Chat instance not initialized, ignoring reaction")
		return
	}
	item := record(raw["item"])
	if anyString(item["type"]) != "message" {
		a.logger.Debug("Ignoring reaction to non-message item", "itemType", item["type"])
		return
	}
	channel := anyString(item["channel"])
	itemTs := anyString(item["ts"])
	parentTs := itemTs
	resp, err := a.apiClient().Call(ctx, "conversations.replies", map[string]any{
		"channel": channel, "ts": itemTs, "limit": 1,
	}, api.EncodingForm)
	if err != nil || !resp.OK {
		a.logger.Warn("Failed to resolve parent thread for reaction, using message ts",
			"error", errString(err), "channel", channel, "ts", itemTs)
	} else {
		var payload struct {
			Messages []struct {
				ThreadTs string `json:"thread_ts"`
			} `json:"messages"`
		}
		if json.Unmarshal(resp.Raw, &payload) == nil && len(payload.Messages) > 0 && payload.Messages[0].ThreadTs != "" {
			parentTs = payload.Messages[0].ThreadTs
		}
	}
	threadID := a.encodeThreadID(ThreadID{Channel: channel, ThreadTS: parentTs})
	userID := anyString(raw["user"])
	rawEmoji := anyString(raw["reaction"])
	info := a.lookupUser(ctx, userID)
	userName, fullName, isBot := userID, userID, false
	if info != nil {
		userName = info.DisplayName
		fullName = info.RealName
		isBot = info.IsBot
	}
	isMe := a.isReactionFromSelf(ctx, userID)
	_ = a.chat.ProcessReaction(ctx, chat.ProcessReactionInput{
		Adapter:   a,
		Added:     anyString(raw["type"]) == "reaction_added",
		Emoji:     chat.DefaultEmojiResolver.FromSlack(rawEmoji),
		MessageID: itemTs,
		Raw:       raw,
		RawEmoji:  rawEmoji,
		ThreadID:  threadID,
		User: chat.Author{
			UserID:   userID,
			UserName: userName,
			FullName: fullName,
			IsBot:    boolPtr(isBot),
			IsMe:     isMe,
		},
	})
}

func (a *SlackAdapter) isReactionFromSelf(ctx context.Context, userID string) bool {
	if bot := a.botUserIDFrom(ctx); bot != "" && userID == bot {
		return true
	}
	a.mu.Lock()
	botID := a.botID
	a.mu.Unlock()
	return botID != "" && userID == botID
}

func (a *SlackAdapter) handleAssistantThreadStarted(ctx context.Context, raw map[string]any) {
	if a.chat == nil {
		a.logger.Warn("Chat instance not initialized, ignoring assistant_thread_started")
		return
	}
	thread := record(raw["assistant_thread"])
	if len(thread) == 0 {
		a.logger.Warn("Malformed assistant_thread_started: missing assistant_thread")
		return
	}
	ctxRec := record(thread["context"])
	channelID := anyString(thread["channel_id"])
	threadTs := anyString(thread["thread_ts"])
	userID := anyString(thread["user_id"])
	a.applyConfiguredSuggestedPrompts(ctx, SuggestedPromptsContext{
		ChannelID:    channelID,
		EnterpriseID: anyString(ctxRec["enterprise_id"]),
		TeamID:       anyString(ctxRec["team_id"]),
		ThreadTS:     threadTs,
		UserID:       userID,
	})
	_ = a.chat.ProcessAssistantThreadStarted(ctx, assistantInput(a, channelID, threadTs, userID, ctxRec))
}

func (a *SlackAdapter) handleAssistantContextChanged(ctx context.Context, raw map[string]any) {
	if a.chat == nil {
		a.logger.Warn("Chat instance not initialized, ignoring assistant_thread_context_changed")
		return
	}
	thread := record(raw["assistant_thread"])
	if len(thread) == 0 {
		a.logger.Warn("Malformed assistant_thread_context_changed: missing assistant_thread")
		return
	}
	ctxRec := record(thread["context"])
	channelID := anyString(thread["channel_id"])
	threadTs := anyString(thread["thread_ts"])
	_ = a.chat.ProcessAssistantContextChanged(ctx, assistantInput(a, channelID, threadTs, anyString(thread["user_id"]), ctxRec))
}

func assistantInput(a *SlackAdapter, channelID, threadTs, userID string, ctxRec map[string]any) chat.ProcessAssistantThreadInput {
	return chat.ProcessAssistantThreadInput{
		Adapter:   a,
		ChannelID: channelID,
		Context: chat.AssistantThreadContext{
			ChannelID:        anyString(ctxRec["channel_id"]),
			EnterpriseID:     anyString(ctxRec["enterprise_id"]),
			ForceSearch:      anyBool(ctxRec["force_search"]),
			TeamID:           anyString(ctxRec["team_id"]),
			ThreadEntryPoint: anyString(ctxRec["thread_entry_point"]),
		},
		ThreadID: a.encodeThreadID(ThreadID{Channel: channelID, ThreadTS: threadTs}),
		ThreadTs: threadTs,
		UserID:   userID,
	}
}

func (a *SlackAdapter) handleAgentSessionStopped(ctx context.Context, raw map[string]any) {
	if a.chat == nil {
		a.logger.Warn("Chat instance not initialized, ignoring agent_session_stopped")
		return
	}
	channel := anyString(raw["channel"])
	threadTs := anyString(raw["thread_ts"])
	threadID := a.encodeThreadID(ThreadID{Channel: channel, ThreadTS: threadTs})
	a.spawn(func() {
		if aborter, ok := a.chat.(chat.TurnAborter); ok {
			if err := aborter.AbortTurn(ctx, threadID); err != nil {
				a.logger.Warn("Failed to abort stopped Slack agent session", "error", err, "threadId", threadID)
			}
		}
		if err := a.setSessionStatus(ctx, channel, threadTs, "active", ""); err != nil {
			a.logger.Warn("Failed to activate stopped Slack agent session", "error", err, "threadId", threadID)
		}
		if p, ok := a.chat.(sessionStoppedProcessor); ok {
			_ = p.ProcessAgentSessionStopped(ctx, processSessionStoppedInput{
				Adapter:            a,
				ChannelID:          channel,
				StreamingMessageTs: raw["streaming_message_ts"],
				ThreadID:           threadID,
				ThreadTs:           threadTs,
				UserID:             anyString(raw["user"]),
			})
		}
	})
}

func (a *SlackAdapter) handleAgentSessionTitleChanged(ctx context.Context, raw map[string]any) {
	if a.chat == nil {
		a.logger.Warn("Chat instance not initialized, ignoring agent_session_title_changed")
		return
	}
	if p, ok := a.chat.(titleChangedProcessor); ok {
		channel := anyString(raw["channel"])
		threadTs := anyString(raw["thread_ts"])
		_ = p.ProcessAgentSessionTitleChanged(ctx, processTitleChangedInput{
			Adapter:       a,
			ChannelID:     channel,
			PreviousTitle: anyString(raw["previous_title"]),
			ThreadID:      a.encodeThreadID(ThreadID{Channel: channel, ThreadTS: threadTs}),
			ThreadTs:      threadTs,
			Title:         anyString(raw["title"]),
			UserID:        anyString(raw["user"]),
		})
	}
}

func (a *SlackAdapter) handleAppHomeOpened(ctx context.Context, raw map[string]any, teamID string) {
	if a.chat == nil {
		a.logger.Warn("Chat instance not initialized, ignoring app_home_opened")
		return
	}
	entities := appHomeEntities(raw)
	if a.agentView && anyString(raw["tab"]) == "messages" {
		a.applyConfiguredSuggestedPrompts(ctx, SuggestedPromptsContext{
			ChannelID: anyString(raw["channel"]),
			Entities:  entities,
			TeamID:    teamID,
			UserID:    anyString(raw["user"]),
		})
	}
	if p, ok := a.chat.(appHomeProcessor); ok {
		_ = p.ProcessAppHomeOpened(ctx, processAppHomeInput{
			Adapter:   a,
			ChannelID: anyString(raw["channel"]),
			Entities:  entities,
			Tab:       anyString(raw["tab"]),
			UserID:    anyString(raw["user"]),
		})
	}
}

func (a *SlackAdapter) handleMemberJoinedChannel(ctx context.Context, raw map[string]any) {
	if a.chat == nil {
		a.logger.Warn("Chat instance not initialized, ignoring member_joined_channel")
		return
	}
	_ = a.chat.ProcessMemberJoinedChannel(ctx, chat.ProcessMemberJoinedInput{
		Adapter:   a,
		ChannelID: a.encodeThreadID(ThreadID{Channel: anyString(raw["channel"])}),
		InviterID: anyString(raw["inviter"]),
		UserID:    anyString(raw["user"]),
	})
}

func (a *SlackAdapter) handleUserChange(ctx context.Context, raw map[string]any) {
	if a.chat == nil {
		return
	}
	userID := anyString(record(raw["user"])["id"])
	if userID == "" {
		return
	}
	st := a.chat.State()
	if st == nil {
		return
	}
	if err := st.Delete(ctx, "slack:user:"+a.installationCacheScope(ctx)+userID); err != nil {
		a.logger.Warn("Failed to invalidate user cache", "userId", userID, "error", err)
	}
}

func (a *SlackAdapter) threadIdForMessageEvent(event SlackEvent) string {
	isDM := event.ChannelType == "im"
	threadTs := event.ThreadTs
	if !isDM || a.agentView {
		if threadTs == "" {
			threadTs = event.Ts
		}
	}
	return a.encodeThreadID(ThreadID{Channel: event.Channel, ThreadTS: threadTs})
}

func (a *SlackAdapter) markEventDelivered(ctx context.Context, payload map[string]any) {
	eventID := anyString(payload["event_id"])
	if eventID == "" || a.chat == nil {
		return
	}
	st := a.chat.State()
	if st == nil {
		return
	}
	_ = chat.StateSet(ctx, st, "slack:event-delivered:"+eventID, true, eventDedupeTTL)
}

func (a *SlackAdapter) isDuplicateEventDelivery(ctx context.Context, payload map[string]any, retryNum float64) bool {
	eventID := anyString(payload["event_id"])
	if eventID == "" || a.chat == nil || retryNum <= 0 {
		return false
	}
	st := a.chat.State()
	if st == nil {
		return false
	}
	seen, ok, err := chat.StateGet[bool](ctx, st, "slack:event-delivered:"+eventID)
	if err != nil {
		return false
	}
	if ok && seen {
		a.logger.Info("Skipping duplicate event delivery", "eventId", eventID, "retryNum", retryNum)
		return true
	}
	return false
}

func (a *SlackAdapter) unfurlCacheKey(ctx context.Context, channelID, messageTs string) string {
	return "slack:unfurls:" + a.installationCacheScope(ctx) + channelID + ":" + messageTs
}

type tokenCtx struct {
	token     string
	botUserID string
}

func (a *SlackAdapter) resolveEventRequestContext(ctx context.Context, payload map[string]any) (requestContext, string) {
	if a.token != nil || anyString(payload["type"]) != "event_callback" {
		return requestContext{}, "not-applicable"
	}
	auth := firstAuthorization(payload)
	isEnterprise := anyBool(auth["is_enterprise_install"])
	if len(auth) == 0 {
		isEnterprise = anyBool(payload["is_enterprise_install"])
	} else if _, ok := auth["is_enterprise_install"]; !ok {
		isEnterprise = anyBool(payload["is_enterprise_install"])
	}
	enterpriseID := firstOf(anyString(auth["enterprise_id"]), anyString(payload["enterprise_id"]))
	teamID := firstOf(anyString(auth["team_id"]), anyString(payload["team_id"]))
	installationID := teamID
	if isEnterprise {
		installationID = enterpriseID
	}
	if installationID == "" {
		return requestContext{}, "not-applicable"
	}
	tok, ok := a.resolveTokenForTeam(ctx, installationID, isEnterprise)
	if !ok {
		a.logger.Warn("Could not resolve token for installation",
			"installationId", installationID, "isEnterpriseInstall", isEnterprise)
		return requestContext{}, "unresolved"
	}
	return requestContext{
		token:               tok.token,
		botUserID:           tok.botUserID,
		enterpriseID:        enterpriseID,
		isEnterpriseInstall: isEnterprise,
		installationID:      installationID,
		teamID:              teamID,
		contextTeamID:       anyString(payload["context_team_id"]),
		contextChannel:      anyString(record(payload["event"])["channel"]),
	}, ""
}

type interactiveInstall struct {
	installationID      string
	isEnterpriseInstall bool
	enterpriseID        string
	teamID              string
}

func extractInstallationFromInteractive(body string) *interactiveInstall {
	params, _ := url.ParseQuery(body)
	payloadStr := params.Get("payload")
	if payloadStr == "" {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadStr), &payload); err != nil {
		return nil
	}
	return extractInstallationFromInteractivePayload(payload)
}

func extractInstallationFromInteractivePayload(payload map[string]any) *interactiveInstall {
	isEnterprise := anyBool(payload["is_enterprise_install"])
	enterprise := record(payload["enterprise"])
	team := record(payload["team"])
	enterpriseID := firstOf(anyString(enterprise["id"]), anyString(payload["enterprise_id"]))
	teamID := firstOf(anyString(team["id"]), anyString(payload["team_id"]))
	installationID := teamID
	if isEnterprise {
		installationID = enterpriseID
	}
	if installationID == "" {
		return nil
	}
	return &interactiveInstall{
		installationID:      installationID,
		isEnterpriseInstall: isEnterprise,
		enterpriseID:        enterpriseID,
		teamID:              teamID,
	}
}

func firstAuthorization(payload map[string]any) map[string]any {
	arr, _ := payload["authorizations"].([]any)
	if len(arr) == 0 {
		return nil
	}
	return record(arr[0])
}

func record(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func anyString(v any) string {
	s, _ := v.(string)
	return s
}

func anyBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "true"
	default:
		return false
	}
}

func parseFloat(s string) (float64, error) {
	var n float64
	err := json.Unmarshal([]byte(s), &n)
	return n, err
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func asSelectOptions(result any) []chat.SelectOptionElement {
	switch v := result.(type) {
	case []chat.SelectOptionElement:
		return v
	case []any:
		out := make([]chat.SelectOptionElement, 0, len(v))
		for _, item := range v {
			if o, ok := item.(chat.SelectOptionElement); ok {
				out = append(out, o)
			} else if m := record(item); m != nil {
				out = append(out, chat.SelectOptionElement{
					Label: anyString(m["label"]),
					Value: anyString(m["value"]),
				})
			}
		}
		return out
	default:
		return nil
	}
}

func asOptionsGroups(result any) ([]optionsLoadGroup, bool) {
	switch v := result.(type) {
	case []optionsLoadGroup:
		return v, true
	case []any:
		if len(v) == 0 {
			return nil, false
		}
		first := record(v[0])
		if first == nil {
			return nil, false
		}
		if _, ok := first["options"]; !ok {
			return nil, false
		}
		out := make([]optionsLoadGroup, 0, len(v))
		for _, item := range v {
			m := record(item)
			opts := asSelectOptions(m["options"])
			out = append(out, optionsLoadGroup{Label: anyString(m["label"]), Options: opts})
		}
		return out, true
	default:
		return nil, false
	}
}
