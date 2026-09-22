package chat

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shoenig/test/must"
)

var (
	_ StreamChunk            = MarkdownTextChunk{}
	_ StreamChunk            = TaskUpdateChunk{}
	_ StreamChunk            = PlanUpdateChunk{}
	_ AdapterPostableMessage = PostableText("")
	_ AdapterPostableMessage = (*Message)(nil)
	_ AdapterPostableMessage = Card{}
	_ interface {
		StartTyping(context.Context, string, string, TypingOptions) error
	} = Adapter(nil)
	_ interface {
		EndTyping(context.Context, string, AgentSessionStatus) error
	} = TypingNotifier(nil)
	_ interface {
		ListThreads(context.Context, string, FetchOptions) (ListThreadsResult, error)
	} = ChannelReader(nil)
)

func TestStateGetSetRoundTrip(t *testing.T) {
	t.Parallel()

	kv := newMapKV()
	ctx := t.Context()

	type payload struct {
		Name string `json:"name"`
		N    int    `json:"n"`
	}

	must.NoError(t, StateSet(ctx, kv, "user", payload{Name: "ada", N: 7}, 0))

	got, ok, err := StateGet[payload](ctx, kv, "user")
	must.NoError(t, err)
	must.True(t, ok)
	must.Eq(t, payload{Name: "ada", N: 7}, got)
}

func TestStateGetMissing(t *testing.T) {
	t.Parallel()

	got, ok, err := StateGet[int](t.Context(), newMapKV(), "missing")
	must.NoError(t, err)
	must.False(t, ok)
	must.Eq(t, 0, got)
}

type mapKV struct {
	m map[string]json.RawMessage
}

func newMapKV() *mapKV {
	return &mapKV{m: map[string]json.RawMessage{}}
}

func (k *mapKV) Get(_ context.Context, key string) (json.RawMessage, error) {
	v, ok := k.m[key]
	if !ok {
		return nil, nil
	}
	return v, nil
}

func (k *mapKV) Set(_ context.Context, key string, value json.RawMessage, _ time.Duration) error {
	k.m[key] = append(json.RawMessage(nil), value...)
	return nil
}

func (k *mapKV) SetIfNotExists(_ context.Context, key string, value json.RawMessage, _ time.Duration) (bool, error) {
	if _, ok := k.m[key]; ok {
		return false, nil
	}
	k.m[key] = append(json.RawMessage(nil), value...)
	return true, nil
}

func (k *mapKV) Delete(_ context.Context, key string) error {
	delete(k.m, key)
	return nil
}
