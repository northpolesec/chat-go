// Ported from packages/adapter-slack/src/index.test.ts @ 6adca36 (chat v4.40.0)
// (partial: Task 31 — Attachment.fetchData token resolution, reverse user
// lookup / rehydrateAttachment, installationProvider rehydrateAttachment).
package slack

import (
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/northpolesec/chat-go/slack/api"
	"github.com/shoenig/test/must"
)

var fileEvent = SlackEvent{
	Type: "message", User: "U123", Channel: "C456",
	Text: "with file", Ts: "1234567890.123456",
	Files: []SlackFile{{
		ID: "F123", Mimetype: "application/pdf",
		URLPrivate: "https://files.slack.com/file.pdf",
		Name:       "doc.pdf", Size: 100,
	}},
}

type recordedFileFetch struct {
	mu      sync.Mutex
	url     string
	headers map[string]string
}

func (r *recordedFileFetch) transport(_ context.Context, u *url.URL, headers map[string]string) (*http.Response, error) {
	r.mu.Lock()
	r.url = u.String()
	r.headers = maps.Clone(headers)
	r.mu.Unlock()
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/octet-stream"}},
		Body:       io.NopCloser(strings.NewReader("file-bytes")),
	}, nil
}

func (r *recordedFileFetch) auth() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.headers["authorization"]
}

type rotatingToken struct {
	mu     sync.Mutex
	n      int
	tokens []string
}

func (r *rotatingToken) Token(context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tok := r.tokens[r.n]
	r.n++
	return tok, nil
}

func (r *rotatingToken) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

func TestAttachmentFetchDataTokenResolution(t *testing.T) {
	t.Parallel()

	t.Run("snapshots ctx token at attachment creation in multi-workspace mode", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{Token: nil, ClientID: "client-id"})
		rec := &recordedFileFetch{}
		adapter.fileTransport = rec.transport
		var att chat.Attachment
		adapter.WithBotToken(t.Context(), "xoxb-team-snapshot", func(ctx context.Context) {
			msg := adapter.parseSlackMessageSync(ctx, fileEvent, "slack:C456:1234567890.123456")
			must.True(t, len(msg.Attachments) > 0)
			att = msg.Attachments[0]
		}, nil)
		must.True(t, att.FetchData != nil)
		_, err := att.FetchData()
		must.NoError(t, err)
		must.Eq(t, "https://files.slack.com/file.pdf", rec.url)
		must.Eq(t, "Bearer xoxb-team-snapshot", rec.auth())
	})

	t.Run("re-resolves the default provider at fetch time in single-workspace mode", func(t *testing.T) {
		t.Parallel()
		tok := &rotatingToken{tokens: []string{"xoxb-stale", "xoxb-fresh"}}
		adapter := mustNew(t, Config{Token: tok})
		rec := &recordedFileFetch{}
		adapter.fileTransport = rec.transport
		msg, err := adapter.ParseMessage(fileEvent)
		must.NoError(t, err)
		must.True(t, msg.Attachments[0].FetchData != nil)
		must.Eq(t, 0, tok.calls())

		_, err = msg.Attachments[0].FetchData()
		must.NoError(t, err)
		must.Eq(t, "Bearer xoxb-stale", rec.auth())

		_, err = msg.Attachments[0].FetchData()
		must.NoError(t, err)
		must.Eq(t, "Bearer xoxb-fresh", rec.auth())
		must.Eq(t, 2, tok.calls())
	})
}

func TestRehydrateAttachment(t *testing.T) {
	t.Parallel()

	t.Run("should resolve token from installation when teamId is present", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{Token: nil, ClientID: "client-id", ClientSecret: "client-secret"})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		must.NoError(t, adapter.SetInstallation(t.Context(), "T_MULTI_1", Installation{
			BotToken:  "xoxb-multi-workspace-token",
			BotUserID: "U_BOT_MULTI",
		}))
		rehydrated := adapter.RehydrateAttachment(chat.Attachment{
			Type: chat.AttachmentImage,
			URL:  "https://files.slack.com/img.png",
			FetchMetadata: map[string]string{
				"url":    "https://files.slack.com/img.png",
				"teamId": "T_MULTI_1",
			},
		})
		must.True(t, rehydrated.FetchData != nil)
	})

	t.Run("should fall back to getToken when no teamId in fetchMetadata", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{Token: api.StaticToken("xoxb-single")})
		rehydrated := adapter.RehydrateAttachment(chat.Attachment{
			Type:          chat.AttachmentImage,
			URL:           "https://files.slack.com/img.png",
			FetchMetadata: map[string]string{"url": "https://files.slack.com/img.png"},
		})
		must.True(t, rehydrated.FetchData != nil)
	})

	t.Run("should return attachment unchanged when no url", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{})
		attachment := chat.Attachment{Type: chat.AttachmentFile, Name: "test.bin"}
		rehydrated := adapter.RehydrateAttachment(attachment)
		must.True(t, rehydrated.FetchData == nil)
		must.Eq(t, attachment, rehydrated)
	})
}

