package github

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/internal/shared"
)

const (
	defaultUserName = "github-bot"

	envToken          = "GITHUB_TOKEN"
	envAppID          = "GITHUB_APP_ID"
	envPrivateKey     = "GITHUB_PRIVATE_KEY"
	envInstallationID = "GITHUB_INSTALLATION_ID"
	envWebhookSecret  = "GITHUB_WEBHOOK_SECRET"
	envBotUserName    = "GITHUB_BOT_USERNAME"
	envBotUserID      = "GITHUB_BOT_USER_ID"
	envAPIURL         = "GITHUB_API_URL"

	webhookSecretRequiredMessage = "webhookSecret or webhookVerifier is required. Set GITHUB_WEBHOOK_SECRET, provide webhookSecret in config, or provide a webhookVerifier."
	authRequiredMessage          = "Authentication is required. Set GITHUB_TOKEN or GITHUB_APP_ID/GITHUB_PRIVATE_KEY, or provide token/appId+privateKey in config."
)

// Config is the GitHub adapter constructor input (upstream GitHubAdapterConfig
// union flattened; see PORTING.md). Auth is Token (PAT or any resolver) or
// AppID+PrivateKey+InstallationID (single-tenant App). Env fallbacks apply
// only when no auth field is set.
type Config struct {
	Token           TokenSource
	AppID           string
	PrivateKey      []byte // PEM
	InstallationID  int64
	WebhookSecret   string
	WebhookVerifier func(r *http.Request, body []byte) error // wins over WebhookSecret
	UserName        string                                   // mention name; default "github-bot"
	BotUserID       string                                   // numeric id as a string; skips detection
	APIURL          string                                   // default https://api.github.com
	HTTPClient      HTTPDoer
	Logger          *slog.Logger
}

// LogValue redacts credential-bearing fields.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("Token", "REDACTED"),
		slog.String("PrivateKey", "REDACTED"),
		slog.String("WebhookSecret", "REDACTED"),
		slog.String("AppID", c.AppID),
		slog.Int64("InstallationID", c.InstallationID),
		slog.String("UserName", c.UserName),
		slog.String("BotUserID", c.BotUserID),
		slog.String("APIURL", c.APIURL),
	)
}

// Adapter is the GitHub chat.Adapter (upstream GitHubAdapter).
type Adapter struct {
	userName        string
	api             *client
	app             *AppInstallation // nil in PAT/resolver mode
	webhookSecret   string
	webhookVerifier func(r *http.Request, body []byte) error
	logger          *slog.Logger
	format          *githubFormatConverter

	mu        sync.Mutex
	botUserID string
	chat      chat.ChatInstance
}

// New is upstream's constructor / createGitHubAdapter.
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
		if v, err := strconv.Atoi(os.Getenv(envBotUserID)); err == nil {
			botUserID = strconv.Itoa(v)
		}
	}
	apiURL := cfg.APIURL
	if apiURL == "" {
		apiURL = os.Getenv(envAPIURL)
	}

	explicit := cfg.Token != nil || cfg.AppID != "" || len(cfg.PrivateKey) > 0
	var tok TokenSource
	var app *AppInstallation
	switch {
	case cfg.Token != nil:
		tok = cfg.Token
	case cfg.AppID != "" && len(cfg.PrivateKey) > 0 && cfg.InstallationID != 0:
		a, err := NewAppInstallation(cfg.AppID, cfg.PrivateKey, cfg.InstallationID, AppOptions{HTTPClient: cfg.HTTPClient, APIURL: apiURL})
		if err != nil {
			return nil, err
		}
		tok, app = a, a
	case explicit:
		return nil, shared.NewValidationError(adapterName, authRequiredMessage)
	default: // zero-config env fallback
		if t := os.Getenv(envToken); t != "" {
			tok = StaticToken(t)
			break
		}
		id, _ := strconv.ParseInt(os.Getenv(envInstallationID), 10, 64)
		appID, key := os.Getenv(envAppID), os.Getenv(envPrivateKey)
		if appID == "" || key == "" || id == 0 {
			return nil, shared.NewValidationError(adapterName, authRequiredMessage)
		}
		a, err := NewAppInstallation(appID, []byte(key), id, AppOptions{HTTPClient: cfg.HTTPClient, APIURL: apiURL})
		if err != nil {
			return nil, err
		}
		tok, app = a, a
	}
	return &Adapter{
		userName:        userName,
		api:             newClient(cfg.HTTPClient, apiURL, tok),
		app:             app,
		webhookSecret:   secret,
		webhookVerifier: verifier,
		logger:          logger,
		format:          newGitHubFormatConverter(),
		botUserID:       botUserID,
	}, nil
}

