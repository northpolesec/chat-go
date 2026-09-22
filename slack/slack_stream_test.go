// Ported from packages/adapter-slack/src/index.test.ts @ 6adca36 (chat v4.40.0)
// (partial: Task 27 describe blocks — stream with empty threadTs, native
// stream rotation, native streaming fallback, native streaming outgoing
// mention resolution, feedbackButtons). handleWebhook feedback click is
// Task 28. ChatStreamer mocks → httptest chat.startStream/appendStream/
// stopStream (and chat.postMessage/chat.update for fallback).
package slack

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/northpolesec/chat-go/chat"
	"github.com/shoenig/test/must"
)

const (
	streamThread    = "slack:D123:1234567890.000000"
	streamToken     = "xoxb-test-token"
	streamMaxAge    = 100 * time.Millisecond
	streamPastGrace = streamMaxAge + 30001*time.Millisecond
)

func mdStream(parts ...string) iter.Seq2[chat.StreamChunk, error] {
	return func(yield func(chat.StreamChunk, error) bool) {
		for _, p := range parts {
			if !yield(chat.MarkdownTextChunk{Text: p}, nil) {
				return
			}
		}
	}
}

func unusedStream(consumed *bool) iter.Seq2[chat.StreamChunk, error] {
	return func(yield func(chat.StreamChunk, error) bool) {
		*consumed = true
		yield(chat.MarkdownTextChunk{Text: "x"}, nil)
	}
}

type testClock struct {
	mu sync.Mutex
	ms int64
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.UnixMilli(c.ms)
}

func (c *testClock) Set(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ms = d.Milliseconds()
}

type logBuf struct {
	mu    sync.Mutex
	msgs  []string
	attrs []map[string]any
}

func (l *logBuf) Enabled(context.Context, slog.Level) bool { return true }
func (l *logBuf) WithAttrs([]slog.Attr) slog.Handler       { return l }
func (l *logBuf) WithGroup(string) slog.Handler            { return l }
func (l *logBuf) Handle(_ context.Context, r slog.Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.msgs = append(l.msgs, r.Message)
	m := map[string]any{}
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.Any()
		return true
	})
	l.attrs = append(l.attrs, m)
	return nil
}

func (l *logBuf) joined() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.msgs, "\n")
}

func (l *logBuf) lastAttr(key string) any {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, attrs := range slices.Backward(l.attrs) {
		if v, ok := attrs[key]; ok {
			return v
		}
	}
	return nil
}

func jsonStr(c recordedCall, key string) string {
	s, _ := c.JSON[key].(string)
	return s
}

func jsonChunks(c recordedCall) []any {
	raw, _ := c.JSON["chunks"].([]any)
	return raw
}

// streamText is the text a start/append/stop call carries: the markdown_text
// chunks, which is the only shape the streamer sends (see takeChunks).
func streamText(c recordedCall) string {
	var b strings.Builder
	for _, ch := range jsonChunks(c) {
		cm, _ := ch.(map[string]any)
		if cm["type"] == "markdown_text" {
			s, _ := cm["text"].(string)
			b.WriteString(s)
		}
	}
	return b.String()
}

func appendedMarkdown(m *slackAPIMock) string {
	var b strings.Builder
	for _, c := range m.all(methodStartStream) {
		b.WriteString(streamText(c))
	}
	for _, c := range m.all(methodAppendStream) {
		b.WriteString(streamText(c))
	}
	return b.String()
}

func (m *slackAPIMock) nativeOK() {
	var mu sync.Mutex
	n := 0
	m.on(methodStartStream, func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		cur := n
		n++
		mu.Unlock()
		writeJSON(w, 200, map[string]any{"ok": true, "ts": "1234567890." + strconv.Itoa(cur)})
	})
	m.ok(methodAppendStream, nil)
	m.on(methodStopStream, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		ts, _ := body["ts"].(string)
		writeJSON(w, 200, map[string]any{"ok": true, "ts": ts})
	})
}

func (m *slackAPIMock) failNthStop(n int, code string) {
	var mu sync.Mutex
	count := 0
	m.on(methodStopStream, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		ts, _ := body["ts"].(string)
		mu.Lock()
		count++
		cur := count
		mu.Unlock()
		if cur == n {
			writeJSON(w, 200, map[string]any{"ok": false, "error": code})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "ts": ts})
	})
}

func streamAdapter(t *testing.T, extra Config) (*SlackAdapter, *slackAPIMock, *testClock, *logBuf) {
	t.Helper()
	clock := &testClock{}
	logs := &logBuf{}
	extra.Logger = slog.New(logs)
	if extra.StreamSegmentMaxAge == 0 {
		extra.StreamSegmentMaxAge = streamMaxAge
	}
	apiMock := newSlackAPIMock(t)
	apiMock.nativeOK()
	adapter := apiMock.adapter(t, extra)
	adapter.streamBufferSize = 1
	adapter.now = clock.Now
	return adapter, apiMock, clock, logs
}

