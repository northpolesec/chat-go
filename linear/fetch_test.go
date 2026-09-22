package linear

import (
	"errors"
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/shoenig/test/must"
)

const activitiesJSON = `{"data":{"agentSession":{"activities":{"nodes":[
 {"id":"a1","createdAt":"2026-09-20T12:00:00Z","ephemeral":false,"content":{"__typename":"AgentActivityPromptContent","body":"what is this?"},"user":{"id":"u-1","name":"Alice","email":"alice@x","url":"https://linear.app/example/profiles/alice"},"sourceComment":{"id":"c-1"}},
 {"id":"a2","createdAt":"2026-09-20T12:01:00Z","ephemeral":false,"content":{"__typename":"AgentActivityResponseContent","body":"It is a widget."},"user":null,"sourceComment":{"id":"c-2"}},
 {"id":"a3","createdAt":"2026-09-20T12:02:00Z","ephemeral":false,"content":{"__typename":"AgentActivityErrorContent","body":"I hit an error: boom"},"user":null,"sourceComment":null}
],"pageInfo":{"hasNextPage":false,"hasPreviousPage":true,"startCursor":"cur-start","endCursor":"cur-end"}}}}}`

func TestFetchMessagesBackwardUsesLastAndFilters(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	got := m.onCapture(opActivities, 200, activitiesJSON)
	a := newAdapter(t, m)
	res, err := a.FetchMessages(t.Context(), thread, chat.FetchOptions{Limit: 50})
	must.NoError(t, err)
	vars := got.vars(t, 0)
	must.Eq(t, "sess-1", vars["id"])
	must.Eq(t, 50.0, vars["last"])
	must.Nil(t, vars["first"])
	must.Len(t, 3, res.Messages)
	must.Eq(t, "c-1", res.Messages[0].ID)
	must.Eq(t, "what is this?", res.Messages[0].Text)
	must.False(t, res.Messages[0].Author.IsMe)
	must.Eq(t, "alice@x", res.Messages[0].Author.Email)
	must.Eq(t, "c-2", res.Messages[1].ID)
	must.True(t, res.Messages[1].Author.IsMe)
	must.Eq(t, testBotUserID, res.Messages[1].Author.UserID)
	must.Eq(t, "a3", res.Messages[2].ID)
	must.True(t, res.Messages[2].Author.IsMe)
	must.Eq(t, "cur-start", res.NextCursor)
	must.Eq[any](t, []any{typePrompt, typeResponse, typeError, typeElicitation}, vars["types"])
}

func TestFetchMessagesForwardUsesFirstAndAfter(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	got := m.onCapture(opActivities, 200, activitiesJSON)
	a := newAdapter(t, m)
	res, err := a.FetchMessages(t.Context(), thread, chat.FetchOptions{Limit: 10, Direction: chat.FetchForward, Cursor: "c0"})
	must.NoError(t, err)
	vars := got.vars(t, 0)
	must.Eq(t, 10.0, vars["first"])
	must.Eq(t, "c0", vars["after"])
	must.Nil(t, vars["last"])
	must.Eq(t, "", res.NextCursor) // hasNextPage false
}

func TestFetchMessagesSkipsEphemeral(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	m.on(opActivities, 200, `{"data":{"agentSession":{"activities":{"nodes":[
 {"id":"a1","createdAt":"2026-09-20T12:00:00Z","ephemeral":false,"content":{"__typename":"AgentActivityPromptContent","body":"keep"},"user":{"id":"u-1","name":"Alice"},"sourceComment":{"id":"c-1"}},
 {"id":"a-eph","createdAt":"2026-09-20T12:01:00Z","ephemeral":true,"content":{"__typename":"AgentActivityThoughtContent","body":"Thinking…"},"user":null,"sourceComment":null},
 {"id":"a2","createdAt":"2026-09-20T12:02:00Z","ephemeral":false,"content":{"__typename":"AgentActivityResponseContent","body":"done"},"user":null,"sourceComment":{"id":"c-2"}}
],"pageInfo":{"hasNextPage":false,"hasPreviousPage":false}}}}}`)
	a := newAdapter(t, m)
	res, err := a.FetchMessages(t.Context(), thread, chat.FetchOptions{Limit: 50})
	must.NoError(t, err)
	must.Len(t, 2, res.Messages)
	must.Eq(t, "c-1", res.Messages[0].ID)
	must.Eq(t, "c-2", res.Messages[1].ID)
}

func TestFetchMessagesMissingSession(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	m.on(opActivities, 200, `{"data":{"agentSession":null}}`)
	a := newAdapter(t, m)
	_, err := a.FetchMessages(t.Context(), thread, chat.FetchOptions{})
	var nfe *shared.ResourceNotFoundError
	must.True(t, errors.As(err, &nfe))
}

const issueJSON = `{"data":{"issue":{"id":"iss-1","identifier":"INF-7","title":"Fix the thing","description":"desc","url":"https://linear.app/example/issue/INF-7/fix","state":{"name":"In Progress"},"assignee":{"id":"u-2","displayName":"bob"},"labels":{"nodes":[{"name":"bug"},{"name":"a11y"}]},"team":{"id":"t-1","key":"INF","name":"Infrastructure"}}}}`

func TestFetchThread(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	m.on(opIssue, 200, issueJSON)
	a := newAdapter(t, m)
	info, err := a.FetchThread(t.Context(), thread)
	must.NoError(t, err)
	must.Eq(t, thread, info.ID)
	must.Eq(t, "linear:iss-1", info.ChannelID)
	must.Eq(t, "INF-7: Fix the thing", info.ChannelName)
	must.Eq(t, "sess-1", info.Metadata["sessionId"])
	must.Eq(t, "INF-7", info.Metadata["identifier"])
}

func TestFetchSubject(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	m.on(opIssue, 200, issueJSON)
	a := newAdapter(t, m)
	subj, err := a.FetchSubject(t.Context(), rawFrom(createdEvent()))
	must.NoError(t, err)
	must.Eq(t, "issue", subj.Type)
	must.Eq(t, "INF-7", subj.ID)
	must.Eq(t, "In Progress", subj.Status)
	must.Eq(t, []string{"bug", "a11y"}, subj.Labels)
	must.Eq(t, "bob", subj.Assignee.Name)
}

func TestFetchSubjectSwallowsAPIErrors(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	m.on(opIssue, 500, `no`)
	a := newAdapter(t, m)
	subj, err := a.FetchSubject(t.Context(), rawFrom(createdEvent()))
	must.NoError(t, err)
	must.Nil(t, subj)
}

func TestGetUser(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	m.on(opUser, 200, `{"data":{"user":{"id":"u-1","name":"Alice Example","displayName":"alice","email":"alice@x","avatarUrl":"https://a/x.png"}}}`)
	a := newAdapter(t, m)
	u, err := a.GetUser(t.Context(), "u-1")
	must.NoError(t, err)
	must.Eq(t, &chat.UserInfo{UserID: "u-1", UserName: "alice", FullName: "Alice Example", Email: "alice@x", AvatarURL: "https://a/x.png"}, u)
	m.on(opUser, 404, `{"errors":[{"message":"Entity not found"}]}`)
	u, err = a.GetUser(t.Context(), "nope")
	must.NoError(t, err)
	must.Nil(t, u)
}