// Name is adapter.name.
func (a *Adapter) Name() string { return adapterName }

// UserName is adapter.userName (the @mention name).
func (a *Adapter) UserName() string { return a.userName }

// BotUserID implements chat.Adapter: the bot's numeric user id as a string,
// "" until detected.
func (a *Adapter) BotUserID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.botUserID
}

func (a *Adapter) setBotUserID(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.botUserID == "" && id != "" {
		a.botUserID = id
	}
}

// Initialize implements chat.Adapter: store the chat and detect the bot
// user id when not configured. Detection failure is a warning (upstream).
func (a *Adapter) Initialize(ctx context.Context, instance chat.ChatInstance) error {
	a.mu.Lock()
	a.chat = instance
	need := a.botUserID == ""
	a.mu.Unlock()
	if need {
		a.detectBotUserID(ctx)
	}
	return nil
}

// detectBotUserID is upstream detectBotUserId: GET /user for a PAT (or any
// resolver); for an App, GET /app with the App JWT then GET /users/{slug}[bot].
func (a *Adapter) detectBotUserID(ctx context.Context) {
	if a.app == nil {
		var u User
		if err := a.api.do(ctx, http.MethodGet, "/user", nil, nil, &u); err != nil {
			a.logger.Warn("Could not auto-detect GitHub bot user ID", "error", err)
			return
		}
		a.setBotUserID(strconv.FormatInt(u.ID, 10))
		a.logger.Info("GitHub bot user ID auto-detected", "botUserId", u.ID, "login", u.Login)
		return
	}
	jwt, err := a.app.AppJWT()
	if err != nil {
		a.logger.Warn("Could not auto-detect GitHub bot user ID", "error", err)
		return
	}
	var app struct {
		Slug string `json:"slug"`
	}
	if err := newClient(a.api.http, a.api.base, StaticToken(jwt)).do(ctx, http.MethodGet, "/app", nil, nil, &app); err != nil || app.Slug == "" {
		a.logger.Warn("Could not auto-detect GitHub bot user ID", "error", err)
		return
	}
	var u User
	if err := a.api.do(ctx, http.MethodGet, "/users/"+url.PathEscape(app.Slug+"[bot]"), nil, nil, &u); err != nil {
		a.logger.Warn("Could not auto-detect GitHub bot user ID", "error", err)
		return
	}
	a.setBotUserID(strconv.FormatInt(u.ID, 10))
	a.logger.Info("GitHub bot user ID auto-detected via app slug", "botUserId", u.ID, "login", app.Slug+"[bot]")
}

// captureBotUserID is upstream captureBotUserId: learn the id from a
// comment the bot just created.
func (a *Adapter) captureBotUserID(u User) {
	if u.ID != 0 {
		a.setBotUserID(strconv.FormatInt(u.ID, 10))
	}
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
	return "", shared.NewValidationError(adapterName, fmt.Sprintf("EncodeThreadID expects a github.ThreadID, got %T", platformData))
}

// DecodeThreadID implements chat.Adapter; returns a ThreadID value.
func (a *Adapter) DecodeThreadID(threadID string) (any, error) { return decodeThreadID(threadID) }

// ChannelIDFromThreadID implements chat.Adapter: github:{owner}/{repo}.
func (a *Adapter) ChannelIDFromThreadID(threadID string) string {
	id, err := decodeThreadID(threadID)
	if err != nil {
		return ""
	}
	return adapterName + ":" + id.Owner + "/" + id.Repo
}