func TestStreamWithEmptyThreadTs(t *testing.T) {
	t.Parallel()

	t.Run("delegates to fallback before consuming the stream", func(t *testing.T) {
		t.Parallel()
		adapter, _, _, _ := streamAdapter(t, Config{})
		consumed := false
		msg, err := adapter.Stream(t.Context(), "slack:C123:", unusedStream(&consumed), chat.StreamOptions{
			RecipientUserID: "U123",
			RecipientTeamID: "T123",
		})
		must.NoError(t, err)
		must.Nil(t, msg)
		must.False(t, consumed)
	})

	t.Run("delegates channel streams without recipient context to fallback", func(t *testing.T) {
		t.Parallel()
		adapter, _, _, _ := streamAdapter(t, Config{})
		consumed := false
		msg, err := adapter.Stream(t.Context(), "slack:C123:1234567890.000000", unusedStream(&consumed), chat.StreamOptions{})
		must.NoError(t, err)
		must.Nil(t, msg)
		must.False(t, consumed)
	})

	t.Run("allows DM streams without recipient context", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, _, _ := streamAdapter(t, Config{})
		_, err := adapter.Stream(t.Context(), streamThread, mdStream("hello"), chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, 1, apiMock.count(methodStartStream))
		start := apiMock.last(methodStartStream)
		must.Eq(t, "D123", jsonStr(start, "channel"))
		must.Eq(t, "1234567890.000000", jsonStr(start, "thread_ts"))
	})

	t.Run("passes token on stream stop", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, _, _ := streamAdapter(t, Config{})
		_, err := adapter.Stream(t.Context(), "slack:C123:1234567890.000000", mdStream("hello"), chat.StreamOptions{
			RecipientUserID: "U123",
			RecipientTeamID: "T123",
		})
		must.NoError(t, err)
		must.Eq(t, "Bearer "+streamToken, apiMock.last(methodStopStream).Auth)
	})

	t.Run("passes token on every stream append", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, _, _ := streamAdapter(t, Config{})
		stream := func(yield func(chat.StreamChunk, error) bool) {
			if !yield(chat.TaskUpdateChunk{ID: "task-1", Title: "Task one", Status: chat.TaskInProgress, Details: "first"}, nil) {
				return
			}
			yield(chat.TaskUpdateChunk{ID: "task-2", Title: "Task two", Status: chat.TaskInProgress, Details: "second"}, nil)
		}
		_, err := adapter.Stream(t.Context(), "slack:C123:1234567890.000000", stream, chat.StreamOptions{
			RecipientUserID: "U123",
			RecipientTeamID: "T123",
		})
		must.NoError(t, err)
		starts := apiMock.all(methodStartStream)
		appends := apiMock.all(methodAppendStream)
		must.Eq(t, 2, len(starts)+len(appends))
		for _, c := range append(starts, appends...) {
			must.Eq(t, "Bearer "+streamToken, c.Auth)
		}
	})
}

