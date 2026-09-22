// Package linear is the Go port of @chat-adapter/linear v4.40.0 (upstream
// packages/adapter-linear), agent-sessions mode only. Comments mode,
// multi-tenant installations, Vercel Connect, and token encryption are not
// ported (see PORTING.md).
package linear

import (
	"fmt"
	"regexp"
	"time"

	"github.com/northpolesec/chat-go/internal/shared"
)

const (
	adapterName = "linear"

	actionCreated  = "created"
	actionPrompted = "prompted"
	signalStop     = "stop"
	contentPrompt  = "prompt"
	eventType      = "AgentSessionEvent"

	// syntheticIDPrefix marks a message id minted for a session that has no
	// source comment (delegation without a comment). Never a Linear id.
	syntheticIDPrefix = "agent-session-"
)

// ThreadID is the decoded thread id: one agent session on one issue.
type ThreadID struct {
	IssueID   string
	SessionID string
}

var (
	sessionThreadRe  = regexp.MustCompile(`^linear:([^:]+):s:([^:]+)$`)
	commentsThreadRe = regexp.MustCompile(`^linear:[^:]+(:c:[^:]+(:s:[^:]+)?)?$`)
)

// encodeThreadID is upstream encodeThreadId for the `:s:` shape only.
func encodeThreadID(id ThreadID) (string, error) {
	if id.IssueID == "" || id.SessionID == "" {
		return "", shared.NewValidationError(adapterName, "thread id needs an issue id and a session id")
	}
	return adapterName + ":" + id.IssueID + ":s:" + id.SessionID, nil
}

// decodeThreadID accepts linear:{issueId}:s:{sessionId}. The three comments
// mode shapes are recognised and rejected by name so a stale id fails
// loudly instead of posting a comment.
func decodeThreadID(threadID string) (ThreadID, error) {
	if m := sessionThreadRe.FindStringSubmatch(threadID); m != nil {
		return ThreadID{IssueID: m[1], SessionID: m[2]}, nil
	}
	if commentsThreadRe.MatchString(threadID) {
		return ThreadID{}, shared.NewValidationError(adapterName, fmt.Sprintf("thread id %q is a comments mode thread; only agent sessions are supported", threadID))
	}
	return ThreadID{}, shared.NewValidationError(adapterName, fmt.Sprintf("Invalid Linear thread ID: %s", threadID))
}

// User is UserChildWebhookPayload / the user query subset.
type User struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	Email       string `json:"email"`
	AvatarURL   string `json:"avatarUrl"`
	URL         string `json:"url"`
}

// Team is the team subset carried on an issue.
type Team struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

// Issue is IssueWithDescriptionChildWebhookPayload / the issue query subset.
type Issue struct {
	ID          string `json:"id"`
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	TeamID      string `json:"teamId"`
	Team        Team   `json:"team"`
}

// Comment is CommentChildWebhookPayload.
type Comment struct {
	ID        string    `json:"id"`
	Body      string    `json:"body"`
	UserID    string    `json:"userId"`
	IssueID   string    `json:"issueId"`
	CreatedAt time.Time `json:"createdAt"`
}

// AgentSession is AgentSessionWebhookPayload.
type AgentSession struct {
	ID             string    `json:"id"`
	AppUserID      string    `json:"appUserId"`
	OrganizationID string    `json:"organizationId"`
	Status         string    `json:"status"`
	URL            string    `json:"url"`
	Issue          *Issue    `json:"issue"`
	IssueID        string    `json:"issueId"`
	Comment        *Comment  `json:"comment"`
	CommentID      string    `json:"commentId"`
	Creator        *User     `json:"creator"`
	CreatorID      string    `json:"creatorId"`
	CreatedAt      time.Time `json:"createdAt"`
}

// ActivityContent is the JSONObject content of an agent activity. One
// struct for every type; unused fields are empty. Parameter is a pointer
// so an action can send "" (Linear requires the key) while thought,
// response, and error omit it.
type ActivityContent struct {
	Type      string  `json:"type"`
	Body      string  `json:"body,omitempty"`
	Action    string  `json:"action,omitempty"`
	Parameter *string `json:"parameter,omitempty"`
	Result    string  `json:"result,omitempty"`
}

// AgentActivity is AgentActivityWebhookPayload / the activities query node.
type AgentActivity struct {
	ID              string          `json:"id"`
	AgentSessionID  string          `json:"agentSessionId"`
	SourceCommentID string          `json:"sourceCommentId"`
	Content         ActivityContent `json:"content"`
	Signal          string          `json:"signal"`
	Ephemeral       bool            `json:"ephemeral"`
	User            *User           `json:"user"`
	UserID          string          `json:"userId"`
	CreatedAt       time.Time       `json:"createdAt"`
}

// sessionEventPayload is the AgentSessionEvent webhook envelope. guidance
// and previousComments are not decoded: promptContext already renders both.
type sessionEventPayload struct {
	Action           string         `json:"action"`
	Type             string         `json:"type"`
	OrganizationID   string         `json:"organizationId"`
	AppUserID        string         `json:"appUserId"`
	WebhookID        string         `json:"webhookId"`
	WebhookTimestamp float64        `json:"webhookTimestamp"`
	AgentSession     AgentSession   `json:"agentSession"`
	AgentActivity    *AgentActivity `json:"agentActivity"`
	PromptContext    string         `json:"promptContext"`
}

// RawMessage is chat.Message.Raw for Linear.
type RawMessage struct {
	Action         string         `json:"action"` // actionCreated | actionPrompted
	Session        AgentSession   `json:"session"`
	Activity       *AgentActivity `json:"activity,omitempty"` // prompted only
	PromptContext  string         `json:"promptContext,omitempty"`
	OrganizationID string         `json:"organizationId"`
}
