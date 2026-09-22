package github

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/yuin/goldmark/ast"
)

var (
	_ chat.Adapter        = (*Adapter)(nil)
	_ chat.Streamer       = (*Adapter)(nil)
	_ chat.SubjectFetcher = (*Adapter)(nil)

	errInvalidRepoFullName = errors.New("invalid repository full_name")
)

// render is upstream's card-or-postable body plus emoji placeholder conversion.
func (a *Adapter) render(msg chat.AdapterPostableMessage) string {
	var body string
	if card := shared.ExtractCard(msg); card != nil {
		body = CardToGitHubMarkdown(*card)
	} else {
		body = a.format.RenderPostable(msg)
	}
	return chat.ConvertEmojiPlaceholders(body, adapterName)
}

func (a *Adapter) raw(id ThreadID, c Comment) *chat.RawMessage {
	repo := Repository{Name: id.Repo, FullName: id.Owner + "/" + id.Repo, Owner: User{Login: id.Owner, Type: "User"}}
	rm := RawMessage{Type: rawIssueComment, Comment: c, Repository: repo, Number: id.Number, ThreadType: id.Type}
	if id.ReviewCommentID != 0 {
		rm = RawMessage{Type: rawReviewComment, Comment: c, Repository: repo, Number: id.Number}
	}
	threadID, _ := encodeThreadID(id)
	return &chat.RawMessage{ID: strconv.FormatInt(c.ID, 10), Channel: threadID, Raw: rm}
}

func repoPath(id ThreadID) string {
	return "/repos/" + url.PathEscape(id.Owner) + "/" + url.PathEscape(id.Repo)
}

func parseChannelID(channelID string) (owner, repo string, err error) {
	const prefix = adapterName + ":"
	if !strings.HasPrefix(channelID, prefix) {
		return "", "", shared.NewValidationError(adapterName, "Invalid GitHub channel ID: "+channelID)
	}
	owner, repo, ok := strings.Cut(channelID[len(prefix):], "/")
	if !ok {
		return "", "", shared.NewValidationError(adapterName, "Invalid GitHub channel ID: "+channelID)
	}
	return owner, repo, nil
}

func commentReactionPath(id ThreadID, messageID string) string {
	base := repoPath(id)
	if id.ReviewCommentID != 0 {
		return base + "/pulls/comments/" + url.PathEscape(messageID) + "/reactions"
	}
	return base + "/issues/comments/" + url.PathEscape(messageID) + "/reactions"
}

func userOrZero(u *User) User {
	if u == nil {
		return User{}
	}
	return *u
}

// PostMessage implements chat.Adapter.
func (a *Adapter) PostMessage(ctx context.Context, threadID string, msg chat.AdapterPostableMessage) (*chat.RawMessage, error) {
	id, err := decodeThreadID(threadID)
	if err != nil {
		return nil, err
	}
	body := map[string]string{"body": a.render(msg)}
	var c Comment
	path := repoPath(id) + "/issues/" + strconv.Itoa(id.Number) + "/comments"
	if id.ReviewCommentID != 0 {
		path = repoPath(id) + "/pulls/" + strconv.Itoa(id.Number) + "/comments/" + strconv.Itoa(id.ReviewCommentID) + "/replies"
	}
	if err := a.api.do(ctx, http.MethodPost, path, nil, body, &c); err != nil {
		return nil, err
	}
	a.captureBotUserID(c.User)
	return a.raw(id, c), nil
}

// EditMessage implements chat.Adapter.
func (a *Adapter) EditMessage(ctx context.Context, threadID, messageID string, msg chat.AdapterPostableMessage) (*chat.RawMessage, error) {
	id, err := decodeThreadID(threadID)
	if err != nil {
		return nil, err
	}
	body := map[string]string{"body": a.render(msg)}
	var c Comment
	path := repoPath(id) + "/issues/comments/" + url.PathEscape(messageID)
	if id.ReviewCommentID != 0 {
		path = repoPath(id) + "/pulls/comments/" + url.PathEscape(messageID)
	}
	if err := a.api.do(ctx, http.MethodPatch, path, nil, body, &c); err != nil {
		return nil, err
	}
	return a.raw(id, c), nil
}

// DeleteMessage implements chat.Adapter.
func (a *Adapter) DeleteMessage(ctx context.Context, threadID, messageID string) error {
	id, err := decodeThreadID(threadID)
	if err != nil {
		return err
	}
	path := repoPath(id) + "/issues/comments/" + url.PathEscape(messageID)
	if id.ReviewCommentID != 0 {
		path = repoPath(id) + "/pulls/comments/" + url.PathEscape(messageID)
	}
	return a.api.do(ctx, http.MethodDelete, path, nil, nil, nil)
}

