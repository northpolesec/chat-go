// Ported from packages/adapter-slack/src/index.ts @ 6adca36 (chat v4.40.0)
// (partial: lookupUser, reverse index, outgoing mentions, ephemeral IDs).
package slack

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/northpolesec/chat-go/slack/api"
)

const (
	userCacheTTL      = 8 * 24 * time.Hour
	channelCacheTTL   = 8 * 24 * time.Hour
	reverseIndexTTL   = 8 * 24 * time.Hour
	reverseIndexMax   = 50
	participantMax    = 100
	invalidEphemeral  = "Invalid Slack ephemeral message ID"
	untrustedResponse = "Refusing to send content to an untrusted Slack response_url"
	untrustedEncode   = "Refusing to encode an untrusted Slack response_url"
	replaceNeedsMsg   = "Message required for replace action"
)

var (
	slackUserIDExact = regexp.MustCompile(`^[UW][A-Z0-9]+$`)
)

type cachedChannel struct {
	Name string `json:"name"`
}

// UserInfo is chat.UserInfo; the alias keeps the Slack package's exported name.
type UserInfo = chat.UserInfo

func (a *SlackAdapter) installationCacheScope(ctx context.Context) string {
	if rc := requestContextFrom(ctx); rc != nil && rc.installationID != "" {
		return rc.installationID + ":"
	}
	return ""
}

func (a *SlackAdapter) state() chat.StateAdapter {
	if a.chat == nil {
		return nil
	}
	return a.chat.State()
}

func (a *SlackAdapter) lookupUser(ctx context.Context, userID string) *userInfo {
	cacheKey := "slack:user:" + a.installationCacheScope(ctx) + userID
	if st := a.state(); st != nil {
		if cached, ok, err := chat.StateGet[userInfo](ctx, st, cacheKey); err == nil && ok {
			return &cached
		}
	} else {
		a.userCacheMu.Lock()
		if cached, ok := a.userCache[userID]; ok {
			a.userCacheMu.Unlock()
			return &cached
		}
		a.userCacheMu.Unlock()
	}

	resp, err := a.apiClient().Call(ctx, "users.info", map[string]any{"user": userID}, api.EncodingForm)
	if err != nil || !resp.OK {
		a.logger.Warn("Could not fetch user info", "userId", userID, "error", err)
		return nil
	}
	var payload struct {
		User struct {
			IsBot    bool   `json:"is_bot"`
			Name     string `json:"name"`
			RealName string `json:"real_name"`
			Tz       string `json:"tz"`
			Profile  struct {
				DisplayName string `json:"display_name"`
				Email       string `json:"email"`
				Image192    string `json:"image_192"`
				RealName    string `json:"real_name"`
			} `json:"profile"`
		} `json:"user"`
	}
	if err := json.Unmarshal(resp.Raw, &payload); err != nil {
		a.logger.Warn("Could not fetch user info", "userId", userID, "error", err)
		return nil
	}
	display := firstOf(
		payload.User.Profile.DisplayName,
		payload.User.Profile.RealName,
		payload.User.RealName,
		payload.User.Name,
		userID,
	)
	realName := firstOf(payload.User.RealName, payload.User.Profile.RealName, display)
	info := userInfo{
		AvatarURL:   payload.User.Profile.Image192,
		DisplayName: display,
		Email:       payload.User.Profile.Email,
		IsBot:       payload.User.IsBot,
		RealName:    realName,
		Tz:          payload.User.Tz,
	}
	a.userCacheMu.Lock()
	a.userCache[userID] = info
	a.userCacheMu.Unlock()
	if st := a.state(); st != nil {
		if err := chat.StateSet(ctx, st, cacheKey, info, userCacheTTL); err != nil {
			a.logger.Warn("Could not cache user info", "userId", userID, "error", err)
		}
		reverseKey := "slack:user-by-name:" + a.installationCacheScope(ctx) + strings.ToLower(display)
		existing, err := st.GetList(ctx, reverseKey)
		if err != nil {
			a.logger.Warn("Could not read reverse user index", "userId", userID, "error", err)
		} else if !listHasString(existing, userID) {
			raw, _ := json.Marshal(userID)
			if err := st.AppendToList(ctx, reverseKey, raw, reverseIndexMax, reverseIndexTTL); err != nil {
				a.logger.Warn("Could not write reverse user index", "userId", userID, "error", err)
			}
		}
	}
	return &info
}

