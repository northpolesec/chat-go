// Ported from packages/adapter-slack/src/index.ts @ 6adca36 (chat v4.40.0)
// (partial: OAuth callback, installation store, withBotToken, Client/WebClient
// analog, withToken enterprise extras, unfurl-cache enrichLinks).
// Divergences: request context rides ctx values (not AsyncLocalStorage);
// WebClient → *api.Client (cached per token); oauth.v2.access via api.Client
// with an empty StaticToken (no bot token on the exchange; empty token
// omits Authorization); withBotToken is a ctx callback, not a generic ALS
// runner; EncryptedTokenData stays unexported (store encrypts/decrypts
// internally); Grid extras ride api.Client.Extra so every Call path gets
// them once (withToken stays the unit-testable map helper). enrichLinks
// returns (links, err) — upstream swallows state errors (index.ts:4481-4487);
// no production caller yet.
package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/northpolesec/chat-go/slack/api"
)

const (
	notInitializedMessage = "Adapter not initialized. Ensure chat.initialize() has been called first."
	oauthCredsRequired    = "clientId and clientSecret are required for OAuth. Pass them in createSlackAdapter()."
	oauthMissingCode      = "Missing 'code' query parameter in OAuth callback request."
	webClientNoToken      = "No bot token available. In multi-workspace mode, ensure the webhook is being processed or use `adapter.withBotToken(token, fn)` to bind a token explicitly."
)

// SlackOAuthCallbackOptions is upstream SlackOAuthCallbackOptions.
type SlackOAuthCallbackOptions struct {
	RedirectURI string
}

// OAuthCallbackResult is the return of HandleOAuthCallback.
type OAuthCallbackResult struct {
	TeamID              string
	EnterpriseID        string
	IsEnterpriseInstall bool
	Installation        Installation
}

// LogValue redacts Installation.BotToken (slog only resolves a top-level LogValuer).
func (r OAuthCallbackResult) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("TeamID", r.TeamID),
		slog.String("EnterpriseID", r.EnterpriseID),
		slog.Bool("IsEnterpriseInstall", r.IsEnterpriseInstall),
		slog.Attr{Key: "Installation", Value: r.Installation.LogValue()},
	)
}

// BotTokenOptions is the options bag on WithBotToken.
type BotTokenOptions struct {
	InstallationID string
}

type installationRecord struct {
	BotToken            any    `json:"botToken"`
	BotUserID           string `json:"botUserId,omitempty"`
	EnterpriseID        string `json:"enterpriseId,omitempty"`
	IsEnterpriseInstall bool   `json:"isEnterpriseInstall,omitempty"`
	TeamName            string `json:"teamName,omitempty"`
}

func (a *SlackAdapter) installationKey(teamID string) string {
	return a.installationKeyPrefix + ":" + teamID
}

func (a *SlackAdapter) requireState() (chat.StateAdapter, error) {
	if a.chat == nil {
		return nil, shared.NewValidationError(adapterName, notInitializedMessage)
	}
	st := a.chat.State()
	if st == nil {
		return nil, shared.NewValidationError(adapterName, notInitializedMessage)
	}
	return st, nil
}

// SetInstallation is adapter.setInstallation.
func (a *SlackAdapter) SetInstallation(ctx context.Context, teamID string, inst Installation) error {
	st, err := a.requireState()
	if err != nil {
		return err
	}
	rec := installationRecord{
		BotToken:            inst.BotToken,
		BotUserID:           inst.BotUserID,
		EnterpriseID:        inst.EnterpriseID,
		IsEnterpriseInstall: inst.IsEnterpriseInstall,
		TeamName:            inst.TeamName,
	}
	if len(a.encryptionKey) > 0 {
		enc, err := encryptToken(inst.BotToken, a.encryptionKey)
		if err != nil {
			return err
		}
		rec.BotToken = enc
	}
	if err := chat.StateSet(ctx, st, a.installationKey(teamID), rec, 0); err != nil {
		return err
	}
	a.logger.Info("Slack installation saved", "teamId", teamID, "teamName", inst.TeamName)
	return nil
}

// GetInstallation is adapter.getInstallation. Missing key → (nil, nil).
// Encrypted tokens that cannot be decrypted return an error (fail-closed).
func (a *SlackAdapter) GetInstallation(ctx context.Context, teamID string) (*Installation, error) {
	st, err := a.requireState()
	if err != nil {
		return nil, err
	}
	raw, err := st.Get(ctx, a.installationKey(teamID))
	if err != nil || len(raw) == 0 {
		return nil, err
	}
	var rec installationRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, err
	}
	inst := Installation{
		BotUserID:           rec.BotUserID,
		EnterpriseID:        rec.EnterpriseID,
		IsEnterpriseInstall: rec.IsEnterpriseInstall,
		TeamName:            rec.TeamName,
	}
	if isEncryptedTokenData(rec.BotToken) {
		if len(a.encryptionKey) == 0 {
			return nil, fmt.Errorf("encrypted installation cannot be decrypted: no encryption key")
		}
		tokRaw, err := json.Marshal(rec.BotToken)
		if err != nil {
			return nil, err
		}
		var enc encryptedTokenData
		if err := json.Unmarshal(tokRaw, &enc); err != nil {
			return nil, err
		}
		plain, err := decryptToken(enc, a.encryptionKey)
		if err != nil {
			return nil, err
		}
		inst.BotToken = plain
	} else if s, ok := rec.BotToken.(string); ok {
		inst.BotToken = s
	}
	return &inst, nil
}

