package chat

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shoenig/test/must"
)

func boolPtr(v bool) *bool { return &v }

func defaultMessageData() MessageData {
	return MessageData{
		ID:        "msg-1",
		ThreadID:  "slack:C123:1234.5678",
		Text:      "Hello world",
		Formatted: ParseMarkdown("Hello world"),
		Raw:       map[string]any{"platform": "test"},
		Author: Author{
			UserID:   "U123",
			UserName: "testuser",
			FullName: "Test User",
			IsBot:    boolPtr(false),
			IsMe:     false,
		},
		Metadata: MessageMetadata{
			DateSent: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
			Edited:   false,
		},
		Attachments: []Attachment{},
	}
}

func makeMessage(overrides ...MessageData) *Message {
	data := defaultMessageData()
	if len(overrides) > 0 {
		o := overrides[0]
		if o.ID != "" {
			data.ID = o.ID
		}
		if o.ThreadID != "" {
			data.ThreadID = o.ThreadID
		}
		if o.Text != "" {
			data.Text = o.Text
		}
		if o.Formatted != nil {
			data.Formatted = o.Formatted
		}
		if o.Raw != nil {
			data.Raw = o.Raw
		}
		if o.Author.UserID != "" || o.Author.UserName != "" || o.Author.FullName != "" || o.Author.Email != "" || o.Author.IsSystem {
			data.Author = o.Author
		}
		if !o.Metadata.DateSent.IsZero() || o.Metadata.Edited || o.Metadata.EditedAt != nil {
			data.Metadata = o.Metadata
		}
		if o.Attachments != nil {
			data.Attachments = o.Attachments
		}
		if o.ReplyTo != nil {
			data.ReplyTo = o.ReplyTo
		}
		if o.IsMention != nil {
			data.IsMention = o.IsMention
		}
		if o.Links != nil {
			data.Links = o.Links
		}
	}
	return NewMessage(data)
}

func jsonRoundtrip(t *testing.T, msg *SerializedMessage) *SerializedMessage {
	t.Helper()
	b, err := json.Marshal(msg)
	must.NoError(t, err)
	var out SerializedMessage
	must.NoError(t, json.Unmarshal(b, &out))
	return &out
}

func attachmentJSONKeys(t *testing.T, att SerializedAttachment) map[string]any {
	t.Helper()
	b, err := json.Marshal(att)
	must.NoError(t, err)
	var m map[string]any
	must.NoError(t, json.Unmarshal(b, &m))
	return m
}

type stubSubjectFetcher struct {
	calls atomic.Int32
	ret   *MessageSubject
	err   error
}

func (s *stubSubjectFetcher) FetchSubject(_ context.Context, _ any) (*MessageSubject, error) {
	s.calls.Add(1)
	return s.ret, s.err
}

