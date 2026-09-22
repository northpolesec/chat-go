package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/northpolesec/chat-go/chat"
)

// maxWebhookBody is GitHub's webhook payload cap (25 MB).
const maxWebhookBody = 25 << 20

const (
	headerSignature = "X-Hub-Signature-256"
	headerEvent     = "X-GitHub-Event"
	eventPing       = "ping"
	eventIssue      = "issue_comment"
	eventReview     = "pull_request_review_comment"
	actionCreated   = "created"
)

// HandleWebhook is adapter.handleWebhook. It verifies, parses, dispatches
// created comments to chat.ProcessMessage (awaited, under a context that
// survives the request), and acks.
func (a *Adapter) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "Payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "Could not read body", http.StatusBadRequest)
		return
	}
	if a.webhookVerifier != nil {
		if err := a.webhookVerifier(r, body); err != nil {
			a.logger.Warn("GitHub webhook verifier rejected the request", "error", err)
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}
	} else if !verifySignature(a.webhookSecret, body, r.Header.Get(headerSignature)) {
		a.logger.Debug("GitHub webhook signature verification failed", "event", r.Header.Get(headerEvent), "bytes", len(body))
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}
	event := r.Header.Get(headerEvent)
	if event == eventPing {
		a.logger.Info("GitHub webhook ping received")
		_, _ = io.WriteString(w, "pong")
		return
	}
	ctx := context.WithoutCancel(r.Context())
	switch event {
	case eventIssue:
		var p issueCommentPayload
		if err := json.Unmarshal(body, &p); err != nil {
			a.logger.Error("GitHub webhook invalid JSON", "error", err)
			http.Error(w, "Invalid JSON. Make sure webhook Content-Type is set to application/json", http.StatusBadRequest)
			return
		}
		if p.Action == actionCreated {
			a.handleIssueComment(ctx, p)
		}
	case eventReview:
		var p reviewCommentPayload
		if err := json.Unmarshal(body, &p); err != nil {
			a.logger.Error("GitHub webhook invalid JSON", "error", err)
			http.Error(w, "Invalid JSON. Make sure webhook Content-Type is set to application/json", http.StatusBadRequest)
			return
		}
		if p.Action == actionCreated {
			a.handleReviewComment(ctx, p)
		}
	default:
		if !json.Valid(body) {
			http.Error(w, "Invalid JSON. Make sure webhook Content-Type is set to application/json", http.StatusBadRequest)
			return
		}
	}
	_, _ = io.WriteString(w, "ok")
}

func (a *Adapter) chatInstance() chat.ChatInstance {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.chat
}

func (a *Adapter) isSelf(sender User) bool {
	return a.BotUserID() != "" && strconv.FormatInt(sender.ID, 10) == a.BotUserID()
}

// handleIssueComment is upstream handleIssueComment (PR conversation or issue).
func (a *Adapter) handleIssueComment(ctx context.Context, p issueCommentPayload) {
	c := a.chatInstance()
	if c == nil {
		a.logger.Warn("Chat instance not initialized, ignoring comment")
		return
	}
	threadType := ThreadTypeIssue
	if p.Issue.PullRequest != nil {
		threadType = ThreadTypePR
	}
	threadID, err := encodeThreadID(ThreadID{Owner: p.Repository.Owner.Login, Repo: p.Repository.Name, Number: p.Issue.Number, Type: threadType})
	if err != nil {
		a.logger.Warn("encode thread id", "error", err)
		return
	}
	if a.isSelf(p.Sender) {
		a.logger.Debug("Ignoring message from self", "messageId", p.Comment.ID)
		return
	}
	msg := a.parseIssueComment(p.Comment, p.Repository, p.Issue.Number, threadID, threadType)
	_ = c.ProcessMessage(ctx, chat.ProcessMessageInput{Adapter: a, Message: msg, ThreadID: threadID})
}

// handleReviewComment is upstream handleReviewComment (inline review thread).
func (a *Adapter) handleReviewComment(ctx context.Context, p reviewCommentPayload) {
	c := a.chatInstance()
	if c == nil {
		a.logger.Warn("Chat instance not initialized, ignoring comment")
		return
	}
	root := p.Comment.InReplyToID
	if root == 0 {
		root = p.Comment.ID
	}
	threadID, err := encodeThreadID(ThreadID{Owner: p.Repository.Owner.Login, Repo: p.Repository.Name, Number: p.PullRequest.Number, ReviewCommentID: int(root)})
	if err != nil {
		a.logger.Warn("encode thread id", "error", err)
		return
	}
	if a.isSelf(p.Sender) {
		a.logger.Debug("Ignoring message from self", "messageId", p.Comment.ID)
		return
	}
	msg := a.parseReviewComment(p.Comment, p.Repository, p.PullRequest.Number, threadID)
	_ = c.ProcessMessage(ctx, chat.ProcessMessageInput{Adapter: a, Message: msg, ThreadID: threadID})
}
