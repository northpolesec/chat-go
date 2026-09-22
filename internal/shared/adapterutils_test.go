package shared

import (
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/shoenig/test/must"
)

func TestExtractCard(t *testing.T) {
	t.Parallel()

	t.Run("with CardElement", func(t *testing.T) {
		t.Parallel()

		t.Run("extracts a CardElement passed directly", func(t *testing.T) {
			t.Parallel()
			card := &chat.Card{
				Type:     "card",
				Title:    "Test Card",
				Children: []any{chat.CardText("Content")},
			}
			result := ExtractCard(card)
			must.True(t, result == card)
		})

		t.Run("extracts a card with all properties", func(t *testing.T) {
			t.Parallel()
			card := chat.Card{
				Type:     "card",
				Title:    "Order #123",
				Subtitle: "Processing",
				ImageURL: "https://example.com/img.png",
				Children: []any{chat.CardText("Details")},
			}
			result := ExtractCard(card)
			must.Eq(t, card, *result)
			must.Eq(t, "Order #123", result.Title)
			must.Eq(t, "Processing", result.Subtitle)
		})
	})

	t.Run("with PostableCard object", func(t *testing.T) {
		t.Parallel()

		t.Run("extracts card from { card: CardElement }", func(t *testing.T) {
			t.Parallel()
			card := chat.Card{Type: "card", Title: "Nested Card"}
			message := chat.PostableCard{Card: card}
			result := ExtractCard(message)
			must.Eq(t, card, *result)
		})

		t.Run("extracts card from PostableCard with fallbackText", func(t *testing.T) {
			t.Parallel()
			card := chat.Card{Type: "card", Title: "With Fallback"}
			message := chat.PostableCard{Card: card, FallbackText: "Plain text version"}
			result := ExtractCard(message)
			must.Eq(t, card, *result)
		})

		t.Run("extracts card from PostableCard with files", func(t *testing.T) {
			t.Parallel()
			card := chat.Card{Type: "card", Title: "With Files"}
			files := []chat.FileUpload{{Data: []byte("test"), Filename: "test.txt"}}
			message := chat.PostableCard{Card: card, Files: files}
			result := ExtractCard(message)
			must.Eq(t, card, *result)
		})
	})

	t.Run("with non-card messages", func(t *testing.T) {
		t.Parallel()

		t.Run("returns null for plain string", func(t *testing.T) {
			t.Parallel()
			must.Nil(t, ExtractCard("Hello world"))
		})

		t.Run("returns null for PostableRaw", func(t *testing.T) {
			t.Parallel()
			must.Nil(t, ExtractCard(chat.PostableRaw{Raw: "Raw text"}))
		})

		t.Run("returns null for PostableMarkdown", func(t *testing.T) {
			t.Parallel()
			must.Nil(t, ExtractCard(chat.PostableMarkdown{Markdown: "**Bold** text"}))
		})

		t.Run("returns null for PostableAst", func(t *testing.T) {
			t.Parallel()
			must.Nil(t, ExtractCard(chat.PostableAst{AST: map[string]any{"type": "root", "children": []any{}}}))
		})

		t.Run("returns null for null input", func(t *testing.T) {
			t.Parallel()
			must.Nil(t, ExtractCard(nil))
		})

		t.Run("returns null for undefined input", func(t *testing.T) {
			t.Parallel()
			var message any
			must.Nil(t, ExtractCard(message))
		})

		t.Run("returns null for object without card or type", func(t *testing.T) {
			t.Parallel()
			must.Nil(t, ExtractCard(struct{ Something string }{Something: "else"}))
		})

		t.Run("returns null for non-card type object", func(t *testing.T) {
			t.Parallel()
			must.Nil(t, ExtractCard(struct {
				Type    string
				Content string
			}{Type: "text", Content: "not a card"}))
		})
	})
}