// DeleteInstallation is adapter.deleteInstallation.
func (a *SlackAdapter) DeleteInstallation(ctx context.Context, teamID string) error {
	st, err := a.requireState()
	if err != nil {
		return err
	}
	if err := st.Delete(ctx, a.installationKey(teamID)); err != nil {
		return err
	}
	a.logger.Info("Slack installation deleted", "teamId", teamID)
	return nil
}

// HandleOAuthCallback is adapter.handleOAuthCallback.
func (a *SlackAdapter) HandleOAuthCallback(ctx context.Context, r *http.Request, opts *SlackOAuthCallbackOptions) (OAuthCallbackResult, error) {
	var zero OAuthCallbackResult
	if a.clientID == "" || a.clientSecret == "" {
		return zero, shared.NewValidationError(adapterName, oauthCredsRequired)
	}
	q := r.URL.Query()
	code := q.Get("code")
	if code == "" {
		return zero, shared.NewValidationError(adapterName, oauthMissingCode)
	}
	redirectURI := ""
	if opts != nil {
		redirectURI = opts.RedirectURI
	}
	if redirectURI == "" {
		redirectURI = q.Get("redirect_uri")
	}
	body := map[string]any{
		"client_id":     a.clientID,
		"client_secret": a.clientSecret,
		"code":          code,
	}
	if redirectURI != "" {
		body["redirect_uri"] = redirectURI
	}
	client := &api.Client{
		HTTPClient: a.httpClient,
		APIURL:     a.apiURL,
		Token:      api.StaticToken(""),
	}
	resp, err := client.Call(ctx, "oauth.v2.access", body, api.EncodingForm)
	if err != nil {
		return zero, err
	}
	var payload oauthAccessResponse
	if err := json.Unmarshal(resp.Raw, &payload); err != nil {
		return zero, err
	}
	isEnterprise := payload.IsEnterpriseInstall
	enterpriseID := ""
	if payload.Enterprise != nil {
		enterpriseID = payload.Enterprise.ID
	}
	installationID := ""
	if isEnterprise {
		installationID = enterpriseID
	} else if payload.Team != nil {
		installationID = payload.Team.ID
	}
	if !payload.OK || payload.AccessToken == "" || installationID == "" {
		missing := "missing access_token or team.id"
		if isEnterprise {
			missing = "missing access_token or enterprise.id"
		}
		msg := payload.Error
		if msg == "" {
			msg = missing
		}
		return zero, shared.NewAuthenticationError(adapterName, "Slack OAuth failed: "+msg)
	}
	teamName := ""
	if payload.Team != nil {
		teamName = payload.Team.Name
	}
	if teamName == "" && payload.Enterprise != nil {
		teamName = payload.Enterprise.Name
	}
	inst := Installation{
		BotToken:  payload.AccessToken,
		BotUserID: payload.BotUserID,
		TeamName:  teamName,
	}
	if enterpriseID != "" {
		inst.EnterpriseID = enterpriseID
	}
	if isEnterprise {
		inst.IsEnterpriseInstall = true
	}
	if err := a.SetInstallation(ctx, installationID, inst); err != nil {
		return zero, err
	}
	return OAuthCallbackResult{
		TeamID:              installationID,
		EnterpriseID:        enterpriseID,
		IsEnterpriseInstall: isEnterprise,
		Installation:        inst,
	}, nil
}

type oauthAccessResponse struct {
	OK                  bool   `json:"ok"`
	Error               string `json:"error"`
	AccessToken         string `json:"access_token"`
	BotUserID           string `json:"bot_user_id"`
	IsEnterpriseInstall bool   `json:"is_enterprise_install"`
	Team                *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"team"`
	Enterprise *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"enterprise"`
}

// WithBotToken is adapter.withBotToken. The callback runs with token (and
// optional installationId) on ctx.
func (a *SlackAdapter) WithBotToken(ctx context.Context, token string, fn func(context.Context), opts *BotTokenOptions) {
	rc := &requestContext{token: token}
	if opts != nil {
		rc.installationID = opts.InstallationID
	}
	fn(withRequestContext(ctx, rc))
}

// Client is the Go analog of adapter.client / adapter.webClient: an
// *api.Client bound to the current request token or the configured default.
func (a *SlackAdapter) Client(ctx context.Context) (*api.Client, error) {
	tok, err := a.clientToken(ctx)
	if err != nil {
		return nil, err
	}
	return a.clientForToken(tok), nil
}

// WebClient is the preferred name; Client is the deprecated alias.
func (a *SlackAdapter) WebClient(ctx context.Context) (*api.Client, error) {
	return a.Client(ctx)
}

