// Ported from packages/adapter-slack/src/index.test.ts @ 6adca36 (chat v4.40.0)
// (partial: Task 30 describe blocks — multi-workspace mode, installationProvider,
// encryption, installationKeyPrefix, handleOAuthCallback, withBotToken,
// adapter.client / webClient, botToken as function, installation-scoped
// caches, withToken enterprise context). rehydrateAttachment its inside
// installationProvider landed in Task 31. webClientOptions + sync-getter
// async resolver its are dropped (WebClient / webClientOptions cut).
package slack

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/northpolesec/chat-go/slack/api"
	"github.com/shoenig/test/must"
)

func newMulti(t *testing.T, extra Config) *SlackAdapter {
	t.Helper()
	if extra.SigningSecret == "" && extra.WebhookVerifier == nil {
		extra.SigningSecret = webhookSecret
	}
	a, err := New(extra)
	must.NoError(t, err)
	return a
}

func newMultiAPI(t *testing.T, extra Config) (*SlackAdapter, *slackAPIMock) {
	t.Helper()
	m := newSlackAPIMock(t)
	extra.APIURL = m.server.URL + "/"
	extra.HTTPClient = m.client()
	if extra.SigningSecret == "" && extra.WebhookVerifier == nil {
		extra.SigningSecret = webhookSecret
	}
	a, err := New(extra)
	must.NoError(t, err)
	return a, m
}

func initMulti(t *testing.T, a *SlackAdapter) *mockChat {
	t.Helper()
	mc := newMockChat(t)
	must.NoError(t, a.Initialize(t.Context(), mc))
	return mc
}

func usersInfoOK() map[string]any {
	return map[string]any{
		"user": map[string]any{
			"name":      "user",
			"real_name": "User",
			"profile":   map[string]any{"display_name": "User", "real_name": "User"},
		},
	}
}

type mockInstallProvider struct {
	mu    sync.Mutex
	calls []struct {
		id  string
		ent bool
	}
	inst *Installation
	err  error
}

func (m *mockInstallProvider) GetInstallation(_ context.Context, id string, ent bool) (*Installation, error) {
	m.mu.Lock()
	m.calls = append(m.calls, struct {
		id  string
		ent bool
	}{id, ent})
	m.mu.Unlock()
	return m.inst, m.err
}

func (m *mockInstallProvider) last() (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	mustLast := m.calls[len(m.calls)-1]
	return mustLast.id, mustLast.ent
}

func (m *mockInstallProvider) n() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func oauthGET(rawURL string) *http.Request {
	return httptest.NewRequest(http.MethodGet, rawURL, nil)
}

func TestMultiWorkspaceMode(t *testing.T) {
	t.Parallel()

	t.Run("creates adapter without botToken", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{})
		must.Eq(t, "slack", adapter.Name())
	})

	t.Run("setInstallation throws before initialize", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{})
		err := adapter.SetInstallation(t.Context(), "T123", Installation{BotToken: "xoxb-token"})
		must.Error(t, err)
		must.StrContains(t, err.Error(), "Adapter not initialized")
	})

	t.Run("setInstallation / getInstallation round-trip", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{})
		_ = initMulti(t, adapter)
		installation := Installation{
			BotToken:  "xoxb-workspace-token",
			BotUserID: "U_BOT_123",
			TeamName:  "Test Team",
		}
		must.NoError(t, adapter.SetInstallation(t.Context(), "T_TEAM_1", installation))
		retrieved, err := adapter.GetInstallation(t.Context(), "T_TEAM_1")
		must.NoError(t, err)
		must.Eq(t, &installation, retrieved)
	})

	t.Run("getInstallation returns null for unknown team", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{})
		_ = initMulti(t, adapter)
		result, err := adapter.GetInstallation(t.Context(), "T_UNKNOWN")
		must.NoError(t, err)
		must.Nil(t, result)
	})

	t.Run("deleteInstallation removes data", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{})
		_ = initMulti(t, adapter)
		must.NoError(t, adapter.SetInstallation(t.Context(), "T_TEAM_2", Installation{BotToken: "xoxb-token"}))
		got, err := adapter.GetInstallation(t.Context(), "T_TEAM_2")
		must.NoError(t, err)
		must.True(t, got != nil)
		must.NoError(t, adapter.DeleteInstallation(t.Context(), "T_TEAM_2"))
		got, err = adapter.GetInstallation(t.Context(), "T_TEAM_2")
		must.NoError(t, err)
		must.Nil(t, got)
	})

	t.Run("handleWebhook resolves token from state for event_callback", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock := newMultiAPI(t, Config{})
		apiMock.ok("users.info", usersInfoOK())
		mc := initMulti(t, adapter)
		must.NoError(t, adapter.SetInstallation(t.Context(), "T_MULTI_1", Installation{
			BotToken: "xoxb-multi-token-1", BotUserID: "U_BOT_M1",
		}))
		body, err := json.Marshal(map[string]any{
			"type": "event_callback", "team_id": "T_MULTI_1",
			"event": map[string]any{
				"type": "message", "user": "U123", "channel": "C456",
				"text": "Hello multi", "ts": "1234567890.123456",
			},
		})
		must.NoError(t, err)
		resp := postJSON(t, adapter, webhookSecret, string(body))
		must.Eq(t, 200, resp.status)
		must.Eq(t, 1, len(mc.messages))
	})

	t.Run("handleWebhook resolves token for interactive payloads", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{})
		mc := initMulti(t, adapter)
		must.NoError(t, adapter.SetInstallation(t.Context(), "T_INTER_1", Installation{BotToken: "xoxb-inter-token"}))
		resp := postForm(t, adapter, webhookSecret, formPayload(t, map[string]any{
			"type": "block_actions",
			"team": map[string]any{"id": "T_INTER_1"},
			"user": map[string]any{"id": "U123", "username": "testuser", "name": "Test User"},
			"container": map[string]any{
				"type": "message", "message_ts": "1234567890.123456", "channel_id": "C456",
			},
			"channel": map[string]any{"id": "C456", "name": "general"},
			"message": map[string]any{"ts": "1234567890.123456"},
			"actions": []any{map[string]any{"type": "button", "action_id": "test_action", "value": "v"}},
		}))
		must.Eq(t, 200, resp.status)
		must.Eq(t, 1, len(mc.actions))
	})

	t.Run("handleWebhook resolves token for block_suggestion payloads", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{})
		mc := initMulti(t, adapter)
		mc.optionsRet = []chat.SelectOptionElement{{Label: "Maria Garcia", Value: "person_123"}}
		must.NoError(t, adapter.SetInstallation(t.Context(), "T_INTER_2", Installation{BotToken: "xoxb-inter-token-2"}))
		resp := postForm(t, adapter, webhookSecret, formPayload(t, map[string]any{
			"type":      "block_suggestion",
			"team":      map[string]any{"id": "T_INTER_2"},
			"user":      map[string]any{"id": "U123", "username": "testuser", "name": "Test User"},
			"action_id": "person_select",
			"block_id":  "person_block",
			"value":     "mar",
		}))
		must.Eq(t, 200, resp.status)
		must.Eq(t, 1, len(mc.options))
		must.Eq(t, "person_select", mc.options[0].ActionID)
		must.Eq(t, "mar", mc.options[0].Query)
		var body map[string]any
		must.NoError(t, json.Unmarshal([]byte(resp.body), &body))
		must.Eq(t, map[string]any{
			"options": []any{
				map[string]any{
					"text":  map[string]any{"type": "plain_text", "text": "Maria Garcia"},
					"value": "person_123",
				},
			},
		}, body)
	})

	t.Run("URL verification works without token", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{})
		body, err := json.Marshal(map[string]any{
			"type": "url_verification", "challenge": "challenge-multi-123",
		})
		must.NoError(t, err)
		resp := postJSON(t, adapter, webhookSecret, string(body))
		must.Eq(t, 200, resp.status)
		var got map[string]any
		must.NoError(t, json.Unmarshal([]byte(resp.body), &got))
		must.Eq(t, map[string]any{"challenge": "challenge-multi-123"}, got)
	})
}

