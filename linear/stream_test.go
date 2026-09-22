package linear

import (
	"context"
	"errors"
	"iter"
	"net/http"
	"strings"
	"testing"

	"github.com/northpolesec/chat-go/chat"
	"github.com/shoenig/test/must"
)

func streamChunks(chunks ...chat.StreamChunk) iter.Seq2[chat.StreamChunk, error] {
	return func(yield func(chat.StreamChunk, error) bool) {
		for _, c := range chunks {
			if !yield(c, nil) {
				return
			}
		}
	}
}

func activitySeq(t *testing.T, c *captured) []string {
	t.Helper()
	var out []string
	for i := range c.count() {
		typ, content, eph := activityInput(t, c, i)
		s := typ
		if b, ok := content["body"].(string); ok && b != "" {
			s += ":" + b
		}
		if act, ok := content["action"].(string); ok && act != "" {
			s += ":" + act
		}
		if r, ok := content["result"].(string); ok && r != "" {
			s += "=" + r
		}
		if eph {
			s += "(eph)"
		}
		out = append(out, s)
	}
	return out
}

func TestStreamTextAndTasks(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	posted := m.onCapture(opActivityCreate, 200, activityCreatedJSON)
	a := newAdapter(t, m)
	res, err := a.Stream(t.Context(), thread, streamChunks(
		chat.MarkdownTextChunk{Text: "Looking at "},
		chat.MarkdownTextChunk{Text: "the issue.\n"},
		chat.TaskUpdateChunk{ID: "t1", Title: "Using linear graphql", Status: chat.TaskInProgress},
		chat.TaskUpdateChunk{ID: "t1", Title: "Using linear graphql", Status: chat.TaskComplete},
		chat.TaskUpdateChunk{ID: "t2", Title: "Using bash", Status: chat.TaskPending},
		chat.TaskUpdateChunk{ID: "t2", Title: "Using bash", Status: chat.TaskError},
		chat.MarkdownTextChunk{Text: "Here is the answer.\n"},
	), chat.StreamOptions{})
	must.NoError(t, err)
	must.Eq(t, "c-new", res.ID)
	must.Eq(t, []string{
		"thought:Looking at the issue.",
		"action:Using linear graphql(eph)",
		"action:Using linear graphql",
		"action:Using bash(eph)",
		"action:Using bash=failed",
		"response:Here is the answer.",
	}, activitySeq(t, posted))
	_, thought, _ := activityInput(t, posted, 0)
	_, thoughtHasParam := thought["parameter"]
	must.False(t, thoughtHasParam)
	_, action, _ := activityInput(t, posted, 1)
	must.Eq(t, "", action["parameter"])
}

func TestStreamEmptyTextPostsDone(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	posted := m.onCapture(opActivityCreate, 200, activityCreatedJSON)
	a := newAdapter(t, m)
	_, err := a.Stream(t.Context(), thread, streamChunks(), chat.StreamOptions{})
	must.NoError(t, err)
	must.Eq(t, []string{"response:Done."}, activitySeq(t, posted))
}

func TestStreamIteratorErrorPostsNothing(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	posted := m.onCapture(opActivityCreate, 200, activityCreatedJSON)
	a := newAdapter(t, m)
	boom := errors.New("boom")
	seq := func(yield func(chat.StreamChunk, error) bool) {
		if !yield(chat.MarkdownTextChunk{Text: "partial"}, nil) {
			return
		}
		yield(chat.MarkdownTextChunk{}, boom)
	}
	res, err := a.Stream(t.Context(), thread, seq, chat.StreamOptions{})
	must.Nil(t, res)
	must.ErrorIs(t, err, boom)
	must.Eq(t, 0, posted.count())
}

