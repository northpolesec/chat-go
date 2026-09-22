// Ported from packages/adapter-shared/src/adapter-utils.ts @ 6adca36 (chat v4.40.0).
// Divergences: message is any (TS AdapterPostableMessage plus the null/string
// cases the tests pass); Card / PostableCard are chat types; missing files
// and attachments return empty slices, not nil; FileUpload.Data is []byte
// (no Blob / ArrayBuffer).
package shared

import "github.com/northpolesec/chat-go/chat"

// ExtractCard returns the card on a CardElement or PostableCard, or nil.
func ExtractCard(message any) *chat.Card {
	if chat.IsCardElement(message) {
		switch m := message.(type) {
		case chat.Card:
			return &m
		case *chat.Card:
			return m
		}
	}
	switch m := message.(type) {
	case chat.PostableCard:
		return &m.Card
	case *chat.PostableCard:
		if m == nil {
			return nil
		}
		return &m.Card
	}
	return nil
}

// ExtractFiles returns the files attached to a postable message.
func ExtractFiles(message any) []chat.FileUpload {
	switch m := message.(type) {
	case chat.PostableRaw:
		return orEmptyFiles(m.Files)
	case *chat.PostableRaw:
		if m == nil {
			return []chat.FileUpload{}
		}
		return orEmptyFiles(m.Files)
	case chat.PostableMarkdown:
		return orEmptyFiles(m.Files)
	case *chat.PostableMarkdown:
		if m == nil {
			return []chat.FileUpload{}
		}
		return orEmptyFiles(m.Files)
	case chat.PostableAst:
		return orEmptyFiles(m.Files)
	case *chat.PostableAst:
		if m == nil {
			return []chat.FileUpload{}
		}
		return orEmptyFiles(m.Files)
	case chat.PostableCard:
		return orEmptyFiles(m.Files)
	case *chat.PostableCard:
		if m == nil {
			return []chat.FileUpload{}
		}
		return orEmptyFiles(m.Files)
	}
	return []chat.FileUpload{}
}

// ExtractPostableAttachments returns the attachments on a postable message.
func ExtractPostableAttachments(message any) []chat.Attachment {
	switch m := message.(type) {
	case chat.PostableRaw:
		return orEmptyAttachments(m.Attachments)
	case *chat.PostableRaw:
		if m == nil {
			return []chat.Attachment{}
		}
		return orEmptyAttachments(m.Attachments)
	case chat.PostableMarkdown:
		return orEmptyAttachments(m.Attachments)
	case *chat.PostableMarkdown:
		if m == nil {
			return []chat.Attachment{}
		}
		return orEmptyAttachments(m.Attachments)
	case chat.PostableAst:
		return orEmptyAttachments(m.Attachments)
	case *chat.PostableAst:
		if m == nil {
			return []chat.Attachment{}
		}
		return orEmptyAttachments(m.Attachments)
	}
	return []chat.Attachment{}
}

func orEmptyFiles(files []chat.FileUpload) []chat.FileUpload {
	if files == nil {
		return []chat.FileUpload{}
	}
	return files
}

func orEmptyAttachments(attachments []chat.Attachment) []chat.Attachment {
	if attachments == nil {
		return []chat.Attachment{}
	}
	return attachments
}
