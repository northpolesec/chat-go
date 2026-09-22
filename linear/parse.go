// Ported from packages/adapter-linear/src/index.ts (parseMessage,
// parseAgentSessionMessage) @ vercel/chat v4.40.0. Divergences: a
// delegation without a comment gets a short Text (upstream used
// promptContext as the body); promptContext rides on Raw.
package linear

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/internal/shared"
)

const automationUserID = "linear-automation"

func boolPtr(b bool) *bool { return &b }

// ParseMessage implements chat.Adapter. raw is a RawMessage, *RawMessage,
// or a JSON-shaped value.
func (a *Adapter) ParseMessage(raw any) (*chat.Message, error) {
	rm, err := asRawMessage(raw)
	if err != nil {
		return nil, err
	}
	msg, _, err := a.message(rm)
	return msg, err
}

func asRawMessage(raw any) (RawMessage, error) {
	switch v := raw.(type) {
	case RawMessage:
		return v, nil
	case *RawMessage:
		if v != nil {
			return *v, nil
		}
	default:
		b, err := json.Marshal(raw)
		if err == nil {
			var rm RawMessage
			if err := json.Unmarshal(b, &rm); err == nil && rm.Action != "" {
				return rm, nil
			}
		}
	}
	return RawMessage{}, shared.NewValidationError(adapterName, fmt.Sprintf("ParseMessage expects a linear.RawMessage, got %T", raw))
}

// issueID prefers the embedded issue's id, then the flat issueId.
func issueID(s AgentSession) string {
	if s.Issue != nil && s.Issue.ID != "" {
		return s.Issue.ID
	}
	return s.IssueID
}

// message builds the chat.Message and its thread id for a created or
// prompted event.
func (a *Adapter) message(rm RawMessage) (*chat.Message, string, error) {
	threadID, err := encodeThreadID(ThreadID{IssueID: issueID(rm.Session), SessionID: rm.Session.ID})
	if err != nil {
		return nil, "", err
	}
	botID := a.BotUserID()
	data := chat.MessageData{ThreadID: threadID, Raw: rm, IsMention: boolPtr(true), Attachments: []chat.Attachment{}}
	switch rm.Action {
	case actionCreated:
		s := rm.Session
		if s.Comment != nil && s.Comment.ID != "" {
			data.ID, data.Text = s.Comment.ID, s.Comment.Body
		} else {
			data.ID = syntheticIDPrefix + s.ID
			data.Text = delegatedText(s.Issue)
		}
		data.Author = authorFrom(s.Creator, botID)
		data.Metadata = chat.MessageMetadata{DateSent: s.CreatedAt}
	case actionPrompted:
		act := rm.Activity
		if act == nil {
			return nil, "", shared.NewValidationError(adapterName, "prompted event without agentActivity")
		}
		data.ID = act.SourceCommentID
		if data.ID == "" {
			data.ID = act.ID
		}
		data.Text = act.Content.Body
		data.Author = authorFrom(act.User, botID)
		if act.User != nil && act.User.Email == "" && rm.Session.Creator != nil && act.User.ID == rm.Session.Creator.ID {
			data.Author.Email = rm.Session.Creator.Email
		}
		data.Metadata = chat.MessageMetadata{DateSent: act.CreatedAt}
	default:
		return nil, "", shared.NewValidationError(adapterName, fmt.Sprintf("unknown agent session action %q", rm.Action))
	}
	data.Formatted = a.format.ToAst(data.Text)
	msg := chat.NewMessage(data)
	chat.SetMessageAdapter(msg, a)
	return msg, threadID, nil
}

// delegatedText is the short user turn for a delegation with no comment;
// the full promptContext reaches the model through the consumer's prompt.
func delegatedText(issue *Issue) string {
	if issue == nil {
		return "Delegated an issue"
	}
	return "Delegated " + issue.Identifier + ": " + issue.Title
}

// authorFrom maps a Linear user; nil is a session created by an automation.
func authorFrom(u *User, botID string) chat.Author {
	if u == nil {
		return chat.Author{UserID: automationUserID, UserName: automationUserID, FullName: "Linear automation", IsBot: boolPtr(true)}
	}
	return chat.Author{
		UserID:   u.ID,
		UserName: userName(*u),
		FullName: u.Name,
		Email:    u.Email,
		IsBot:    boolPtr(false),
		IsMe:     u.ID != "" && u.ID == botID,
	}
}

// userName is the profile URL's last segment (upstream
// extractDisplayNameFromUrl), else the display name, else the name.
func userName(u User) string {
	if i := strings.LastIndex(u.URL, "/profiles/"); i >= 0 {
		rest := u.URL[i+len("/profiles/"):]
		if j := strings.IndexAny(rest, "/?#"); j >= 0 {
			rest = rest[:j]
		}
		if rest != "" {
			return rest
		}
	}
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Name
}
