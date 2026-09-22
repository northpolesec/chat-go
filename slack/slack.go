// Ported from packages/adapter-slack/src/index.ts @ 6adca36 (chat v4.40.0)
// (partial: construction, config, thread IDs, parseMessage, core messaging;
// Stream lives in slack_stream.go).
// Divergences: createSlackAdapter → New; botToken → api.TokenSource;
// logger → *slog.Logger (nil → slog.New(slog.DiscardHandler)); default-true
// flags → DisableNativeStreaming only (default-true); agentView stays AgentView
// (upstream default false, so inversion does not apply); webhookVerifier is
// func(http.Header, []byte) error; socket fields and webClientOptions dropped;
// Message.Raw is the inner Slack event; Formatted is goldmark; InstallationProvider
// is the external lookup (Task 30); RawMessage.Channel holds the adapter thread ID
// (upstream RawMessage.threadId); PostEphemeral usedFallback is always false and
// is not on chat.RawMessage; StartTyping "" is unset (JS ""→active cannot be
// expressed); files go through api.Client.UploadFiles, not files.uploadV2.
package slack

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/northpolesec/chat-go/slack/api"
)

const (
	adapterName                    = "slack"
	defaultUserName                = "bot"
	defaultInstallationKeyPrefix   = "slack:installation"
	defaultStreamSegmentMaxAge     = 240 * time.Second
	slackSystemUserID              = "USLACK"
	signingSecretRequiredMessage   = "signingSecret or webhookVerifier is required for webhook mode. Set SLACK_SIGNING_SECRET, provide signingSecret in config, or provide a webhookVerifier."
	noBotTokenMessage              = "No bot token available. In multi-workspace mode, ensure the webhook is being processed."
	invalidThreadIDPrefix          = "Invalid Slack thread ID: "
	suggestedPromptsBothSetMessage = "suggestedPrompts and suggestedPromptsResolver cannot both be set"
	sessionTitleBothSetMessage     = "sessionTitle and sessionTitleResolver cannot both be set"
	envSigningSecret               = "SLACK_SIGNING_SECRET"
	envBotToken                    = "SLACK_BOT_TOKEN"
	envAPIURL                      = "SLACK_API_URL"
	envClientID                    = "SLACK_CLIENT_ID"
	envClientSecret                = "SLACK_CLIENT_SECRET"
	envEncryptionKey               = "SLACK_ENCRYPTION_KEY"
)

// FeedbackButtonsOptions is upstream SlackFeedbackButtonsOptions.
type FeedbackButtonsOptions struct {
	ActionID      string
	NegativeLabel string
	NegativeValue string
	PositiveLabel string
	PositiveValue string
}

// SessionTitleContext is upstream SlackSessionTitleContext.
type SessionTitleContext struct {
	ChannelID string
	Text      string
	ThreadTS  string
	UserID    string
}

// SessionTitleResolver is the function form of SlackSessionTitle.
type SessionTitleResolver func(ctx context.Context, in SessionTitleContext) (string, error)

// Installation is upstream SlackInstallation.
type Installation struct {
	BotToken            string `json:"botToken"`
	BotUserID           string `json:"botUserId,omitempty"`
	EnterpriseID        string `json:"enterpriseId,omitempty"`
	IsEnterpriseInstall bool   `json:"isEnterpriseInstall,omitempty"`
	TeamName            string `json:"teamName,omitempty"`
}

// LogValue redacts the bot token.
func (i Installation) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("BotToken", "REDACTED"),
		slog.String("BotUserID", i.BotUserID),
		slog.String("EnterpriseID", i.EnterpriseID),
		slog.Bool("IsEnterpriseInstall", i.IsEnterpriseInstall),
		slog.String("TeamName", i.TeamName),
	)
}

// InstallationProvider is the external getInstallation override
// (upstream config.installationProvider). Read-only: Set/Delete/OAuth
// still write the internal state store.
type InstallationProvider interface {
	GetInstallation(ctx context.Context, installationID string, isEnterpriseInstall bool) (*Installation, error)
}

// Config is the Slack adapter constructor input (brief-normative).
type Config struct {
	Token                    api.TokenSource
	SigningSecret            string
	BotUserID                string
	UserName                 string
	APIURL                   string
	HTTPClient               api.HTTPDoer
	Logger                   *slog.Logger
	ClientID                 string
	ClientSecret             string
	EncryptionKey            string
	InstallationKeyPrefix    string
	InstallationProvider     InstallationProvider
	DisableNativeStreaming   bool
	AgentView                bool // upstream agentView ?? false (not inverted)
	FeedbackButtons          *FeedbackButtonsOptions
	LoadingMessages          []string
	SessionTitle             *bool
	SessionTitleResolver     SessionTitleResolver
	SuggestedPrompts         *chat.SuggestedPrompts
	SuggestedPromptsResolver SuggestedPromptsResolver
	StreamSegmentMaxAge      time.Duration
	WebhookVerifier          func(header http.Header, body []byte) error
}