func (a *SlackAdapter) lookupChannel(ctx context.Context, channelID string) string {
	cacheKey := "slack:channel:" + a.installationCacheScope(ctx) + channelID
	if st := a.state(); st != nil {
		if cached, ok, err := chat.StateGet[cachedChannel](ctx, st, cacheKey); err == nil && ok {
			return cached.Name
		}
	}
	resp, err := a.apiClient().Call(ctx, "conversations.info", map[string]any{"channel": channelID}, api.EncodingForm)
	if err != nil || !resp.OK {
		a.logger.Warn("Could not fetch channel info", "channelId", channelID, "error", err)
		return channelID
	}
	var payload struct {
		Channel struct {
			Name string `json:"name"`
		} `json:"channel"`
	}
	if err := json.Unmarshal(resp.Raw, &payload); err != nil {
		return channelID
	}
	name := payload.Channel.Name
	if name == "" {
		name = channelID
	}
	if st := a.state(); st != nil {
		_ = chat.StateSet(ctx, st, cacheKey, cachedChannel{Name: name}, channelCacheTTL)
	}
	return name
}

// GetUser is adapter.getUser.
func (a *SlackAdapter) GetUser(ctx context.Context, userID string) *UserInfo {
	cached := a.lookupUser(ctx, userID)
	if cached == nil {
		return nil
	}
	return &UserInfo{
		AvatarURL: cached.AvatarURL,
		Email:     cached.Email,
		FullName:  cached.RealName,
		IsBot:     cached.IsBot,
		Tz:        cached.Tz,
		UserID:    userID,
		UserName:  cached.DisplayName,
	}
}

func (a *SlackAdapter) trackThreadParticipant(ctx context.Context, threadID, userID string) {
	st := a.state()
	if userID == "" || st == nil {
		return
	}
	key := "slack:thread-participants:" + threadID
	existing, err := st.GetList(ctx, key)
	if err != nil {
		a.logger.Warn("Failed to track thread participant", "threadId", threadID, "userId", userID, "error", err)
		return
	}
	if listHasString(existing, userID) {
		return
	}
	raw, _ := json.Marshal(userID)
	if err := st.AppendToList(ctx, key, raw, participantMax, reverseIndexTTL); err != nil {
		a.logger.Warn("Failed to track thread participant", "threadId", threadID, "userId", userID, "error", err)
	}
}

func (a *SlackAdapter) resolveInlineMentions(ctx context.Context, text string) string {
	users := map[string]struct{}{}
	channels := map[string]struct{}{}
	collectMentionIDs(text, users, channels)
	userIDs := make([]string, 0, len(users))
	for id := range users {
		userIDs = append(userIDs, id)
	}
	channelIDs := make([]string, 0, len(channels))
	for id := range channels {
		channelIDs = append(channelIDs, id)
	}
	names, _ := a.lookupMentionNames(ctx, userIDs, channelIDs)
	return applyMentionNames(text, names)
}

func (a *SlackAdapter) resolveOutgoingMentions(ctx context.Context, text, threadID string) (string, error) {
	state := a.state()
	if state == nil {
		return text, nil
	}
	mentions := map[string][]string{}
	shared.ReplaceBareMentions(text, func(mention, name string) string {
		if slackUserIDExact.MatchString(name) {
			return mention
		}
		key := strings.ToLower(name)
		if _, ok := mentions[key]; !ok {
			mentions[key] = nil
		}
		return mention
	})
	if len(mentions) == 0 {
		return text, nil
	}
	for name := range mentions {
		raw, err := state.GetList(ctx, "slack:user-by-name:"+a.installationCacheScope(ctx)+name)
		if err != nil {
			return "", err
		}
		mentions[name] = uniqueStrings(raw)
	}
	var participants map[string]struct{}
	for _, ids := range mentions {
		if len(ids) > 1 {
			raw, err := state.GetList(ctx, "slack:thread-participants:"+threadID)
			if err != nil {
				return "", err
			}
			participants = map[string]struct{}{}
			for _, id := range uniqueStrings(raw) {
				participants[id] = struct{}{}
			}
			break
		}
	}
	return shared.ReplaceBareMentions(text, func(mention, name string) string {
		if slackUserIDExact.MatchString(name) {
			return mention
		}
		userIDs := mentions[strings.ToLower(name)]
		switch len(userIDs) {
		case 0:
			return mention
		case 1:
			return "<@" + userIDs[0] + ">"
		}
		if participants != nil {
			var inThread []string
			for _, id := range userIDs {
				if _, ok := participants[id]; ok {
					inThread = append(inThread, id)
				}
			}
			if len(inThread) == 1 {
				return "<@" + inThread[0] + ">"
			}
		}
		return mention
	}), nil
}