func TestInstallationProvider(t *testing.T) {
	t.Parallel()

	t.Run("uses installationProvider for token resolution in event_callback", func(t *testing.T) {
		t.Parallel()
		prov := &mockInstallProvider{inst: &Installation{BotToken: "xoxb-external-token", BotUserID: "U_BOT_EXT"}}
		adapter, apiMock := newMultiAPI(t, Config{InstallationProvider: prov})
		apiMock.ok("users.info", usersInfoOK())
		_ = initMulti(t, adapter)
		body, err := json.Marshal(map[string]any{
			"type": "event_callback", "team_id": "T_EXTERNAL_1",
			"event": map[string]any{
				"type": "message", "channel": "C_TEST", "user": "U_USER",
				"text": "hello", "ts": "1234567890.123456",
			},
		})
		must.NoError(t, err)
		resp := postJSON(t, adapter, webhookSecret, string(body))
		must.Eq(t, 200, resp.status)
		id, ent := prov.last()
		must.Eq(t, "T_EXTERNAL_1", id)
		must.False(t, ent)
	})

	t.Run("uses enterprise_id when is_enterprise_install is true", func(t *testing.T) {
		t.Parallel()
		prov := &mockInstallProvider{inst: &Installation{BotToken: "xoxb-enterprise-token", BotUserID: "U_BOT_ENT"}}
		adapter, apiMock := newMultiAPI(t, Config{InstallationProvider: prov})
		apiMock.ok("users.info", usersInfoOK())
		_ = initMulti(t, adapter)
		body, err := json.Marshal(map[string]any{
			"type": "event_callback", "team_id": "T_WORKSPACE_1",
			"enterprise_id": "E_ENTERPRISE_1", "is_enterprise_install": true,
			"event": map[string]any{
				"type": "message", "channel": "C_TEST", "user": "U_USER",
				"text": "hello from enterprise", "ts": "1234567890.123456",
			},
		})
		must.NoError(t, err)
		resp := postJSON(t, adapter, webhookSecret, string(body))
		must.Eq(t, 200, resp.status)
		id, ent := prov.last()
		must.Eq(t, "E_ENTERPRISE_1", id)
		must.True(t, ent)
	})

	t.Run("uses team_id when is_enterprise_install is false", func(t *testing.T) {
		t.Parallel()
		prov := &mockInstallProvider{inst: &Installation{BotToken: "xoxb-team-token", BotUserID: "U_BOT_TEAM"}}
		adapter, apiMock := newMultiAPI(t, Config{InstallationProvider: prov})
		apiMock.ok("users.info", usersInfoOK())
		_ = initMulti(t, adapter)
		body, err := json.Marshal(map[string]any{
			"type": "event_callback", "team_id": "T_TEAM_ONLY",
			"enterprise_id": "E_SHOULD_IGNORE", "is_enterprise_install": false,
			"event": map[string]any{
				"type": "message", "channel": "C_TEST", "user": "U_USER",
				"text": "hello", "ts": "1234567890.123456",
			},
		})
		must.NoError(t, err)
		_ = postJSON(t, adapter, webhookSecret, string(body))
		id, ent := prov.last()
		must.Eq(t, "T_TEAM_ONLY", id)
		must.False(t, ent)
	})

	t.Run("returns 200 ok when installationProvider returns null", func(t *testing.T) {
		t.Parallel()
		prov := &mockInstallProvider{}
		adapter := newMulti(t, Config{InstallationProvider: prov})
		_ = initMulti(t, adapter)
		body, err := json.Marshal(map[string]any{
			"type": "event_callback", "team_id": "T_UNKNOWN",
			"event": map[string]any{
				"type": "message", "channel": "C_TEST", "user": "U_USER",
				"text": "hello", "ts": "1234567890.123456",
			},
		})
		must.NoError(t, err)
		resp := postJSON(t, adapter, webhookSecret, string(body))
		must.Eq(t, 200, resp.status)
		id, ent := prov.last()
		must.Eq(t, "T_UNKNOWN", id)
		must.False(t, ent)
	})

	t.Run("uses installationProvider for slash commands", func(t *testing.T) {
		t.Parallel()
		prov := &mockInstallProvider{inst: &Installation{BotToken: "xoxb-slash-token", BotUserID: "U_BOT_SLASH"}}
		adapter, apiMock := newMultiAPI(t, Config{InstallationProvider: prov})
		apiMock.ok("users.info", usersInfoOK())
		_ = initMulti(t, adapter)
		body := url.Values{
			"command": {"/test"}, "text": {"hello"}, "team_id": {"T_SLASH_TEAM"},
			"channel_id": {"C_SLASH"}, "user_id": {"U_SLASHER"},
			"response_url": {"https://hooks.slack.com/commands/xxx"},
		}.Encode()
		_ = postForm(t, adapter, webhookSecret, body)
		id, ent := prov.last()
		must.Eq(t, "T_SLASH_TEAM", id)
		must.False(t, ent)
	})

	t.Run("drops slash commands when no installation is found", func(t *testing.T) {
		t.Parallel()
		prov := &mockInstallProvider{}
		adapter := newMulti(t, Config{InstallationProvider: prov})
		mc := initMulti(t, adapter)
		body := url.Values{
			"command": {"/test"}, "team_id": {"T_UNKNOWN"},
			"channel_id": {"C_SLASH"}, "user_id": {"U_SLASHER"},
		}.Encode()
		resp := postForm(t, adapter, webhookSecret, body)
		must.Eq(t, 200, resp.status)
		id, ent := prov.last()
		must.Eq(t, "T_UNKNOWN", id)
		must.False(t, ent)
		must.Eq(t, 0, len(mc.slashes))
	})

	t.Run("uses enterprise_id for slash commands in Enterprise Grid", func(t *testing.T) {
		t.Parallel()
		prov := &mockInstallProvider{inst: &Installation{BotToken: "xoxb-ent-slash-token", BotUserID: "U_BOT_ENT_SLASH"}}
		adapter, apiMock := newMultiAPI(t, Config{InstallationProvider: prov})
		apiMock.ok("users.info", usersInfoOK())
		_ = initMulti(t, adapter)
		body := url.Values{
			"command": {"/test"}, "text": {"hello"}, "team_id": {"T_ENT_WORKSPACE"},
			"enterprise_id": {"E_ENT_ORG"}, "is_enterprise_install": {"true"},
			"channel_id": {"C_SLASH"}, "user_id": {"U_SLASHER"},
			"response_url": {"https://hooks.slack.com/commands/xxx"},
		}.Encode()
		_ = postForm(t, adapter, webhookSecret, body)
		id, ent := prov.last()
		must.Eq(t, "E_ENT_ORG", id)
		must.True(t, ent)
	})

	t.Run("does not fall back to state when installationProvider is set", func(t *testing.T) {
		t.Parallel()
		prov := &mockInstallProvider{}
		adapter := newMulti(t, Config{InstallationProvider: prov})
		mc := initMulti(t, adapter)
		must.NoError(t, adapter.SetInstallation(t.Context(), "T_STATE_TEAM", Installation{
			BotToken: "xoxb-state-token", BotUserID: "U_BOT_STATE",
		}))
		body, err := json.Marshal(map[string]any{
			"type": "event_callback", "team_id": "T_STATE_TEAM",
			"event": map[string]any{
				"type": "message", "channel": "C_TEST", "user": "U_USER",
				"text": "hello", "ts": "1234567890.123456",
			},
		})
		must.NoError(t, err)
		resp := postJSON(t, adapter, webhookSecret, string(body))
		must.Eq(t, 200, resp.status)
		must.True(t, prov.n() > 0)
		must.Eq(t, 0, len(mc.messages))
	})

	t.Run("uses installationProvider for interactive payloads (block_actions)", func(t *testing.T) {
		t.Parallel()
		prov := &mockInstallProvider{inst: &Installation{BotToken: "xoxb-interactive-token", BotUserID: "U_BOT_INTER"}}
		adapter := newMulti(t, Config{InstallationProvider: prov})
		_ = initMulti(t, adapter)
		resp := postForm(t, adapter, webhookSecret, formPayload(t, map[string]any{
			"type": "block_actions",
			"team": map[string]any{"id": "T_INTER_PROVIDER"},
			"user": map[string]any{"id": "U123", "username": "testuser", "name": "Test User"},
			"container": map[string]any{
				"type": "message", "message_ts": "1234567890.123456", "channel_id": "C_INTER",
			},
			"channel": map[string]any{"id": "C_INTER", "name": "general"},
			"message": map[string]any{"ts": "1234567890.123456"},
			"actions": []any{map[string]any{"type": "button", "action_id": "test_action", "value": "v"}},
		}))
		must.Eq(t, 200, resp.status)
		id, ent := prov.last()
		must.Eq(t, "T_INTER_PROVIDER", id)
		must.False(t, ent)
	})

	t.Run("drops interactive payloads when no installation is found", func(t *testing.T) {
		t.Parallel()
		prov := &mockInstallProvider{}
		adapter := newMulti(t, Config{InstallationProvider: prov})
		mc := initMulti(t, adapter)
		resp := postForm(t, adapter, webhookSecret, formPayload(t, map[string]any{
			"type":    "block_actions",
			"team":    map[string]any{"id": "T_UNKNOWN"},
			"user":    map[string]any{"id": "U123", "username": "testuser"},
			"channel": map[string]any{"id": "C_INTER", "name": "general"},
			"message": map[string]any{"ts": "1234567890.123456"},
			"actions": []any{map[string]any{"type": "button", "action_id": "test_action", "value": "v"}},
		}))
		must.Eq(t, 200, resp.status)
		id, ent := prov.last()
		must.Eq(t, "T_UNKNOWN", id)
		must.False(t, ent)
		must.Eq(t, 0, len(mc.actions))
	})

	t.Run("uses enterprise_id for interactive payloads in Enterprise Grid", func(t *testing.T) {
		t.Parallel()
		prov := &mockInstallProvider{inst: &Installation{BotToken: "xoxb-ent-interactive-token", BotUserID: "U_BOT_ENT_INTER"}}
		adapter := newMulti(t, Config{InstallationProvider: prov})
		_ = initMulti(t, adapter)
		resp := postForm(t, adapter, webhookSecret, formPayload(t, map[string]any{
			"type":                  "block_actions",
			"team":                  map[string]any{"id": "T_ENT_INTER_WORKSPACE"},
			"enterprise":            map[string]any{"id": "E_ENT_INTER_ORG"},
			"is_enterprise_install": true,
			"user":                  map[string]any{"id": "U123", "username": "testuser", "name": "Test User"},
			"container":             map[string]any{"type": "message", "message_ts": "1234567890.123456", "channel_id": "C_ENT_INTER"},
			"channel":               map[string]any{"id": "C_ENT_INTER", "name": "general"},
			"message":               map[string]any{"ts": "1234567890.123456"},
			"actions":               []any{map[string]any{"type": "button", "action_id": "test_action", "value": "v"}},
		}))
		must.Eq(t, 200, resp.status)
		id, ent := prov.last()
		must.Eq(t, "E_ENT_INTER_ORG", id)
		must.True(t, ent)
	})

	t.Run("event_callback with file attachment captures Enterprise Grid metadata", func(t *testing.T) {
		t.Parallel()
		prov := &mockInstallProvider{inst: &Installation{BotToken: "xoxb-ent-event-token", BotUserID: "U_BOT_ENT_EVENT"}}
		adapter := newMulti(t, Config{InstallationProvider: prov})
		mc := initMulti(t, adapter)
		body, err := json.Marshal(map[string]any{
			"type": "event_callback", "team_id": "T_ENT_WORKSPACE",
			"enterprise_id": "E_ENT_FILE_ORG", "is_enterprise_install": true,
			"event": map[string]any{
				"type": "message", "channel": "C_TEST", "user": "U_USER",
				"username": "testuser", "text": "with file", "ts": "1234567890.123456",
				"files": []any{map[string]any{
					"id": "F1", "mimetype": "image/png",
					"url_private": "https://files.slack.com/captured.png", "name": "captured.png",
				}},
			},
		})
		must.NoError(t, err)
		_ = postJSON(t, adapter, webhookSecret, string(body))
		must.Eq(t, 1, len(mc.messages))
		must.True(t, len(mc.messages[0].Message.Attachments) > 0)
		meta := mc.messages[0].Message.Attachments[0].FetchMetadata
		must.Eq(t, "https://files.slack.com/captured.png", meta["url"])
		must.Eq(t, "T_ENT_WORKSPACE", meta["teamId"])
		must.Eq(t, "E_ENT_FILE_ORG", meta["enterpriseId"])
		must.Eq(t, "true", meta["isEnterpriseInstall"])
	})
}