// LogValue redacts credential-bearing fields.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("Token", "REDACTED"),
		slog.String("SigningSecret", "REDACTED"),
		slog.String("ClientSecret", "REDACTED"),
		slog.String("EncryptionKey", "REDACTED"),
		slog.String("BotUserID", c.BotUserID),
		slog.String("UserName", c.UserName),
		slog.String("APIURL", c.APIURL),
		slog.String("ClientID", c.ClientID),
		slog.String("InstallationKeyPrefix", c.InstallationKeyPrefix),
		slog.Bool("DisableNativeStreaming", c.DisableNativeStreaming),
		slog.Bool("AgentView", c.AgentView),
	)
}

// ThreadID is upstream SlackThreadId.
type ThreadID struct {
	Channel  string
	ThreadTS string
}

// SlackAdapter is the Slack chat.Adapter (Task 25 slice).
type SlackAdapter struct {
	name     string
	userName string

	token           api.TokenSource
	signingSecret   string
	webhookVerifier func(http.Header, []byte) error
	httpClient      api.HTTPDoer
	apiURL          string
	logger          *slog.Logger

	clientID              string
	clientSecret          string
	encryptionKey         []byte
	installationKeyPrefix string
	installationProvider  InstallationProvider

	disableNativeStreaming   bool
	agentView                bool
	supportsTurnCancellation bool
	feedbackButtons          *FeedbackButtonsOptions
	loadingMessages          []string
	sessionTitle             bool
	sessionTitleResolver     SessionTitleResolver
	suggestedPrompts         *chat.SuggestedPrompts
	suggestedPromptsResolver SuggestedPromptsResolver
	streamSegmentMaxAge      time.Duration

	format *slackFormatConverter

	botUserID string
	botID     string

	chat chat.ChatInstance

	mu sync.Mutex

	userCache       map[string]userInfo
	userCacheMu     sync.Mutex
	fileTransport   shared.AttachmentTransport
	externalChans   map[string]struct{}
	externalChansMu sync.Mutex

	// nativeBroken latches when the workspace rejects native streaming with a
	// permanent platform error (unknown_method, …). atomic: no mutex across
	// the network call that discovers brokenness.
	nativeBroken atomic.Bool
	// now is Date.now(); tests inject a fake clock. nil → time.Now.
	now func() time.Time
	// streamBufferSize is ChatStreamer's buffer_size (default 256). Tests
	// that mock flush-every-append set this to 1.
	streamBufferSize int

	// pending tracks goroutines HandleWebhook detaches (reactions,
	// user_change, agent-view DM routing, agent_session_stopped). Tests
	// call waitPending; production callers treat them as fire-and-forget
	// matching upstream waitUntil / unawaited promises.
	pending sync.WaitGroup
	// optionsLoadTimeout is Slack's 2.5s block_suggestion budget. 0 → 2500ms.
	// Tests that pin the timeout set a shorter value (no time.Sleep).
	optionsLoadTimeout time.Duration

	// tokenClients caches *api.Client per resolved bot token (WebClient analog).
	tokenClients map[string]*api.Client
}

type requestContext struct {
	token               string
	botUserID           string
	installationID      string
	enterpriseID        string
	isEnterpriseInstall bool
	teamID              string
	contextTeamID       string
	contextChannel      string
}

type requestContextKey struct{}

func withRequestContext(ctx context.Context, rc *requestContext) context.Context {
	if rc == nil {
		return ctx
	}
	return context.WithValue(ctx, requestContextKey{}, rc)
}

func requestContextFrom(ctx context.Context) *requestContext {
	rc, _ := ctx.Value(requestContextKey{}).(*requestContext)
	return rc
}

type userInfo struct {
	AvatarURL   string `json:"avatarUrl,omitempty"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email,omitempty"`
	IsBot       bool   `json:"isBot,omitempty"`
	RealName    string `json:"realName"`
	Tz          string `json:"tz,omitempty"`
}

