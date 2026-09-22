// Ported from packages/adapter-linear/src/index.ts (client credentials token exchange) @ vercel/chat v4.40.0. Divergences: fixed scopes; cached token re-minted inside tokenRefreshMargin of expiry and on Invalidate(); no multi-tenant installations.
package linear

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/northpolesec/chat-go/internal/shared"
)

const (
	defaultAPIURL = "https://api.linear.app"
	// scopes never varies between mints: Linear revokes every live app token
	// when a client_credentials request carries a different scope set.
	scopes = "read,write,app:mentionable,app:assignable"
	// tokenRefreshMargin: client_credentials tokens live 30 days; re-mint
	// when less than this remains so a turn never straddles expiry.
	tokenRefreshMargin = time.Hour
	maxResponseBytes   = 8 << 20
)

// HTTPDoer is satisfied by *http.Client.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// defaultHTTPClient is package-owned, never http.DefaultClient.
var defaultHTTPClient = &http.Client{}

// TokenSource resolves the bearer token per API call.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

type staticToken string

// StaticToken returns a TokenSource that always yields tok.
func StaticToken(tok string) TokenSource { return staticToken(tok) }

func (s staticToken) Token(context.Context) (string, error) { return string(s), nil }

// invalidator is what the API client asks a TokenSource for after an
// authentication failure; StaticToken has none.
type invalidator interface{ Invalidate() }

// ClientCredentialsOptions configures the token exchange.
type ClientCredentialsOptions struct {
	APIURL     string   // "" → defaultAPIURL
	HTTPClient HTTPDoer // nil → defaultHTTPClient
}

// ClientCredentials mints app-actor tokens with the OAuth client_credentials
// grant (POST /oauth/token) and caches one until tokenRefreshMargin before
// expiry. The mutex guards only the cache; the network call runs outside it,
// so a lost race mints twice (harmless, within Linear's 1000-token cap).
type ClientCredentials struct {
	clientID     string
	clientSecret string
	http         HTTPDoer
	apiURL       string
	now          func() time.Time

	mu      sync.Mutex
	token   string
	expires time.Time
}

// NewClientCredentials returns a TokenSource for one OAuth app.
func NewClientCredentials(clientID, clientSecret string, opts ClientCredentialsOptions) *ClientCredentials {
	c := &ClientCredentials{clientID: clientID, clientSecret: clientSecret, http: opts.HTTPClient, apiURL: strings.TrimRight(opts.APIURL, "/"), now: time.Now}
	if c.http == nil {
		c.http = defaultHTTPClient
	}
	if c.apiURL == "" {
		c.apiURL = defaultAPIURL
	}
	return c
}

// Token implements TokenSource.
func (c *ClientCredentials) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	tok, exp := c.token, c.expires
	c.mu.Unlock()
	if tok != "" && c.now().Add(tokenRefreshMargin).Before(exp) {
		return tok, nil
	}
	tok, exp, err := c.mint(ctx)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.token, c.expires = tok, exp
	c.mu.Unlock()
	return tok, nil
}

// Invalidate drops the cached token; the next Token call mints.
func (c *ClientCredentials) Invalidate() {
	c.mu.Lock()
	c.token, c.expires = "", time.Time{}
	c.mu.Unlock()
}

func (c *ClientCredentials) mint(ctx context.Context) (string, time.Time, error) {
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"scope":         {scopes},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", time.Time{}, shared.NewNetworkError(adapterName, "token request failed", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", time.Time{}, err
	}
	if resp.StatusCode/100 != 2 {
		var e struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		_ = json.Unmarshal(body, &e)
		msg := e.Description
		if msg == "" {
			msg = e.Error
		}
		return "", time.Time{}, shared.NewAuthenticationError(adapterName, "client_credentials token: "+msg)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.AccessToken == "" {
		return "", time.Time{}, shared.NewNetworkError(adapterName, "token response was not JSON with an access_token", err)
	}
	return out.AccessToken, c.now().Add(time.Duration(out.ExpiresIn) * time.Second), nil
}