func TestMultiWorkspaceModeWithEncryption(t *testing.T) {
	t.Parallel()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	must.NoError(t, err)
	encryptionKey := base64.StdEncoding.EncodeToString(key)

	t.Run("setInstallation encrypts token", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{EncryptionKey: encryptionKey})
		mc := initMulti(t, adapter)
		must.NoError(t, adapter.SetInstallation(t.Context(), "T_ENC_1", Installation{
			BotToken: "xoxb-secret-token", BotUserID: "U_BOT_E1",
		}))
		raw, err := mc.State().Get(t.Context(), "slack:installation:T_ENC_1")
		must.NoError(t, err)
		must.True(t, len(raw) > 0)
		var stored map[string]any
		must.NoError(t, json.Unmarshal(raw, &stored))
		rawToken, ok := stored["botToken"].(map[string]any)
		must.True(t, ok)
		_, hasIV := rawToken["iv"]
		_, hasData := rawToken["data"]
		_, hasTag := rawToken["tag"]
		must.True(t, hasIV)
		must.True(t, hasData)
		must.True(t, hasTag)
		must.True(t, fmt.Sprint(rawToken) != "xoxb-secret-token")
	})

	t.Run("getInstallation decrypts token", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{EncryptionKey: encryptionKey})
		_ = initMulti(t, adapter)
		must.NoError(t, adapter.SetInstallation(t.Context(), "T_ENC_2", Installation{
			BotToken: "xoxb-encrypted-token", TeamName: "Encrypted Team",
		}))
		installation, err := adapter.GetInstallation(t.Context(), "T_ENC_2")
		must.NoError(t, err)
		must.True(t, installation != nil)
		must.Eq(t, "xoxb-encrypted-token", installation.BotToken)
		must.Eq(t, "Encrypted Team", installation.TeamName)
	})

	t.Run("invalid encryption key throws at construction", func(t *testing.T) {
		t.Parallel()
		short := make([]byte, 16)
		_, err := rand.Read(short)
		must.NoError(t, err)
		_, err = New(Config{
			SigningSecret: webhookSecret,
			EncryptionKey: base64.StdEncoding.EncodeToString(short),
		})
		must.Error(t, err)
		must.StrContains(t, err.Error(), "encryption key must decode to exactly 32 bytes")
	})

	t.Run("getInstallation surfaces a wrong-key decrypt as an error", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{EncryptionKey: encryptionKey})
		_ = initMulti(t, adapter)
		must.NoError(t, adapter.SetInstallation(t.Context(), "T_ENC_3", Installation{BotToken: "xoxb-secret"}))
		other := make([]byte, 32)
		_, err := rand.Read(other)
		must.NoError(t, err)
		wrong := newMulti(t, Config{EncryptionKey: base64.StdEncoding.EncodeToString(other)})
		wrong.chat = adapter.chat
		_, err = wrong.GetInstallation(t.Context(), "T_ENC_3")
		must.Error(t, err)
	})

	t.Run("getInstallation surfaces an unkeyed read of an encrypted record as an error", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{EncryptionKey: encryptionKey})
		_ = initMulti(t, adapter)
		must.NoError(t, adapter.SetInstallation(t.Context(), "T_ENC_4", Installation{BotToken: "xoxb-secret"}))
		unkeyed := newMulti(t, Config{})
		unkeyed.encryptionKey = nil
		unkeyed.chat = adapter.chat
		_, err := unkeyed.GetInstallation(t.Context(), "T_ENC_4")
		must.Error(t, err)
	})
}