// New constructs a SlackAdapter (upstream constructor / createSlackAdapter).
func New(cfg Config) (*SlackAdapter, error) {
	verifier := cfg.WebhookVerifier
	signingSecret := cfg.SigningSecret
	if verifier == nil && signingSecret == "" {
		signingSecret = os.Getenv(envSigningSecret)
	}
	if verifier != nil {
		signingSecret = ""
	}
	if signingSecret == "" && verifier == nil {
		return nil, shared.NewValidationError(adapterName, signingSecretRequiredMessage)
	}

	zeroConfig := cfg.SigningSecret == "" && cfg.Token == nil && cfg.ClientID == "" && cfg.ClientSecret == ""

	token := cfg.Token
	if token == nil && zeroConfig {
		if env := os.Getenv(envBotToken); env != "" {
			token = api.StaticToken(env)
		}
	}

	apiURL := cfg.APIURL
	if apiURL == "" {
		apiURL = os.Getenv(envAPIURL)
	}

	clientID := cfg.ClientID
	clientSecret := cfg.ClientSecret
	if zeroConfig {
		if clientID == "" {
			clientID = os.Getenv(envClientID)
		}
		if clientSecret == "" {
			clientSecret = os.Getenv(envClientSecret)
		}
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	userName := cfg.UserName
	if userName == "" {
		userName = defaultUserName
	}

	prefix := cfg.InstallationKeyPrefix
	if prefix == "" {
		prefix = defaultInstallationKeyPrefix
	}

	segmentAge := cfg.StreamSegmentMaxAge
	if segmentAge <= 0 {
		segmentAge = defaultStreamSegmentMaxAge
	}

	var encKey []byte
	encRaw := cfg.EncryptionKey
	if encRaw == "" {
		encRaw = os.Getenv(envEncryptionKey)
	}
	if encRaw != "" {
		key, err := decodeKey(encRaw)
		if err != nil {
			return nil, err
		}
		encKey = key
	}

	if cfg.SuggestedPrompts != nil && cfg.SuggestedPromptsResolver != nil {
		return nil, shared.NewValidationError(adapterName, suggestedPromptsBothSetMessage)
	}
	if cfg.SessionTitle != nil && cfg.SessionTitleResolver != nil {
		return nil, shared.NewValidationError(adapterName, sessionTitleBothSetMessage)
	}

	sessionTitle := cfg.AgentView
	if cfg.SessionTitle != nil {
		sessionTitle = *cfg.SessionTitle
	}

	return &SlackAdapter{
		name:                     adapterName,
		userName:                 userName,
		token:                    token,
		signingSecret:            signingSecret,
		webhookVerifier:          verifier,
		httpClient:               cfg.HTTPClient,
		apiURL:                   apiURL,
		logger:                   logger,
		clientID:                 clientID,
		clientSecret:             clientSecret,
		encryptionKey:            encKey,
		installationKeyPrefix:    prefix,
		installationProvider:     cfg.InstallationProvider,
		disableNativeStreaming:   cfg.DisableNativeStreaming,
		agentView:                cfg.AgentView,
		supportsTurnCancellation: cfg.AgentView,
		feedbackButtons:          cfg.FeedbackButtons,
		loadingMessages:          cfg.LoadingMessages,
		sessionTitle:             sessionTitle,
		sessionTitleResolver:     cfg.SessionTitleResolver,
		suggestedPrompts:         cfg.SuggestedPrompts,
		suggestedPromptsResolver: cfg.SuggestedPromptsResolver,
		streamSegmentMaxAge:      segmentAge,
		format:                   newSlackFormatConverter(),
		botUserID:                cfg.BotUserID,
		userCache:                map[string]userInfo{},
		externalChans:            map[string]struct{}{},
		now:                      time.Now,
		streamBufferSize:         nativeStreamBufferSize,
		tokenClients:             map[string]*api.Client{},
	}, nil
}

// Name is adapter.name.
func (a *SlackAdapter) Name() string { return a.name }

// UserName is adapter.userName.
func (a *SlackAdapter) UserName() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.userName
}

// BotUserID is adapter.botUserId (config / auth.test). Request-scoped
// overrides live on ctx via withRequestContext; see botUserIDFrom.
func (a *SlackAdapter) BotUserID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.botUserID
}

func (a *SlackAdapter) botUserIDFrom(ctx context.Context) string {
	if rc := requestContextFrom(ctx); rc != nil && rc.botUserID != "" {
		return rc.botUserID
	}
	return a.BotUserID()
}

func (a *SlackAdapter) getToken(ctx context.Context) (string, error) {
	if rc := requestContextFrom(ctx); rc != nil && rc.token != "" {
		return rc.token, nil
	}
	if a.token != nil {
		return a.token.Token(ctx)
	}
	return "", shared.NewAuthenticationError(adapterName, noBotTokenMessage)
}

func (a *SlackAdapter) apiClient() *api.Client {
	return &api.Client{
		HTTPClient: a.httpClient,
		APIURL:     a.apiURL,
		Token:      tokenFunc(a.getToken),
		Extra:      a.enterpriseExtras,
	}
}

type tokenFunc func(context.Context) (string, error)

func (f tokenFunc) Token(ctx context.Context) (string, error) { return f(ctx) }

