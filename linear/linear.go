// Ported from packages/adapter-linear/src/index.ts (constructor, initialize,
// thread ids) @ vercel/chat v4.40.0. Divergences: agent-sessions mode only;
// no apiKey/accessToken/multi-tenant/Connect flavours; Initialize fails
// (not warns) when the viewer query fails.
package linear

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/internal/shared"
)

const (
	defaultUserName = "linear-bot"

	envClientID      = "LINEAR_CLIENT_ID"
	envClientSecret  = "LINEAR_CLIENT_SECRET"
	envWebhookSecret = "LINEAR_WEBHOOK_SECRET"
	envBotUserName   = "LINEAR_BOT_USERNAME"
	envBotUserID     = "LINEAR_BOT_USER_ID"
	envAPIURL        = "LINEAR_API_URL"

	webhookSecretRequiredMessage = "webhookSecret or webhookVerifier is required. Set LINEAR_WEBHOOK_SECRET, provide webhookSecret in config, or provide a webhookVerifier."
	authRequiredMessage          = "Authentication is required. Set LINEAR_CLIENT_ID and LINEAR_CLIENT_SECRET, or provide token or clientId+clientSecret in config."

	viewerQuery = `query LinearAdapterViewer { viewer { id displayName } }`
	opViewer    = "LinearAdapterViewer"
)

// Config is the Linear adapter constructor input. Auth is Token (any
// TokenSource; tests use StaticToken) or ClientID+ClientSecret
// (client_credentials). Env fallbacks apply only when no auth field is set.
type Config struct {
	Token           TokenSource
	ClientID        string
	ClientSecret    string
	WebhookSecret   string
	WebhookVerifier func(r *http.Request, body []byte) error // wins over WebhookSecret; the timestamp window still applies
	UserName        string                                   // display name; default "linear-bot"
	BotUserID       string                                   // app user id; skips the viewer query
	APIURL          string                                   // default https://api.linear.app
	HTTPClient      HTTPDoer
	Logger          *slog.Logger
}

// LogValue redacts credential-bearing fields.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("Token", "REDACTED"),
		slog.String("ClientSecret", "REDACTED"),
		slog.String("WebhookSecret", "REDACTED"),
		slog.String("ClientID", c.ClientID),
		slog.String("UserName", c.UserName),
		slog.String("BotUserID", c.BotUserID),
		slog.String("APIURL", c.APIURL),
	)
}

// Adapter is the Linear chat.Adapter (upstream LinearAdapter).
type Adapter struct {
	userName        string
	api             *client
	webhookSecret   string
	webhookVerifier func(r *http.Request, body []byte) error
	logger          *slog.Logger
	format          *shared.MarkdownFormatConverter

	mu        sync.Mutex
	botUserID string
	chat      chat.ChatInstance
}

// New is upstream's constructor / createLinearAdapter.
func New(cfg Config) (*Adapter, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	verifier := cfg.WebhookVerifier
	secret := cfg.WebhookSecret
	if verifier != nil {
		secret = ""
	} else if secret == "" {
		secret = os.Getenv(envWebhookSecret)
	}
	if secret == "" && verifier == nil {
		return nil, shared.NewValidationError(adapterName, webhookSecretRequiredMessage)
	}
	userName := cfg.UserName
	if userName == "" {
		userName = os.Getenv(envBotUserName)
	}
	if userName == "" {
		userName = defaultUserName
	}
	botUserID := cfg.BotUserID
	if botUserID == "" {
		botUserID = os.Getenv(envBotUserID)
	}
	apiURL := cfg.APIURL
	if apiURL == "" {
		apiURL = os.Getenv(envAPIURL)
	}

	explicit := cfg.Token != nil || cfg.ClientID != "" || cfg.ClientSecret != ""
	var tok TokenSource
	switch {
	case cfg.Token != nil:
		tok = cfg.Token
	case cfg.ClientID != "" && cfg.ClientSecret != "":
		tok = NewClientCredentials(cfg.ClientID, cfg.ClientSecret, ClientCredentialsOptions{APIURL: apiURL, HTTPClient: cfg.HTTPClient})
	case explicit:
		return nil, shared.NewValidationError(adapterName, authRequiredMessage)
	default:
		id, sec := os.Getenv(envClientID), os.Getenv(envClientSecret)
		if id == "" || sec == "" {
			return nil, shared.NewValidationError(adapterName, authRequiredMessage)
		}
		tok = NewClientCredentials(id, sec, ClientCredentialsOptions{APIURL: apiURL, HTTPClient: cfg.HTTPClient})
	}
	return &Adapter{
		userName:        userName,
		api:             newClient(cfg.HTTPClient, apiURL, tok),
		webhookSecret:   secret,
		webhookVerifier: verifier,
		logger:          logger,
		format:          shared.NewMarkdownFormatConverter(),
		botUserID:       botUserID,
	}, nil
}

// Name is adapter.name.
func (a *Adapter) Name() string { return adapterName }

// UserName is the display name; Linear routes by app user id, so it is
// informational.
func (a *Adapter) UserName() string { return a.userName }

// BotUserID implements chat.Adapter: the app user id, "" until Initialize.
func (a *Adapter) BotUserID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.botUserID
}

func (a *Adapter) chatInstance() chat.ChatInstance {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.chat
}

// Initialize implements chat.Adapter: store the chat and resolve the app
// user id with `viewer { id }` when not configured. Divergence from
// upstream: a failed viewer query is an error, because every inbound event
// is filtered on this id.
func (a *Adapter) Initialize(ctx context.Context, instance chat.ChatInstance) error {
	a.mu.Lock()
	a.chat = instance
	need := a.botUserID == ""
	a.mu.Unlock()
	if !need {
		return nil
	}
	id, name, err := a.Probe(ctx)
	if err != nil {
		return fmt.Errorf("linear: resolve app user: %w", err)
	}
	a.mu.Lock()
	a.botUserID = id
	a.mu.Unlock()
	a.logger.Info("Linear app user resolved", "botUserId", id, "displayName", name)
	return nil
}

// Probe runs the viewer query and returns the app user id and display name.
// Initialize calls it when BotUserID is unset.
func (a *Adapter) Probe(ctx context.Context) (id, displayName string, err error) {
	var out struct {
		Viewer struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
		} `json:"viewer"`
	}
	if err := a.api.do(ctx, opViewer, viewerQuery, nil, &out); err != nil {
		return "", "", err
	}
	if out.Viewer.ID == "" {
		return "", "", shared.NewValidationError(adapterName, "viewer query returned no id")
	}
	return out.Viewer.ID, out.Viewer.DisplayName, nil
}

// EncodeThreadID implements chat.Adapter; platformData is ThreadID or *ThreadID.
func (a *Adapter) EncodeThreadID(platformData any) (string, error) {
	switch v := platformData.(type) {
	case ThreadID:
		return encodeThreadID(v)
	case *ThreadID:
		if v != nil {
			return encodeThreadID(*v)
		}
	}
	return "", shared.NewValidationError(adapterName, fmt.Sprintf("EncodeThreadID expects a linear.ThreadID, got %T", platformData))
}

// DecodeThreadID implements chat.Adapter; returns a ThreadID value.
func (a *Adapter) DecodeThreadID(threadID string) (any, error) { return decodeThreadID(threadID) }

// ChannelIDFromThreadID implements chat.Adapter: linear:{issueId}.
func (a *Adapter) ChannelIDFromThreadID(threadID string) string {
	id, err := decodeThreadID(threadID)
	if err != nil {
		return ""
	}
	return adapterName + ":" + id.IssueID
}
