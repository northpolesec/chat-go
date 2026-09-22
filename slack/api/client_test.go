// Ported from packages/adapter-slack/src/api/index.test.ts @ 6adca36 (chat v4.40.0).
// Divergences: free functions + per-call fetch/token → Client + httptest.Server
// (app-scoped values are fields). it.each rows stay named subtests.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/shoenig/test/must"
)

type recordedReq struct {
	Method      string
	URL         string
	Auth        string
	ContentType string
	Body        string
}

type reqSink struct {
	mu   sync.Mutex
	reqs []recordedReq
}

func (s *reqSink) record(req *http.Request, body string) {
	s.mu.Lock()
	s.reqs = append(s.reqs, recordedReq{
		Method:      req.Method,
		URL:         req.URL.String(),
		Auth:        req.Header.Get("Authorization"),
		ContentType: req.Header.Get("Content-Type"),
		Body:        body,
	})
	s.mu.Unlock()
}

func (s *reqSink) get(t *testing.T, i int) recordedReq {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	must.True(t, i >= 0 && i < len(s.reqs))
	return s.reqs[i]
}

type rewriteDoer struct {
	srv  *httptest.Server
	sink *reqSink
}

func (d *rewriteDoer) Do(req *http.Request) (*http.Response, error) {
	var body string
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		body = string(b)
		req.Body = io.NopCloser(bytes.NewReader(b))
	}
	d.sink.record(req, body)
	dest, err := url.Parse(d.srv.URL)
	if err != nil {
		return nil, err
	}
	clone := req.Clone(req.Context())
	clone.URL.Scheme = dest.Scheme
	clone.URL.Host = dest.Host
	clone.Host = dest.Host
	clone.RequestURI = ""
	if body != "" {
		clone.Body = io.NopCloser(bytes.NewReader([]byte(body)))
	}
	return d.srv.Client().Do(clone)
}

func startAPI(t *testing.T, h http.HandlerFunc) (*httptest.Server, *reqSink, *Client) {
	t.Helper()
	return startAPIWith(t, h, StaticToken("xoxb"), "")
}

