// Ported from packages/adapter-linear/src/index.ts (agent-session posting,
// streaming, fetch) @ vercel/chat v4.40.0. See PORTING.md Phase II for the
// divergences (history filter, tool errors as actions, PostError).
package linear

import (
	"context"
	"iter"
	"slices"
	"strings"
	"time"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/internal/shared"
)

var (
	_ chat.Adapter        = (*Adapter)(nil)
	_ chat.ErrorPoster    = (*Adapter)(nil)
	_ chat.Streamer       = (*Adapter)(nil)
	_ chat.SubjectFetcher = (*Adapter)(nil)
)

const (
	opActivityCreate       = "LinearAdapterActivityCreate"
	activityCreateMutation = `mutation LinearAdapterActivityCreate($input: AgentActivityCreateInput!) {
  agentActivityCreate(input: $input) { success agentActivity { id sourceComment { id } } }
}`

	typeThought     = "thought"
	typeAction      = "action"
	typeResponse    = "response"
	typeError       = "error"
	typeElicitation = "elicitation"
	typePrompt      = contentPrompt
)

var historyTypes = []string{typePrompt, typeResponse, typeError, typeElicitation}

// activityRef is what a created activity returns: its id and, when Linear
// mirrored it into the comment thread, the comment's id.
type activityRef struct {
	ID              string
	SourceCommentID string
}

// createActivity posts one agent activity to a session.
func (a *Adapter) createActivity(ctx context.Context, sessionID string, content ActivityContent, ephemeral bool) (activityRef, error) {
	input := map[string]any{"agentSessionId": sessionID, "content": content}
	if ephemeral {
		input["ephemeral"] = true
	}
	var out struct {
		Create struct {
			Activity struct {
				ID            string `json:"id"`
				SourceComment *struct {
					ID string `json:"id"`
				} `json:"sourceComment"`
			} `json:"agentActivity"`
		} `json:"agentActivityCreate"`
	}
	if err := a.api.do(ctx, opActivityCreate, activityCreateMutation, map[string]any{"input": input}, &out); err != nil {
		return activityRef{}, err
	}
	ref := activityRef{ID: out.Create.Activity.ID}
	if sc := out.Create.Activity.SourceComment; sc != nil {
		ref.SourceCommentID = sc.ID
	}
	return ref, nil
}

// raw is the chat.RawMessage for a posted activity: the mirrored comment's
// id when there is one (upstream), else the activity id (sourceComment is
// nullable and a delegation-only session has no comment thread guarantee).
func raw(ref activityRef, threadID string) *chat.RawMessage {
	id := ref.SourceCommentID
	if id == "" {
		id = ref.ID
	}
	return &chat.RawMessage{ID: id, Channel: threadID, Raw: ref}
}

const (
	opReactionCreate       = "LinearAdapterReactionCreate"
	reactionCreateMutation = `mutation LinearAdapterReactionCreate($input: ReactionCreateInput!) { reactionCreate(input: $input) { success } }`

	defaultTypingBody = "Thinking…"
	appendOnlyMessage = "agent sessions are append-only; activities cannot be edited or deleted"
)

// render is upstream renderMessageToLinearMarkdown: card or postable, then
// emoji placeholders.
func (a *Adapter) render(msg chat.AdapterPostableMessage) string {
	var body string
	switch m := msg.(type) {
	case chat.Card:
		body = shared.CardToMarkdown(m)
	case *chat.Card:
		if m != nil {
			body = shared.CardToMarkdown(*m)
		}
	default:
		if card := shared.ExtractCard(msg); card != nil {
			body = shared.CardToMarkdown(*card)
		} else {
			body = a.format.RenderPostable(msg)
		}
	}
	return chat.ConvertEmojiPlaceholders(body, adapterName)
}

// PostMessage implements chat.Adapter: a response activity.
func (a *Adapter) PostMessage(ctx context.Context, threadID string, msg chat.AdapterPostableMessage) (*chat.RawMessage, error) {
	id, err := decodeThreadID(threadID)
	if err != nil {
		return nil, err
	}
	ref, err := a.createActivity(ctx, id.SessionID, ActivityContent{Type: typeResponse, Body: a.render(msg)}, false)
	if err != nil {
		return nil, err
	}
	return raw(ref, threadID), nil
}

// PostError implements chat.ErrorPoster: an error activity, which puts the
// session in the error state.
func (a *Adapter) PostError(ctx context.Context, threadID, text string) (*chat.RawMessage, error) {
	id, err := decodeThreadID(threadID)
	if err != nil {
		return nil, err
	}
	ref, err := a.createActivity(ctx, id.SessionID, ActivityContent{Type: typeError, Body: text}, false)
	if err != nil {
		return nil, err
	}
	return raw(ref, threadID), nil
}