func (a *SlackAdapter) resolveMessageMentions(ctx context.Context, message chat.AdapterPostableMessage, threadID string) (chat.AdapterPostableMessage, error) {
	if a.chat == nil {
		return message, nil
	}
	switch m := message.(type) {
	case chat.PostableText:
		s, err := a.resolveOutgoingMentions(ctx, string(m), threadID)
		return chat.PostableText(s), err
	case chat.PostableRaw:
		s, err := a.resolveOutgoingMentions(ctx, m.Raw, threadID)
		if err != nil {
			return nil, err
		}
		m.Raw = s
		return m, nil
	case chat.PostableMarkdown:
		s, err := a.resolveOutgoingMentions(ctx, m.Markdown, threadID)
		if err != nil {
			return nil, err
		}
		m.Markdown = s
		return m, nil
	default:
		return message, nil
	}
}

func (a *SlackAdapter) isSelfMentioned(ctx context.Context, event SlackEvent) bool {
	bot := a.botUserIDFrom(ctx)
	if bot == "" {
		return false
	}
	if strings.Contains(strings.ToLower(event.Text), "<@"+strings.ToLower(bot)) {
		return true
	}
	users := map[string]struct{}{}
	channels := map[string]struct{}{}
	collectMentionIDs(event.Text, users, channels)
	tables := eventTables(event)
	atts := authorAttachments(event)
	contents := make([]slackAttachmentContent, len(atts))
	for i, att := range atts {
		contents[i] = attachmentContent(att)
	}
	moreUsers, _ := mentionIDs(tables, contents)
	for _, id := range moreUsers {
		users[id] = struct{}{}
	}
	_, ok := users[bot]
	return ok
}

func (a *SlackAdapter) encodeEphemeralMessageID(messageTS, responseURL, userID string) (string, error) {
	if !isTrustedSlackResponseURL(responseURL) {
		return "", shared.NewValidationError(adapterName, untrustedEncode)
	}
	data, err := json.Marshal(map[string]string{"responseUrl": responseURL, "userId": userID})
	if err != nil {
		return "", err
	}
	return "ephemeral:" + messageTS + ":" + base64.StdEncoding.EncodeToString(data), nil
}

type ephemeralID struct {
	MessageTS   string
	ResponseURL string
	UserID      string
}

func (a *SlackAdapter) decodeEphemeralMessageID(messageID string) *ephemeralID {
	if !strings.HasPrefix(messageID, "ephemeral:") {
		return nil
	}
	parts := strings.Split(messageID, ":")
	if len(parts) < 3 {
		return nil
	}
	messageTS := parts[1]
	encoded := strings.Join(parts[2:], ":")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		a.logger.Warn("Failed to decode ephemeral messageId", "messageId", messageID)
		return nil
	}
	var data map[string]any
	if err := json.Unmarshal(decoded, &data); err != nil {
		return nil
	}
	responseURL, _ := data["responseUrl"].(string)
	userID, _ := data["userId"].(string)
	if responseURL == "" || !isTrustedSlackResponseURL(responseURL) || userID == "" {
		return nil
	}
	return &ephemeralID{MessageTS: messageTS, ResponseURL: responseURL, UserID: userID}
}

func (a *SlackAdapter) sendToResponseURL(ctx context.Context, responseURL, action string, message chat.AdapterPostableMessage, threadTS string) (map[string]any, error) {
	if !isTrustedSlackResponseURL(responseURL) {
		return nil, shared.NewValidationError(adapterName, untrustedResponse)
	}
	var payload api.ResponseURLPayload
	switch action {
	case "delete":
		del := true
		payload.DeleteOriginal = &del
	default:
		if message == nil {
			return nil, shared.NewValidationError(adapterName, replaceNeedsMsg)
		}
		replace := true
		payload.ReplaceOriginal = &replace
		if card := shared.ExtractCard(message); card != nil {
			payload.Text = CardToFallbackText(*card)
			payload.Blocks = blocksAsAny(CardToBlockKit(*card))
		} else {
			payload.Text = a.format.ToResponseUrlText(message)
		}
		if threadTS != "" {
			payload.ThreadTS = threadTS
		}
	}
	if err := a.apiClient().SendResponseURL(ctx, responseURL, payload); err != nil {
		return nil, shared.NewNetworkError(adapterName, "Failed to "+action+" via response_url: "+err.Error(), err)
	}
	return map[string]any{}, nil
}

func isTrustedSlackResponseURL(value string) bool {
	return api.TrustedResponseURL(value)
}

func firstOf(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func listHasString(raw []json.RawMessage, want string) bool {
	return slices.Contains(uniqueStrings(raw), want)
}

func uniqueStrings(raw []json.RawMessage) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, item := range raw {
		var s string
		if err := json.Unmarshal(item, &s); err != nil || s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func blocksAsAny(blocks []SlackBlock) []any {
	out := make([]any, len(blocks))
	for i, b := range blocks {
		out[i] = b
	}
	return out
}