func TestNativeStreamRotation(t *testing.T) {
	t.Parallel()

	t.Run("rotates at a paragraph break past the max age and carries an open fence over", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, clock, _ := streamAdapter(t, Config{})
		stream := func(yield func(chat.StreamChunk, error) bool) {
			if !yield(chat.MarkdownTextChunk{Text: "```ts\nconst first = true;\n"}, nil) {
				return
			}
			clock.Set(streamMaxAge + time.Millisecond)
			if !yield(chat.MarkdownTextChunk{Text: "const second = true;\n\nconst third = true;\n"}, nil) {
				return
			}
			yield(chat.MarkdownTextChunk{Text: "```\n"}, nil)
		}
		result, err := adapter.Stream(t.Context(), streamThread, stream, chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, 2, apiMock.count(methodStartStream))
		stop0 := apiMock.nth(methodStopStream, 1)
		must.Eq(t, "const second = true;\n\n```", streamText(stop0))
		start1 := apiMock.nth(methodStartStream, 2)
		must.Eq(t, "```ts\nconst third = true;\n", streamText(start1))
		appends := apiMock.all(methodAppendStream)
		must.True(t, len(appends) >= 1)
		must.Eq(t, "```\n", streamText(appends[len(appends)-1]))
		must.Eq(t, "1234567890.1", result.ID)
	})

	t.Run("waits for a paragraph break within the grace window, then cuts at a line break", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, clock, _ := streamAdapter(t, Config{})
		stream := func(yield func(chat.StreamChunk, error) bool) {
			if !yield(chat.MarkdownTextChunk{Text: "First line.\n"}, nil) {
				return
			}
			clock.Set(streamMaxAge + time.Millisecond)
			if !yield(chat.MarkdownTextChunk{Text: "Second line.\n"}, nil) {
				return
			}
			clock.Set(streamPastGrace)
			if !yield(chat.MarkdownTextChunk{Text: "Third line.\n"}, nil) {
				return
			}
			yield(chat.MarkdownTextChunk{Text: "Fourth line.\n"}, nil)
		}
		_, err := adapter.Stream(t.Context(), streamThread, stream, chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, 2, apiMock.count(methodStartStream))
		must.Eq(t, "Third line.\n", streamText(apiMock.nth(methodStopStream, 1)))
		must.Eq(t, "Fourth line.\n", streamText(apiMock.nth(methodStartStream, 2)))
	})

	t.Run("does not rotate when the final flush has nothing new to send", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, clock, _ := streamAdapter(t, Config{})
		stream := func(yield func(chat.StreamChunk, error) bool) {
			if !yield(chat.MarkdownTextChunk{Text: "hello\n"}, nil) {
				return
			}
			clock.Set(streamPastGrace)
		}
		result, err := adapter.Stream(t.Context(), streamThread, stream, chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, 1, apiMock.count(methodStartStream))
		must.Eq(t, 1, apiMock.count(methodStopStream))
		must.Eq(t, "", streamText(apiMock.last(methodStopStream)))
		must.Eq(t, "1234567890.0", result.ID)
	})

	t.Run("starts the segment clock at the first call Slack accepts, not at construction", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, clock, _ := streamAdapter(t, Config{})
		adapter.streamBufferSize = 1 << 20
		stream := func(yield func(chat.StreamChunk, error) bool) {
			if !yield(chat.MarkdownTextChunk{Text: "a\n"}, nil) {
				return
			}
			clock.Set(1000 * time.Millisecond)
			adapter.streamBufferSize = 1
			if !yield(chat.MarkdownTextChunk{Text: "b\n"}, nil) {
				return
			}
			clock.Set(1000*time.Millisecond + streamMaxAge - time.Millisecond)
			if !yield(chat.MarkdownTextChunk{Text: "c\n\nd\n"}, nil) {
				return
			}
			must.Eq(t, 1, apiMock.count(methodStartStream))
			clock.Set(1000*time.Millisecond + streamMaxAge + time.Millisecond)
			yield(chat.MarkdownTextChunk{Text: "e\n\nf\n"}, nil)
		}
		_, err := adapter.Stream(t.Context(), streamThread, stream, chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, 2, apiMock.count(methodStartStream))
	})

	t.Run("keeps the agent session processing while the reply continues", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, clock, _ := streamAdapter(t, Config{AgentView: true})
		stream := func(yield func(chat.StreamChunk, error) bool) {
			if !yield(chat.MarkdownTextChunk{Text: "a\n"}, nil) {
				return
			}
			clock.Set(streamMaxAge + time.Millisecond)
			yield(chat.MarkdownTextChunk{Text: "b\n\nc\n"}, nil)
		}
		_, err := adapter.Stream(t.Context(), streamThread, stream, chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, "processing", jsonStr(apiMock.nth(methodStopStream, 1), "session_status"))
		must.Eq(t, "active", jsonStr(apiMock.nth(methodStopStream, 2), "session_status"))
	})

	t.Run("continues in a new message when Slack expired the segment before rotation", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, clock, logs := streamAdapter(t, Config{})
		apiMock.failNthStop(1, streamExpiredError)
		stream := func(yield func(chat.StreamChunk, error) bool) {
			if !yield(chat.MarkdownTextChunk{Text: "first\n"}, nil) {
				return
			}
			adapter.streamBufferSize = 1 << 20
			if !yield(chat.MarkdownTextChunk{Text: "second\n"}, nil) {
				return
			}
			clock.Set(streamMaxAge + time.Millisecond)
			yield(chat.MarkdownTextChunk{Text: "third\n\nfourth\n"}, nil)
		}
		result, err := adapter.Stream(t.Context(), streamThread, stream, chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, "second\nthird\n\nfourth\n", streamText(apiMock.nth(methodStartStream, 2)))
		must.Eq(t, "1234567890.1", result.ID)
		must.StrContains(t, logs.joined(), "expired before rotation")
	})

	t.Run("delivers unconfirmed text in a new message when the last segment expired before stop", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, _, _ := streamAdapter(t, Config{})
		apiMock.failNthStop(1, streamExpiredError)
		stream := func(yield func(chat.StreamChunk, error) bool) {
			if !yield(chat.MarkdownTextChunk{Text: "first\n"}, nil) {
				return
			}
			adapter.streamBufferSize = 1 << 20
			yield(chat.MarkdownTextChunk{Text: "second\n"}, nil)
		}
		result, err := adapter.Stream(t.Context(), streamThread, stream, chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, 0, len(apiMock.all(methodAppendStream)))
		must.Eq(t, "second\n", streamText(apiMock.last(methodStopStream)))
		must.Eq(t, "1234567890.1", result.ID)
	})

	t.Run("returns the finalized message when the last segment expired with everything delivered", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, _, logs := streamAdapter(t, Config{FeedbackButtons: &FeedbackButtonsOptions{}})
		apiMock.failNthStop(1, streamExpiredError)
		result, err := adapter.Stream(t.Context(), streamThread, mdStream("first\n"), chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, 1, apiMock.count(methodStartStream))
		must.Eq(t, "1234567890.0", result.ID)
		must.StrContains(t, logs.joined(), "stream-end blocks skipped")
		switch v := logs.lastAttr("skippedBlocks").(type) {
		case int:
			must.Eq(t, 1, v)
		case int64:
			must.Eq(t, int64(1), v)
		default:
			t.Fatalf("skippedBlocks=%T %#v", v, v)
		}
	})

	t.Run("still propagates non-expiry failures during rotation", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, clock, _ := streamAdapter(t, Config{})
		apiMock.failNthStop(1, "rotate boom")
		stream := func(yield func(chat.StreamChunk, error) bool) {
			if !yield(chat.MarkdownTextChunk{Text: "a\n"}, nil) {
				return
			}
			clock.Set(streamMaxAge + time.Millisecond)
			yield(chat.MarkdownTextChunk{Text: "b\n\nc\n"}, nil)
		}
		_, err := adapter.Stream(t.Context(), streamThread, stream, chat.StreamOptions{})
		must.Error(t, err)
		must.StrContains(t, err.Error(), "rotate boom")
	})

	t.Run("never switches streaming mode after a task card", func(t *testing.T) {
		t.Parallel()
		// Slack pins a stream to the mode chat.startStream used; a card
		// then text then stop used to flip chunks → markdown_text and die
		// with streaming_mode_mismatch on the first tool-calling turn.
		adapter, apiMock, _, _ := streamAdapter(t, Config{})
		stream := func(yield func(chat.StreamChunk, error) bool) {
			if !yield(chat.TaskUpdateChunk{ID: "t1", Title: "Using bash", Status: chat.TaskInProgress}, nil) {
				return
			}
			if !yield(chat.TaskUpdateChunk{ID: "t1", Title: "Using bash", Status: chat.TaskComplete}, nil) {
				return
			}
			yield(chat.MarkdownTextChunk{Text: "Linux aarch64\n"}, nil)
		}
		_, err := adapter.Stream(t.Context(), streamThread, stream, chat.StreamOptions{})
		must.NoError(t, err)
		for _, method := range []string{methodStartStream, methodAppendStream, methodStopStream} {
			for _, c := range apiMock.all(method) {
				_, bare := c.JSON["markdown_text"]
				must.False(t, bare, must.Sprintf("%s sent a bare markdown_text", method))
			}
		}
		must.Eq(t, "Linux aarch64\n", appendedMarkdown(apiMock)+streamText(apiMock.last(methodStopStream)))
	})

	t.Run("replays the plan and open task cards into the new segment", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, clock, _ := streamAdapter(t, Config{})
		plan := chat.PlanUpdateChunk{Title: "Plan"}
		oneInProgress := chat.TaskUpdateChunk{ID: "t1", Title: "One", Status: chat.TaskInProgress}
		oneComplete := chat.TaskUpdateChunk{ID: "t1", Title: "One", Status: chat.TaskComplete}
		twoComplete := chat.TaskUpdateChunk{ID: "t2", Title: "Two", Status: chat.TaskComplete}
		stream := func(yield func(chat.StreamChunk, error) bool) {
			if !yield(plan, nil) {
				return
			}
			if !yield(oneInProgress, nil) {
				return
			}
			if !yield(twoComplete, nil) {
				return
			}
			clock.Set(streamMaxAge + time.Millisecond)
			yield(oneComplete, nil)
		}
		_, err := adapter.Stream(t.Context(), streamThread, stream, chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, "", streamText(apiMock.nth(methodStopStream, 1)))
		start1 := apiMock.nth(methodStartStream, 2)
		chunks := jsonChunks(start1)
		if len(chunks) == 0 {
			if len(apiMock.all(methodAppendStream)) > 0 {
				chunks = jsonChunks(apiMock.all(methodAppendStream)[0])
			}
		}
		must.Eq(t, 2, len(chunks))
		c0, _ := chunks[0].(map[string]any)
		c1, _ := chunks[1].(map[string]any)
		must.Eq(t, "plan_update", c0["type"])
		must.Eq(t, "Plan", c0["title"])
		must.Eq(t, "task_update", c1["type"])
		must.Eq(t, "t1", c1["id"])
		must.Eq(t, "in_progress", c1["status"])
	})

	t.Run("repeats the table header when a table continues in the new segment", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, clock, _ := streamAdapter(t, Config{})
		stream := func(yield func(chat.StreamChunk, error) bool) {
			if !yield(chat.MarkdownTextChunk{Text: "| a | b |\n|---|---|\n| 1 | 2 |\n"}, nil) {
				return
			}
			clock.Set(streamPastGrace)
			if !yield(chat.MarkdownTextChunk{Text: "| 3 | 4 |\n"}, nil) {
				return
			}
			yield(chat.MarkdownTextChunk{Text: "| 5 | 6 |\n"}, nil)
		}
		_, err := adapter.Stream(t.Context(), streamThread, stream, chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, "| 3 | 4 |\n", streamText(apiMock.nth(methodStopStream, 1)))
		must.Eq(t, "| a | b |\n|---|---|\n| 5 | 6 |\n", streamText(apiMock.nth(methodStartStream, 2)))
	})

	t.Run("does not repeat the table header when the new segment starts with prose", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, clock, _ := streamAdapter(t, Config{})
		stream := func(yield func(chat.StreamChunk, error) bool) {
			if !yield(chat.MarkdownTextChunk{Text: "| a | b |\n|---|---|\n| 1 | 2 |\n"}, nil) {
				return
			}
			clock.Set(streamPastGrace)
			if !yield(chat.MarkdownTextChunk{Text: "| 3 | 4 |\n"}, nil) {
				return
			}
			yield(chat.MarkdownTextChunk{Text: "\nSummary.\n"}, nil)
		}
		_, err := adapter.Stream(t.Context(), streamThread, stream, chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, "\nSummary.\n", streamText(apiMock.nth(methodStartStream, 2)))
	})

	t.Run("closes a tilde fence with tildes even when backtick fences appear inside it", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, clock, _ := streamAdapter(t, Config{})
		stream := func(yield func(chat.StreamChunk, error) bool) {
			if !yield(chat.MarkdownTextChunk{Text: "~~~\n```js\nconst x = 1;\n```\n"}, nil) {
				return
			}
			clock.Set(streamMaxAge + time.Millisecond)
			yield(chat.MarkdownTextChunk{Text: "still literal\n\nmore\n"}, nil)
		}
		_, err := adapter.Stream(t.Context(), streamThread, stream, chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, "still literal\n\n~~~", streamText(apiMock.nth(methodStopStream, 1)))
		must.Eq(t, "~~~\nmore\n", streamText(apiMock.nth(methodStartStream, 2)))
	})

	t.Run("finishes a fenced block in the old segment when the pending text closes it", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, clock, _ := streamAdapter(t, Config{})
		stream := func(yield func(chat.StreamChunk, error) bool) {
			if !yield(chat.MarkdownTextChunk{Text: "```ts\nconst a = 1;\n"}, nil) {
				return
			}
			clock.Set(streamMaxAge + time.Millisecond)
			yield(chat.MarkdownTextChunk{Text: "```\n\nAfter.\n"}, nil)
		}
		_, err := adapter.Stream(t.Context(), streamThread, stream, chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, "```\n\n", streamText(apiMock.nth(methodStopStream, 1)))
		must.Eq(t, "After.\n", streamText(apiMock.nth(methodStartStream, 2)))
	})

	t.Run("treats a pending partial closing fence as the block's end", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, clock, _ := streamAdapter(t, Config{})
		stream := func(yield func(chat.StreamChunk, error) bool) {
			if !yield(chat.MarkdownTextChunk{Text: "```ts\nconst a = 1;\n"}, nil) {
				return
			}
			clock.Set(streamPastGrace)
			if !yield(chat.MarkdownTextChunk{Text: "```"}, nil) {
				return
			}
			yield(chat.MarkdownTextChunk{Text: "\nAfter.\n"}, nil)
		}
		_, err := adapter.Stream(t.Context(), streamThread, stream, chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, "```", streamText(apiMock.nth(methodStopStream, 1)))
		must.Eq(t, "\nAfter.\n", streamText(apiMock.nth(methodStartStream, 2)))
	})

	t.Run("does not rotate a segment before Slack has started it", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, clock, _ := streamAdapter(t, Config{})
		adapter.streamBufferSize = 1 << 20
		stream := func(yield func(chat.StreamChunk, error) bool) {
			if !yield(chat.MarkdownTextChunk{Text: "a\n"}, nil) {
				return
			}
			clock.Set(streamPastGrace)
			yield(chat.MarkdownTextChunk{Text: "b\n\nc\n"}, nil)
		}
		_, err := adapter.Stream(t.Context(), streamThread, stream, chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, 1, apiMock.count(methodStartStream))
		must.Eq(t, 1, apiMock.count(methodStopStream))
	})

	t.Run("falls back to the default max age for streamSegmentMaxAgeMs 0, -5", func(t *testing.T) {
		t.Parallel()
		for _, age := range []time.Duration{0, -5 * time.Millisecond} {
			t.Run(age.String(), func(t *testing.T) {
				t.Parallel()
				clock := &testClock{}
				apiMock := newSlackAPIMock(t)
				apiMock.nativeOK()
				adapter := apiMock.adapter(t, Config{StreamSegmentMaxAge: age})
				adapter.streamBufferSize = 1
				adapter.now = clock.Now
				stream := func(yield func(chat.StreamChunk, error) bool) {
					if !yield(chat.MarkdownTextChunk{Text: "a\n"}, nil) {
						return
					}
					clock.Set(239999 * time.Millisecond)
					if !yield(chat.MarkdownTextChunk{Text: "b\n\nc\n"}, nil) {
						return
					}
					must.Eq(t, 1, apiMock.count(methodStartStream))
					clock.Set(240000 * time.Millisecond)
					yield(chat.MarkdownTextChunk{Text: "d\n\ne\n"}, nil)
				}
				_, err := adapter.Stream(t.Context(), streamThread, stream, chat.StreamOptions{})
				must.NoError(t, err)
				must.Eq(t, 2, apiMock.count(methodStartStream))
			})
		}
	})

	t.Run("never rotates when streamSegmentMaxAgeMs is Infinity", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, clock, _ := streamAdapter(t, Config{StreamSegmentMaxAge: time.Duration(math.MaxInt64)})
		stream := func(yield func(chat.StreamChunk, error) bool) {
			if !yield(chat.MarkdownTextChunk{Text: "a\n"}, nil) {
				return
			}
			clock.Set(10_000_000 * time.Millisecond)
			yield(chat.MarkdownTextChunk{Text: "b\n\nc\n"}, nil)
		}
		_, err := adapter.Stream(t.Context(), streamThread, stream, chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, 1, apiMock.count(methodStartStream))
	})
}