func TestRehydrateAttachmentInstallationProvider(t *testing.T) {
	t.Parallel()

	t.Run("rehydrateAttachment uses installationProvider for token resolution", func(t *testing.T) {
		t.Parallel()
		provider := &mockInstallProvider{inst: &Installation{
			BotToken: "xoxb-rehydrate-token", BotUserID: "U_BOT_REHYDRATE",
		}}
		adapter := mustNew(t, Config{Token: nil, ClientID: "client-id", InstallationProvider: provider})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		rec := &recordedFileFetch{}
		adapter.fileTransport = rec.transport
		rehydrated := adapter.RehydrateAttachment(chat.Attachment{
			Type: chat.AttachmentImage,
			URL:  "https://files.slack.com/img.png",
			FetchMetadata: map[string]string{
				"url":    "https://files.slack.com/img.png",
				"teamId": "T_REHYDRATE",
			},
		})
		must.True(t, rehydrated.FetchData != nil)
		_, err := rehydrated.FetchData()
		must.NoError(t, err)
		id, ent := provider.last()
		must.Eq(t, "T_REHYDRATE", id)
		must.False(t, ent)
		must.Eq(t, "Bearer xoxb-rehydrate-token", rec.auth())
	})

	t.Run("rehydrateAttachment does not send installation tokens off Slack", func(t *testing.T) {
		t.Parallel()
		provider := &mockInstallProvider{inst: &Installation{
			BotToken: "xoxb-rehydrate-token", BotUserID: "U_BOT_REHYDRATE",
		}}
		adapter := mustNew(t, Config{Token: nil, ClientID: "client-id", InstallationProvider: provider})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		rec := &recordedFileFetch{}
		adapter.fileTransport = rec.transport
		rehydrated := adapter.RehydrateAttachment(chat.Attachment{
			Type: chat.AttachmentFile,
			URL:  "https://attacker.example/file.txt",
			FetchMetadata: map[string]string{
				"url":    "https://attacker.example/file.txt",
				"teamId": "T_REHYDRATE",
			},
		})
		_, err := rehydrated.FetchData()
		must.NoError(t, err)
		must.Eq(t, 0, provider.n())
		must.Eq(t, "", rec.auth())
		must.Eq(t, "https://attacker.example/file.txt", rec.url)
	})

	t.Run("rehydrateAttachment uses enterprise_id when isEnterpriseInstall is true", func(t *testing.T) {
		t.Parallel()
		provider := &mockInstallProvider{inst: &Installation{
			BotToken: "xoxb-ent-rehydrate-token", BotUserID: "U_BOT_ENT_REHYDRATE",
		}}
		adapter := mustNew(t, Config{Token: nil, ClientID: "client-id", InstallationProvider: provider})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		rec := &recordedFileFetch{}
		adapter.fileTransport = rec.transport
		rehydrated := adapter.RehydrateAttachment(chat.Attachment{
			Type: chat.AttachmentImage,
			URL:  "https://files.slack.com/img.png",
			FetchMetadata: map[string]string{
				"url":                 "https://files.slack.com/img.png",
				"teamId":              "T_WORKSPACE",
				"enterpriseId":        "E_ORG",
				"isEnterpriseInstall": "true",
			},
		})
		_, err := rehydrated.FetchData()
		must.NoError(t, err)
		id, ent := provider.last()
		must.Eq(t, "E_ORG", id)
		must.True(t, ent)
		must.Eq(t, "Bearer xoxb-ent-rehydrate-token", rec.auth())
	})

	t.Run("rehydrateAttachment throws when installationProvider returns null", func(t *testing.T) {
		t.Parallel()
		provider := &mockInstallProvider{}
		adapter := mustNew(t, Config{Token: nil, ClientID: "client-id", InstallationProvider: provider})
		sc, _ := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		rehydrated := adapter.RehydrateAttachment(chat.Attachment{
			Type: chat.AttachmentImage,
			URL:  "https://files.slack.com/img.png",
			FetchMetadata: map[string]string{
				"url":    "https://files.slack.com/img.png",
				"teamId": "T_MISSING",
			},
		})
		_, err := rehydrated.FetchData()
		must.Error(t, err)
		must.StrContains(t, err.Error(), "Installation not found for team T_MISSING")
		var ae *shared.AuthenticationError
		must.True(t, errors.As(err, &ae))
		id, ent := provider.last()
		must.Eq(t, "T_MISSING", id)
		must.False(t, ent)
	})
}