// AddReaction implements chat.Adapter.
func (a *Adapter) AddReaction(ctx context.Context, threadID, messageID string, emoji chat.EmojiValue) error {
	id, err := decodeThreadID(threadID)
	if err != nil {
		return err
	}
	body := map[string]string{"content": emojiToReaction(emoji)}
	return a.api.do(ctx, http.MethodPost, commentReactionPath(id, messageID), nil, body, nil)
}

// RemoveReaction implements chat.Adapter: list reactions and delete only the
// bot's own match. detectBotUserID runs first when BotUserID is empty.
func (a *Adapter) RemoveReaction(ctx context.Context, threadID, messageID string, emoji chat.EmojiValue) error {
	id, err := decodeThreadID(threadID)
	if err != nil {
		return err
	}
	if a.BotUserID() == "" {
		a.detectBotUserID(ctx)
	}
	content := emojiToReaction(emoji)
	path := commentReactionPath(id, messageID)
	var reactions []Reaction
	if err := a.api.do(ctx, http.MethodGet, path, nil, nil, &reactions); err != nil {
		return err
	}
	botID := a.BotUserID()
	for _, r := range reactions {
		if r.Content == content && strconv.FormatInt(r.User.ID, 10) == botID {
			return a.api.do(ctx, http.MethodDelete, path+"/"+strconv.FormatInt(r.ID, 10), nil, nil, nil)
		}
	}
	return nil
}

// Stream implements chat.Streamer: accumulate text and post once at the
// end (upstream: GitHub 422s empty edits and rate-limits rapid ones). An
// iterator error or a canceled ctx returns without posting; empty text
// returns (nil, nil).
func (a *Adapter) Stream(ctx context.Context, threadID string, stream iter.Seq2[chat.StreamChunk, error], _ chat.StreamOptions) (*chat.RawMessage, error) {
	var b strings.Builder
	for chunk, err := range stream {
		if err != nil {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, context.Cause(ctx)
		}
		if t, ok := chunk.(chat.MarkdownTextChunk); ok {
			b.WriteString(t.Text)
		}
	}
	if ctx.Err() != nil {
		return nil, context.Cause(ctx)
	}
	if b.Len() == 0 {
		return nil, nil
	}
	return a.PostMessage(ctx, threadID, chat.PostableMarkdown{Markdown: b.String()})
}

// StartTyping implements chat.Adapter: GitHub has no typing indicator.
func (a *Adapter) StartTyping(context.Context, string, string, chat.TypingOptions) error { return nil }

// emojiToReaction is upstream emojiToGitHubReaction; unknown → "+1".
func emojiToReaction(e chat.EmojiValue) string {
	switch e.Name {
	case "thumbs_up", "+1":
		return "+1"
	case "thumbs_down", "-1":
		return "-1"
	case "laugh", "smile":
		return "laugh"
	case "confused", "thinking":
		return "confused"
	case "heart", "love_eyes":
		return "heart"
	case "hooray", "party", "confetti":
		return "hooray"
	case "rocket":
		return "rocket"
	case "eyes":
		return "eyes"
	}
	return "+1"
}