func TestInstallationKeyPrefix(t *testing.T) {
	t.Parallel()

	t.Run("uses custom installationKeyPrefix for storage key", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{InstallationKeyPrefix: "myapp:workspaces"})
		mc := initMulti(t, adapter)
		must.NoError(t, adapter.SetInstallation(t.Context(), "T_CUSTOM_1", Installation{BotToken: "xoxb-token"}))
		custom, err := mc.State().Get(t.Context(), "myapp:workspaces:T_CUSTOM_1")
		must.NoError(t, err)
		must.True(t, len(custom) > 0)
		def, err := mc.State().Get(t.Context(), "slack:installation:T_CUSTOM_1")
		must.NoError(t, err)
		must.Eq(t, 0, len(def))
		retrieved, err := adapter.GetInstallation(t.Context(), "T_CUSTOM_1")
		must.NoError(t, err)
		must.Eq(t, "xoxb-token", retrieved.BotToken)
	})

	t.Run("uses default slack:installation prefix when installationKeyPrefix is omitted", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{})
		mc := initMulti(t, adapter)
		must.NoError(t, adapter.SetInstallation(t.Context(), "T_DEFAULT_1", Installation{BotToken: "xoxb-token"}))
		raw, err := mc.State().Get(t.Context(), "slack:installation:T_DEFAULT_1")
		must.NoError(t, err)
		must.True(t, len(raw) > 0)
	})
}

func TestHandleOAuthCallback(t *testing.T) {
	t.Parallel()

	oauthOK := func() map[string]any {
		return map[string]any{
			"access_token": "xoxb-oauth-bot-token",
			"bot_user_id":  "U_BOT_OAUTH",
			"team":         map[string]any{"id": "T_OAUTH_1", "name": "OAuth Team"},
		}
	}

	t.Run("exchanges code for token and saves installation", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock := newMultiAPI(t, Config{ClientID: "client-id", ClientSecret: "client-secret"})
		apiMock.ok("oauth.v2.access", oauthOK())
		_ = initMulti(t, adapter)
		result, err := adapter.HandleOAuthCallback(t.Context(), oauthGET("https://example.com/auth/callback/slack?code=oauth-code-123"), nil)
		must.NoError(t, err)
		must.Eq(t, "T_OAUTH_1", result.TeamID)
		must.Eq(t, "xoxb-oauth-bot-token", result.Installation.BotToken)
		must.Eq(t, "U_BOT_OAUTH", result.Installation.BotUserID)
		must.Eq(t, "OAuth Team", result.Installation.TeamName)
		stored, err := adapter.GetInstallation(t.Context(), "T_OAUTH_1")
		must.NoError(t, err)
		must.True(t, stored != nil)
		must.Eq(t, "xoxb-oauth-bot-token", stored.BotToken)
		call := apiMock.last("oauth.v2.access")
		must.Eq(t, "client-id", call.Form.Get("client_id"))
		must.Eq(t, "client-secret", call.Form.Get("client_secret"))
		must.Eq(t, "oauth-code-123", call.Form.Get("code"))
		must.Eq(t, "", call.Form.Get("redirect_uri"))
	})

	t.Run("keys org-wide installs by enterprise ID (team is null)", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock := newMultiAPI(t, Config{ClientID: "client-id", ClientSecret: "client-secret"})
		apiMock.ok("oauth.v2.access", map[string]any{
			"access_token":          "xoxb-org-bot-token",
			"bot_user_id":           "U_BOT_ORG",
			"team":                  nil,
			"enterprise":            map[string]any{"id": "E_ORG_1", "name": "Acme Org"},
			"is_enterprise_install": true,
		})
		_ = initMulti(t, adapter)
		result, err := adapter.HandleOAuthCallback(t.Context(), oauthGET("https://example.com/auth/callback/slack?code=oauth-code-org"), nil)
		must.NoError(t, err)
		must.Eq(t, "E_ORG_1", result.TeamID)
		must.Eq(t, "E_ORG_1", result.EnterpriseID)
		must.True(t, result.IsEnterpriseInstall)
		must.Eq(t, "Acme Org", result.Installation.TeamName)
		stored, err := adapter.GetInstallation(t.Context(), "E_ORG_1")
		must.NoError(t, err)
		must.Eq(t, "xoxb-org-bot-token", stored.BotToken)
		must.Eq(t, "E_ORG_1", stored.EnterpriseID)
		must.True(t, stored.IsEnterpriseInstall)
	})

	t.Run("records the enterprise ID on workspace installs within a Grid org", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock := newMultiAPI(t, Config{ClientID: "client-id", ClientSecret: "client-secret"})
		apiMock.ok("oauth.v2.access", map[string]any{
			"access_token":          "xoxb-grid-workspace-token",
			"bot_user_id":           "U_BOT_GRID",
			"team":                  map[string]any{"id": "T_GRID_1", "name": "Grid Workspace"},
			"enterprise":            map[string]any{"id": "E_ORG_1", "name": "Acme Org"},
			"is_enterprise_install": false,
		})
		_ = initMulti(t, adapter)
		result, err := adapter.HandleOAuthCallback(t.Context(), oauthGET("https://example.com/auth/callback/slack?code=oauth-code-grid"), nil)
		must.NoError(t, err)
		must.Eq(t, "T_GRID_1", result.TeamID)
		must.Eq(t, "E_ORG_1", result.EnterpriseID)
		must.False(t, result.IsEnterpriseInstall)
		stored, err := adapter.GetInstallation(t.Context(), "T_GRID_1")
		must.NoError(t, err)
		must.Eq(t, "xoxb-grid-workspace-token", stored.BotToken)
		must.Eq(t, "E_ORG_1", stored.EnterpriseID)
		must.False(t, stored.IsEnterpriseInstall)
	})

	t.Run("throws when an org-wide install response is missing enterprise.id", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock := newMultiAPI(t, Config{ClientID: "client-id", ClientSecret: "client-secret"})
		apiMock.ok("oauth.v2.access", map[string]any{
			"access_token":          "xoxb-org-bot-token",
			"team":                  nil,
			"enterprise":            nil,
			"is_enterprise_install": true,
		})
		_ = initMulti(t, adapter)
		_, err := adapter.HandleOAuthCallback(t.Context(), oauthGET("https://example.com/auth/callback/slack?code=oauth-code-org"), nil)
		must.Error(t, err)
		must.StrContains(t, err.Error(), "missing access_token or enterprise.id")
	})

	t.Run("org-wide OAuth install round-trips with org-wide event webhooks", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock := newMultiAPI(t, Config{ClientID: "client-id", ClientSecret: "client-secret"})
		apiMock.ok("oauth.v2.access", map[string]any{
			"access_token":          "xoxb-org-bot-token",
			"bot_user_id":           "U_BOT_ORG",
			"team":                  nil,
			"enterprise":            map[string]any{"id": "E_ORG_1", "name": "Acme Org"},
			"is_enterprise_install": true,
		})
		apiMock.ok("users.info", usersInfoOK())
		mc := initMulti(t, adapter)
		_, err := adapter.HandleOAuthCallback(t.Context(), oauthGET("https://example.com/auth/callback/slack?code=oauth-code"), nil)
		must.NoError(t, err)
		body, err := json.Marshal(map[string]any{
			"type": "event_callback", "team_id": "T_GRID_1",
			"enterprise_id": "E_ORG_1", "is_enterprise_install": true,
			"event": map[string]any{
				"type": "message", "user": "U123", "channel": "C456",
				"text": "Hello org", "ts": "1234567890.123456",
			},
		})
		must.NoError(t, err)
		resp := postJSON(t, adapter, webhookSecret, string(body))
		must.Eq(t, 200, resp.status)
		must.Eq(t, 1, len(mc.messages))
	})

	t.Run("forwards redirect_uri from callback options", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock := newMultiAPI(t, Config{ClientID: "client-id", ClientSecret: "client-secret"})
		apiMock.ok("oauth.v2.access", oauthOK())
		_ = initMulti(t, adapter)
		_, err := adapter.HandleOAuthCallback(t.Context(), oauthGET("https://example.com/auth/callback/slack?code=oauth-code-123"), &SlackOAuthCallbackOptions{
			RedirectURI: "https://example.com/install/callback",
		})
		must.NoError(t, err)
		call := apiMock.last("oauth.v2.access")
		must.Eq(t, "client-id", call.Form.Get("client_id"))
		must.Eq(t, "client-secret", call.Form.Get("client_secret"))
		must.Eq(t, "oauth-code-123", call.Form.Get("code"))
		must.Eq(t, "https://example.com/install/callback", call.Form.Get("redirect_uri"))
	})

	t.Run("prefers callback options redirect_uri over the query param", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock := newMultiAPI(t, Config{ClientID: "client-id", ClientSecret: "client-secret"})
		apiMock.ok("oauth.v2.access", oauthOK())
		_ = initMulti(t, adapter)
		_, err := adapter.HandleOAuthCallback(t.Context(), oauthGET("https://example.com/auth/callback/slack?code=oauth-code-123&redirect_uri=https%3A%2F%2Fexample.com%2Fquery-callback"), &SlackOAuthCallbackOptions{
			RedirectURI: "https://example.com/explicit-callback",
		})
		must.NoError(t, err)
		call := apiMock.last("oauth.v2.access")
		must.Eq(t, "https://example.com/explicit-callback", call.Form.Get("redirect_uri"))
	})

	t.Run("falls back to redirect_uri from the callback query param", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock := newMultiAPI(t, Config{ClientID: "client-id", ClientSecret: "client-secret"})
		apiMock.ok("oauth.v2.access", oauthOK())
		_ = initMulti(t, adapter)
		_, err := adapter.HandleOAuthCallback(t.Context(), oauthGET("https://example.com/auth/callback/slack?code=oauth-code-123&redirect_uri=https%3A%2F%2Fexample.com%2Fquery-callback"), nil)
		must.NoError(t, err)
		call := apiMock.last("oauth.v2.access")
		must.Eq(t, "https://example.com/query-callback", call.Form.Get("redirect_uri"))
	})

	t.Run("throws when the callback code is missing", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{ClientID: "client-id", ClientSecret: "client-secret"})
		_ = initMulti(t, adapter)
		_, err := adapter.HandleOAuthCallback(t.Context(), oauthGET("https://example.com/auth/callback/slack"), nil)
		must.Error(t, err)
		must.StrContains(t, err.Error(), "Missing 'code' query parameter in OAuth callback request.")
	})

	t.Run("throws without clientId and clientSecret", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{})
		_ = initMulti(t, adapter)
		_, err := adapter.HandleOAuthCallback(t.Context(), oauthGET("https://example.com/auth/callback/slack?code=test"), nil)
		must.Error(t, err)
		must.StrContains(t, err.Error(), "clientId and clientSecret are required")
	})
}