// Initialize stores the chat instance and, in single-workspace mode, calls auth.test.
// Identity field writes (botUserID/botID/userName) take a.mu; the lock is not held across auth.test.
func (a *SlackAdapter) Initialize(ctx context.Context, instance chat.ChatInstance) error {
	a.chat = instance
	a.mu.Lock()
	needAuth := a.token != nil && a.botUserID == ""
	a.mu.Unlock()
	if !needAuth {
		if a.token == nil {
			a.logger.Info("Slack adapter initialized in multi-workspace mode")
		}
		return nil
	}
	resp, err := a.apiClient().Call(ctx, "auth.test", map[string]any{}, api.EncodingForm)
	if err != nil || !resp.OK {
		a.logger.Warn("Could not fetch bot user ID", "error", err)
		return nil
	}
	var payload struct {
		BotID  string `json:"bot_id"`
		User   string `json:"user"`
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(resp.Raw, &payload); err != nil {
		a.logger.Warn("Could not fetch bot user ID", "error", err)
		return nil
	}
	a.mu.Lock()
	a.botUserID = payload.UserID
	a.botID = payload.BotID
	if payload.User != "" {
		a.userName = payload.User
	}
	loggedUser, loggedBot := a.botUserID, a.botID
	a.mu.Unlock()
	a.logger.Info("Slack auth completed", "botUserId", loggedUser, "botId", loggedBot)
	return nil
}

func (a *SlackAdapter) encodeThreadID(id ThreadID) string {
	return adapterName + ":" + id.Channel + ":" + id.ThreadTS
}

// EncodeThreadID implements chat.Adapter.
func (a *SlackAdapter) EncodeThreadID(platformData any) (string, error) {
	switch v := platformData.(type) {
	case ThreadID:
		return a.encodeThreadID(v), nil
	case *ThreadID:
		if v == nil {
			return "", shared.NewValidationError(adapterName, invalidThreadIDPrefix+"<nil>")
		}
		return a.encodeThreadID(*v), nil
	default:
		return "", shared.NewValidationError(adapterName, invalidThreadIDPrefix+"unsupported type")
	}
}

func (a *SlackAdapter) decodeThreadID(threadID string) (ThreadID, error) {
	parts := strings.Split(threadID, ":")
	if len(parts) < 2 || len(parts) > 3 || parts[0] != adapterName {
		return ThreadID{}, shared.NewValidationError(adapterName, invalidThreadIDPrefix+threadID)
	}
	ts := ""
	if len(parts) == 3 {
		ts = parts[2]
	}
	return ThreadID{Channel: parts[1], ThreadTS: ts}, nil
}

// DecodeThreadID implements chat.Adapter.
func (a *SlackAdapter) DecodeThreadID(threadID string) (any, error) {
	id, err := a.decodeThreadID(threadID)
	if err != nil {
		return nil, err
	}
	return id, nil
}

// ChannelIDFromThreadID is adapter.channelIdFromThreadId.
func (a *SlackAdapter) ChannelIDFromThreadID(threadID string) string {
	id, err := a.decodeThreadID(threadID)
	if err != nil {
		return ""
	}
	return adapterName + ":" + id.Channel
}

// IsDM is adapter.isDM (channel IDs starting with D).
func (a *SlackAdapter) IsDM(threadID string) bool {
	id, err := a.decodeThreadID(threadID)
	if err != nil {
		return false
	}
	return strings.HasPrefix(id.Channel, "D")
}

// GetChannelVisibility is adapter.getChannelVisibility.
func (a *SlackAdapter) GetChannelVisibility(threadID string) chat.ChannelVisibility {
	id, err := a.decodeThreadID(threadID)
	if err != nil {
		return chat.ChannelUnknown
	}
	if a.isExternalChannel(id.Channel) {
		return chat.ChannelExternal
	}
	if strings.HasPrefix(id.Channel, "G") || strings.HasPrefix(id.Channel, "D") {
		return chat.ChannelPrivate
	}
	if strings.HasPrefix(id.Channel, "C") {
		return chat.ChannelWorkspace
	}
	return chat.ChannelUnknown
}

func (a *SlackAdapter) markExternalChannel(channel string) {
	a.externalChansMu.Lock()
	a.externalChans[channel] = struct{}{}
	a.externalChansMu.Unlock()
}

func (a *SlackAdapter) isExternalChannel(channel string) bool {
	a.externalChansMu.Lock()
	_, ok := a.externalChans[channel]
	a.externalChansMu.Unlock()
	return ok
}

func (a *SlackAdapter) isMessageFromSelf(ctx context.Context, event SlackEvent) bool {
	userID := event.User
	if userID == "" && event.BotProfile != nil {
		userID = event.BotProfile.UserID
	}
	if bot := a.botUserIDFrom(ctx); bot != "" && userID == bot {
		return true
	}
	a.mu.Lock()
	botID := a.botID
	a.mu.Unlock()
	return botID != "" && event.BotID != "" && event.BotID == botID
}