func TestExtractFiles(t *testing.T) {
	t.Parallel()

	t.Run("with files present", func(t *testing.T) {
		t.Parallel()

		t.Run("extracts files array from PostableRaw", func(t *testing.T) {
			t.Parallel()
			files := []chat.FileUpload{
				{Data: []byte("content1"), Filename: "file1.txt"},
				{Data: []byte("content2"), Filename: "file2.txt"},
			}
			message := chat.PostableRaw{Raw: "Text", Files: files}
			result := ExtractFiles(message)
			must.Eq(t, files, result)
			must.Eq(t, 2, len(result))
		})

		t.Run("extracts files array from PostableMarkdown", func(t *testing.T) {
			t.Parallel()
			files := []chat.FileUpload{
				{Data: []byte("image"), Filename: "image.png", MIMEType: "image/png"},
			}
			message := chat.PostableMarkdown{Markdown: "**Text**", Files: files}
			result := ExtractFiles(message)
			must.Eq(t, files, result)
			must.Eq(t, "image/png", result[0].MIMEType)
		})

		t.Run("extracts files array from PostableCard", func(t *testing.T) {
			t.Parallel()
			card := chat.Card{Type: "card", Title: "Test"}
			files := []chat.FileUpload{{Data: []byte("doc"), Filename: "doc.pdf"}}
			message := chat.PostableCard{Card: card, Files: files}
			result := ExtractFiles(message)
			must.Eq(t, files, result)
		})

		t.Run("handles Blob data in files", func(t *testing.T) {
			t.Parallel()
			blob := []byte("content")
			files := []chat.FileUpload{{Data: blob, Filename: "blob.txt"}}
			message := chat.PostableRaw{Raw: "Text", Files: files}
			result := ExtractFiles(message)
			must.Eq(t, 1, len(result))
			must.Eq(t, blob, result[0].Data)
		})

		t.Run("handles ArrayBuffer data in files", func(t *testing.T) {
			t.Parallel()
			buffer := make([]byte, 8)
			files := []chat.FileUpload{{Data: buffer, Filename: "binary.bin"}}
			message := chat.PostableRaw{Raw: "Text", Files: files}
			result := ExtractFiles(message)
			must.Eq(t, 1, len(result))
			must.Eq(t, buffer, result[0].Data)
		})
	})

	t.Run("with empty or missing files", func(t *testing.T) {
		t.Parallel()

		t.Run("returns empty array when files property is empty array", func(t *testing.T) {
			t.Parallel()
			message := chat.PostableRaw{Raw: "Text", Files: []chat.FileUpload{}}
			result := ExtractFiles(message)
			must.Eq(t, []chat.FileUpload{}, result)
		})

		t.Run("returns empty array when files property is undefined", func(t *testing.T) {
			t.Parallel()
			message := chat.PostableRaw{Raw: "Text"}
			result := ExtractFiles(message)
			must.Eq(t, []chat.FileUpload{}, result)
		})

		t.Run("returns empty array for PostableRaw without files", func(t *testing.T) {
			t.Parallel()
			result := ExtractFiles(chat.PostableRaw{Raw: "Just text"})
			must.Eq(t, []chat.FileUpload{}, result)
		})

		t.Run("returns empty array for PostableMarkdown without files", func(t *testing.T) {
			t.Parallel()
			result := ExtractFiles(chat.PostableMarkdown{Markdown: "**Bold**"})
			must.Eq(t, []chat.FileUpload{}, result)
		})
	})

	t.Run("with non-object messages", func(t *testing.T) {
		t.Parallel()

		t.Run("returns empty array for plain string", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, []chat.FileUpload{}, ExtractFiles("Hello world"))
		})

		t.Run("returns empty array for CardElement (no files property)", func(t *testing.T) {
			t.Parallel()
			card := chat.Card{Type: "card", Title: "Test"}
			must.Eq(t, []chat.FileUpload{}, ExtractFiles(card))
		})

		t.Run("returns empty array for null input", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, []chat.FileUpload{}, ExtractFiles(nil))
		})

		t.Run("returns empty array for undefined input", func(t *testing.T) {
			t.Parallel()
			var message any
			must.Eq(t, []chat.FileUpload{}, ExtractFiles(message))
		})
	})
}

func TestExtractPostableAttachments(t *testing.T) {
	t.Parallel()

	t.Run("with attachments present", func(t *testing.T) {
		t.Parallel()

		t.Run("extracts attachments array from PostableRaw", func(t *testing.T) {
			t.Parallel()
			attachments := []chat.Attachment{
				{Data: []byte("content1"), Name: "file1.txt", Type: chat.AttachmentFile},
				{Data: []byte("content2"), Name: "file2.txt", Type: chat.AttachmentFile},
			}
			message := chat.PostableRaw{Raw: "Text", Attachments: attachments}
			result := ExtractPostableAttachments(message)
			must.Eq(t, attachments, result)
			must.Eq(t, 2, len(result))
		})

		t.Run("extracts attachments array from PostableMarkdown", func(t *testing.T) {
			t.Parallel()
			attachments := []chat.Attachment{
				{Data: []byte("image"), Name: "image.png", MIMEType: "image/png", Type: chat.AttachmentImage},
			}
			message := chat.PostableMarkdown{Markdown: "**Text**", Attachments: attachments}
			result := ExtractPostableAttachments(message)
			must.Eq(t, attachments, result)
			must.Eq(t, "image/png", result[0].MIMEType)
		})

		t.Run("extracts attachments array from PostableAst", func(t *testing.T) {
			t.Parallel()
			attachments := []chat.Attachment{
				{Data: []byte("doc"), Name: "doc.pdf", Type: chat.AttachmentFile},
			}
			message := chat.PostableAst{
				AST:         map[string]any{"type": "root", "children": []any{}},
				Attachments: attachments,
			}
			result := ExtractPostableAttachments(message)
			must.Eq(t, attachments, result)
		})
	})

	t.Run("with empty or missing attachments", func(t *testing.T) {
		t.Parallel()

		t.Run("returns empty array when attachments property is empty array", func(t *testing.T) {
			t.Parallel()
			message := chat.PostableRaw{Raw: "Text", Attachments: []chat.Attachment{}}
			must.Eq(t, []chat.Attachment{}, ExtractPostableAttachments(message))
		})

		t.Run("returns empty array when attachments property is undefined", func(t *testing.T) {
			t.Parallel()
			message := chat.PostableRaw{Raw: "Text"}
			must.Eq(t, []chat.Attachment{}, ExtractPostableAttachments(message))
		})

		t.Run("returns empty array for PostableRaw without attachments", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, []chat.Attachment{}, ExtractPostableAttachments(chat.PostableRaw{Raw: "Just text"}))
		})

		t.Run("returns empty array for PostableMarkdown without attachments", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, []chat.Attachment{}, ExtractPostableAttachments(chat.PostableMarkdown{Markdown: "**Bold**"}))
		})
	})

	t.Run("with non-object messages", func(t *testing.T) {
		t.Parallel()

		t.Run("returns empty array for plain string", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, []chat.Attachment{}, ExtractPostableAttachments("Hello world"))
		})

		t.Run("returns empty array for CardElement", func(t *testing.T) {
			t.Parallel()
			card := chat.Card{Type: "card", Title: "Test"}
			must.Eq(t, []chat.Attachment{}, ExtractPostableAttachments(card))
		})

		t.Run("returns empty array for null input", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, []chat.Attachment{}, ExtractPostableAttachments(nil))
		})

		t.Run("returns empty array for undefined input", func(t *testing.T) {
			t.Parallel()
			var message any
			must.Eq(t, []chat.Attachment{}, ExtractPostableAttachments(message))
		})
	})
}