func TestStreamCancelledContextPostsNothingAfterCancel(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	posted := m.onCapture(opActivityCreate, 200, activityCreatedJSON)
	a := newAdapter(t, m)
	ctx, cancel := context.WithCancel(t.Context())
	seq := func(yield func(chat.StreamChunk, error) bool) {
		if !yield(chat.MarkdownTextChunk{Text: "first line\n"}, nil) {
			return
		}
		if !yield(chat.TaskUpdateChunk{ID: "t1", Title: "Using bash", Status: chat.TaskInProgress}, nil) {
			return
		}
		cancel()
		yield(chat.MarkdownTextChunk{Text: "never posted"}, nil)
	}
	res, err := a.Stream(ctx, thread, seq, chat.StreamOptions{})
	must.Nil(t, res)
	must.ErrorIs(t, err, context.Canceled)
	must.Eq(t, []string{"thought:first line", "action:Using bash(eph)"}, activitySeq(t, posted))
}

func TestStreamStopsPostingAfterFirstFailure(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	m.on(opActivityCreate, 502, `down`)
	a := newAdapter(t, m)
	drained := 0
	seq := func(yield func(chat.StreamChunk, error) bool) {
		for _, c := range []chat.StreamChunk{
			chat.MarkdownTextChunk{Text: "a\n"},
			chat.TaskUpdateChunk{ID: "t1", Title: "Using bash", Status: chat.TaskInProgress},
			chat.TaskUpdateChunk{ID: "t1", Title: "Using bash", Status: chat.TaskComplete},
			chat.MarkdownTextChunk{Text: "b\n"},
		} {
			drained++
			if !yield(c, nil) {
				return
			}
		}
	}
	res, err := a.Stream(t.Context(), thread, seq, chat.StreamOptions{})
	must.Nil(t, res)
	var ae *APIError
	must.True(t, errors.As(err, &ae))
	must.Eq(t, 4, drained)                              // iterator fully consumed
	must.Eq(t, 1, countOf(m.calls(), opActivityCreate)) // one failed post, then silence
}

func TestStreamTableIsNotRepostedInResponse(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	posted := m.onCapture(opActivityCreate, 200, activityCreatedJSON)
	a := newAdapter(t, m)
	table := "| a | b |\n|---|---|\n| 1 | 2 |"
	_, err := a.Stream(t.Context(), thread, streamChunks(
		chat.MarkdownTextChunk{Text: table + "\n"},
		chat.TaskUpdateChunk{ID: "t1", Title: "Using bash", Status: chat.TaskInProgress},
		chat.TaskUpdateChunk{ID: "t1", Title: "Using bash", Status: chat.TaskComplete},
		chat.MarkdownTextChunk{Text: "Done with the table.\n"},
	), chat.StreamOptions{})
	must.NoError(t, err)
	_, thought, _ := activityInput(t, posted, 0)
	thoughtBody, _ := thought["body"].(string)
	must.Eq(t, 1, strings.Count(thoughtBody, table))
	must.False(t, strings.Contains(thoughtBody, "```"))
	_, resp, _ := activityInput(t, posted, 3)
	must.Eq(t, "Done with the table.", resp["body"])
}

func TestStreamStopsPostingWhenCancelledDuringPost(t *testing.T) {
	t.Parallel()
	m := newLnMock(t)
	a := newAdapter(t, m)
	ctx, cancel := context.WithCancel(t.Context())
	m.mu.Lock()
	m.handlers[opActivityCreate] = func(w http.ResponseWriter, _ []byte) {
		cancel()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(activityCreatedJSON))
	}
	m.mu.Unlock()
	seq := func(yield func(chat.StreamChunk, error) bool) {
		if !yield(chat.MarkdownTextChunk{Text: "first line\n"}, nil) {
			return
		}
		if !yield(chat.TaskUpdateChunk{ID: "t1", Title: "Using bash", Status: chat.TaskInProgress}, nil) {
			return
		}
		if !yield(chat.TaskUpdateChunk{ID: "t1", Title: "Using bash", Status: chat.TaskComplete}, nil) {
			return
		}
		yield(chat.MarkdownTextChunk{Text: "never posted"}, nil)
	}
	res, err := a.Stream(ctx, thread, seq, chat.StreamOptions{})
	must.Nil(t, res)
	must.ErrorIs(t, err, context.Canceled)
	must.Eq(t, 1, countOf(m.calls(), opActivityCreate))
}