func TestWithBotToken(t *testing.T) {
	t.Parallel()

	t.Run("sets token for duration of callback", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{})
		_ = initMulti(t, adapter)
		callbackRan := false
		adapter.WithBotToken(t.Context(), "xoxb-context-token", func(context.Context) {
			callbackRan = true
		}, nil)
		must.True(t, callbackRan)
	})

	t.Run("concurrent calls with different tokens are isolated", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{})
		_ = initMulti(t, adapter)
		var (
			mu     sync.Mutex
			tokens []string
			wg     sync.WaitGroup
		)
		wg.Add(2)
		go func() {
			defer wg.Done()
			adapter.WithBotToken(t.Context(), "xoxb-token-A", func(ctx context.Context) {
				tok, err := adapter.getToken(ctx)
				must.NoError(t, err)
				must.Eq(t, "xoxb-token-A", tok)
				mu.Lock()
				tokens = append(tokens, "A")
				mu.Unlock()
			}, nil)
		}()
		go func() {
			defer wg.Done()
			adapter.WithBotToken(t.Context(), "xoxb-token-B", func(ctx context.Context) {
				tok, err := adapter.getToken(ctx)
				must.NoError(t, err)
				must.Eq(t, "xoxb-token-B", tok)
				mu.Lock()
				tokens = append(tokens, "B")
				mu.Unlock()
			}, nil)
		}()
		wg.Wait()
		must.SliceContains(t, tokens, "A")
		must.SliceContains(t, tokens, "B")
	})
}

func clientToken(t *testing.T, c *api.Client) string {
	t.Helper()
	tok, err := c.Token.Token(t.Context())
	must.NoError(t, err)
	return tok
}

func TestDirectWebClientAccessViaAdapterClient(t *testing.T) {
	t.Run("returns a WebClient bound to the static botToken in single-workspace mode", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{Token: api.StaticToken("xoxb-static-token")})
		c, err := adapter.Client(t.Context())
		must.NoError(t, err)
		must.Eq(t, "xoxb-static-token", clientToken(t, c))
	})

	t.Run("caches the WebClient for repeated access with the same token", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{Token: api.StaticToken("xoxb-static-token")})
		a, err := adapter.Client(t.Context())
		must.NoError(t, err)
		b, err := adapter.Client(t.Context())
		must.NoError(t, err)
		must.True(t, a == b)
	})

	t.Run("uses a synchronous botToken resolver", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{Token: tokenFunc(func(context.Context) (string, error) {
			return "xoxb-sync-resolved", nil
		})})
		c, err := adapter.Client(t.Context())
		must.NoError(t, err)
		must.Eq(t, "xoxb-sync-resolved", clientToken(t, c))
	})

	t.Run("returns a WebClient bound to the context token under withBotToken", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{})
		var observed string
		adapter.WithBotToken(t.Context(), "xoxb-context-token", func(ctx context.Context) {
			c, err := adapter.Client(ctx)
			must.NoError(t, err)
			observed = clientToken(t, c)
		}, nil)
		must.Eq(t, "xoxb-context-token", observed)
	})

	t.Run("prefers the context token over the default in single-workspace mode", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{Token: api.StaticToken("xoxb-default")})
		var observed string
		adapter.WithBotToken(t.Context(), "xoxb-override", func(ctx context.Context) {
			c, err := adapter.Client(ctx)
			must.NoError(t, err)
			observed = clientToken(t, c)
		}, nil)
		must.Eq(t, "xoxb-override", observed)
		c, err := adapter.Client(t.Context())
		must.NoError(t, err)
		must.Eq(t, "xoxb-default", clientToken(t, c))
	})

	t.Run("propagates apiUrl from createSlackAdapter to the bound WebClient", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{
			Token:  api.StaticToken("xoxb-static-token"),
			APIURL: "https://slack-gov.com/api/",
		})
		c, err := adapter.Client(t.Context())
		must.NoError(t, err)
		must.Eq(t, "https://slack-gov.com/api/", c.APIURL)
	})

	t.Run("throws when no token is available (multi-workspace, outside context)", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{ClientID: "test-client-id", ClientSecret: "test-client-secret"})
		_, err := adapter.Client(t.Context())
		must.Error(t, err)
		var ae *shared.AuthenticationError
		must.True(t, errors.As(err, &ae))
	})

	t.Run("returns distinct WebClient instances for distinct tokens", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{})
		var clientA, clientB *api.Client
		adapter.WithBotToken(t.Context(), "xoxb-token-A", func(ctx context.Context) {
			c, err := adapter.Client(ctx)
			must.NoError(t, err)
			clientA = c
		}, nil)
		adapter.WithBotToken(t.Context(), "xoxb-token-B", func(ctx context.Context) {
			c, err := adapter.Client(ctx)
			must.NoError(t, err)
			clientB = c
		}, nil)
		must.True(t, clientA != clientB)
		must.Eq(t, "xoxb-token-A", clientToken(t, clientA))
		must.Eq(t, "xoxb-token-B", clientToken(t, clientB))
	})

	t.Run("picks up apiUrl from SLACK_API_URL env var", func(t *testing.T) {
		t.Setenv(envAPIURL, "https://slack-gov.com/api/")
		adapter, err := New(Config{
			SigningSecret: webhookSecret,
			Token:         api.StaticToken("xoxb-static-token"),
		})
		must.NoError(t, err)
		c, err := adapter.Client(t.Context())
		must.NoError(t, err)
		must.Eq(t, "https://slack-gov.com/api/", c.APIURL)
	})
}