// StartTyping implements chat.Adapter: an ephemeral thought, replaced by
// the next activity. This is the acknowledgement Linear wants within 10 s.
func (a *Adapter) StartTyping(ctx context.Context, threadID, status string, _ chat.TypingOptions) error {
	id, err := decodeThreadID(threadID)
	if err != nil {
		return err
	}
	if status == "" {
		status = defaultTypingBody
	}
	_, err = a.createActivity(ctx, id.SessionID, ActivityContent{Type: typeThought, Body: status}, true)
	return err
}

// EditMessage implements chat.Adapter: not possible on a session.
func (a *Adapter) EditMessage(context.Context, string, string, chat.AdapterPostableMessage) (*chat.RawMessage, error) {
	return nil, shared.NewValidationError(adapterName, appendOnlyMessage)
}

// DeleteMessage implements chat.Adapter: not possible on a session.
func (a *Adapter) DeleteMessage(context.Context, string, string) error {
	return shared.NewValidationError(adapterName, appendOnlyMessage)
}

// AddReaction implements chat.Adapter: reactionCreate on the comment.
func (a *Adapter) AddReaction(ctx context.Context, threadID, messageID string, emoji chat.EmojiValue) error {
	if _, err := decodeThreadID(threadID); err != nil {
		return err
	}
	if strings.HasPrefix(messageID, syntheticIDPrefix) {
		return shared.NewValidationError(adapterName, "message has no Linear comment to react to")
	}
	input := map[string]any{"commentId": messageID, "emoji": emojiToLinear(emoji)}
	return a.api.do(ctx, opReactionCreate, reactionCreateMutation, map[string]any{"input": input}, nil)
}

// RemoveReaction implements chat.Adapter: a no-op, Linear needs the
// reaction id the adapter never kept (upstream).
func (a *Adapter) RemoveReaction(_ context.Context, threadID, messageID string, _ chat.EmojiValue) error {
	a.logger.Warn("RemoveReaction is not supported on Linear", "threadId", threadID, "messageId", messageID)
	return nil
}

// emojiToLinear is upstream's name → unicode table; unknown names pass through.
func emojiToLinear(e chat.EmojiValue) string {
	switch e.Name {
	case "thumbs_up", "+1":
		return "👍"
	case "thumbs_down", "-1":
		return "👎"
	case "heart":
		return "❤️"
	case "fire":
		return "🔥"
	case "rocket":
		return "🚀"
	case "eyes":
		return "👀"
	case "check", "white_check_mark":
		return "✅"
	case "warning":
		return "⚠️"
	case "sparkles":
		return "✨"
	case "wave":
		return "👋"
	case "raised_hands":
		return "🙌"
	case "laugh", "smile":
		return "😄"
	case "hooray", "tada":
		return "🎉"
	case "confused":
		return "😕"
	}
	return e.Name
}

const (
	opActivities    = "LinearAdapterActivities"
	activitiesQuery = `query LinearAdapterActivities($id: String!, $last: Int, $before: String, $first: Int, $after: String, $types: [String!]!) {
  agentSession(id: $id) {
    activities(
      filter: { type: { in: $types } }
      last: $last, before: $before, first: $first, after: $after
    ) {
      nodes {
        id createdAt ephemeral
        content {
          __typename
          ... on AgentActivityPromptContent { body }
          ... on AgentActivityResponseContent { body }
          ... on AgentActivityErrorContent { body }
          ... on AgentActivityElicitationContent { body }
        }
        user { id name displayName email url }
        sourceComment { id }
      }
      pageInfo { hasNextPage hasPreviousPage startCursor endCursor }
    }
  }
}`

	opIssue    = "LinearAdapterIssue"
	issueQuery = `query LinearAdapterIssue($id: String!) {
  issue(id: $id) {
    id identifier title description url
    state { name }
    assignee { id displayName }
    labels(first: 50) { nodes { name } }
    team { id key name }
  }
}`

	opUser    = "LinearAdapterUser"
	userQuery = `query LinearAdapterUser($id: String!) { user(id: $id) { id name displayName email avatarUrl } }`

	defaultFetchLimit = 50
)

type activityNode struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	Ephemeral bool      `json:"ephemeral"`
	Content   struct {
		Typename string `json:"__typename"`
		Body     string `json:"body"`
	} `json:"content"`
	User          *User `json:"user"`
	SourceComment *struct {
		ID string `json:"id"`
	} `json:"sourceComment"`
}