func (a *SlackAdapter) clientToken(ctx context.Context) (string, error) {
	if rc := requestContextFrom(ctx); rc != nil && rc.token != "" {
		return rc.token, nil
	}
	if a.token != nil {
		return a.token.Token(ctx)
	}
	return "", shared.NewAuthenticationError(adapterName, webClientNoToken)
}

func (a *SlackAdapter) clientForToken(token string) *api.Client {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tokenClients == nil {
		a.tokenClients = map[string]*api.Client{}
	}
	if c, ok := a.tokenClients[token]; ok {
		return c
	}
	c := &api.Client{
		HTTPClient: a.httpClient,
		APIURL:     a.apiURL,
		Token:      api.StaticToken(token),
		Extra:      a.enterpriseExtras,
	}
	a.tokenClients[token] = c
	return c
}

func (a *SlackAdapter) resolveTokenForTeam(ctx context.Context, installationID string, isEnterpriseInstall bool) (tokenCtx, bool) {
	if a.installationProvider != nil {
		inst, err := a.installationProvider.GetInstallation(ctx, installationID, isEnterpriseInstall)
		if err != nil {
			a.logger.Error("Failed to resolve token for team",
				"installationId", installationID, "isEnterpriseInstall", isEnterpriseInstall, "error", err)
			return tokenCtx{}, false
		}
		if inst == nil {
			a.logger.Warn("No installation found from provider",
				"installationId", installationID, "isEnterpriseInstall", isEnterpriseInstall)
			return tokenCtx{}, false
		}
		return tokenCtx{token: inst.BotToken, botUserID: inst.BotUserID}, true
	}
	inst, err := a.GetInstallation(ctx, installationID)
	if err != nil {
		a.logger.Error("Failed to resolve token for team",
			"installationId", installationID, "isEnterpriseInstall", isEnterpriseInstall, "error", err)
		return tokenCtx{}, false
	}
	if inst == nil {
		a.logger.Warn("No installation found for team",
			"installationId", installationID, "isEnterpriseInstall", isEnterpriseInstall)
		return tokenCtx{}, false
	}
	return tokenCtx{token: inst.BotToken, botUserID: inst.BotUserID}, true
}

func (a *SlackAdapter) withToken(ctx context.Context, options map[string]any) (map[string]any, error) {
	tok, err := a.getToken(ctx)
	if err != nil {
		return nil, err
	}
	out := maps.Clone(a.enterpriseExtras(ctx, options))
	if out == nil {
		out = map[string]any{}
	}
	out["token"] = tok
	return out, nil
}

// enterpriseExtras injects Grid team_id / client_context_team_id from request
// ctx. No extras → returns options unchanged (existing exact-body tests stay
// green). Wired as api.Client.Extra so every Call path gets them once.
func (a *SlackAdapter) enterpriseExtras(ctx context.Context, options map[string]any) map[string]any {
	rc := requestContextFrom(ctx)
	if rc == nil {
		return options
	}
	needTeam := rc.isEnterpriseInstall && rc.teamID != ""
	if _, ok := options["team_id"]; ok {
		needTeam = false
	}
	needContext := false
	if rc.contextTeamID != "" {
		if ch, ok := options["channel"]; ok && ch == rc.contextChannel {
			if _, exists := options["client_context_team_id"]; !exists {
				needContext = true
			}
		}
	}
	if !needTeam && !needContext {
		return options
	}
	out := make(map[string]any, len(options)+2)
	maps.Copy(out, options)
	if needTeam {
		out["team_id"] = rc.teamID
	}
	if needContext {
		out["client_context_team_id"] = rc.contextTeamID
	}
	return out
}

func (a *SlackAdapter) enrichLinks(ctx context.Context, links []chat.LinkPreview, channelID, messageTS string) ([]chat.LinkPreview, error) {
	if a.chat == nil || channelID == "" || messageTS == "" || len(links) == 0 {
		return links, nil
	}
	allHave := true
	for _, l := range links {
		if l.Title == "" && l.FetchMessage == nil {
			allHave = false
			break
		}
	}
	if allHave {
		return links, nil
	}
	st := a.chat.State()
	if st == nil {
		return links, nil
	}
	stored, ok, err := chat.StateGet[map[string]map[string]string](ctx, st, a.unfurlCacheKey(ctx, channelID, messageTS))
	if err != nil || !ok {
		return links, err
	}
	out := make([]chat.LinkPreview, len(links))
	for i, link := range links {
		out[i] = link
		if link.Title != "" {
			continue
		}
		meta := stored[link.URL]
		if meta == nil {
			meta = stored[trailingSlashPattern.ReplaceAllString(link.URL, "")]
		}
		if meta == nil {
			meta = stored[link.URL+"/"]
		}
		if meta != nil {
			out[i].Title = meta["title"]
			out[i].Description = meta["description"]
			out[i].ImageURL = meta["imageUrl"]
			out[i].SiteName = meta["siteName"]
		}
	}
	return out, nil
}
