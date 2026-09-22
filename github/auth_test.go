package github

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/shoenig/test/must"
)

func testKey(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	must.NoError(t, err)
	return key, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

func TestStaticToken(t *testing.T) {
	t.Parallel()
	tok, err := StaticToken("ghp_x").Token(t.Context())
	must.NoError(t, err)
	must.Eq(t, "ghp_x", tok)
}

func TestNewAppInstallationParsesPKCS1AndPKCS8(t *testing.T) {
	t.Parallel()
	key, pkcs1 := testKey(t)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	must.NoError(t, err)
	pkcs8 := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	for name, p := range map[string][]byte{"pkcs1": pkcs1, "pkcs8": pkcs8} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewAppInstallation("12345", p, 99, AppOptions{})
			must.NoError(t, err)
		})
	}
	_, err = NewAppInstallation("12345", []byte("not pem"), 99, AppOptions{})
	must.ErrorContains(t, err, "private key")
}

func TestAppJWTClaimsAndSignature(t *testing.T) {
	t.Parallel()
	key, p := testKey(t)
	a, err := NewAppInstallation("12345", p, 99, AppOptions{})
	must.NoError(t, err)
	fixed := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return fixed }
	jwt, err := a.AppJWT()
	must.NoError(t, err)
	parts := strings.Split(jwt, ".")
	must.Len(t, 3, parts)
	hdr, _ := base64.RawURLEncoding.DecodeString(parts[0])
	must.Eq(t, `{"alg":"RS256","typ":"JWT"}`, string(hdr))
	var claims struct {
		Iat int64  `json:"iat"`
		Exp int64  `json:"exp"`
		Iss string `json:"iss"`
	}
	body, _ := base64.RawURLEncoding.DecodeString(parts[1])
	must.NoError(t, json.Unmarshal(body, &claims))
	must.Eq(t, fixed.Add(-60*time.Second).Unix(), claims.Iat)
	must.Eq(t, fixed.Add(9*time.Minute).Unix(), claims.Exp)
	must.Eq(t, "12345", claims.Iss)
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	must.NoError(t, rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, sum[:], sig))
}

func TestTokenExchangeCachesUntilRefreshMargin(t *testing.T) {
	t.Parallel()
	_, p := testKey(t)
	var mints atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		must.Eq(t, http.MethodPost, r.Method)
		must.Eq(t, "/app/installations/99/access_tokens", r.URL.Path)
		must.StrHasPrefix(t, "Bearer ", r.Header.Get("Authorization"))
		must.Eq(t, "application/vnd.github+json", r.Header.Get("Accept"))
		n := mints.Add(1)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"token":"ghs_` + string(rune('0'+n)) + `","expires_at":"2026-09-17T13:00:00Z"}`))
	}))
	t.Cleanup(srv.Close)
	a, err := NewAppInstallation("12345", p, 99, AppOptions{HTTPClient: srv.Client(), APIURL: srv.URL})
	must.NoError(t, err)
	clock := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return clock }

	tok, err := a.Token(t.Context())
	must.NoError(t, err)
	must.Eq(t, "ghs_1", tok)
	tok, err = a.Token(t.Context())
	must.NoError(t, err)
	must.Eq(t, "ghs_1", tok)
	clock = clock.Add(56 * time.Minute)
	tok, err = a.Token(t.Context())
	must.NoError(t, err)
	must.Eq(t, "ghs_2", tok)
	must.Eq(t, int32(2), mints.Load())
}

func TestTokenExchangeMapsAuthFailure(t *testing.T) {
	t.Parallel()
	_, p := testKey(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"A JSON web token could not be decoded"}`))
	}))
	t.Cleanup(srv.Close)
	a, err := NewAppInstallation("12345", p, 99, AppOptions{HTTPClient: srv.Client(), APIURL: srv.URL})
	must.NoError(t, err)
	_, err = a.Token(t.Context())
	var authErr *shared.AuthenticationError
	must.True(t, errors.As(err, &authErr))
}