func TestNativeStreamingFallback(t *testing.T) {
	t.Parallel()

	t.Run("finishes agent streams with an active session status", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, _, _ := streamAdapter(t, Config{AgentView: true})
		_, err := adapter.Stream(t.Context(), streamThread, mdStream("hello"), chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, "active", jsonStr(apiMock.last(methodStopStream), "session_status"))
	})

	t.Run("returns null before consuming the stream when nativeStreaming is false", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, _, _ := streamAdapter(t, Config{DisableNativeStreaming: true})
		consumed := false
		msg, err := adapter.Stream(t.Context(), streamThread, unusedStream(&consumed), chat.StreamOptions{})
		must.NoError(t, err)
		must.Nil(t, msg)
		must.False(t, consumed)
		must.Eq(t, 0, apiMock.count(methodStartStream))
	})

	t.Run("falls back to post-and-edit when the first native call fails", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, _, _ := streamAdapter(t, Config{})
		apiMock.fail(methodStartStream, "no streaming here")
		apiMock.ok("chat.postMessage", map[string]any{"ts": "fallback-ts"})
		result, err := adapter.Stream(t.Context(), streamThread, mdStream("hello ", "world"), chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, 1, apiMock.count("chat.postMessage"))
		must.Eq(t, 0, apiMock.count(methodStopStream))
		must.Eq(t, "fallback-ts", result.ID)
		last := "chat.postMessage"
		if apiMock.count("chat.update") > 0 {
			last = "chat.update"
		}
		must.StrContains(t, apiMock.last(last).Form.Get("markdown_text"), "world")
	})

	t.Run("streams fallback updates through markdown, not plain text", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, _, _ := streamAdapter(t, Config{})
		apiMock.fail(methodStartStream, "no streaming here")
		apiMock.ok("chat.postMessage", map[string]any{"ts": "fallback-ts"})
		apiMock.ok("chat.update", map[string]any{"ts": "fallback-ts"})
		_, err := adapter.Stream(t.Context(), streamThread, mdStream("**bold** ", "and `code`"), chat.StreamOptions{})
		must.NoError(t, err)
		for _, c := range apiMock.all("chat.postMessage") {
			must.True(t, c.Form.Get("markdown_text") != "")
		}
		for _, c := range apiMock.all("chat.update") {
			must.True(t, c.Form.Get("markdown_text") != "")
		}
		last := "chat.postMessage"
		if apiMock.count("chat.update") > 0 {
			last = "chat.update"
		}
		must.StrContains(t, apiMock.last(last).Form.Get("markdown_text"), "**bold**")
	})

	t.Run("latches native streaming off after an unsupported-method platform error", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, _, _ := streamAdapter(t, Config{})
		apiMock.fail(methodStartStream, "unknown_method")
		apiMock.ok("chat.postMessage", map[string]any{"ts": "fallback-ts"})
		_, err := adapter.Stream(t.Context(), streamThread, mdStream("hello"), chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, 1, apiMock.count(methodStartStream))
		consumed := false
		msg, err := adapter.Stream(t.Context(), "slack:D123:1234567890.111111", unusedStream(&consumed), chat.StreamOptions{})
		must.NoError(t, err)
		must.Nil(t, msg)
		must.Eq(t, 1, apiMock.count(methodStartStream))
		must.False(t, consumed)
	})

	t.Run("does not latch native streaming off after a transient error", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, _, _ := streamAdapter(t, Config{})
		var mu sync.Mutex
		n := 0
		apiMock.on(methodStartStream, func(w http.ResponseWriter, _ *http.Request) {
			mu.Lock()
			n++
			cur := n
			mu.Unlock()
			if cur == 1 {
				writeJSON(w, 200, map[string]any{"ok": false, "error": "socket hang up"})
				return
			}
			writeJSON(w, 200, map[string]any{"ok": true, "ts": "1234567890.1"})
		})
		apiMock.ok("chat.postMessage", map[string]any{"ts": "fallback-ts"})
		_, err := adapter.Stream(t.Context(), streamThread, mdStream("hello"), chat.StreamOptions{})
		must.NoError(t, err)
		_, err = adapter.Stream(t.Context(), "slack:D123:1234567890.111111", mdStream("hello again"), chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, 2, apiMock.count(methodStartStream))
	})

	t.Run("propagates mid-stream failures once native content has rendered", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, _, _ := streamAdapter(t, Config{})
		apiMock.on(methodAppendStream, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, 200, map[string]any{"ok": false, "error": "mid-stream boom"})
		})
		stream := func(yield func(chat.StreamChunk, error) bool) {
			if !yield(chat.TaskUpdateChunk{ID: "task-1", Title: "Task", Status: chat.TaskInProgress, Details: "step"}, nil) {
				return
			}
			yield(chat.MarkdownTextChunk{Text: "hello"}, nil)
		}
		_, err := adapter.Stream(t.Context(), streamThread, stream, chat.StreamOptions{})
		must.Error(t, err)
		must.StrContains(t, err.Error(), "mid-stream boom")
		must.Eq(t, 0, apiMock.count("chat.postMessage"))
	})

	t.Run("falls back when buffered appends never hit the API and stop() fails", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, _, _ := streamAdapter(t, Config{})
		adapter.streamBufferSize = 1 << 20
		apiMock.fail(methodStartStream, "no streaming here")
		apiMock.ok("chat.postMessage", map[string]any{"ts": "fallback-ts"})
		result, err := adapter.Stream(t.Context(), streamThread, mdStream("short reply"), chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, "fallback-ts", result.ID)
		must.Eq(t, 1, apiMock.count("chat.postMessage"))
		must.StrContains(t, apiMock.last("chat.postMessage").Form.Get("markdown_text"), "short reply")
	})

	t.Run("propagates stop() failures once native content has rendered", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, _, _ := streamAdapter(t, Config{})
		apiMock.fail(methodStopStream, "stop boom")
		_, err := adapter.Stream(t.Context(), streamThread, mdStream("hello"), chat.StreamOptions{})
		must.Error(t, err)
		must.StrContains(t, err.Error(), "stop boom")
		must.Eq(t, 0, apiMock.count("chat.postMessage"))
	})

	t.Run("skips structured chunks in fallback mode without failing the stream", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, _, _ := streamAdapter(t, Config{})
		apiMock.fail(methodStartStream, "no streaming here")
		apiMock.ok("chat.postMessage", map[string]any{"ts": "fallback-ts"})
		stream := func(yield func(chat.StreamChunk, error) bool) {
			if !yield(chat.MarkdownTextChunk{Text: "hello"}, nil) {
				return
			}
			if !yield(chat.TaskUpdateChunk{ID: "task-1", Title: "Task", Status: chat.TaskInProgress, Details: "step"}, nil) {
				return
			}
			yield(chat.MarkdownTextChunk{Text: " world"}, nil)
		}
		result, err := adapter.Stream(t.Context(), streamThread, stream, chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, "fallback-ts", result.ID)
		must.Eq(t, 1, apiMock.count("chat.postMessage"))
		must.StrContains(t, apiMock.last("chat.postMessage").Form.Get("markdown_text"), "hello")
	})
}

