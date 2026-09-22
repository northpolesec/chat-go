// Ported from packages/chat/src/message.ts @ 6adca36 (chat v4.40.0).
// Divergences: dates are time.Time (metadata.dateSent / editedAt); serialized
// dates are ISO-8601 strings with milliseconds (JS Date.toISOString);
// raw / Formatted / MessageSubject.Raw are any (TS unknown / mdast Root);
// WeakMap adapter → unexported field; subject Promise → Subject(ctx)
// (*MessageSubject, errors become nil); fetchSubject → SubjectFetcher;
// WORKFLOW_SERIALIZE/DESERIALIZE → WorkflowSerialize/WorkflowDeserialize
// (no @workflow/serde); goldmark ast.Node is not mdast JSON (ToJSON keeps the
// node; encoding/json emits {}); IsMention is *bool (nil = absent);
// Author.IsSystem omitted from JSON when false; UserKey is not serialized
// (absent from SerializedMessage, same as upstream).
package chat

import (
	"context"
	"sync"
	"time"
)

const (
	messageTypeTag = "chat:Message"
	isoMillis      = "2006-01-02T15:04:05.000Z"
)

// MessageData is the constructor input (upstream MessageData).
type MessageData struct {
	Attachments []Attachment
	Author      Author
	Formatted   FormattedContent
	ID          string
	IsMention   *bool
	Links       []LinkPreview
	Metadata    MessageMetadata
	Raw         any
	ReplyTo     *Message
	Text        string
	ThreadID    string
}

// SerializedMessage is the JSON form (upstream SerializedMessage).
type SerializedMessage struct {
	Type        string                    `json:"_type"`
	Attachments []SerializedAttachment    `json:"attachments"`
	Author      SerializedAuthor          `json:"author"`
	Formatted   any                       `json:"formatted"`
	ID          string                    `json:"id"`
	IsMention   *bool                     `json:"isMention,omitempty"`
	Links       []SerializedLinkPreview   `json:"links,omitempty"`
	Metadata    SerializedMessageMetadata `json:"metadata"`
	Raw         any                       `json:"raw"`
	ReplyTo     *SerializedMessage        `json:"replyTo,omitempty"`
	Text        string                    `json:"text"`
	ThreadID    string                    `json:"threadId"`
}

type SerializedAuthor struct {
	UserID   string `json:"userId"`
	UserName string `json:"userName"`
	FullName string `json:"fullName"`
	Email    string `json:"email,omitempty"`
	IsBot    any    `json:"isBot"`
	IsMe     bool   `json:"isMe"`
	IsSystem *bool  `json:"isSystem,omitempty"`
}

type SerializedMessageMetadata struct {
	DateSent string `json:"dateSent"`
	Edited   bool   `json:"edited"`
	EditedAt string `json:"editedAt,omitempty"`
}

type SerializedAttachment struct {
	Type          AttachmentType    `json:"type"`
	URL           string            `json:"url,omitempty"`
	Name          string            `json:"name,omitempty"`
	MIMEType      string            `json:"mimeType,omitempty"`
	Size          int               `json:"size,omitempty"`
	Width         int               `json:"width,omitempty"`
	Height        int               `json:"height,omitempty"`
	FetchMetadata map[string]string `json:"fetchMetadata,omitempty"`
}

