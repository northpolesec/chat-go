// Ported from packages/adapter-slack/src/agent-context.ts @ 6adca36 (chat v4.40.0).
// Divergences: getAppContext / normalizeAppContextEntities are exported
// (index.ts re-exports them); SlackAppContext | undefined is
// *SlackAppContext; Message.Raw is any so app_context is read from
// map[string]any (typed SlackAppContext or a JSON object); non-string
// values on typed tokens become "" (TS `as string` is compile-only);
// empty results are a non-nil slice.
package slack

import "github.com/northpolesec/chat-go/chat"

const (
	channelToken = "slack#/types/channel_id"
	canvasToken  = "slack#/types/canvas_id"
	listToken    = "slack#/types/list_id"
	messageToken = "slack#/types/message_context"
)

// SlackAppContextEntity is a single entity in a Slack active-view context (wire shape).
type SlackAppContextEntity struct {
	EnterpriseID string `json:"enterprise_id,omitempty"`
	TeamID       string `json:"team_id,omitempty"`
	Type         string `json:"type"`
	Value        any    `json:"value"`
}

// SlackAppContext is Slack active-view context (`app_context` on messages, `context` elsewhere).
type SlackAppContext struct {
	Entities []SlackAppContextEntity `json:"entities,omitempty"`
}

// SlackAppContextChangedEvent is the Slack `app_context_changed` payload (wire shape, agent_view only).
type SlackAppContextChangedEvent struct {
	Channel string          `json:"channel"`
	Context SlackAppContext `json:"context"`
	EventTs string          `json:"event_ts"`
	Type    string          `json:"type"`
	User    string          `json:"user"`
}

// NormalizeAppContextEntities maps Slack active-view entities to chat.AppContextEntity.
// Unrecognized types become kind "unknown". A missing or empty context is [].
func NormalizeAppContextEntities(context *SlackAppContext) []chat.AppContextEntity {
	if context == nil || context.Entities == nil {
		return []chat.AppContextEntity{}
	}
	out := make([]chat.AppContextEntity, len(context.Entities))
	for i, entity := range context.Entities {
		out[i] = normalizeEntity(entity)
	}
	return out
}

func normalizeEntity(entity SlackAppContextEntity) chat.AppContextEntity {
	base := chat.AppContextEntity{
		TeamID:       entity.TeamID,
		EnterpriseID: entity.EnterpriseID,
	}
	switch entity.Type {
	case channelToken:
		id, _ := entity.Value.(string)
		base.Kind = chat.AppContextChannel
		base.ChannelID = id
		return base
	case canvasToken:
		id, _ := entity.Value.(string)
		base.Kind = chat.AppContextCanvas
		base.CanvasID = id
		return base
	case listToken:
		id, _ := entity.Value.(string)
		base.Kind = chat.AppContextList
		base.ListID = id
		return base
	case messageToken:
		if messageTs, channelID, ok := messageContextIDs(entity.Value); ok {
			base.Kind = chat.AppContextMessage
			base.MessageTs = messageTs
			base.ChannelID = channelID
			return base
		}
	}
	base.Kind = chat.AppContextUnknown
	base.Type = entity.Type
	base.Value = entity.Value
	return base
}

func messageContextIDs(value any) (messageTs, channelID string, ok bool) {
	m, ok := value.(map[string]any)
	if !ok || m == nil {
		return "", "", false
	}
	messageTs, tsOK := m["message_ts"].(string)
	channelID, chOK := m["channel_id"].(string)
	if !tsOK || !chOK {
		return "", "", false
	}
	return messageTs, channelID, true
}

// GetAppContext reads folded `app_context` on message.Raw and normalizes it.
func GetAppContext(message *chat.Message) []chat.AppContextEntity {
	if message == nil {
		return []chat.AppContextEntity{}
	}
	context := appContextFromRaw(message.Raw)
	if context == nil {
		return []chat.AppContextEntity{}
	}
	return NormalizeAppContextEntities(context)
}

func appContextFromRaw(raw any) *SlackAppContext {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	v, ok := m["app_context"]
	if !ok || v == nil {
		return nil
	}
	return slackAppContextFromAny(v)
}

func slackAppContextFromAny(v any) *SlackAppContext {
	switch c := v.(type) {
	case SlackAppContext:
		return &c
	case *SlackAppContext:
		return c
	case map[string]any:
		ctx := &SlackAppContext{}
		switch ents := c["entities"].(type) {
		case []SlackAppContextEntity:
			ctx.Entities = ents
		case []any:
			ctx.Entities = make([]SlackAppContextEntity, len(ents))
			for i, e := range ents {
				ctx.Entities[i] = slackEntityFromAny(e)
			}
		}
		return ctx
	default:
		return nil
	}
}

func slackEntityFromAny(v any) SlackAppContextEntity {
	switch e := v.(type) {
	case SlackAppContextEntity:
		return e
	case map[string]any:
		ent := SlackAppContextEntity{Value: e["value"]}
		if t, ok := e["type"].(string); ok {
			ent.Type = t
		}
		if s, ok := e["team_id"].(string); ok {
			ent.TeamID = s
		}
		if s, ok := e["enterprise_id"].(string); ok {
			ent.EnterpriseID = s
		}
		return ent
	default:
		return SlackAppContextEntity{}
	}
}
