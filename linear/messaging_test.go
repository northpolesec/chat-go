package linear

import (
	"errors"
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/chattest"
	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/shoenig/test/must"
)

const thread = "linear:iss-1:s:sess-1"

func newAdapter(t *testing.T, m *lnMock) *Adapter {
	t.Helper()
	a, err := New(mockConfig(m))
	must.NoError(t, err)
	must.NoError(t, a.Initialize(t.Context(), chattest.NewMockChat(nil)))
	return a
}

func activityInput(t *testing.T, c *captured, i int) (string, map[string]any, bool) {
	t.Helper()
	input, _ := c.vars(t, i)["input"].(map[string]any)
	content, _ := input["content"].(map[string]any)
	eph, _ := input["ephemeral"].(bool)
	typ, _ := content["type"].(string)
	return typ, content, eph
}

func TestPostMessageIsAResponseActivity(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	posted := m.onCapture(opActivityCreate, 200, activityCreatedJSON)
	a := newAdapter(t, m)
	res, err := a.PostMessage(t.Context(), thread, chat.PostableMarkdown{Markdown: "Hello **there** {{emoji:eyes}}"})
	must.NoError(t, err)
	must.Eq(t, "c-new", res.ID)
	must.Eq(t, thread, res.Channel)
	typ, content, eph := activityInput(t, posted, 0)
	must.Eq(t, typeResponse, typ)
	must.Eq(t, "Hello **there** 👀", content["body"])
	must.False(t, eph)
	input, _ := posted.vars(t, 0)["input"].(map[string]any)
	must.Eq(t, "sess-1", input["agentSessionId"])
}

func TestPostMessageFallsBackToActivityID(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	m.on(opActivityCreate, 200, `{"data":{"agentActivityCreate":{"success":true,"agentActivity":{"id":"act-only","sourceComment":null}}}}`)
	a := newAdapter(t, m)
	res, err := a.PostMessage(t.Context(), thread, chat.PostableText("x"))
	must.NoError(t, err)
	must.Eq(t, "act-only", res.ID)
}

func TestPostMessageRendersCards(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	posted := m.onCapture(opActivityCreate, 200, activityCreatedJSON)
	a := newAdapter(t, m)
	_, err := a.PostMessage(t.Context(), thread, chat.Card{Title: "Deploy", Children: []any{chat.Fields(chat.Field("Env", "prod"))}})
	must.NoError(t, err)
	_, content, _ := activityInput(t, posted, 0)
	must.Eq(t, "**Deploy**\n\n**Env:** prod", content["body"])
}

func TestPostErrorIsAnErrorActivity(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	posted := m.onCapture(opActivityCreate, 200, activityCreatedJSON)
	a := newAdapter(t, m)
	_, err := a.PostError(t.Context(), thread, "I hit an error: boom")
	must.NoError(t, err)
	typ, content, _ := activityInput(t, posted, 0)
	must.Eq(t, typeError, typ)
	must.Eq(t, "I hit an error: boom", content["body"])
}

func TestStartTypingIsAnEphemeralThought(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	posted := m.onCapture(opActivityCreate, 200, activityCreatedJSON)
	a := newAdapter(t, m)
	must.NoError(t, a.StartTyping(t.Context(), thread, "", chat.TypingOptions{}))
	must.NoError(t, a.StartTyping(t.Context(), thread, "Reading the issue", chat.TypingOptions{}))
	typ, content, eph := activityInput(t, posted, 0)
	must.Eq(t, typeThought, typ)
	must.Eq(t, "Thinking…", content["body"])
	must.True(t, eph)
	_, content, _ = activityInput(t, posted, 1)
	must.Eq(t, "Reading the issue", content["body"])
}

func TestEditAndDeleteAreAppendOnlyErrors(t *testing.T) {
	t.Parallel()
	a := newAdapter(t, newLnMock(t))
	_, err := a.EditMessage(t.Context(), thread, "c-1", chat.PostableText("x"))
	var ve *shared.ValidationError
	must.True(t, errors.As(err, &ve))
	must.StrContains(t, err.Error(), "append-only")
	must.True(t, errors.As(a.DeleteMessage(t.Context(), thread, "c-1"), &ve))
}

func TestAddReaction(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	posted := m.onCapture(opReactionCreate, 200, `{"data":{"reactionCreate":{"success":true}}}`)
	a := newAdapter(t, m)
	must.NoError(t, a.AddReaction(t.Context(), thread, "c-1", chat.GetEmoji("eyes")))
	input, _ := posted.vars(t, 0)["input"].(map[string]any)
	must.Eq(t, "c-1", input["commentId"])
	must.Eq(t, "👀", input["emoji"])
	must.NoError(t, a.AddReaction(t.Context(), thread, "c-1", chat.EmojiValue{Name: "custom_one"}))
	input, _ = posted.vars(t, 1)["input"].(map[string]any)
	must.Eq(t, "custom_one", input["emoji"])
}

func TestAddReactionRejectsSyntheticID(t *testing.T) {
	t.Parallel()
	a := newAdapter(t, newLnMock(t))
	err := a.AddReaction(t.Context(), thread, "agent-session-sess-1", chat.GetEmoji("eyes"))
	var ve *shared.ValidationError
	must.True(t, errors.As(err, &ve))
}

func TestRemoveReactionIsANoOp(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	a := newAdapter(t, m)
	must.NoError(t, a.RemoveReaction(t.Context(), thread, "c-1", chat.GetEmoji("eyes")))
	must.Len(t, 0, m.calls())
}