func TestWebClientGetter(t *testing.T) {
	t.Parallel()

	t.Run("returns the underlying WebClient bound to the static botToken", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{Token: api.StaticToken("xoxb-static-token")})
		c, err := adapter.WebClient(t.Context())
		must.NoError(t, err)
		must.Eq(t, "xoxb-static-token", clientToken(t, c))
	})

	t.Run("returns the same instance across calls in single-workspace mode", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{Token: api.StaticToken("xoxb-static-token")})
		a, err := adapter.WebClient(t.Context())
		must.NoError(t, err)
		b, err := adapter.WebClient(t.Context())
		must.NoError(t, err)
		must.True(t, a == b)
	})

	t.Run("exposes the same instance via the deprecated `client` alias", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{Token: api.StaticToken("xoxb-static-token")})
		c, err := adapter.Client(t.Context())
		must.NoError(t, err)
		w, err := adapter.WebClient(t.Context())
		must.NoError(t, err)
		must.True(t, c == w)
	})

	t.Run("throws on both `webClient` and the `client` alias in multi-workspace mode without context", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{ClientID: "test-client-id", ClientSecret: "test-client-secret"})
		_, err := adapter.WebClient(t.Context())
		must.Error(t, err)
		var ae *shared.AuthenticationError
		must.True(t, errors.As(err, &ae))
		_, err = adapter.Client(t.Context())
		must.Error(t, err)
		must.True(t, errors.As(err, &ae))
	})

	t.Run("uses the request context token under withBotToken via `webClient`", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{})
		var observed string
		adapter.WithBotToken(t.Context(), "xoxb-context-token", func(ctx context.Context) {
			c, err := adapter.WebClient(ctx)
			must.NoError(t, err)
			observed = clientToken(t, c)
		}, nil)
		must.Eq(t, "xoxb-context-token", observed)
	})
}

type actionTokenChat struct {
	mockChat
	seen string
}

func (c *actionTokenChat) ProcessAction(ctx context.Context, in chat.ProcessActionInput) error {
	if a, ok := in.Adapter.(*SlackAdapter); ok {
		cl, err := a.Client(ctx)
		if err == nil {
			c.seen, _ = cl.Token.Token(ctx)
		}
	}
	return c.mockChat.ProcessAction(ctx, in)
}

func TestAdapterClientEndToEndWithMultiWorkspaceWebhook(t *testing.T) {
	t.Parallel()

	t.Run("resolves the installation's bot token in the action handler context", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{})
		_, st := newStateChat(t)
		mc := &actionTokenChat{mockChat: mockChat{state: st}}
		must.NoError(t, adapter.Initialize(t.Context(), mc))
		must.NoError(t, adapter.SetInstallation(t.Context(), "T_E2E_1", Installation{
			BotToken: "xoxb-installation-token", BotUserID: "U_BOT_E2E",
		}))
		resp := postForm(t, adapter, webhookSecret, formPayload(t, map[string]any{
			"type": "block_actions",
			"team": map[string]any{"id": "T_E2E_1"},
			"user": map[string]any{"id": "U_USER", "username": "u", "name": "User"},
			"container": map[string]any{
				"type": "message", "message_ts": "1234567890.123456", "channel_id": "C_E2E",
			},
			"channel": map[string]any{"id": "C_E2E", "name": "general"},
			"message": map[string]any{"ts": "1234567890.123456"},
			"actions": []any{map[string]any{"type": "button", "action_id": "noop", "value": "v"}},
		}))
		must.Eq(t, 200, resp.status)
		must.Eq(t, 1, len(mc.actions))
		must.Eq(t, "xoxb-installation-token", mc.seen)
	})
}

type seqToken struct {
	tokens []string
	i      int
}

func (s *seqToken) Token(context.Context) (string, error) {
	if s.i >= len(s.tokens) {
		return "", errors.New("exhausted")
	}
	tok := s.tokens[s.i]
	s.i++
	return tok, nil
}

type errToken struct{ err error }

func (e errToken) Token(context.Context) (string, error) { return "", e.err }

func TestBotTokenAsFunction(t *testing.T) {
	t.Parallel()

	t.Run("accepts a sync resolver and uses its return value on API calls", func(t *testing.T) {
		t.Parallel()
		resolver := &countingToken{tok: "xoxb-sync-token"}
		apiMock := newSlackAPIMock(t)
		apiMock.ok("chat.postMessage", map[string]any{"ts": "1234567890.999999"})
		adapter := apiMock.adapter(t, Config{Token: resolver, BotUserID: "U_BOT"})
		_, err := adapter.PostMessage(t.Context(), "slack:C123:1234567890.000000", chat.PostableText("hi"))
		must.NoError(t, err)
		must.Eq(t, "Bearer xoxb-sync-token", apiMock.last("chat.postMessage").Auth)
		must.True(t, resolver.n > 0)
	})

	t.Run("accepts an async resolver and awaits the returned promise", func(t *testing.T) {
		t.Parallel()
		resolver := tokenFunc(func(context.Context) (string, error) {
			return "xoxb-async-token", nil
		})
		apiMock := newSlackAPIMock(t)
		apiMock.ok("chat.postMessage", map[string]any{"ts": "1234567890.999999"})
		adapter := apiMock.adapter(t, Config{Token: resolver, BotUserID: "U_BOT"})
		_, err := adapter.PostMessage(t.Context(), "slack:C123:1234567890.000000", chat.PostableText("hi"))
		must.NoError(t, err)
		must.Eq(t, "Bearer xoxb-async-token", apiMock.last("chat.postMessage").Auth)
	})

	t.Run("invokes the resolver per API call (supports rotation)", func(t *testing.T) {
		t.Parallel()
		resolver := &seqToken{tokens: []string{"xoxb-token-1", "xoxb-token-2", "xoxb-token-3"}}
		apiMock := newSlackAPIMock(t)
		apiMock.ok("chat.postMessage", map[string]any{"ts": "1234567890.999999"})
		adapter := apiMock.adapter(t, Config{Token: resolver, BotUserID: "U_BOT"})
		_, err := adapter.PostMessage(t.Context(), "slack:C123:1234567890.000000", chat.PostableText("first"))
		must.NoError(t, err)
		_, err = adapter.PostMessage(t.Context(), "slack:C123:1234567890.000000", chat.PostableText("second"))
		must.NoError(t, err)
		_, err = adapter.PostMessage(t.Context(), "slack:C123:1234567890.000000", chat.PostableText("third"))
		must.NoError(t, err)
		calls := apiMock.all("chat.postMessage")
		must.Eq(t, 3, len(calls))
		must.Eq(t, "Bearer xoxb-token-1", calls[0].Auth)
		must.Eq(t, "Bearer xoxb-token-2", calls[1].Auth)
		must.Eq(t, "Bearer xoxb-token-3", calls[2].Auth)
	})

	t.Run("treats a function botToken as single-workspace mode", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("auth.test", map[string]any{
			"user_id": "U_BOT", "bot_id": "B_BOT", "user": "fnbot",
		})
		adapter := apiMock.adapter(t, Config{
			Token: tokenFunc(func(context.Context) (string, error) { return "xoxb-fn-token", nil }),
		})
		must.NoError(t, adapter.Initialize(t.Context(), stubChat{}))
		must.Eq(t, 1, apiMock.count("auth.test"))
		must.Eq(t, "Bearer xoxb-fn-token", apiMock.last("auth.test").Auth)
		must.Eq(t, "U_BOT", adapter.BotUserID())
	})

	t.Run("propagates errors thrown by the resolver", func(t *testing.T) {
		t.Parallel()
		adapter := mustNew(t, Config{
			Token:     errToken{err: errors.New("token fetch failed")},
			BotUserID: "U_BOT",
		})
		_, err := adapter.PostMessage(t.Context(), "slack:C123:1234567890.000000", chat.PostableText("hi"))
		must.Error(t, err)
		must.StrContains(t, err.Error(), "token fetch failed")
	})
}

