package statememory

import (
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/northpolesec/chat-go/chat"
	"github.com/shoenig/test/must"
)

func TestMemoryStateAdapter(t *testing.T) {
	t.Parallel()

	t.Run("subscriptions", func(t *testing.T) {
		t.Parallel()

		t.Run("should subscribe to a thread", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			ctx := t.Context()
			must.NoError(t, s.Subscribe(ctx, "slack:C123:1234.5678"))
			ok, err := s.IsSubscribed(ctx, "slack:C123:1234.5678")
			must.NoError(t, err)
			must.True(t, ok)
		})

		t.Run("should unsubscribe from a thread", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			ctx := t.Context()
			must.NoError(t, s.Subscribe(ctx, "slack:C123:1234.5678"))
			must.NoError(t, s.Unsubscribe(ctx, "slack:C123:1234.5678"))
			ok, err := s.IsSubscribed(ctx, "slack:C123:1234.5678")
			must.NoError(t, err)
			must.False(t, ok)
		})
	})

	t.Run("locking", func(t *testing.T) {
		t.Parallel()

		t.Run("should acquire a lock", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			lock, err := s.AcquireLock(t.Context(), "thread1", 5000*time.Millisecond)
			must.NoError(t, err)
			must.True(t, lock != nil)
			must.Eq(t, "thread1", lock.ThreadID)
			must.True(t, lock.Token != "")
		})

		t.Run("should prevent double-locking", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			ctx := t.Context()
			lock1, err := s.AcquireLock(ctx, "thread1", 5000*time.Millisecond)
			must.NoError(t, err)
			lock2, err := s.AcquireLock(ctx, "thread1", 5000*time.Millisecond)
			must.NoError(t, err)
			must.True(t, lock1 != nil)
			must.True(t, lock2 == nil)
		})

		t.Run("should release a lock", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			ctx := t.Context()
			lock, err := s.AcquireLock(ctx, "thread1", 5000*time.Millisecond)
			must.NoError(t, err)
			must.True(t, lock != nil)
			must.NoError(t, s.ReleaseLock(ctx, lock))
			lock2, err := s.AcquireLock(ctx, "thread1", 5000*time.Millisecond)
			must.NoError(t, err)
			must.True(t, lock2 != nil)
		})

		t.Run("should not release a lock with wrong token", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			ctx := t.Context()
			lock, err := s.AcquireLock(ctx, "thread1", 5000*time.Millisecond)
			must.NoError(t, err)

			must.NoError(t, s.ReleaseLock(ctx, &chat.Lock{
				ThreadID:  "thread1",
				Token:     "fake-token",
				ExpiresAt: s.currentTime().Add(5000 * time.Millisecond),
			}))

			lock2, err := s.AcquireLock(ctx, "thread1", 5000*time.Millisecond)
			must.NoError(t, err)
			must.True(t, lock2 == nil)

			must.True(t, lock != nil)
			must.NoError(t, s.ReleaseLock(ctx, lock))
		})

		t.Run("should allow re-locking after expiry", func(t *testing.T) {
			t.Parallel()
			s, clk := connectedAt(t, time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC))
			ctx := t.Context()
			lock1, err := s.AcquireLock(ctx, "thread1", 10*time.Millisecond)
			must.NoError(t, err)
			clk.Advance(20 * time.Millisecond)
			lock2, err := s.AcquireLock(ctx, "thread1", 5000*time.Millisecond)
			must.NoError(t, err)
			must.True(t, lock2 != nil)
			must.NotEq(t, lock1.Token, lock2.Token)
		})

		t.Run("should extend a lock", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			ctx := t.Context()
			lock, err := s.AcquireLock(ctx, "thread1", 100*time.Millisecond)
			must.NoError(t, err)
			must.True(t, lock != nil)
			extended, err := s.ExtendLock(ctx, lock, 5000*time.Millisecond)
			must.NoError(t, err)
			must.True(t, extended)
			lock2, err := s.AcquireLock(ctx, "thread1", 5000*time.Millisecond)
			must.NoError(t, err)
			must.True(t, lock2 == nil)
		})

		t.Run("should force-release a lock regardless of token", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			ctx := t.Context()
			lock, err := s.AcquireLock(ctx, "thread1", 5000*time.Millisecond)
			must.NoError(t, err)
			must.True(t, lock != nil)
			must.NoError(t, s.ForceReleaseLock(ctx, "thread1"))
			lock2, err := s.AcquireLock(ctx, "thread1", 5000*time.Millisecond)
			must.NoError(t, err)
			must.True(t, lock2 != nil)
			must.NotEq(t, lock.Token, lock2.Token)
		})

		t.Run("should no-op when force-releasing a non-existent lock", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			must.NoError(t, s.ForceReleaseLock(t.Context(), "nonexistent"))
		})

		t.Run("should not extend an expired lock", func(t *testing.T) {
			t.Parallel()
			s, clk := connectedAt(t, time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC))
			ctx := t.Context()
			lock, err := s.AcquireLock(ctx, "thread1", 10*time.Millisecond)
			must.NoError(t, err)
			must.True(t, lock != nil)
			clk.Advance(20 * time.Millisecond)
			extended, err := s.ExtendLock(ctx, lock, 5000*time.Millisecond)
			must.NoError(t, err)
			must.False(t, extended)
		})
	})

	t.Run("setIfNotExists", func(t *testing.T) {
		t.Parallel()

		t.Run("should set a value when key does not exist", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			ctx := t.Context()
			ok, err := s.SetIfNotExists(ctx, "key1", raw(t, "value1"), 0)
			must.NoError(t, err)
			must.True(t, ok)
			got, err := s.Get(ctx, "key1")
			must.NoError(t, err)
			must.Eq(t, raw(t, "value1"), got)
		})

		t.Run("should not overwrite an existing key", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			ctx := t.Context()
			_, err := s.SetIfNotExists(ctx, "key1", raw(t, "first"), 0)
			must.NoError(t, err)
			ok, err := s.SetIfNotExists(ctx, "key1", raw(t, "second"), 0)
			must.NoError(t, err)
			must.False(t, ok)
			got, err := s.Get(ctx, "key1")
			must.NoError(t, err)
			must.Eq(t, raw(t, "first"), got)
		})

		t.Run("should allow setting after TTL expiry", func(t *testing.T) {
			t.Parallel()
			s, clk := connectedAt(t, time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC))
			ctx := t.Context()
			_, err := s.SetIfNotExists(ctx, "key1", raw(t, "first"), 10*time.Millisecond)
			must.NoError(t, err)
			clk.Advance(20 * time.Millisecond)
			ok, err := s.SetIfNotExists(ctx, "key1", raw(t, "second"), 0)
			must.NoError(t, err)
			must.True(t, ok)
			got, err := s.Get(ctx, "key1")
			must.NoError(t, err)
			must.Eq(t, raw(t, "second"), got)
		})

		t.Run("should respect TTL on the new value", func(t *testing.T) {
			t.Parallel()
			s, clk := connectedAt(t, time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC))
			ctx := t.Context()
			_, err := s.SetIfNotExists(ctx, "key1", raw(t, "value"), 10*time.Millisecond)
			must.NoError(t, err)
			clk.Advance(20 * time.Millisecond)
			got, err := s.Get(ctx, "key1")
			must.NoError(t, err)
			must.True(t, got == nil)
		})
	})

	t.Run("appendToList / getList", func(t *testing.T) {
		t.Parallel()

		t.Run("should append and retrieve list items", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			ctx := t.Context()
			must.NoError(t, s.AppendToList(ctx, "list1", json.RawMessage(`{"id":1}`), 0, 0))
			must.NoError(t, s.AppendToList(ctx, "list1", json.RawMessage(`{"id":2}`), 0, 0))
			got, err := s.GetList(ctx, "list1")
			must.NoError(t, err)
			must.Eq(t, []json.RawMessage{json.RawMessage(`{"id":1}`), json.RawMessage(`{"id":2}`)}, got)
		})

		t.Run("should return empty array for non-existent list", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			got, err := s.GetList(t.Context(), "nonexistent")
			must.NoError(t, err)
			must.Eq(t, []json.RawMessage{}, got)
		})

		t.Run("should trim to maxLength, keeping newest", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			ctx := t.Context()
			for i := 1; i <= 5; i++ {
				item, err := json.Marshal(map[string]int{"id": i})
				must.NoError(t, err)
				must.NoError(t, s.AppendToList(ctx, "list1", item, 3, 0))
			}
			got, err := s.GetList(ctx, "list1")
			must.NoError(t, err)
			must.Eq(t, []json.RawMessage{
				json.RawMessage(`{"id":3}`),
				json.RawMessage(`{"id":4}`),
				json.RawMessage(`{"id":5}`),
			}, got)
		})

		t.Run("should respect TTL on lists", func(t *testing.T) {
			t.Parallel()
			s, clk := connectedAt(t, time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC))
			ctx := t.Context()
			must.NoError(t, s.AppendToList(ctx, "list1", json.RawMessage(`{"id":1}`), 0, 10*time.Millisecond))
			clk.Advance(20 * time.Millisecond)
			got, err := s.GetList(ctx, "list1")
			must.NoError(t, err)
			must.Eq(t, []json.RawMessage{}, got)
		})

		t.Run("should refresh TTL on subsequent appends", func(t *testing.T) {
			t.Parallel()
			s, clk := connectedAt(t, time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC))
			ctx := t.Context()
			must.NoError(t, s.AppendToList(ctx, "list1", json.RawMessage(`{"id":1}`), 0, 50*time.Millisecond))
			clk.Advance(30 * time.Millisecond)
			must.NoError(t, s.AppendToList(ctx, "list1", json.RawMessage(`{"id":2}`), 0, 50*time.Millisecond))
			got, err := s.GetList(ctx, "list1")
			must.NoError(t, err)
			must.Eq(t, []json.RawMessage{json.RawMessage(`{"id":1}`), json.RawMessage(`{"id":2}`)}, got)
		})

		t.Run("should keep lists isolated by key", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			ctx := t.Context()
			must.NoError(t, s.AppendToList(ctx, "list-a", raw(t, "a"), 0, 0))
			must.NoError(t, s.AppendToList(ctx, "list-b", raw(t, "b"), 0, 0))
			a, err := s.GetList(ctx, "list-a")
			must.NoError(t, err)
			must.Eq(t, []json.RawMessage{raw(t, "a")}, a)
			b, err := s.GetList(ctx, "list-b")
			must.NoError(t, err)
			must.Eq(t, []json.RawMessage{raw(t, "b")}, b)
		})

		t.Run("should start fresh after expired list", func(t *testing.T) {
			t.Parallel()
			s, clk := connectedAt(t, time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC))
			ctx := t.Context()
			must.NoError(t, s.AppendToList(ctx, "list1", json.RawMessage(`{"id":1}`), 0, 10*time.Millisecond))
			clk.Advance(20 * time.Millisecond)
			must.NoError(t, s.AppendToList(ctx, "list1", json.RawMessage(`{"id":2}`), 0, 0))
			got, err := s.GetList(ctx, "list1")
			must.NoError(t, err)
			must.Eq(t, []json.RawMessage{json.RawMessage(`{"id":2}`)}, got)
		})
	})

	t.Run("enqueue / dequeue / queueDepth", func(t *testing.T) {
		t.Parallel()

		t.Run("should enqueue and dequeue a single entry", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			ctx := t.Context()
			now := s.currentTime()
			entry := chat.QueueEntry{
				Message:    &chat.Message{ID: "m1", Text: "hello"},
				EnqueuedAt: now,
				ExpiresAt:  now.Add(90 * time.Second),
			}
			depth, err := s.Enqueue(ctx, "thread1", entry, 10)
			must.NoError(t, err)
			must.Eq(t, 1, depth)
			got, err := s.Dequeue(ctx, "thread1")
			must.NoError(t, err)
			must.True(t, got != nil)
			must.Eq(t, entry, *got)
		})

		t.Run("should return null when dequeuing from empty queue", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			got, err := s.Dequeue(t.Context(), "thread1")
			must.NoError(t, err)
			must.True(t, got == nil)
		})

		t.Run("should return null when dequeuing from nonexistent thread", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			got, err := s.Dequeue(t.Context(), "nonexistent")
			must.NoError(t, err)
			must.True(t, got == nil)
		})

		t.Run("should return 0 depth for empty queue", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			depth, err := s.QueueDepth(t.Context(), "thread1")
			must.NoError(t, err)
			must.Eq(t, 0, depth)
		})

		t.Run("should dequeue in FIFO order", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			ctx := t.Context()
			expires := s.currentTime().Add(90 * time.Second)
			must.NoError(t, enqueue(t, s, "thread1", "m1", 1000, expires, 10))
			must.NoError(t, enqueue(t, s, "thread1", "m2", 2000, expires, 10))
			must.NoError(t, enqueue(t, s, "thread1", "m3", 3000, expires, 10))
			depth, err := s.QueueDepth(ctx, "thread1")
			must.NoError(t, err)
			must.Eq(t, 3, depth)
			must.Eq(t, "m1", dequeueID(t, s, "thread1"))
			must.Eq(t, "m2", dequeueID(t, s, "thread1"))
			must.Eq(t, "m3", dequeueID(t, s, "thread1"))
			got, err := s.Dequeue(ctx, "thread1")
			must.NoError(t, err)
			must.True(t, got == nil)
			depth, err = s.QueueDepth(ctx, "thread1")
			must.NoError(t, err)
			must.Eq(t, 0, depth)
		})

		t.Run("should trim to maxSize keeping newest entries", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			ctx := t.Context()
			expires := s.currentTime().Add(90 * time.Second)
			for i := 1; i <= 5; i++ {
				must.NoError(t, enqueue(t, s, "thread1", "m"+strconv.Itoa(i), int64(i*1000), expires, 3))
			}
			depth, err := s.QueueDepth(ctx, "thread1")
			must.NoError(t, err)
			must.Eq(t, 3, depth)
			must.Eq(t, "m3", dequeueID(t, s, "thread1"))
			must.Eq(t, "m4", dequeueID(t, s, "thread1"))
			must.Eq(t, "m5", dequeueID(t, s, "thread1"))
		})

		t.Run("should handle maxSize of 1 (debounce behavior)", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			ctx := t.Context()
			expires := s.currentTime().Add(90 * time.Second)
			must.NoError(t, enqueue(t, s, "thread1", "m1", 1000, expires, 1))
			must.NoError(t, enqueue(t, s, "thread1", "m2", 2000, expires, 1))
			must.NoError(t, enqueue(t, s, "thread1", "m3", 3000, expires, 1))
			depth, err := s.QueueDepth(ctx, "thread1")
			must.NoError(t, err)
			must.Eq(t, 1, depth)
			must.Eq(t, "m3", dequeueID(t, s, "thread1"))
		})

		t.Run("should keep queues isolated by thread", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			ctx := t.Context()
			expires := s.currentTime().Add(90 * time.Second)
			must.NoError(t, enqueue(t, s, "thread-a", "a1", 1000, expires, 10))
			must.NoError(t, enqueue(t, s, "thread-b", "b1", 1000, expires, 10))
			da, err := s.QueueDepth(ctx, "thread-a")
			must.NoError(t, err)
			must.Eq(t, 1, da)
			db, err := s.QueueDepth(ctx, "thread-b")
			must.NoError(t, err)
			must.Eq(t, 1, db)
			must.Eq(t, "a1", dequeueID(t, s, "thread-a"))
			must.Eq(t, "b1", dequeueID(t, s, "thread-b"))
		})

		t.Run("should clear queues on disconnect", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			ctx := t.Context()
			expires := s.currentTime().Add(90 * time.Second)
			must.NoError(t, enqueue(t, s, "thread1", "m1", 1000, expires, 10))
			must.NoError(t, s.Disconnect(ctx))
			must.NoError(t, s.Connect(ctx))
			depth, err := s.QueueDepth(ctx, "thread1")
			must.NoError(t, err)
			must.Eq(t, 0, depth)
			got, err := s.Dequeue(ctx, "thread1")
			must.NoError(t, err)
			must.True(t, got == nil)
		})
	})

	t.Run("connection", func(t *testing.T) {
		t.Parallel()

		t.Run("should throw when not connected", func(t *testing.T) {
			t.Parallel()
			s := New()
			err := s.Subscribe(t.Context(), "test")
			must.ErrorContains(t, err, "not connected")
		})

		t.Run("should clear state on disconnect", func(t *testing.T) {
			t.Parallel()
			s := connected(t)
			ctx := t.Context()
			must.NoError(t, s.Subscribe(ctx, "thread1"))
			_, err := s.AcquireLock(ctx, "thread1", 5000*time.Millisecond)
			must.NoError(t, err)
			must.NoError(t, s.Disconnect(ctx))
			must.NoError(t, s.Connect(ctx))
			ok, err := s.IsSubscribed(ctx, "thread1")
			must.NoError(t, err)
			must.False(t, ok)
			lock, err := s.AcquireLock(ctx, "thread1", 5000*time.Millisecond)
			must.NoError(t, err)
			must.True(t, lock != nil)
		})
	})
}

func connected(t *testing.T) *State {
	t.Helper()
	s := New()
	must.NoError(t, s.Connect(t.Context()))
	return s
}

func connectedAt(t *testing.T, now time.Time) (*State, *frozenClock) {
	t.Helper()
	clk := &frozenClock{t: now}
	s := New()
	s.now = clk.Now
	must.NoError(t, s.Connect(t.Context()))
	return s, clk
}

type frozenClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *frozenClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *frozenClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func raw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	must.NoError(t, err)
	return b
}

func enqueue(t *testing.T, s *State, threadID, id string, enqueuedAtMs int64, expiresAt time.Time, maxSize int) error {
	t.Helper()
	_, err := s.Enqueue(t.Context(), threadID, chat.QueueEntry{
		Message:    &chat.Message{ID: id},
		EnqueuedAt: time.UnixMilli(enqueuedAtMs),
		ExpiresAt:  expiresAt,
	}, maxSize)
	return err
}

func dequeueID(t *testing.T, s *State, threadID string) string {
	t.Helper()
	got, err := s.Dequeue(t.Context(), threadID)
	must.NoError(t, err)
	must.True(t, got != nil && got.Message != nil)
	return got.Message.ID
}