func TestNativeStreamingOutgoingMentionResolution(t *testing.T) {
	t.Parallel()

	mentionAdapter := func(t *testing.T) (*SlackAdapter, *slackAPIMock, chat.StateAdapter) {
		t.Helper()
		adapter, apiMock, _, _ := streamAdapter(t, Config{})
		sc, st := newStateChat(t)
		must.NoError(t, adapter.Initialize(t.Context(), sc))
		return adapter, apiMock, st
	}

	t.Run("resolves cached @name mentions on the native streaming path", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, st := mentionAdapter(t)
		mustAppend(t, st, "slack:user-by-name:alice", "U_ALICE_1")
		_, err := adapter.Stream(t.Context(), streamThread, mdStream("Thanks, @alice"), chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, "Thanks, <@U_ALICE_1>", appendedMarkdown(apiMock))
	})

	t.Run("resolves mentions that span source chunks", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, st := mentionAdapter(t)
		mustAppend(t, st, "slack:user-by-name:alice", "U_ALICE_1")
		_, err := adapter.Stream(t.Context(), streamThread, mdStream("Thanks, @ali", "ce"), chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, "Thanks, <@U_ALICE_1>", appendedMarkdown(apiMock))
	})

	t.Run("resolves mentions on lines committed mid-stream", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, st := mentionAdapter(t)
		mustAppend(t, st, "slack:user-by-name:alice", "U_ALICE_1")
		_, err := adapter.Stream(t.Context(), streamThread, mdStream("Hi @alice\nmore ", "text"), chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, "Hi <@U_ALICE_1>\n", streamText(apiMock.nth(methodStartStream, 1)))
		must.Eq(t, "Hi <@U_ALICE_1>\nmore text", appendedMarkdown(apiMock))
	})

	t.Run("leaves ambiguous mentions as plain text", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, st := mentionAdapter(t)
		mustAppend(t, st, "slack:user-by-name:alice", "U_ALICE_1")
		mustAppend(t, st, "slack:user-by-name:alice", "U_ALICE_2")
		_, err := adapter.Stream(t.Context(), streamThread, mdStream("hey @alice"), chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, "hey @alice", appendedMarkdown(apiMock))
	})

	t.Run("disambiguates ambiguous mentions using thread participants", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, st := mentionAdapter(t)
		mustAppend(t, st, "slack:user-by-name:alice", "U_ALICE_1")
		mustAppend(t, st, "slack:user-by-name:alice", "U_ALICE_2")
		mustAppend(t, st, "slack:thread-participants:"+streamThread, "U_ALICE_2")
		_, err := adapter.Stream(t.Context(), streamThread, mdStream("hey @alice"), chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, "hey <@U_ALICE_2>", appendedMarkdown(apiMock))
	})

	t.Run("keeps mentions literal inside code fences", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, st := mentionAdapter(t)
		mustAppend(t, st, "slack:user-by-name:alice", "U_ALICE_1")
		_, err := adapter.Stream(t.Context(), streamThread, mdStream("```\n@alice\n```\nping @alice"), chat.StreamOptions{})
		must.NoError(t, err)
		must.Eq(t, "```\n@alice\n```\nping <@U_ALICE_1>", appendedMarkdown(apiMock))
	})
}