type SerializedLinkPreview struct {
	URL         string `json:"url"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	ImageURL    string `json:"imageUrl,omitempty"`
	SiteName    string `json:"siteName,omitempty"`
}

// Message is a chat message with JSON serialization.
type Message struct {
	Attachments []Attachment
	Author      Author
	Formatted   FormattedContent
	ID          string
	IsMention   *bool
	Links       []LinkPreview
	Metadata    MessageMetadata
	Raw         any
	ReplyTo     *Message
	Text        string
	ThreadID    string
	UserKey     string

	adapter     any
	subject     *MessageSubject
	subjectOnce sync.Once
}

func (*Message) isPostable() {}

// NewMessage constructs a Message (upstream constructor).
func NewMessage(data MessageData) *Message {
	links := data.Links
	if links == nil {
		links = []LinkPreview{}
	}
	return &Message{
		Attachments: data.Attachments,
		Author:      data.Author,
		Formatted:   data.Formatted,
		ID:          data.ID,
		IsMention:   data.IsMention,
		Links:       links,
		Metadata:    data.Metadata,
		Raw:         data.Raw,
		ReplyTo:     data.ReplyTo,
		Text:        data.Text,
		ThreadID:    data.ThreadID,
	}
}

// SetMessageAdapter attaches an adapter for Subject (and recursively ReplyTo).
func SetMessageAdapter(message *Message, adapter any) {
	message.adapter = adapter
	if message.ReplyTo != nil {
		SetMessageAdapter(message.ReplyTo, adapter)
	}
}

// Subject returns the adapter-fetched subject, caching the first result
// (including nil). Errors and a missing SubjectFetcher yield nil.
func (m *Message) Subject(ctx context.Context) *MessageSubject {
	m.subjectOnce.Do(func() {
		sf, ok := m.adapter.(SubjectFetcher)
		if !ok {
			return
		}
		subj, err := sf.FetchSubject(ctx, m.Raw)
		if err != nil {
			return
		}
		m.subject = subj
	})
	return m.subject
}

// ToJSON serializes the message (upstream toJSON).
func (m *Message) ToJSON() *SerializedMessage {
	var replyTo *SerializedMessage
	if m.ReplyTo != nil {
		replyTo = m.ReplyTo.ToJSON()
	}
	attachments := make([]SerializedAttachment, len(m.Attachments))
	for i, att := range m.Attachments {
		attachments[i] = SerializedAttachment{
			Type:          att.Type,
			URL:           att.URL,
			Name:          att.Name,
			MIMEType:      att.MIMEType,
			Size:          att.Size,
			Width:         att.Width,
			Height:        att.Height,
			FetchMetadata: att.FetchMetadata,
		}
	}
	var links []SerializedLinkPreview
	if len(m.Links) > 0 {
		links = make([]SerializedLinkPreview, len(m.Links))
		for i, link := range m.Links {
			links[i] = SerializedLinkPreview{
				URL:         link.URL,
				Title:       link.Title,
				Description: link.Description,
				ImageURL:    link.ImageURL,
				SiteName:    link.SiteName,
			}
		}
	}
	var editedAt string
	if m.Metadata.EditedAt != nil {
		editedAt = formatISO(*m.Metadata.EditedAt)
	}
	var isSystem *bool
	if m.Author.IsSystem {
		v := true
		isSystem = &v
	}
	return &SerializedMessage{
		Type:      messageTypeTag,
		ID:        m.ID,
		ThreadID:  m.ThreadID,
		Text:      m.Text,
		Formatted: m.Formatted,
		Raw:       m.Raw,
		Author: SerializedAuthor{
			UserID:   m.Author.UserID,
			UserName: m.Author.UserName,
			FullName: m.Author.FullName,
			Email:    m.Author.Email,
			IsBot:    serializeIsBot(m.Author.IsBot),
			IsMe:     m.Author.IsMe,
			IsSystem: isSystem,
		},
		Metadata: SerializedMessageMetadata{
			DateSent: formatISO(m.Metadata.DateSent),
			Edited:   m.Metadata.Edited,
			EditedAt: editedAt,
		},
		Attachments: attachments,
		ReplyTo:     replyTo,
		IsMention:   m.IsMention,
		Links:       links,
	}
}

// MessageFromJSON reconstructs a Message (upstream fromJSON).
func MessageFromJSON(j *SerializedMessage) *Message {
	if j == nil {
		return nil
	}
	var editedAt *time.Time
	if j.Metadata.EditedAt != "" {
		t := parseISO(j.Metadata.EditedAt)
		editedAt = &t
	}
	var replyTo *Message
	if j.ReplyTo != nil {
		replyTo = MessageFromJSON(j.ReplyTo)
	}
	attachments := make([]Attachment, len(j.Attachments))
	for i, att := range j.Attachments {
		attachments[i] = Attachment{
			Type:          att.Type,
			URL:           att.URL,
			Name:          att.Name,
			MIMEType:      att.MIMEType,
			Size:          att.Size,
			Width:         att.Width,
			Height:        att.Height,
			FetchMetadata: att.FetchMetadata,
		}
	}
	var links []LinkPreview
	if j.Links != nil {
		links = make([]LinkPreview, len(j.Links))
		for i, link := range j.Links {
			links[i] = LinkPreview{
				URL:         link.URL,
				Title:       link.Title,
				Description: link.Description,
				ImageURL:    link.ImageURL,
				SiteName:    link.SiteName,
			}
		}
	}
	return NewMessage(MessageData{
		ID:        j.ID,
		ThreadID:  j.ThreadID,
		Text:      j.Text,
		Formatted: j.Formatted,
		Raw:       j.Raw,
		Author: Author{
			UserID:   j.Author.UserID,
			UserName: j.Author.UserName,
			FullName: j.Author.FullName,
			Email:    j.Author.Email,
			IsBot:    isBotFromSerialized(j.Author.IsBot),
			IsMe:     j.Author.IsMe,
			IsSystem: j.Author.IsSystem != nil && *j.Author.IsSystem,
		},
		Metadata: MessageMetadata{
			DateSent: parseISO(j.Metadata.DateSent),
			Edited:   j.Metadata.Edited,
			EditedAt: editedAt,
		},
		Attachments: attachments,
		ReplyTo:     replyTo,
		IsMention:   j.IsMention,
		Links:       links,
	})
}

// WorkflowSerialize is upstream Message[WORKFLOW_SERIALIZE].
func WorkflowSerialize(instance *Message) *SerializedMessage {
	return instance.ToJSON()
}

// WorkflowDeserialize is upstream Message[WORKFLOW_DESERIALIZE].
func WorkflowDeserialize(data *SerializedMessage) *Message {
	return MessageFromJSON(data)
}

func formatISO(t time.Time) string {
	return t.UTC().Format(isoMillis)
}

func parseISO(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func serializeIsBot(b *bool) any {
	if b == nil {
		return "unknown"
	}
	return *b
}

func isBotFromSerialized(v any) *bool {
	switch x := v.(type) {
	case bool:
		return &x
	case string:
		if x == "unknown" {
			return nil
		}
	}
	return nil
}
