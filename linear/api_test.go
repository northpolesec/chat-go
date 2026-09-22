package linear

import (
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/northpolesec/chat-go/internal/shared"
	"github.com/shoenig/test/must"
)

func TestDoSendsOperationNameAndBearer(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	got := m.onCapture("LinearAdapterViewer", 200, viewerJSON)
	c := newClient(m.srv.Client(), m.srv.URL, StaticToken("t1"))
	var out struct {
		Viewer struct{ ID string } `json:"viewer"`
	}
	const query = "query LinearAdapterViewer { viewer { id } }"
	vars := map[string]any{"k": "v"}
	must.NoError(t, c.do(t.Context(), "LinearAdapterViewer", query, vars, &out))
	must.Eq(t, "app-user-1", out.Viewer.ID)
	must.Eq(t, 1, got.count())
	reqs := m.recorded()
	must.Eq(t, 1, len(reqs))
	must.Eq(t, http.MethodPost, reqs[0].method)
	must.Eq(t, "/graphql", reqs[0].path)
	must.Eq(t, "Bearer t1", reqs[0].auth)
	must.Eq(t, "LinearAdapterViewer", reqs[0].body["operationName"])
	must.Eq(t, query, reqs[0].body["query"])
	gotVars, _ := reqs[0].body["variables"].(map[string]any)
	must.Eq(t, map[string]any{"k": "v"}, gotVars)
}

func TestDo401InvalidatesAndRetriesOnce(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	m.onSeq("LinearAdapterViewer",
		struct {
			status int
			body   string
		}{401, `{"errors":[{"message":"unauthorized"}]}`},
		struct {
			status int
			body   string
		}{200, viewerJSON})
	cc := NewClientCredentials("cid", "sec", ClientCredentialsOptions{APIURL: m.srv.URL, HTTPClient: m.srv.Client()})
	c := newClient(m.srv.Client(), m.srv.URL, cc)
	must.NoError(t, c.do(t.Context(), "LinearAdapterViewer", "query LinearAdapterViewer { viewer { id } }", nil, nil))
	must.Eq(t, 2, m.tokenMints())
	must.Eq(t, 2, countOf(m.calls(), "LinearAdapterViewer"))
}

func TestDoAuthenticationErrorCodeRetriesThenFails(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	m.on("LinearAdapterViewer", 400, `{"errors":[{"message":"Authentication required","extensions":{"code":"AUTHENTICATION_ERROR"}}]}`)
	c := newClient(m.srv.Client(), m.srv.URL, StaticToken("t1"))
	err := c.do(t.Context(), "LinearAdapterViewer", "q", nil, nil)
	var ae *shared.AuthenticationError
	must.True(t, errors.As(err, &ae))
	must.Eq(t, 2, countOf(m.calls(), "LinearAdapterViewer"))
}

func TestDoRateLimitedUsesMillisecondReset(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	reset := time.Now().Add(90 * time.Second).UnixMilli()
	m.mu.Lock()
	m.handlers["LinearAdapterViewer"] = func(w http.ResponseWriter, _ []byte) {
		w.Header().Set("X-RateLimit-Requests-Reset", strconv.FormatInt(reset, 10))
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"errors":[{"message":"Rate limited","extensions":{"code":"RATELIMITED"}}]}`))
	}
	m.mu.Unlock()
	c := newClient(m.srv.Client(), m.srv.URL, StaticToken("t1"))
	err := c.do(t.Context(), "LinearAdapterViewer", "q", nil, nil)
	var rl *shared.AdapterRateLimitError
	must.True(t, errors.As(err, &rl))
	must.Between(t, 85, rl.RetryAfter, 92)
}

func TestDoGraphQLErrorsAreValidationErrors(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	m.on("LinearAdapterViewer", 200, `{"errors":[{"message":"Argument Validation Error"},{"message":"second"}]}`)
	c := newClient(m.srv.Client(), m.srv.URL, StaticToken("t1"))
	err := c.do(t.Context(), "LinearAdapterViewer", "q", nil, nil)
	var ve *shared.ValidationError
	must.True(t, errors.As(err, &ve))
	must.StrContains(t, err.Error(), "Argument Validation Error; second")
}

func TestDo2xxNonJSONIsNetworkError(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	m.on("LinearAdapterViewer", 200, `not json`)
	c := newClient(m.srv.Client(), m.srv.URL, StaticToken("t1"))
	var out struct {
		Viewer struct{ ID string } `json:"viewer"`
	}
	out.Viewer.ID = "preset"
	err := c.do(t.Context(), "LinearAdapterViewer", "q", nil, &out)
	var ne *shared.NetworkError
	must.True(t, errors.As(err, &ne))
	must.Eq(t, "preset", out.Viewer.ID)
}

func TestDoOtherStatusIsAPIError(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	m.on("LinearAdapterViewer", 502, `bad gateway`)
	c := newClient(m.srv.Client(), m.srv.URL, StaticToken("t1"))
	err := c.do(t.Context(), "LinearAdapterViewer", "q", nil, nil)
	var ae *APIError
	must.True(t, errors.As(err, &ae))
	must.Eq(t, 502, ae.Status)
}