func TestFeedbackButtons(t *testing.T) {
	t.Parallel()

	t.Run("appends a feedback context_actions block on stream stop", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, _, _ := streamAdapter(t, Config{FeedbackButtons: &FeedbackButtonsOptions{}})
		_, err := adapter.Stream(t.Context(), streamThread, mdStream("hello"), chat.StreamOptions{})
		must.NoError(t, err)
		blocks, _ := apiMock.last(methodStopStream).JSON["blocks"].([]any)
		must.Eq(t, 1, len(blocks))
		b0, _ := blocks[0].(map[string]any)
		must.Eq(t, "context_actions", b0["type"])
		els, _ := b0["elements"].([]any)
		el, _ := els[0].(map[string]any)
		must.Eq(t, "feedback_buttons", el["type"])
		must.Eq(t, "message_feedback", el["action_id"])
		pos, _ := el["positive_button"].(map[string]any)
		neg, _ := el["negative_button"].(map[string]any)
		must.Eq(t, "positive", pos["value"])
		must.Eq(t, "negative", neg["value"])
	})

	t.Run("honors custom labels, values, and action id", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, _, _ := streamAdapter(t, Config{FeedbackButtons: &FeedbackButtonsOptions{
			ActionID:      "ai_feedback",
			NegativeLabel: "Nope",
			NegativeValue: "down",
			PositiveLabel: "Nice",
			PositiveValue: "up",
		}})
		_, err := adapter.Stream(t.Context(), streamThread, mdStream("hello"), chat.StreamOptions{})
		must.NoError(t, err)
		blocks, _ := apiMock.last(methodStopStream).JSON["blocks"].([]any)
		b0, _ := blocks[0].(map[string]any)
		els, _ := b0["elements"].([]any)
		el, _ := els[0].(map[string]any)
		must.Eq(t, "ai_feedback", el["action_id"])
		pos, _ := el["positive_button"].(map[string]any)
		neg, _ := el["negative_button"].(map[string]any)
		posText, _ := pos["text"].(map[string]any)
		negText, _ := neg["text"].(map[string]any)
		must.Eq(t, "Nice", posText["text"])
		must.Eq(t, "up", pos["value"])
		must.Eq(t, "Nope", negText["text"])
		must.Eq(t, "down", neg["value"])
	})

	t.Run("places feedback buttons after caller stopBlocks", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, _, _ := streamAdapter(t, Config{FeedbackButtons: &FeedbackButtonsOptions{}})
		endWith := map[string]any{"type": "actions", "elements": []any{}}
		_, err := adapter.Stream(t.Context(), streamThread, mdStream("hello"), chat.StreamOptions{
			StopBlocks: []any{endWith},
		})
		must.NoError(t, err)
		blocks, _ := apiMock.last(methodStopStream).JSON["blocks"].([]any)
		must.Eq(t, 2, len(blocks))
		b0, _ := blocks[0].(map[string]any)
		b1, _ := blocks[1].(map[string]any)
		must.Eq(t, "actions", b0["type"])
		must.Eq(t, "context_actions", b1["type"])
	})

	t.Run("attaches no blocks when unconfigured", func(t *testing.T) {
		t.Parallel()
		adapter, apiMock, _, _ := streamAdapter(t, Config{})
		_, err := adapter.Stream(t.Context(), streamThread, mdStream("hello"), chat.StreamOptions{})
		must.NoError(t, err)
		_, ok := apiMock.last(methodStopStream).JSON["blocks"]
		must.False(t, ok)
	})

	t.Run("exposes buildFeedbackButtonsBlock defaults", func(t *testing.T) {
		t.Parallel()
		got := BuildFeedbackButtonsBlock(nil)
		must.Eq(t, "context_actions", got["type"])
		els, _ := got["elements"].([]any)
		el, _ := els[0].(map[string]any)
		must.Eq(t, "feedback_buttons", el["type"])
		must.Eq(t, "message_feedback", el["action_id"])
		pos, _ := el["positive_button"].(map[string]any)
		neg, _ := el["negative_button"].(map[string]any)
		posText, _ := pos["text"].(map[string]any)
		negText, _ := neg["text"].(map[string]any)
		must.Eq(t, "plain_text", posText["type"])
		must.Eq(t, "Good response", posText["text"])
		must.Eq(t, "positive", pos["value"])
		must.Eq(t, "plain_text", negText["type"])
		must.Eq(t, "Bad response", negText["text"])
		must.Eq(t, "negative", neg["value"])
	})
}