func startAPIWith(t *testing.T, h http.HandlerFunc, token TokenSource, apiURL string) (*httptest.Server, *reqSink, *Client) {
	t.Helper()
	sink := &reqSink{}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, sink, &Client{
		HTTPClient: &rewriteDoer{srv: srv, sink: sink},
		APIURL:     apiURL,
		Token:      token,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func formVals(t *testing.T, body string) url.Values {
	t.Helper()
	v, err := url.ParseQuery(body)
	must.NoError(t, err)
	return v
}

func jsonVal(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	must.NoError(t, err)
	var out any
	must.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func rawJSON(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var out any
	must.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func boolPtr(v bool) *bool { return &v }

type spyToken struct {
	mu    sync.Mutex
	calls int
	tok   string
}

func (s *spyToken) Token(context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return s.tok, nil
}

func (s *spyToken) called() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func TestSlackAPIPrimitives(t *testing.T) {
	t.Parallel()

	t.Run("form-encodes Slack API bodies with JSON object values", func(t *testing.T) {
		t.Parallel()
		encoded, contentType := EncodeSlackAPIBody(map[string]any{
			"blocks":          []any{map[string]any{"type": "section"}},
			"channel":         "C123",
			"reply_broadcast": false,
			"text":            "hello",
			"thread_ts":       nil,
		}, EncodingForm)
		must.Eq(t, "application/x-www-form-urlencoded", contentType)
		params := formVals(t, encoded)
		must.Eq(t, `[{"type":"section"}]`, params.Get("blocks"))
		must.Eq(t, "false", params.Get("reply_broadcast"))
		must.False(t, params.Has("thread_ts"))
	})

	t.Run("calls Slack Web API with bearer token auth", func(t *testing.T) {
		t.Parallel()
		token := &spyToken{tok: "xoxb-token"}
		_, sink, c := startAPIWith(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		}, token, "")

		_, err := c.Call(t.Context(), "chat.postMessage", map[string]any{
			"channel": "C123",
			"text":    "hello",
		}, EncodingForm)
		must.NoError(t, err)

		got := sink.get(t, 0)
		must.Eq(t, "https://slack.com/api/chat.postMessage", got.URL)
		must.Eq(t, "Bearer xoxb-token", got.Auth)
		must.Eq(t, "hello", formVals(t, got.Body).Get("text"))
	})

	t.Run("omits Authorization when the token is empty", func(t *testing.T) {
		t.Parallel()
		_, sink, c := startAPIWith(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		}, StaticToken(""), "")

		_, err := c.Call(t.Context(), "oauth.v2.access", map[string]any{
			"code": "oauth-code",
		}, EncodingForm)
		must.NoError(t, err)
		must.Eq(t, "", sink.get(t, 0).Auth)
	})

	t.Run("supports custom API origins for tests and proxies", func(t *testing.T) {
		t.Parallel()
		_, sink, c := startAPIWith(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		}, StaticToken("xoxb-token"), "https://proxy.example/slack/")

		_, err := c.Call(t.Context(), "chat.postMessage", map[string]any{}, EncodingForm)
		must.NoError(t, err)
		must.Eq(t, "https://proxy.example/slack/chat.postMessage", sink.get(t, 0).URL)
	})

	t.Run("throws for non-2xx Slack API HTTP responses", func(t *testing.T) {
		t.Parallel()
		_, _, c := startAPI(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "ratelimited", "ok": false})
		})

		_, err := c.Call(t.Context(), "chat.postMessage", map[string]any{}, EncodingForm)
		var apiErr *APIError
		must.True(t, errors.As(err, &apiErr))
		must.Eq(t, "ratelimited", apiErr.Code)
		must.StrContains(t, apiErr.Error(), "chat.postMessage")
		must.StrContains(t, apiErr.Error(), "HTTP 429")
	})

	t.Run("posts messages and returns the Slack timestamp", func(t *testing.T) {
		t.Parallel()
		_, sink, c := startAPI(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"channel": "C123", "ok": true, "ts": "1.23"})
		})

		result, err := c.PostMessage(t.Context(), MessageOptions{
			Channel:      "C123",
			MarkdownText: "**hello**",
			UnfurlLinks:  boolPtr(false),
			UnfurlMedia:  boolPtr(false),
		})
		must.NoError(t, err)

		params := formVals(t, sink.get(t, 0).Body)
		must.Eq(t, "**hello**", params.Get("markdown_text"))
		must.Eq(t, "", params.Get("text"))
		must.False(t, params.Has("text"))
		must.False(t, params.Has("blocks"))
		must.Eq(t, "false", params.Get("unfurl_links"))
		must.Eq(t, "C123", result.Channel)
		must.Eq(t, "1.23", result.ID)
		must.Eq(t, jsonVal(t, map[string]any{"channel": "C123", "ok": true, "ts": "1.23"}), rawJSON(t, result.Raw.Raw))
	})

	t.Run("rejects markdown_text conflicts locally", func(t *testing.T) {
		t.Parallel()
		called := false
		_, _, c := startAPI(t, func(http.ResponseWriter, *http.Request) {
			called = true
		})

		_, err := c.PostMessage(t.Context(), MessageOptions{
			Channel:      "C123",
			MarkdownText: "**hello**",
			Text:         "hello",
		})
		must.Error(t, err)
		must.StrContains(t, err.Error(), "markdownText cannot be used with text or blocks")
		must.False(t, called)
	})

	t.Run("posts ephemeral messages", func(t *testing.T) {
		t.Parallel()
		_, sink, c := startAPI(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"channel": "C123", "message_ts": "1.24", "ok": true})
		})

		result, err := c.PostEphemeral(t.Context(), EphemeralOptions{
			MessageOptions: MessageOptions{Channel: "C123", Text: "hello"},
			User:           "U123",
		})
		must.NoError(t, err)

		got := sink.get(t, 0)
		must.Eq(t, "https://slack.com/api/chat.postEphemeral", got.URL)
		must.Eq(t, "U123", formVals(t, got.Body).Get("user"))
		must.Eq(t, "1.24", result.ID)
	})

	t.Run("updates messages", func(t *testing.T) {
		t.Parallel()
		_, sink, c := startAPI(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"channel": "C123", "ok": true, "ts": "1.25"})
		})

		result, err := c.UpdateMessage(t.Context(), UpdateOptions{
			MessageOptions: MessageOptions{
				Blocks:  []any{map[string]any{"type": "section"}},
				Channel: "C123",
				Text:    "fallback",
			},
			TS: "1.23",
		})
		must.NoError(t, err)

		got := sink.get(t, 0)
		params := formVals(t, got.Body)
		must.Eq(t, "https://slack.com/api/chat.update", got.URL)
		must.Eq(t, "1.23", params.Get("ts"))
		must.Eq(t, `[{"type":"section"}]`, params.Get("blocks"))
		must.Eq(t, "1.25", result.ID)
	})

	t.Run("deletes messages", func(t *testing.T) {
		t.Parallel()
		_, sink, c := startAPI(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ts": "1.23"})
		})

		must.NoError(t, c.DeleteMessage(t.Context(), "C123", "1.23"))

		got := sink.get(t, 0)
		params := formVals(t, got.Body)
		must.Eq(t, "https://slack.com/api/chat.delete", got.URL)
		must.Eq(t, "C123", params.Get("channel"))
		must.Eq(t, "1.23", params.Get("ts"))
	})

	t.Run("throws SlackApiError for ok false helper responses", func(t *testing.T) {
		t.Parallel()
		_, _, c := startAPI(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"error": "channel_not_found", "ok": false})
		})

		_, err := c.PostMessage(t.Context(), MessageOptions{Channel: "C123", Text: "hello"})
		var apiErr *APIError
		must.True(t, errors.As(err, &apiErr))
		must.Eq(t, "channel_not_found", apiErr.Code)
	})

	t.Run("sends response_url JSON payloads", func(t *testing.T) {
		t.Parallel()
		_, sink, c := startAPI(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		err := c.SendResponseURL(t.Context(), "https://hooks.slack.com/actions/T/1/abc", ResponseURLPayload{
			ReplaceOriginal: boolPtr(true),
			Text:            "updated",
		})
		must.NoError(t, err)

		got := sink.get(t, 0)
		must.Eq(t, "https://hooks.slack.com/actions/T/1/abc", got.URL)
		must.Eq(t, jsonVal(t, map[string]any{"replace_original": true, "text": "updated"}), rawJSON(t, json.RawMessage(got.Body)))
	})

	t.Run("refuses an untrusted response_url", func(t *testing.T) {
		t.Parallel()
		for _, raw := range []string{
			"https://example.com/hook",
			"http://hooks.slack.com/actions/T/1/abc",
			"https://hooks.slack.com.attacker.example/hook",
			"https://user@hooks.slack.com/actions/T/1/abc",
			"https://hooks.slack.com:444/actions/T/1/abc",
		} {
			_, _, c := startAPI(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			err := c.SendResponseURL(t.Context(), raw, ResponseURLPayload{Text: "nope"})
			var apiErr *APIError
			must.True(t, errors.As(err, &apiErr))
			must.Eq(t, "Refusing to send content to an untrusted Slack response_url", apiErr.Error())
		}
	})

	t.Run("uploads files with Slack external upload flow", func(t *testing.T) {
		t.Parallel()
		var n int
		_, sink, c := startAPI(t, func(w http.ResponseWriter, r *http.Request) {
			n++
			switch n {
			case 1:
				writeJSON(w, http.StatusOK, map[string]any{
					"file_id":    "F123",
					"ok":         true,
					"upload_url": "https://files.slack.com/upload/v1/abc",
				})
			case 2:
				w.WriteHeader(http.StatusOK)
			case 3:
				writeJSON(w, http.StatusOK, map[string]any{"files": []any{map[string]any{"id": "F123"}}, "ok": true})
			default:
				http.Error(w, "unexpected extra request", http.StatusInternalServerError)
			}
			_ = r
		})

		result, err := c.UploadFiles(t.Context(), UploadOptions{
			ChannelID:      "C123",
			InitialComment: "here",
			ThreadTS:       "1.23",
			Files:          []FileUpload{{Data: []byte{1, 2, 3}, Filename: "report.txt"}},
		})
		must.NoError(t, err)

		must.Eq(t, "https://slack.com/api/files.getUploadURLExternal", sink.get(t, 0).URL)
		must.Eq(t, "3", formVals(t, sink.get(t, 0).Body).Get("length"))
		must.Eq(t, "https://files.slack.com/upload/v1/abc", sink.get(t, 1).URL)
		must.Eq(t, "Bearer xoxb", sink.get(t, 1).Auth)
		must.Eq(t, "https://slack.com/api/files.completeUploadExternal", sink.get(t, 2).URL)
		must.Eq(t, []UploadedFile{{ID: "F123"}}, result)
	})

	t.Run("fetches private Slack file URLs with bearer auth", func(t *testing.T) {
		t.Parallel()
		_, sink, c := startAPI(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("file"))
		})

		rc, err := c.FetchFile(t.Context(), "https://files.slack.com/files-pri/T/F/report.txt")
		must.NoError(t, err)
		t.Cleanup(func() { _ = rc.Close() })
		got, err := io.ReadAll(rc)
		must.NoError(t, err)
		must.Eq(t, []byte("file"), got)
		must.Eq(t, "Bearer xoxb", sink.get(t, 0).Auth)
	})

	for _, raw := range []string{
		"https://docs.google.com/document/d/external",
		"http://files.slack.com/files-pri/T/F/report.txt",
		"https://files.slack.com.attacker.example/report.txt",
		"https://files.slack.com@attacker.example/report.txt",
		"https://user@files.slack.com/files-pri/T/F/report.txt",
	} {
		t.Run("refuses non-Slack file URL "+raw, func(t *testing.T) {
			t.Parallel()
			token := &spyToken{tok: "xoxb"}
			_, _, c := startAPIWith(t, func(w http.ResponseWriter, _ *http.Request) {
				t.Fatal("fetch reached the HTTP client")
			}, token, "")

			rc, err := c.FetchFile(t.Context(), raw)
			must.Nil(t, rc)
			var apiErr *APIError
			must.True(t, errors.As(err, &apiErr))
			must.Eq(t, "Refusing to fetch a non-Slack file URL", apiErr.Error())
			must.Eq(t, 0, token.called())
		})
	}

	t.Run("authenticates file URLs on the configured API origin", func(t *testing.T) {
		t.Parallel()
		_, sink, c := startAPIWith(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("file"))
		}, StaticToken("xoxb"), "https://slack-gov.com/api/")

		rc, err := c.FetchFile(t.Context(), "https://slack-gov.com/files-pri/T/F/report.txt")
		must.NoError(t, err)
		t.Cleanup(func() { _ = rc.Close() })
		_, err = io.ReadAll(rc)
		must.NoError(t, err)
		must.Eq(t, "Bearer xoxb", sink.get(t, 0).Auth)
	})

	t.Run("fetches thread replies with cursor metadata", func(t *testing.T) {
		t.Parallel()
		payload := map[string]any{
			"messages":          []any{map[string]any{"text": "root", "ts": "1.23"}},
			"ok":                true,
			"response_metadata": map[string]any{"next_cursor": "next"},
		}
		_, sink, c := startAPI(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, payload)
		})

		result, err := c.FetchThreadReplies(t.Context(), RepliesOptions{
			Channel: "C123",
			Limit:   50,
			TS:      "1.23",
		})
		must.NoError(t, err)

		must.Eq(t, "https://slack.com/api/conversations.replies", sink.get(t, 0).URL)
		must.Eq(t, "1.23", formVals(t, sink.get(t, 0).Body).Get("ts"))
		must.Eq(t, "next", result.ResponseMetadata.NextCursor)
		must.Eq(t, jsonVal(t, payload), rawJSON(t, result.Raw))
	})

	t.Run("opens Slack views with trigger ids", func(t *testing.T) {
		t.Parallel()
		_, sink, c := startAPI(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":   true,
				"view": map[string]any{"id": "V123", "type": "modal"},
			})
		})

		result, err := c.OpenView(t.Context(), "trigger", json.RawMessage(`{"type":"modal"}`))
		must.NoError(t, err)

		must.Eq(t, "https://slack.com/api/views.open", sink.get(t, 0).URL)
		var view any
		must.NoError(t, json.Unmarshal([]byte(formVals(t, sink.get(t, 0).Body).Get("view")), &view))
		must.Eq(t, jsonVal(t, map[string]any{"type": "modal"}), view)
		must.Eq(t, jsonVal(t, map[string]any{"id": "V123", "type": "modal"}), rawJSON(t, result.Raw).(map[string]any)["view"])
	})
}