// FetchMessages implements chat.Adapter: the session's conversation
// activities (prompt/response/error/elicitation), oldest first. Backward
// (the default) reads the newest `limit` with last/before; forward uses
// first/after. thought and action never appear, so the bot's own progress
// is not fed back as history (divergence from upstream renderActivity).
func (a *Adapter) FetchMessages(ctx context.Context, threadID string, opts chat.FetchOptions) (chat.FetchResult, error) {
	id, err := decodeThreadID(threadID)
	if err != nil {
		return chat.FetchResult{}, err
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultFetchLimit
	}
	vars := map[string]any{"id": id.SessionID, "types": historyTypes}
	forward := opts.Direction == chat.FetchForward
	if forward {
		vars["first"] = limit
		if opts.Cursor != "" {
			vars["after"] = opts.Cursor
		}
	} else {
		vars["last"] = limit
		if opts.Cursor != "" {
			vars["before"] = opts.Cursor
		}
	}
	var out struct {
		Session *struct {
			Activities struct {
				Nodes    []activityNode `json:"nodes"`
				PageInfo struct {
					HasNextPage     bool   `json:"hasNextPage"`
					HasPreviousPage bool   `json:"hasPreviousPage"`
					StartCursor     string `json:"startCursor"`
					EndCursor       string `json:"endCursor"`
				} `json:"pageInfo"`
			} `json:"activities"`
		} `json:"agentSession"`
	}
	if err := a.api.do(ctx, opActivities, activitiesQuery, vars, &out); err != nil {
		return chat.FetchResult{}, err
	}
	if out.Session == nil {
		return chat.FetchResult{}, shared.NewResourceNotFoundError(adapterName, "agent session", id.SessionID)
	}
	botID := a.BotUserID()
	messages := make([]*chat.Message, 0, len(out.Session.Activities.Nodes))
	for _, n := range out.Session.Activities.Nodes {
		if n.Ephemeral {
			continue
		}
		mid := n.ID
		if n.SourceComment != nil && n.SourceComment.ID != "" {
			mid = n.SourceComment.ID
		}
		author := chat.Author{UserID: botID, UserName: a.userName, FullName: a.userName, IsBot: boolPtr(true), IsMe: true}
		if n.Content.Typename == "AgentActivityPromptContent" {
			author = authorFrom(n.User, botID)
		}
		msg := chat.NewMessage(chat.MessageData{
			ID: mid, ThreadID: threadID, Text: n.Content.Body, Formatted: a.format.ToAst(n.Content.Body),
			Author: author, Metadata: chat.MessageMetadata{DateSent: n.CreatedAt}, Attachments: []chat.Attachment{},
			Raw: n,
		})
		chat.SetMessageAdapter(msg, a)
		messages = append(messages, msg)
	}
	slices.SortStableFunc(messages, func(x, y *chat.Message) int { return x.Metadata.DateSent.Compare(y.Metadata.DateSent) })
	res := chat.FetchResult{Messages: messages}
	pi := out.Session.Activities.PageInfo
	if forward && pi.HasNextPage {
		res.NextCursor = pi.EndCursor
	} else if !forward && pi.HasPreviousPage {
		res.NextCursor = pi.StartCursor
	}
	return res, nil
}

type issueDetail struct {
	Issue
	State struct {
		Name string `json:"name"`
	} `json:"state"`
	Assignee *struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
	} `json:"assignee"`
	Labels struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
}

func (a *Adapter) fetchIssue(ctx context.Context, id string) (*issueDetail, error) {
	var out struct {
		Issue *issueDetail `json:"issue"`
	}
	if err := a.api.do(ctx, opIssue, issueQuery, map[string]any{"id": id}, &out); err != nil {
		return nil, err
	}
	if out.Issue == nil {
		return nil, shared.NewResourceNotFoundError(adapterName, "issue", id)
	}
	return out.Issue, nil
}

// FetchThread returns the session's issue as the thread's channel.
func (a *Adapter) FetchThread(ctx context.Context, threadID string) (chat.ThreadInfo, error) {
	id, err := decodeThreadID(threadID)
	if err != nil {
		return chat.ThreadInfo{}, err
	}
	issue, err := a.fetchIssue(ctx, id.IssueID)
	if err != nil {
		return chat.ThreadInfo{}, err
	}
	return chat.ThreadInfo{
		ID:          threadID,
		ChannelID:   adapterName + ":" + id.IssueID,
		ChannelName: issue.Identifier + ": " + issue.Title,
		Metadata: map[string]any{
			"issueId": id.IssueID, "sessionId": id.SessionID,
			"identifier": issue.Identifier, "title": issue.Title, "url": issue.URL,
		},
	}, nil
}

