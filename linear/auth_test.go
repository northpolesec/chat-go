package linear

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/shoenig/test/must"
)

func tokenServer(t *testing.T, mints *atomic.Int32, lastBody *string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		must.Eq(t, "/oauth/token", r.URL.Path)
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		*lastBody = string(b)
		mu.Unlock()
		mints.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-1","token_type":"Bearer","expires_in":2591999,"scope":"read write"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestClientCredentialsMintsOnceAndCaches(t *testing.T) {
	t.Parallel()
	var mints atomic.Int32
	var body string
	srv := tokenServer(t, &mints, &body)
	cc := NewClientCredentials("cid", "csecret", ClientCredentialsOptions{APIURL: srv.URL, HTTPClient: srv.Client()})

	tok, err := cc.Token(t.Context())
	must.NoError(t, err)
	must.Eq(t, "tok-1", tok)
	must.Eq(t, "client_id=cid&client_secret=csecret&grant_type=client_credentials&scope=read%2Cwrite%2Capp%3Amentionable%2Capp%3Aassignable", body)

	tok, err = cc.Token(t.Context())
	must.NoError(t, err)
	must.Eq(t, "tok-1", tok)
	must.Eq(t, int32(1), mints.Load())
}

func TestClientCredentialsRemintsAfterInvalidate(t *testing.T) {
	t.Parallel()
	var mints atomic.Int32
	var body string
	srv := tokenServer(t, &mints, &body)
	cc := NewClientCredentials("cid", "csecret", ClientCredentialsOptions{APIURL: srv.URL, HTTPClient: srv.Client()})
	_, err := cc.Token(t.Context())
	must.NoError(t, err)
	cc.Invalidate()
	_, err = cc.Token(t.Context())
	must.NoError(t, err)
	must.Eq(t, int32(2), mints.Load())
}

func TestClientCredentialsRemintsInsideRefreshMargin(t *testing.T) {
	t.Parallel()
	var mints atomic.Int32
	var body string
	srv := tokenServer(t, &mints, &body)
	cc := NewClientCredentials("cid", "csecret", ClientCredentialsOptions{APIURL: srv.URL, HTTPClient: srv.Client()})
	base := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	cc.now = func() time.Time { return base }
	_, err := cc.Token(t.Context())
	must.NoError(t, err)
	cc.now = func() time.Time { return base.Add(2591999*time.Second - 30*time.Minute) } // inside the 1h margin
	_, err = cc.Token(t.Context())
	must.NoError(t, err)
	must.Eq(t, int32(2), mints.Load())
}

func TestClientCredentialsConcurrentReadsDoNotRemint(t *testing.T) {
	t.Parallel()
	var mints atomic.Int32
	var body string
	srv := tokenServer(t, &mints, &body)
	cc := NewClientCredentials("cid", "csecret", ClientCredentialsOptions{APIURL: srv.URL, HTTPClient: srv.Client()})
	_, err := cc.Token(t.Context())
	must.NoError(t, err)
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			tok, err := cc.Token(t.Context())
			must.NoError(t, err)
			must.Eq(t, "tok-1", tok)
		})
	}
	wg.Wait()
	must.Eq(t, int32(1), mints.Load())
}

func TestClientCredentialsMintFailureIsAuthenticationError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"Error","error_description":"Client does not support the client_credentials grant type"}`))
	}))
	t.Cleanup(srv.Close)
	cc := NewClientCredentials("cid", "csecret", ClientCredentialsOptions{APIURL: srv.URL, HTTPClient: srv.Client()})
	_, err := cc.Token(t.Context())
	must.ErrorContains(t, err, "Client does not support the client_credentials grant type")
	var ae *shared.AuthenticationError
	must.True(t, errors.As(err, &ae))
}
