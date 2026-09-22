package github

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/shoenig/test/must"
)

func TestDoSendsStandardHeadersAndUnescapedJSON(t *testing.T) {
	t.Parallel()
	var got *http.Request
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(r.Context())
		body, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"id":7}`))
	}))
	t.Cleanup(srv.Close)
	c := newClient(srv.Client(), srv.URL, StaticToken("ghs_t"))
	var out struct {
		ID int64 `json:"id"`
	}
	err := c.do(t.Context(), http.MethodPost, "/repos/o/r/issues/1/comments", url.Values{"per_page": {"5"}}, map[string]string{"body": "a <b> & c"}, &out)
	must.NoError(t, err)
	must.Eq(t, int64(7), out.ID)
	must.Eq(t, "Bearer ghs_t", got.Header.Get("Authorization"))
	must.Eq(t, "application/vnd.github+json", got.Header.Get("Accept"))
	must.Eq(t, "2022-11-28", got.Header.Get("X-GitHub-Api-Version"))
	must.Eq(t, "chat-go-github", got.Header.Get("User-Agent"))
	must.Eq(t, "5", got.URL.Query().Get("per_page"))
	must.Eq(t, "{\"body\":\"a <b> & c\"}\n", string(body)) // SetEscapeHTML(false)
}

func TestDoMapsErrors(t *testing.T) {
	t.Parallel()
	futureReset := strconv.FormatInt(time.Now().Add(2*time.Minute).Unix(), 10)
	cases := []struct {
		name    string
		status  int
		headers map[string]string
		body    string
		check   func(t *testing.T, err error)
	}{
		{"401 auth", 401, nil, `{"message":"Bad credentials"}`, func(t *testing.T, err error) {
			var e *shared.AuthenticationError
			must.True(t, errors.As(err, &e))
		}},
		{"403 primary rate limit", 403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": futureReset}, `{"message":"API rate limit exceeded"}`, func(t *testing.T, err error) {
			var e *shared.AdapterRateLimitError
			must.True(t, errors.As(err, &e))
			must.Positive(t, e.RetryAfter)
		}},
		{"403 secondary rate limit", 403, map[string]string{"Retry-After": "30"}, `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}`, func(t *testing.T, err error) {
			var e *shared.AdapterRateLimitError
			must.True(t, errors.As(err, &e))
			must.Eq(t, 30, e.RetryAfter)
		}},
		{"429", 429, map[string]string{"Retry-After": "7"}, `{"message":"too many"}`, func(t *testing.T, err error) {
			var e *shared.AdapterRateLimitError
			must.True(t, errors.As(err, &e))
			must.Eq(t, 7, e.RetryAfter)
		}},
		{"404", 404, nil, `{"message":"Not Found"}`, func(t *testing.T, err error) {
			var e *shared.ResourceNotFoundError
			must.True(t, errors.As(err, &e))
		}},
		{"422", 422, nil, `{"message":"Validation Failed"}`, func(t *testing.T, err error) {
			var e *shared.ValidationError
			must.True(t, errors.As(err, &e))
			must.ErrorContains(t, err, "Validation Failed")
		}},
		{"500", 500, nil, `{"message":"boom"}`, func(t *testing.T, err error) {
			var e *APIError
			must.True(t, errors.As(err, &e))
			must.Eq(t, 500, e.Status)
			must.Eq(t, "boom", e.Message)
			must.Eq(t, "github: GET /x: HTTP 500: boom", err.Error())
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			c := newClient(srv.Client(), srv.URL, StaticToken("t"))
			err := c.do(t.Context(), http.MethodGet, "/x", nil, nil, nil)
			must.Error(t, err)
			tc.check(t, err)
		})
	}
}

func TestDoOmitsAuthorizationForEmptyToken(t *testing.T) {
	t.Parallel()
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	c := newClient(srv.Client(), srv.URL, StaticToken(""))
	must.NoError(t, c.do(t.Context(), http.MethodGet, "/users/x", nil, nil, nil))
	must.Eq(t, "", auth)
}