func aliceUserInfo() map[string]any {
	return map[string]any{
		"user": map[string]any{
			"name":      "alice",
			"real_name": "Alice Example",
			"profile":   map[string]any{"display_name": "Alice", "real_name": "Alice Example"},
		},
	}
}

func TestInstallationScopedCaches(t *testing.T) {
	t.Parallel()

	newCache := func(t *testing.T) (*SlackAdapter, *slackAPIMock, chat.StateAdapter) {
		t.Helper()
		adapter, apiMock := newMultiAPI(t, Config{})
		apiMock.ok("users.info", aliceUserInfo())
		apiMock.ok("conversations.info", map[string]any{"channel": map[string]any{"name": "general"}})
		mc := initMulti(t, adapter)
		return adapter, apiMock, mc.State()
	}

	scoped := func(ctx context.Context, token, installationID string) context.Context {
		return withRequestContext(ctx, &requestContext{token: token, installationID: installationID})
	}

	t.Run("scopes the user profile cache by installation", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, st := newCache(t)
		_ = adapter.lookupUser(scoped(t.Context(), "xoxb-team-a", "T_A"), "U1")
		_ = adapter.lookupUser(scoped(t.Context(), "xoxb-team-b", "T_B"), "U1")
		must.Eq(t, 2, apiMock.count("users.info"))
		a, err := st.Get(t.Context(), "slack:user:T_A:U1")
		must.NoError(t, err)
		must.True(t, len(a) > 0)
		b, err := st.Get(t.Context(), "slack:user:T_B:U1")
		must.NoError(t, err)
		must.True(t, len(b) > 0)
		g, err := st.Get(t.Context(), "slack:user:U1")
		must.NoError(t, err)
		must.Eq(t, 0, len(g))
	})

	t.Run("scopes the channel-name cache by installation", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, st := newCache(t)
		n := 0
		apiMock.on("conversations.info", func(w http.ResponseWriter, _ *http.Request) {
			name := "team-a-private"
			if n == 1 {
				name = "team-b-general"
			}
			n++
			writeJSON(w, 200, map[string]any{"ok": true, "channel": map[string]any{"name": name}})
		})
		must.Eq(t, "team-a-private", adapter.lookupChannel(scoped(t.Context(), "xoxb-team-a", "T_A"), "C1"))
		must.Eq(t, "team-b-general", adapter.lookupChannel(scoped(t.Context(), "xoxb-team-b", "T_B"), "C1"))
		must.Eq(t, 2, apiMock.count("conversations.info"))
		var a cachedChannel
		must.NoError(t, json.Unmarshal(mustGet(t, st, "slack:channel:T_A:C1"), &a))
		must.Eq(t, cachedChannel{Name: "team-a-private"}, a)
		var b cachedChannel
		must.NoError(t, json.Unmarshal(mustGet(t, st, "slack:channel:T_B:C1"), &b))
		must.Eq(t, cachedChannel{Name: "team-b-general"}, b)
		g, err := st.Get(t.Context(), "slack:channel:C1")
		must.NoError(t, err)
		must.Eq(t, 0, len(g))
	})

	t.Run("uses unscoped keys without a request context (single-workspace)", func(t *testing.T) {
		t.Parallel()
		apiMock := newSlackAPIMock(t)
		apiMock.ok("users.info", aliceUserInfo())
		adapter := apiMock.adapter(t, Config{Token: api.StaticToken("xoxb-single-token")})
		mc := initMulti(t, adapter)
		_ = adapter.lookupUser(t.Context(), "U1")
		raw, err := mc.State().Get(t.Context(), "slack:user:U1")
		must.NoError(t, err)
		must.True(t, len(raw) > 0)
	})

	t.Run("scopes the cache under withBotToken when installationId is passed", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, st := newCache(t)
		adapter.WithBotToken(t.Context(), "xoxb-team-a", func(ctx context.Context) {
			_ = adapter.lookupUser(ctx, "U1")
		}, &BotTokenOptions{InstallationID: "T_A"})
		must.Eq(t, 1, apiMock.count("users.info"))
		raw, err := st.Get(t.Context(), "slack:user:T_A:U1")
		must.NoError(t, err)
		must.True(t, len(raw) > 0)
		g, err := st.Get(t.Context(), "slack:user:U1")
		must.NoError(t, err)
		must.Eq(t, 0, len(g))
	})

	t.Run("uses unscoped keys under withBotToken without installationId", func(t *testing.T) {
		t.Parallel()
		adapter, _, st := newCache(t)
		adapter.WithBotToken(t.Context(), "xoxb-token", func(ctx context.Context) {
			_ = adapter.lookupUser(ctx, "U1")
		}, nil)
		raw, err := st.Get(t.Context(), "slack:user:U1")
		must.NoError(t, err)
		must.True(t, len(raw) > 0)
	})

	t.Run("scopes the display-name reverse index by installation", func(t *testing.T) {
		t.Parallel()
		adapter, _, st := newCache(t)
		_ = adapter.lookupUser(scoped(t.Context(), "xoxb-team-a", "T_A"), "U1")
		list := mustList(t, st, "slack:user-by-name:T_A:alice")
		must.SliceContains(t, list, "U1")
		global := mustList(t, st, "slack:user-by-name:alice")
		must.Eq(t, 0, len(global))
	})

	t.Run("scopes unfurl metadata by installation and channel", func(t *testing.T) {
		t.Parallel()
		adapter, _, st := newCache(t)
		msg := SlackEvent{
			Type: "message", Subtype: "message_changed", Hidden: true,
			Channel: "C1", Ts: "1234567890.123456",
			Message: &SlackEvent{
				Type: "message", Channel: "C1", Ts: "1111111111.111111",
				Attachments: []SlackAttachment{{FromURL: "https://example.com/shared", Title: "Team A"}},
			},
		}
		adapter.handleMessageChanged(scoped(t.Context(), "xoxb-team-a", "T_A"), msg)
		other := msg
		other.Channel = "C2"
		inner := *msg.Message
		inner.Channel = "C2"
		inner.Attachments = []SlackAttachment{{FromURL: "https://example.com/shared", Title: "Team B"}}
		other.Message = &inner
		adapter.handleMessageChanged(scoped(t.Context(), "xoxb-team-b", "T_B"), other)

		a, err := st.Get(t.Context(), "slack:unfurls:T_A:C1:1111111111.111111")
		must.NoError(t, err)
		must.True(t, len(a) > 0)
		b, err := st.Get(t.Context(), "slack:unfurls:T_B:C2:1111111111.111111")
		must.NoError(t, err)
		must.True(t, len(b) > 0)
		g, err := st.Get(t.Context(), "slack:unfurls:1111111111.111111")
		must.NoError(t, err)
		must.Eq(t, 0, len(g))

		linksA, err := adapter.enrichLinks(scoped(t.Context(), "xoxb-team-a", "T_A"),
			[]chat.LinkPreview{{URL: "https://example.com/shared"}}, "C1", "1111111111.111111")
		must.NoError(t, err)
		must.Eq(t, 1, len(linksA))
		must.Eq(t, "Team A", linksA[0].Title)
		linksB, err := adapter.enrichLinks(scoped(t.Context(), "xoxb-team-b", "T_B"),
			[]chat.LinkPreview{{URL: "https://example.com/shared"}}, "C2", "1111111111.111111")
		must.NoError(t, err)
		must.Eq(t, 1, len(linksB))
		must.Eq(t, "Team B", linksB[0].Title)
	})

	t.Run("resolves outgoing mentions from the installation-scoped index", func(t *testing.T) {
		t.Parallel()
		adapter, _, st := newCache(t)
		mustAppend(t, st, "slack:user-by-name:T_A:alice", "U_ALICE_A")
		mustAppend(t, st, "slack:user-by-name:alice", "U_ALICE_GLOBAL")
		resolved, err := adapter.resolveOutgoingMentions(scoped(t.Context(), "xoxb-team-a", "T_A"), "hi @alice", "slack:C1:1.1")
		must.NoError(t, err)
		must.Eq(t, "hi <@U_ALICE_A>", resolved)
	})

	t.Run("invalidates the scoped cache entry on user_change", func(t *testing.T) {
		t.Parallel()
		adapter, _, st := newCache(t)
		ctx := scoped(t.Context(), "xoxb-team-a", "T_A")
		_ = adapter.lookupUser(ctx, "U1")
		raw, err := st.Get(t.Context(), "slack:user:T_A:U1")
		must.NoError(t, err)
		must.True(t, len(raw) > 0)
		adapter.handleUserChange(ctx, map[string]any{"type": "user_change", "user": map[string]any{"id": "U1"}})
		raw, err = st.Get(t.Context(), "slack:user:T_A:U1")
		must.NoError(t, err)
		must.Eq(t, 0, len(raw))
	})
}