func TestMessage(t *testing.T) {
	t.Parallel()

	t.Run("constructor", func(t *testing.T) {
		t.Parallel()

		t.Run("should assign all properties", func(t *testing.T) {
			t.Parallel()
			msg := makeMessage()
			must.Eq(t, "msg-1", msg.ID)
			must.Eq(t, "slack:C123:1234.5678", msg.ThreadID)
			must.Eq(t, "Hello world", msg.Text)
			must.Eq(t, "testuser", msg.Author.UserName)
			must.Eq(t, time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC), msg.Metadata.DateSent.UTC())
			must.Eq(t, []Attachment{}, msg.Attachments)
			must.Nil(t, msg.IsMention)
		})

		t.Run("should assign isMention when provided", func(t *testing.T) {
			t.Parallel()
			msg := makeMessage(MessageData{IsMention: boolPtr(true)})
			must.True(t, *msg.IsMention)
		})
	})

	t.Run("toJSON()", func(t *testing.T) {
		t.Parallel()

		t.Run("should produce correct type tag", func(t *testing.T) {
			t.Parallel()
			j := makeMessage().ToJSON()
			must.Eq(t, "chat:Message", j.Type)
		})

		t.Run("should serialize dates as ISO strings", func(t *testing.T) {
			t.Parallel()
			editedAt := time.Date(2024, 6, 1, 13, 0, 0, 0, time.UTC)
			msg := makeMessage(MessageData{
				Metadata: MessageMetadata{
					DateSent: time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC),
					Edited:   true,
					EditedAt: &editedAt,
				},
			})
			j := msg.ToJSON()
			must.Eq(t, "2024-06-01T12:00:00.000Z", j.Metadata.DateSent)
			must.Eq(t, "2024-06-01T13:00:00.000Z", j.Metadata.EditedAt)
		})

		t.Run("should strip data and fetchData from attachments", func(t *testing.T) {
			t.Parallel()
			msg := makeMessage(MessageData{
				Attachments: []Attachment{{
					Type: AttachmentImage,
					URL:  "https://example.com/img.png",
					Name: "img.png",
					Data: []byte("binary"),
					FetchData: func() ([]byte, error) {
						return []byte("binary"), nil
					},
				}},
			})
			j := msg.ToJSON()
			must.Eq(t, SerializedAttachment{
				Type: AttachmentImage,
				URL:  "https://example.com/img.png",
				Name: "img.png",
			}, j.Attachments[0])
			keys := attachmentJSONKeys(t, j.Attachments[0])
			_, hasData := keys["data"]
			_, hasFetchData := keys["fetchData"]
			must.False(t, hasData)
			must.False(t, hasFetchData)
		})

		t.Run("should preserve fetchMetadata in attachments", func(t *testing.T) {
			t.Parallel()
			msg := makeMessage(MessageData{
				Attachments: []Attachment{{
					Type: AttachmentImage,
					URL:  "https://example.com/img.png",
					FetchMetadata: map[string]string{
						"mediaId": "123",
						"url":     "https://example.com/img.png",
					},
					FetchData: func() ([]byte, error) {
						return []byte("binary"), nil
					},
				}},
			})
			j := msg.ToJSON()
			must.Eq(t, map[string]string{
				"mediaId": "123",
				"url":     "https://example.com/img.png",
			}, j.Attachments[0].FetchMetadata)
			restored := MessageFromJSON(j)
			must.Eq(t, map[string]string{
				"mediaId": "123",
				"url":     "https://example.com/img.png",
			}, restored.Attachments[0].FetchMetadata)
		})

		t.Run("should preserve fetchMetadata through full JSON.stringify/parse roundtrip", func(t *testing.T) {
			t.Parallel()
			msg := makeMessage(MessageData{
				Attachments: []Attachment{{
					Type: AttachmentImage,
					URL:  "https://example.com/img.png",
					FetchMetadata: map[string]string{
						"mediaId": "123",
						"url":     "https://example.com/img.png",
					},
					FetchData: func() ([]byte, error) {
						return []byte("binary"), nil
					},
				}},
			})
			restored := MessageFromJSON(jsonRoundtrip(t, msg.ToJSON()))
			must.Eq(t, map[string]string{
				"mediaId": "123",
				"url":     "https://example.com/img.png",
			}, restored.Attachments[0].FetchMetadata)
			must.Nil(t, restored.Attachments[0].FetchData)
		})

		t.Run("should include isMention flag", func(t *testing.T) {
			t.Parallel()
			j := makeMessage(MessageData{IsMention: boolPtr(true)}).ToJSON()
			must.True(t, *j.IsMention)
		})

		t.Run("should preserve author.isSystem through a full JSON roundtrip", func(t *testing.T) {
			t.Parallel()
			msg := makeMessage(MessageData{
				Author: Author{
					UserID:   "USLACK",
					UserName: "Slack",
					FullName: "Slack",
					IsBot:    boolPtr(false),
					IsMe:     false,
					IsSystem: true,
				},
			})
			restored := MessageFromJSON(jsonRoundtrip(t, msg.ToJSON()))
			must.True(t, restored.Author.IsSystem)
		})

		t.Run("should leave author.isSystem absent for non-system authors", func(t *testing.T) {
			t.Parallel()
			j := makeMessage().ToJSON()
			must.Nil(t, j.Author.IsSystem)
		})

		t.Run("should preserve author email through serialization", func(t *testing.T) {
			t.Parallel()
			original := makeMessage(MessageData{
				Author: Author{
					UserID:   "U123",
					UserName: "testuser",
					FullName: "Test User",
					Email:    "test@example.com",
					IsBot:    boolPtr(false),
					IsMe:     false,
				},
			})
			j := original.ToJSON()
			must.Eq(t, "test@example.com", j.Author.Email)
			must.Eq(t, "test@example.com", MessageFromJSON(j).Author.Email)
		})
	})

	t.Run("fromJSON()", func(t *testing.T) {
		t.Parallel()

		t.Run("should convert ISO strings back to Dates", func(t *testing.T) {
			t.Parallel()
			j := &SerializedMessage{
				Type:     "chat:Message",
				ID:       "msg-2",
				ThreadID: "teams:ch:th",
				Text:     "hi",
				Formatted: map[string]any{
					"type":     "root",
					"children": []any{},
				},
				Raw: map[string]any{},
				Author: SerializedAuthor{
					UserID:   "U1",
					UserName: "u",
					FullName: "U",
					IsBot:    false,
					IsMe:     false,
				},
				Metadata: SerializedMessageMetadata{
					DateSent: "2024-03-01T00:00:00.000Z",
					Edited:   true,
					EditedAt: "2024-03-01T01:00:00.000Z",
				},
				Attachments: []SerializedAttachment{},
			}
			msg := MessageFromJSON(j)
			must.Eq(t, time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), msg.Metadata.DateSent.UTC())
			must.NotNil(t, msg.Metadata.EditedAt)
			must.Eq(t, time.Date(2024, 3, 1, 1, 0, 0, 0, time.UTC), msg.Metadata.EditedAt.UTC())
		})

		t.Run("should handle missing editedAt", func(t *testing.T) {
			t.Parallel()
			j := &SerializedMessage{
				Type:     "chat:Message",
				ID:       "msg-3",
				ThreadID: "t",
				Text:     "t",
				Formatted: map[string]any{
					"type":     "root",
					"children": []any{},
				},
				Raw: map[string]any{},
				Author: SerializedAuthor{
					UserID:   "U",
					UserName: "u",
					FullName: "U",
					IsBot:    false,
					IsMe:     false,
				},
				Metadata: SerializedMessageMetadata{
					DateSent: "2024-01-01T00:00:00.000Z",
					Edited:   false,
				},
				Attachments: []SerializedAttachment{},
			}
			msg := MessageFromJSON(j)
			must.Nil(t, msg.Metadata.EditedAt)
		})
	})

	t.Run("toJSON/fromJSON round-trip", func(t *testing.T) {
		t.Parallel()

		t.Run("should preserve all fields", func(t *testing.T) {
			t.Parallel()
			editedAt := time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC)
			original := makeMessage(MessageData{
				IsMention: boolPtr(true),
				Metadata: MessageMetadata{
					DateSent: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
					Edited:   true,
					EditedAt: &editedAt,
				},
				Attachments: []Attachment{{
					Type: AttachmentFile,
					URL:  "https://example.com/f.pdf",
					Name: "f.pdf",
				}},
			})

			restored := MessageFromJSON(original.ToJSON())
			must.Eq(t, original.ID, restored.ID)
			must.Eq(t, original.Text, restored.Text)
			must.Eq(t, original.IsMention, restored.IsMention)
			must.Eq(t, original.Metadata.DateSent.UnixMilli(), restored.Metadata.DateSent.UnixMilli())
		})
	})

	t.Run("WORKFLOW_SERIALIZE / WORKFLOW_DESERIALIZE", func(t *testing.T) {
		t.Parallel()

		t.Run("should serialize via static method", func(t *testing.T) {
			t.Parallel()
			msg := makeMessage()
			serialized := WorkflowSerialize(msg)
			must.Eq(t, "chat:Message", serialized.Type)
			must.Eq(t, "msg-1", serialized.ID)
		})

		t.Run("should deserialize via static method", func(t *testing.T) {
			t.Parallel()
			msg := makeMessage()
			serialized := WorkflowSerialize(msg)
			restored := WorkflowDeserialize(serialized)
			must.Eq(t, msg.ID, restored.ID)
			must.Eq(t, time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC), restored.Metadata.DateSent.UTC())
		})
	})

	t.Run("subject", func(t *testing.T) {
		t.Parallel()

		t.Run("should return null when no adapter is set", func(t *testing.T) {
			t.Parallel()
			msg := makeMessage()
			must.Nil(t, msg.Subject(t.Context()))
		})

		t.Run("should return null when adapter has no fetchSubject", func(t *testing.T) {
			t.Parallel()
			msg := makeMessage()
			SetMessageAdapter(msg, struct{}{})
			must.Nil(t, msg.Subject(t.Context()))
		})

		t.Run("should return subject from adapter", func(t *testing.T) {
			t.Parallel()
			msg := makeMessage()
			expected := &MessageSubject{
				Type:   "issue",
				ID:     "ENG-123",
				Title:  "Fix bug",
				Status: "In Progress",
				URL:    "https://linear.app/team/ENG-123",
				Raw:    map[string]any{},
			}
			SetMessageAdapter(msg, &stubSubjectFetcher{ret: expected})

			result := msg.Subject(t.Context())
			must.Eq(t, expected, result)
		})

		t.Run("should cache the result", func(t *testing.T) {
			t.Parallel()
			msg := makeMessage()
			fetcher := &stubSubjectFetcher{ret: &MessageSubject{
				Type: "issue",
				ID:   "1",
				Raw:  map[string]any{},
			}}
			SetMessageAdapter(msg, fetcher)

			_ = msg.Subject(t.Context())
			_ = msg.Subject(t.Context())
			must.Eq(t, int32(1), fetcher.calls.Load())
		})

		t.Run("should cache null result", func(t *testing.T) {
			t.Parallel()
			msg := makeMessage()
			fetcher := &stubSubjectFetcher{}
			SetMessageAdapter(msg, fetcher)

			_ = msg.Subject(t.Context())
			_ = msg.Subject(t.Context())
			must.Eq(t, int32(1), fetcher.calls.Load())
		})

		t.Run("should handle concurrent access", func(t *testing.T) {
			t.Parallel()
			msg := makeMessage()
			fetcher := &stubSubjectFetcher{ret: &MessageSubject{
				Type: "issue",
				ID:   "1",
				Raw:  map[string]any{},
			}}
			SetMessageAdapter(msg, fetcher)

			ctx := t.Context()
			var a, b *MessageSubject
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				a = msg.Subject(ctx)
			}()
			go func() {
				defer wg.Done()
				b = msg.Subject(ctx)
			}()
			wg.Wait()
			must.Eq(t, a, b)
			must.Eq(t, int32(1), fetcher.calls.Load())
		})
	})
}
