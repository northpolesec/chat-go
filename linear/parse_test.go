package linear

import (
	"testing"
	"time"

	"github.com/shoenig/test/must"
)

var (
	fixNow   = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	fixIssue = Issue{ID: "iss-1", Identifier: "INF-7", Title: "Fix the thing", URL: "https://linear.app/example/issue/INF-7/fix", Team: Team{Key: "INF", Name: "Infrastructure"}}
	fixUser  = User{ID: "u-1", Name: "Alice Example", Email: "alice@example.com", URL: "https://linear.app/example/profiles/alice"}
)

func createdEvent() sessionEventPayload {
	return sessionEventPayload{
		Action: actionCreated, Type: eventType, OrganizationID: "org-1", AppUserID: testBotUserID,
		WebhookTimestamp: float64(time.Now().UnixMilli()),
		AgentSession: AgentSession{
			ID: "sess-1", AppUserID: testBotUserID, Status: "pending", URL: "https://linear.app/example/agent/sess-1",
			Issue: &fixIssue, IssueID: "iss-1",
			Comment: &Comment{ID: "c-1", Body: "@chatbot what is this?", UserID: "u-1", IssueID: "iss-1", CreatedAt: fixNow},
			Creator: &fixUser, CreatedAt: fixNow,
		},
		PromptContext: "<issue identifier=\"INF-7\"><title>Fix the thing</title></issue>",
	}
}

func promptedEvent() sessionEventPayload {
	p := createdEvent()
	p.Action = actionPrompted
	p.PromptContext = ""
	p.AgentActivity = &AgentActivity{
		ID: "act-9", AgentSessionID: "sess-1", SourceCommentID: "c-2",
		Content: ActivityContent{Type: contentPrompt, Body: "also check the logs"},
		User:    &fixUser, UserID: "u-1", CreatedAt: fixNow.Add(time.Minute),
	}
	return p
}

func rawFrom(p sessionEventPayload) RawMessage {
	return RawMessage{Action: p.Action, Session: p.AgentSession, Activity: p.AgentActivity, PromptContext: p.PromptContext, OrganizationID: p.OrganizationID}
}

func TestParseCreatedWithComment(t *testing.T) {
	t.Parallel()
	a, err := New(Config{Token: StaticToken("x"), WebhookSecret: "s", BotUserID: testBotUserID})
	must.NoError(t, err)
	msg, err := a.ParseMessage(rawFrom(createdEvent()))
	must.NoError(t, err)
	must.Eq(t, "c-1", msg.ID)
	must.Eq(t, "linear:iss-1:s:sess-1", msg.ThreadID)
	must.Eq(t, "@chatbot what is this?", msg.Text)
	must.Eq(t, "u-1", msg.Author.UserID)
	must.Eq(t, "alice", msg.Author.UserName)
	must.Eq(t, "Alice Example", msg.Author.FullName)
	must.Eq(t, "alice@example.com", msg.Author.Email)
	must.False(t, msg.Author.IsMe)
	must.False(t, *msg.Author.IsBot)
	must.True(t, *msg.IsMention)
	must.Eq(t, fixNow, msg.Metadata.DateSent)
	rm, ok := msg.Raw.(RawMessage)
	must.True(t, ok)
	must.Eq(t, actionCreated, rm.Action)
	must.StrContains(t, rm.PromptContext, "INF-7")
}

func TestParseCreatedDelegationWithoutComment(t *testing.T) {
	t.Parallel()
	a, err := New(Config{Token: StaticToken("x"), WebhookSecret: "s", BotUserID: testBotUserID})
	must.NoError(t, err)
	p := createdEvent()
	p.AgentSession.Comment = nil
	msg, err := a.ParseMessage(rawFrom(p))
	must.NoError(t, err)
	must.Eq(t, "agent-session-sess-1", msg.ID)
	must.Eq(t, "Delegated INF-7: Fix the thing", msg.Text)
}

func TestParseCreatedByAutomation(t *testing.T) {
	t.Parallel()
	a, err := New(Config{Token: StaticToken("x"), WebhookSecret: "s", BotUserID: testBotUserID})
	must.NoError(t, err)
	p := createdEvent()
	p.AgentSession.Creator = nil
	msg, err := a.ParseMessage(rawFrom(p))
	must.NoError(t, err)
	must.Eq(t, "linear-automation", msg.Author.UserID)
	must.True(t, *msg.Author.IsBot)
}

func TestParsePrompted(t *testing.T) {
	t.Parallel()
	a, err := New(Config{Token: StaticToken("x"), WebhookSecret: "s", BotUserID: testBotUserID})
	must.NoError(t, err)
	msg, err := a.ParseMessage(rawFrom(promptedEvent()))
	must.NoError(t, err)
	must.Eq(t, "c-2", msg.ID)
	must.Eq(t, "also check the logs", msg.Text)
	must.Eq(t, "linear:iss-1:s:sess-1", msg.ThreadID)
	must.Eq(t, fixNow.Add(time.Minute), msg.Metadata.DateSent)
}

func TestParsePromptedBlankEmailFallsBackToCreator(t *testing.T) {
	t.Parallel()
	a, err := New(Config{Token: StaticToken("x"), WebhookSecret: "s", BotUserID: testBotUserID})
	must.NoError(t, err)

	sameUser := promptedEvent()
	u := *sameUser.AgentActivity.User
	u.Email = ""
	sameUser.AgentActivity.User = &u
	msg, err := a.ParseMessage(rawFrom(sameUser))
	must.NoError(t, err)
	must.Eq(t, "alice@example.com", msg.Author.Email)

	otherUser := promptedEvent()
	otherUser.AgentActivity.User = &User{ID: "u-other", Name: "Bob Example"}
	msg, err = a.ParseMessage(rawFrom(otherUser))
	must.NoError(t, err)
	must.Eq(t, "", msg.Author.Email)
}

func TestParseRejectsMissingIssue(t *testing.T) {
	t.Parallel()
	a, err := New(Config{Token: StaticToken("x"), WebhookSecret: "s", BotUserID: testBotUserID})
	must.NoError(t, err)
	p := createdEvent()
	p.AgentSession.Issue = nil
	p.AgentSession.IssueID = ""
	_, err = a.ParseMessage(rawFrom(p))
	must.Error(t, err)
}