func mustGet(t *testing.T, st chat.StateAdapter, key string) json.RawMessage {
	t.Helper()
	raw, err := st.Get(t.Context(), key)
	must.NoError(t, err)
	must.True(t, len(raw) > 0)
	return raw
}

func TestWithTokenEnterpriseContextInjection(t *testing.T) {
	t.Parallel()

	run := func(ctx context.Context, rc requestContext) context.Context {
		return withRequestContext(ctx, &rc)
	}

	t.Run("injects team_id on org-wide install calls", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{})
		result, err := adapter.withToken(run(t.Context(), requestContext{
			token: "xoxb-org", isEnterpriseInstall: true, teamID: "T_EVENT_1",
		}), map[string]any{"channel": "C1"})
		must.NoError(t, err)
		must.Eq(t, map[string]any{"channel": "C1", "team_id": "T_EVENT_1", "token": "xoxb-org"}, result)
	})

	t.Run("does not inject team_id for workspace installs", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{})
		result, err := adapter.withToken(run(t.Context(), requestContext{
			token: "xoxb-team", isEnterpriseInstall: false, teamID: "T_EVENT_1",
		}), map[string]any{"channel": "C1"})
		must.NoError(t, err)
		must.Eq(t, map[string]any{"channel": "C1", "token": "xoxb-team"}, result)
	})

	t.Run("does not override a caller-specified team_id", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{})
		result, err := adapter.withToken(run(t.Context(), requestContext{
			token: "xoxb-org", isEnterpriseInstall: true, teamID: "T_EVENT_1",
		}), map[string]any{"channel": "C1", "team_id": "T_EXPLICIT"})
		must.NoError(t, err)
		must.Eq(t, "T_EXPLICIT", result["team_id"])
	})

	t.Run("echoes context_team_id as client_context_team_id on calls to the originating channel", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{})
		result, err := adapter.withToken(run(t.Context(), requestContext{
			token: "xoxb-team", contextTeamID: "T_AWAY_HOST", contextChannel: "C1",
		}), map[string]any{"channel": "C1", "text": "hi"})
		must.NoError(t, err)
		must.Eq(t, "T_AWAY_HOST", result["client_context_team_id"])
	})

	t.Run("does not echo client_context_team_id to a different channel", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{})
		result, err := adapter.withToken(run(t.Context(), requestContext{
			token: "xoxb-team", contextTeamID: "T_AWAY_HOST", contextChannel: "C1",
		}), map[string]any{"channel": "C_OTHER", "text": "hi"})
		must.NoError(t, err)
		must.Eq(t, map[string]any{"channel": "C_OTHER", "text": "hi", "token": "xoxb-team"}, result)
	})

	t.Run("does not add client_context_team_id to non-channel calls", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{})
		result, err := adapter.withToken(run(t.Context(), requestContext{
			token: "xoxb-team", contextTeamID: "T_AWAY_HOST", contextChannel: "C1",
		}), map[string]any{"user": "U1"})
		must.NoError(t, err)
		must.Eq(t, map[string]any{"user": "U1", "token": "xoxb-team"}, result)
	})

	t.Run("captures teamId and contextTeamId in the event request context", func(t *testing.T) {
		t.Parallel()
		adapter := newMulti(t, Config{})
		_ = initMulti(t, adapter)
		must.NoError(t, adapter.SetInstallation(t.Context(), "E_ORG_1", Installation{
			BotToken: "xoxb-org", IsEnterpriseInstall: true,
		}))
		resolved, status := adapter.resolveEventRequestContext(t.Context(), map[string]any{
			"type": "event_callback", "team_id": "T_GRID_1",
			"enterprise_id": "E_ORG_1", "is_enterprise_install": true,
			"context_team_id": "T_AWAY_HOST",
			"event":           map[string]any{"type": "message", "channel": "C1", "ts": "1.1"},
		})
		must.Eq(t, "", status)
		must.Eq(t, "E_ORG_1", resolved.installationID)
		must.True(t, resolved.isEnterpriseInstall)
		must.Eq(t, "T_GRID_1", resolved.teamID)
		must.Eq(t, "T_AWAY_HOST", resolved.contextTeamID)
	})

	t.Run("injects team_id on chat.postMessage and client_context_team_id on an away-hosted reply", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock := newMultiAPI(t, Config{Token: api.StaticToken("xoxb-org")})
		apiMock.ok("chat.postMessage", map[string]any{"ts": "1.1", "channel": "C1"})
		apiMock.ok("conversations.replies", map[string]any{"messages": []any{}})
		ctx := withRequestContext(t.Context(), &requestContext{
			token: "xoxb-org", isEnterpriseInstall: true, teamID: "T_EVENT_1",
			contextTeamID: "T_AWAY_HOST", contextChannel: "C1",
		})
		_, err := adapter.PostMessage(ctx, "slack:C1:1.0", chat.PostableText("hi"))
		must.NoError(t, err)
		must.Eq(t, "T_EVENT_1", apiMock.last("chat.postMessage").Form.Get("team_id"))
		_, err = adapter.FetchMessages(ctx, "slack:C1:1.0", chat.FetchOptions{Direction: chat.FetchForward})
		must.NoError(t, err)
		must.Eq(t, "T_AWAY_HOST", apiMock.last("conversations.replies").Form.Get("client_context_team_id"))
	})

	t.Run("resolves the bot token once per slackCall", func(t *testing.T) {
		t.Parallel()
		tok := &countingToken{tok: "xoxb-once"}
		adapter, apiMock := newMultiAPI(t, Config{Token: tok})
		apiMock.ok("reactions.add", nil)
		must.NoError(t, adapter.AddReaction(t.Context(), "slack:C1:1.0", "1.0", chat.GetEmoji("thumbsup")))
		must.Eq(t, 1, tok.n)
	})
}

func TestInstallationLogValueRedactsBotToken(t *testing.T) {
	t.Parallel()
	inst := Installation{BotToken: "xoxb-secret", BotUserID: "U1", TeamName: "Acme"}
	s := inst.LogValue().String()
	must.False(t, strings.Contains(s, "xoxb-secret"))
	must.True(t, strings.Contains(s, "REDACTED"))
}

func TestOAuthCallbackResultLogValueRedactsBotToken(t *testing.T) {
	t.Parallel()
	result := OAuthCallbackResult{
		TeamID:       "T1",
		Installation: Installation{BotToken: "xoxb-secret", BotUserID: "U1"},
	}
	s := result.LogValue().String()
	must.False(t, strings.Contains(s, "xoxb-secret"))
	must.True(t, strings.Contains(s, "REDACTED"))
}
