package github

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/northpolesec/chat-go/internal/shared"
)

const (
	defaultAPIURL = "https://api.github.com"
	// GitHub rejects an App JWT whose exp is more than 10 minutes out and
	// tolerates iat up to 60 s in the past for clock drift.
	jwtBackdate = 60 * time.Second
	jwtLifetime = 9 * time.Minute
	// Installation tokens live one hour; re-mint when less than this remains
	// so a turn never straddles expiry.
	tokenRefreshMargin = 5 * time.Minute
)

// HTTPDoer is satisfied by *http.Client.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// defaultHTTPClient is package-owned, never http.DefaultClient.
var defaultHTTPClient = &http.Client{}

// TokenSource resolves the bearer credential per API call. StaticToken
// covers a PAT and upstream's string installationToken; a custom
// implementation covers upstream's resolver form; AppInstallation covers a
// single-tenant GitHub App.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

type staticToken string

// StaticToken returns a TokenSource that always yields tok.
func StaticToken(tok string) TokenSource { return staticToken(tok) }

func (s staticToken) Token(context.Context) (string, error) { return string(s), nil }

// AppOptions configures AppInstallation's token exchange.
type AppOptions struct {
	HTTPClient HTTPDoer // nil → defaultHTTPClient
	APIURL     string   // "" → defaultAPIURL
}

// AppInstallation mints installation access tokens for one GitHub App
// installation from the App's private key (RS256 JWT → POST
// /app/installations/{id}/access_tokens). Token caches the current token
// and re-mints inside tokenRefreshMargin of expiry.
type AppInstallation struct {
	appID          string
	key            *rsa.PrivateKey
	installationID int64
	http           HTTPDoer
	apiURL         string
	now            func() time.Time

	mu      sync.Mutex
	token   string
	expires time.Time
}

// NewAppInstallation parses a PKCS1 ("RSA PRIVATE KEY") or PKCS8 ("PRIVATE
// KEY") PEM private key.
func NewAppInstallation(appID string, pemKey []byte, installationID int64, opts AppOptions) (*AppInstallation, error) {
	key, err := parseRSAKey(pemKey)
	if err != nil {
		return nil, err
	}
	a := &AppInstallation{appID: appID, key: key, installationID: installationID, http: opts.HTTPClient, apiURL: opts.APIURL, now: time.Now}
	if a.http == nil {
		a.http = defaultHTTPClient
	}
	if a.apiURL == "" {
		a.apiURL = defaultAPIURL
	}
	return a, nil
}

func parseRSAKey(pemKey []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemKey)
	if block == nil {
		return nil, shared.NewValidationError(adapterName, "private key is not PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, shared.NewValidationError(adapterName, "private key is not PKCS1 or PKCS8 RSA: "+err.Error())
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, shared.NewValidationError(adapterName, "private key is not RSA")
	}
	return key, nil
}

// AppJWT signs a fresh App JWT (iss = app id) for the endpoints that
// authenticate as the App itself (/app, token exchange).
func (a *AppInstallation) AppJWT() (string, error) {
	now := a.now()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(struct {
		Iat int64  `json:"iat"`
		Exp int64  `json:"exp"`
		Iss string `json:"iss"`
	}{now.Add(-jwtBackdate).Unix(), now.Add(jwtLifetime).Unix(), a.appID})
	if err != nil {
		return "", err
	}
	signing := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, a.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", fmt.Errorf("sign app jwt: %w", err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// Token implements TokenSource. The mutex guards only the cache; the
// network call runs outside it, so a lost race mints twice (harmless).
func (a *AppInstallation) Token(ctx context.Context) (string, error) {
	a.mu.Lock()
	tok, exp := a.token, a.expires
	a.mu.Unlock()
	if tok != "" && a.now().Add(tokenRefreshMargin).Before(exp) {
		return tok, nil
	}
	tok, exp, err := a.mint(ctx)
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	a.token, a.expires = tok, exp
	a.mu.Unlock()
	return tok, nil
}

func (a *AppInstallation) mint(ctx context.Context) (string, time.Time, error) {
	jwt, err := a.AppJWT()
	if err != nil {
		return "", time.Time{}, err
	}
	url := a.apiURL + "/app/installations/" + strconv.FormatInt(a.installationID, 10) + "/access_tokens"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", time.Time{}, err
	}
	setStandardHeaders(req, jwt)
	resp, err := a.http.Do(req)
	if err != nil {
		return "", time.Time{}, shared.NewNetworkError(adapterName, "installation token request failed", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", time.Time{}, err
	}
	if resp.StatusCode/100 != 2 {
		return "", time.Time{}, mapAPIError(resp, body, http.MethodPost, "/app/installations/{id}/access_tokens")
	}
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Token == "" {
		return "", time.Time{}, shared.NewNetworkError(adapterName, "installation token response was not JSON with a token", errors.Join(err))
	}
	return out.Token, out.ExpiresAt, nil
}