// FetchMessages implements chat.Adapter.
func (a *Adapter) FetchMessages(ctx context.Context, threadID string, opts chat.FetchOptions) (chat.FetchResult, error) {
	id, err := decodeThreadID(threadID)
	if err != nil {
		return chat.FetchResult{}, err
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	var messages []*chat.Message
	if id.ReviewCommentID != 0 {
		var comments []Comment
		if err := a.api.do(ctx, http.MethodGet, repoPath(id)+"/pulls/"+strconv.Itoa(id.Number)+"/comments", url.Values{"per_page": {"100"}}, nil, &comments); err != nil {
			return chat.FetchResult{}, err
		}
		root := int64(id.ReviewCommentID)
		repo := Repository{Name: id.Repo, Owner: User{Login: id.Owner, Type: "User"}}
		for _, c := range comments {
			if c.ID == root || c.InReplyToID == root {
				messages = append(messages, a.parseReviewComment(c, repo, id.Number, threadID))
			}
		}
	} else {
		var comments []Comment
		if err := a.api.do(ctx, http.MethodGet, repoPath(id)+"/issues/"+strconv.Itoa(id.Number)+"/comments", url.Values{"per_page": {strconv.Itoa(limit)}}, nil, &comments); err != nil {
			return chat.FetchResult{}, err
		}
		repo := Repository{Name: id.Repo, Owner: User{Login: id.Owner, Type: "User"}}
		for _, c := range comments {
			messages = append(messages, a.parseIssueComment(c, repo, id.Number, threadID, id.Type))
		}
	}
	slices.SortFunc(messages, func(x, y *chat.Message) int {
		return x.Metadata.DateSent.Compare(y.Metadata.DateSent)
	})
	if len(messages) > limit {
		if opts.Direction == chat.FetchForward {
			messages = messages[:limit]
		} else {
			messages = messages[len(messages)-limit:]
		}
	}
	return chat.FetchResult{Messages: messages}, nil
}

// FetchThread returns thread metadata for a PR, issue, or review thread.
func (a *Adapter) FetchThread(ctx context.Context, threadID string) (chat.ThreadInfo, error) {
	id, err := decodeThreadID(threadID)
	if err != nil {
		return chat.ThreadInfo{}, err
	}
	info := chat.ThreadInfo{
		ID:          threadID,
		ChannelID:   id.Owner + "/" + id.Repo,
		ChannelName: fmt.Sprintf("%s #%d", id.Repo, id.Number),
	}
	if id.Type == ThreadTypeIssue {
		var issue Issue
		if err := a.api.do(ctx, http.MethodGet, repoPath(id)+"/issues/"+strconv.Itoa(id.Number), nil, nil, &issue); err != nil {
			return chat.ThreadInfo{}, err
		}
		info.Metadata = map[string]any{
			"owner":       id.Owner,
			"repo":        id.Repo,
			"issueNumber": id.Number,
			"issueTitle":  issue.Title,
			"issueState":  issue.State,
			"type":        "issue",
		}
		return info, nil
	}
	var pr Issue
	if err := a.api.do(ctx, http.MethodGet, repoPath(id)+"/pulls/"+strconv.Itoa(id.Number), nil, nil, &pr); err != nil {
		return chat.ThreadInfo{}, err
	}
	meta := map[string]any{
		"owner":    id.Owner,
		"repo":     id.Repo,
		"prNumber": id.Number,
		"prTitle":  pr.Title,
		"prState":  pr.State,
	}
	if id.ReviewCommentID != 0 {
		meta["reviewCommentId"] = id.ReviewCommentID
	}
	info.Metadata = meta
	return info, nil
}

// GetUser fetches GET /user/{id}. API errors return (nil, nil).
func (a *Adapter) GetUser(ctx context.Context, userID string) (*chat.UserInfo, error) {
	var u User
	if err := a.api.do(ctx, http.MethodGet, "/user/"+url.PathEscape(userID), nil, nil, &u); err != nil {
		a.logger.Debug("Failed to fetch user", "userId", userID, "error", err)
		return nil, nil
	}
	name := u.Name
	if name == "" {
		name = u.Login
	}
	return &chat.UserInfo{
		AvatarURL: u.AvatarURL,
		Email:     u.Email,
		FullName:  name,
		IsBot:     u.Type == "Bot",
		UserID:    strconv.FormatInt(u.ID, 10),
		UserName:  u.Login,
	}, nil
}

// ListThreads lists open PRs in github:{owner}/{repo} as threads.
func (a *Adapter) ListThreads(ctx context.Context, channelID string, opts chat.FetchOptions) (chat.ListThreadsResult, error) {
	owner, repo, err := parseChannelID(channelID)
	if err != nil {
		return chat.ListThreadsResult{}, err
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 30
	}
	page := 1
	if opts.Cursor != "" {
		if p, convErr := strconv.Atoi(opts.Cursor); convErr == nil && p > 0 {
			page = p
		}
	}
	id := ThreadID{Owner: owner, Repo: repo}
	var pulls []Issue
	q := url.Values{
		"state":     {"open"},
		"sort":      {"updated"},
		"direction": {"desc"},
		"per_page":  {strconv.Itoa(limit)},
		"page":      {strconv.Itoa(page)},
	}
	if err := a.api.do(ctx, http.MethodGet, repoPath(id)+"/pulls", q, nil, &pulls); err != nil {
		return chat.ListThreadsResult{}, err
	}
	threads := make([]chat.ThreadSummary, 0, len(pulls))
	repoRef := Repository{Name: repo, FullName: owner + "/" + repo, Owner: User{Login: owner, Type: "User"}}
	for _, pr := range pulls {
		threadID, encErr := encodeThreadID(ThreadID{Owner: owner, Repo: repo, Number: pr.Number, Type: ThreadTypePR})
		if encErr != nil {
			return chat.ListThreadsResult{}, encErr
		}
		body := pr.Body
		if body == "" {
			body = pr.Title
		}
		root := a.parseIssueComment(Comment{
			ID:        int64(pr.Number),
			Body:      body,
			User:      userOrZero(pr.User),
			CreatedAt: pr.CreatedAt,
			UpdatedAt: pr.UpdatedAt,
			HTMLURL:   pr.HTMLURL,
		}, repoRef, pr.Number, threadID, ThreadTypePR)
		root.Text = pr.Title
		root.Formatted = a.format.ToAst(pr.Title)
		threads = append(threads, chat.ThreadSummary{
			ID:          threadID,
			LastReplyAt: pr.UpdatedAt,
			RootMessage: root,
		})
	}
	next := ""
	if len(pulls) == limit {
		next = strconv.Itoa(page + 1)
	}
	return chat.ListThreadsResult{Threads: threads, NextCursor: next}, nil
}

// FetchChannelInfo returns repository metadata for github:{owner}/{repo}.
func (a *Adapter) FetchChannelInfo(ctx context.Context, channelID string) (chat.ChannelInfo, error) {
	owner, repo, err := parseChannelID(channelID)
	if err != nil {
		return chat.ChannelInfo{}, err
	}
	var data Repository
	if err := a.api.do(ctx, http.MethodGet, repoPath(ThreadID{Owner: owner, Repo: repo}), nil, nil, &data); err != nil {
		return chat.ChannelInfo{}, err
	}
	return chat.ChannelInfo{
		ID:   channelID,
		Name: data.FullName,
		Metadata: map[string]any{
			"owner":           owner,
			"repo":            repo,
			"description":     data.Description,
			"visibility":      data.Visibility,
			"defaultBranch":   data.DefaultBranch,
			"openIssuesCount": data.OpenIssuesCount,
		},
	}, nil
}

// RenderFormatted renders a goldmark AST as GitHub markdown.
func (a *Adapter) RenderFormatted(content chat.FormattedContent) string {
	node, _ := content.(ast.Node)
	return a.format.FromAst(node, nil)
}

// FetchSubject implements chat.SubjectFetcher. raw is a RawMessage. Issue
// threads hit /issues/{n}; everything else hits /pulls/{n}. API errors
// return (nil, nil).
func (a *Adapter) FetchSubject(ctx context.Context, raw any) (*chat.MessageSubject, error) {
	rm, err := asRawMessage(raw)
	if err != nil {
		return nil, err
	}
	parts := strings.SplitN(rm.Repository.FullName, "/", 2)
	if len(parts) != 2 {
		a.logger.Debug("Failed to fetch subject", "owner", "", "repo", "", "number", rm.Number, "error", errInvalidRepoFullName)
		return nil, nil
	}
	owner, repo := parts[0], parts[1]
	id := ThreadID{Owner: owner, Repo: repo}
	isIssue := rm.Type == rawIssueComment && rm.ThreadType == ThreadTypeIssue
	path := repoPath(id) + "/pulls/" + strconv.Itoa(rm.Number)
	subjType := "pull_request"
	if isIssue {
		path = repoPath(id) + "/issues/" + strconv.Itoa(rm.Number)
		subjType = "issue"
	}
	var data Issue
	if err := a.api.do(ctx, http.MethodGet, path, nil, nil, &data); err != nil {
		a.logger.Debug("Failed to fetch subject", "owner", owner, "repo", repo, "number", rm.Number, "error", err)
		return nil, nil
	}
	var author *chat.SubjectPerson
	if data.User != nil {
		author = &chat.SubjectPerson{ID: strconv.FormatInt(data.User.ID, 10), Name: data.User.Login}
	}
	var assignee *chat.SubjectPerson
	if len(data.Assignees) > 0 {
		assignee = &chat.SubjectPerson{ID: strconv.FormatInt(data.Assignees[0].ID, 10), Name: data.Assignees[0].Login}
	}
	labels := make([]string, 0, len(data.Labels))
	for _, l := range data.Labels {
		if l.Name != "" {
			labels = append(labels, l.Name)
		}
	}
	return &chat.MessageSubject{
		Assignee:    assignee,
		Author:      author,
		Description: data.Body,
		ID:          strconv.Itoa(data.Number),
		Labels:      labels,
		Raw:         data,
		Status:      data.State,
		Title:       data.Title,
		Type:        subjType,
		URL:         data.HTMLURL,
	}, nil
}

// Probe proves the App JWT → installation token path: the App slug
// (GET /app as the App) and how many repositories the installation token
// can see. Go-added; App mode only.
func (a *Adapter) Probe(ctx context.Context) (slug string, repos int, err error) {
	if a.app == nil {
		return "", 0, shared.NewValidationError(adapterName, "Probe requires GitHub App credentials")
	}
	jwt, err := a.app.AppJWT()
	if err != nil {
		return "", 0, err
	}
	var app struct {
		Slug string `json:"slug"`
	}
	if err := newClient(a.api.http, a.api.base, StaticToken(jwt)).do(ctx, http.MethodGet, "/app", nil, nil, &app); err != nil {
		return "", 0, err
	}
	var page struct {
		TotalCount int `json:"total_count"`
	}
	if err := a.api.do(ctx, http.MethodGet, "/installation/repositories", url.Values{"per_page": {"1"}}, nil, &page); err != nil {
		return "", 0, err
	}
	return app.Slug, page.TotalCount, nil
}