// A canceled turn still closes the stream: Slack's stop button cancels the
// ctx, and without a chat.stopStream the message stays in the streaming
// state with every open task card spinning. The bare sentinel is returned
// alongside the finalized message.
func TestStreamCancelFinalizesAndReturnsBareSentinel(t *testing.T) {
	t.Parallel()
	adapter, apiMock, _, _ := streamAdapter(t, Config{})
	ctx, cancel := context.WithCancel(t.Context())
	stream := func(yield func(chat.StreamChunk, error) bool) {
		if !yield(chat.TaskUpdateChunk{ID: "t1", Title: "Using bash", Status: chat.TaskInProgress}, nil) {
			return
		}
		if !yield(chat.MarkdownTextChunk{Text: "first\n"}, nil) {
			return
		}
		cancel()
		yield(chat.MarkdownTextChunk{Text: "second\n"}, nil)
	}
	msg, err := adapter.Stream(ctx, streamThread, stream, chat.StreamOptions{})
	must.True(t, err == context.Canceled)
	must.NotNil(t, msg)
	must.Eq(t, 1, apiMock.count(methodStartStream))
	must.Eq(t, 1, apiMock.count(methodStopStream))
	must.StrNotContains(t, appendedMarkdown(apiMock), "second")

	var last map[string]any
	for _, method := range []string{methodStartStream, methodAppendStream, methodStopStream} {
		for _, c := range apiMock.all(method) {
			for _, ch := range jsonChunks(c) {
				if cm, _ := ch.(map[string]any); cm["type"] == "task_update" && cm["id"] == "t1" {
					last = cm
				}
			}
		}
	}
	must.Eq(t, "error", last["status"], must.Sprint("the open task card must be resolved on cancel"))
}

func TestStreamProducerTerminalErrorFinalizes(t *testing.T) {
	t.Parallel()
	adapter, apiMock, _, _ := streamAdapter(t, Config{})
	want := errors.New("producer boom")
	stream := func(yield func(chat.StreamChunk, error) bool) {
		if !yield(chat.MarkdownTextChunk{Text: "hello\n"}, nil) {
			return
		}
		var zero chat.StreamChunk
		yield(zero, want)
	}
	msg, err := adapter.Stream(t.Context(), streamThread, stream, chat.StreamOptions{})
	must.Eq(t, want, err)
	must.True(t, msg != nil)
	must.True(t, msg.ID != "")
	must.Eq(t, 1, apiMock.count(methodStopStream))
}
