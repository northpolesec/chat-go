package github

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/internal/shared"
)

func boolPtr(b bool) *bool { return &b }

// ParseMessage implements chat.Adapter (upstream parseMessage). raw is a
// RawMessage, *RawMessage, or a JSON-shaped value.
func (a *Adapter) ParseMessage(raw any) (*chat.Message, error) {
	rm, err := asRawMessage(raw)
	if err != nil {
		return nil, err
	}
	switch rm.Type {
	case rawIssueComment:
		threadType := rm.ThreadType
		if threadType == "" {
			threadType = ThreadTypePR
		}
		threadID, err := encodeThreadID(ThreadID{Owner: rm.Repository.Owner.Login, Repo: rm.Repository.Name, Number: rm.Number, Type: threadType})
		if err != nil {
			return nil, err
		}
		return a.parseIssueComment(rm.Comment, rm.Repository, rm.Number, threadID, threadType), nil
	case rawReviewComment:
		root := rm.Comment.InReplyToID
		if root == 0 {
			root = rm.Comment.ID
		}
		threadID, err := encodeThreadID(ThreadID{Owner: rm.Repository.Owner.Login, Repo: rm.Repository.Name, Number: rm.Number, ReviewCommentID: int(root)})
		if err != nil {
			return nil, err
		}
		return a.parseReviewComment(rm.Comment, rm.Repository, rm.Number, threadID), nil
	}
	return nil, shared.NewValidationError(adapterName, fmt.Sprintf("unknown raw message type %q", rm.Type))
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
			if err := json.Unmarshal(b, &rm); err == nil && rm.Type != "" {
				return rm, nil
			}
		}
	}
	return RawMessage{}, shared.NewValidationError(adapterName, fmt.Sprintf("ParseMessage expects a github.RawMessage, got %T", raw))
}

// parseIssueComment is upstream parseIssueComment.
func (a *Adapter) parseIssueComment(c Comment, repo Repository, number int, threadID, threadType string) *chat.Message {
	return a.message(c, RawMessage{Type: rawIssueComment, Comment: c, Repository: repoRef(repo), Number: number, ThreadType: threadType}, threadID)
}

// parseReviewComment is upstream parseReviewComment.
func (a *Adapter) parseReviewComment(c Comment, repo Repository, number int, threadID string) *chat.Message {
	return a.message(c, RawMessage{Type: rawReviewComment, Comment: c, Repository: repoRef(repo), Number: number}, threadID)
}

// repoRef is upstream's `{ id: 0, name, full_name, owner }` raw repository.
func repoRef(r Repository) Repository {
	return Repository{Name: r.Name, FullName: r.Owner.Login + "/" + r.Name, Owner: r.Owner}
}

func (a *Adapter) message(c Comment, raw RawMessage, threadID string) *chat.Message {
	formatted := a.format.ToAst(c.Body)
	text := a.format.ExtractPlainText(c.Body)
	var editedAt *time.Time
	edited := !c.UpdatedAt.Equal(c.CreatedAt)
	if edited {
		t := c.UpdatedAt
		editedAt = &t
	}
	msg := chat.NewMessage(chat.MessageData{
		ID:          strconv.FormatInt(c.ID, 10),
		ThreadID:    threadID,
		Text:        text,
		Formatted:   formatted,
		Raw:         raw,
		Author:      a.parseAuthor(c.User),
		Metadata:    chat.MessageMetadata{DateSent: c.CreatedAt, Edited: edited, EditedAt: editedAt},
		Attachments: []chat.Attachment{},
		// IsMention is set here because chat-go has no Chat core.
		IsMention: boolPtr(detectMention(text, a.userName, a.BotUserID())),
	})
	chat.SetMessageAdapter(msg, a)
	return msg
}

// parseAuthor is upstream parseAuthor.
func (a *Adapter) parseAuthor(u User) chat.Author {
	id := strconv.FormatInt(u.ID, 10)
	return chat.Author{
		UserID:   id,
		UserName: u.Login,
		FullName: u.Login, // GitHub does not expose real names on comments
		IsBot:    boolPtr(u.Type == "Bot"),
		IsMe:     id == a.BotUserID() && id != "0",
	}
}