// GetUser looks a user up; an API error is (nil, nil) with a debug log (upstream).
func (a *Adapter) GetUser(ctx context.Context, userID string) (*chat.UserInfo, error) {
	var out struct {
		User *User `json:"user"`
	}
	if err := a.api.do(ctx, opUser, userQuery, map[string]any{"id": userID}, &out); err != nil || out.User == nil {
		a.logger.Debug("Failed to fetch user", "userId", userID, "error", err)
		return nil, nil
	}
	u := out.User
	return &chat.UserInfo{UserID: u.ID, UserName: userName(*u), FullName: u.Name, Email: u.Email, AvatarURL: u.AvatarURL}, nil
}

// FetchSubject implements chat.SubjectFetcher: the session's issue.
func (a *Adapter) FetchSubject(ctx context.Context, raw any) (*chat.MessageSubject, error) {
	rm, err := asRawMessage(raw)
	if err != nil {
		return nil, err
	}
	issue, err := a.fetchIssue(ctx, issueID(rm.Session))
	if err != nil {
		a.logger.Debug("Failed to fetch subject", "issueId", issueID(rm.Session), "error", err)
		return nil, nil
	}
	labels := make([]string, 0, len(issue.Labels.Nodes))
	for _, l := range issue.Labels.Nodes {
		if l.Name != "" {
			labels = append(labels, l.Name)
		}
	}
	var assignee *chat.SubjectPerson
	if issue.Assignee != nil {
		assignee = &chat.SubjectPerson{ID: issue.Assignee.ID, Name: issue.Assignee.DisplayName}
	}
	return &chat.MessageSubject{
		Assignee: assignee, Description: issue.Description, ID: issue.Identifier, Labels: labels,
		Raw: issue, Status: issue.State.Name, Title: issue.Title, Type: "issue", URL: issue.URL,
	}, nil
}

// actionContent is an action activity: Linear requires the parameter key
// even when empty.
func actionContent(title, result string) ActivityContent {
	return ActivityContent{Type: typeAction, Action: title, Parameter: new(string), Result: result}
}

const emptyResponseBody = "Done."

// Stream implements chat.Streamer as upstream streamInAgentSession: text
// accumulates through the streaming-markdown renderer and is flushed as a
// thought before each task card; task cards are actions (ephemeral while
// in progress); the remainder is the response. Divergences: a task error
// is an action with result "failed" (upstream posts an error activity,
// which flips the session to error for a routine tool failure); an empty
// final text still posts a response so the session cannot go stale; after
// the first failed post nothing further is posted and the iterator is
// drained so the producer never blocks.
func (a *Adapter) Stream(ctx context.Context, threadID string, stream iter.Seq2[chat.StreamChunk, error], _ chat.StreamOptions) (*chat.RawMessage, error) {
	id, err := decodeThreadID(threadID)
	if err != nil {
		return nil, err
	}
	// Thoughts are separate messages; Linear renders GFM tables.
	renderer := chat.NewStreamingMarkdownRenderer(chat.StreamingMarkdownOptions{DisableTableWrapForAppend: true})
	var appended string // text already flushed as thoughts
	var postErr error
	post := func(content ActivityContent, ephemeral bool) {
		if postErr != nil || ctx.Err() != nil {
			return
		}
		if _, err := a.createActivity(ctx, id.SessionID, content, ephemeral); err != nil {
			postErr = err
		}
	}
	flushThought := func() {
		delta := strings.TrimSpace(strings.TrimPrefix(renderer.CommittableText(), appended))
		if delta == "" {
			return
		}
		appended = renderer.CommittableText()
		post(ActivityContent{Type: typeThought, Body: delta}, false)
	}
	var iterErr error
	for chunk, err := range stream {
		if err != nil {
			iterErr = err
			continue // drain
		}
		if iterErr != nil || postErr != nil || ctx.Err() != nil {
			continue // drain
		}
		switch c := chunk.(type) {
		case chat.MarkdownTextChunk:
			renderer.Push(c.Text)
		case chat.TaskUpdateChunk:
			flushThought()
			switch c.Status {
			case chat.TaskComplete:
				post(actionContent(c.Title, ""), false)
			case chat.TaskError:
				post(actionContent(c.Title, "failed"), false)
			default: // pending, in progress
				post(actionContent(c.Title, ""), true)
			}
		}
	}
	switch {
	case iterErr != nil:
		return nil, iterErr
	case ctx.Err() != nil:
		return nil, context.Cause(ctx)
	case postErr != nil:
		return nil, postErr
	}
	renderer.Finish()
	final := strings.TrimSpace(strings.TrimPrefix(renderer.CommittableText(), appended))
	if final == "" {
		final = emptyResponseBody
	}
	if ctx.Err() != nil {
		return nil, context.Cause(ctx)
	}
	ref, err := a.createActivity(ctx, id.SessionID, ActivityContent{Type: typeResponse, Body: final}, false)
	if err != nil {
		return nil, err
	}
	return raw(ref, threadID), nil
}
