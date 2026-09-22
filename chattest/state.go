// Conformance suite derived from state-memory and state-pg test suites @ 6adca36 (chat v4.40.0); see PORTING.md.
package chattest

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/northpolesec/chat-go/chat"
	"github.com/shoenig/test/must"
)

const (
	shortTTL  = 100 * time.Millisecond // was 25ms; statepg needs real round-trip margin
	longTTL   = 5 * time.Second
	queueTTL  = 90 * time.Second
	pollEvery = 5 * time.Millisecond // was 2ms
	pollLimit = 2 * time.Second      // was 400ms
)

// RunStateAdapterTests exercises the full chat.StateAdapter contract:
// KV get/set/delete/setnx + TTL expiry, list append/trim/TTL, lock
// acquire/extend/release/force-release token semantics, queue
// FIFO/depth/max-size/stale-discard, subscriptions.
func RunStateAdapterTests(t *testing.T, factory func(t *testing.T) chat.StateAdapter) {
	t.Run("subscriptions", func(t *testing.T) {
		t.Parallel()
		testSubscriptions(t, factory)
	})
	t.Run("locking", func(t *testing.T) {
		t.Parallel()
		testLocking(t, factory)
	})
	t.Run("get / set / delete", func(t *testing.T) {
		t.Parallel()
		testKV(t, factory)
	})
	t.Run("setIfNotExists", func(t *testing.T) {
		t.Parallel()
		testSetIfNotExists(t, factory)
	})
	t.Run("appendToList / getList", func(t *testing.T) {
		t.Parallel()
		testLists(t, factory)
	})
	t.Run("enqueue / dequeue / queueDepth", func(t *testing.T) {
		t.Parallel()
		testQueue(t, factory)
	})
	t.Run("connection", func(t *testing.T) {
		t.Parallel()
		testConnection(t, factory)
	})
}

func testSubscriptions(t *testing.T, factory func(t *testing.T) chat.StateAdapter) {
	t.Run("should subscribe to a thread", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		must.NoError(t, s.Subscribe(ctx, "slack:C123:1234.5678"), clause("Subscribe of a new thread succeeds"))
		ok, err := s.IsSubscribed(ctx, "slack:C123:1234.5678")
		must.NoError(t, err, clause("IsSubscribed after Subscribe"))
		must.True(t, ok, clause("IsSubscribed is true after Subscribe"))
	})

	t.Run("should unsubscribe from a thread", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		must.NoError(t, s.Subscribe(ctx, "slack:C123:1234.5678"), clause("Subscribe before Unsubscribe"))
		must.NoError(t, s.Unsubscribe(ctx, "slack:C123:1234.5678"), clause("Unsubscribe of a subscribed thread succeeds"))
		ok, err := s.IsSubscribed(ctx, "slack:C123:1234.5678")
		must.NoError(t, err, clause("IsSubscribed after Unsubscribe"))
		must.False(t, ok, clause("IsSubscribed is false after Unsubscribe"))
	})
}

func testLocking(t *testing.T, factory func(t *testing.T) chat.StateAdapter) {
	t.Run("should acquire a lock", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		lock, err := s.AcquireLock(t.Context(), "thread1", longTTL)
		must.NoError(t, err, clause("AcquireLock of a free thread succeeds"))
		must.True(t, lock != nil, clause("AcquireLock returns a lock when the thread is free"))
		must.Eq(t, "thread1", lock.ThreadID, clause("AcquireLock sets ThreadID"))
		must.True(t, lock.Token != "", clause("AcquireLock issues a non-empty token"))
	})

	t.Run("should prevent double-locking", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		lock1, err := s.AcquireLock(ctx, "thread1", longTTL)
		must.NoError(t, err, clause("first AcquireLock"))
		lock2, err := s.AcquireLock(ctx, "thread1", longTTL)
		must.NoError(t, err, clause("second AcquireLock while held"))
		must.True(t, lock1 != nil, clause("first AcquireLock returns a lock"))
		must.True(t, lock2 == nil, clause("AcquireLock returns nil while the thread is held"))
	})

	t.Run("should release a lock", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		lock, err := s.AcquireLock(ctx, "thread1", longTTL)
		must.NoError(t, err, clause("AcquireLock before ReleaseLock"))
		must.True(t, lock != nil, clause("AcquireLock before ReleaseLock returns a lock"))
		must.NoError(t, s.ReleaseLock(ctx, lock), clause("ReleaseLock with the holding token succeeds"))
		lock2, err := s.AcquireLock(ctx, "thread1", longTTL)
		must.NoError(t, err, clause("AcquireLock after ReleaseLock"))
		must.True(t, lock2 != nil, clause("AcquireLock succeeds after ReleaseLock"))
	})

	t.Run("should not release a lock with wrong token", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		lock, err := s.AcquireLock(ctx, "thread1", longTTL)
		must.NoError(t, err, clause("AcquireLock before wrong-token ReleaseLock"))

		must.NoError(t, s.ReleaseLock(ctx, &chat.Lock{
			ThreadID:  "thread1",
			Token:     "fake-token",
			ExpiresAt: time.Now().Add(longTTL),
		}), clause("ReleaseLock with a wrong token is a no-op (no error)"))

		lock2, err := s.AcquireLock(ctx, "thread1", longTTL)
		must.NoError(t, err, clause("AcquireLock after wrong-token ReleaseLock"))
		must.True(t, lock2 == nil, clause("wrong-token ReleaseLock must not drop the held lock"))

		must.True(t, lock != nil, clause("original lock is present for cleanup"))
		must.NoError(t, s.ReleaseLock(ctx, lock), clause("ReleaseLock with the holding token"))
	})

	t.Run("should allow re-locking after expiry", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		lock1, err := s.AcquireLock(ctx, "thread1", shortTTL)
		must.NoError(t, err, clause("AcquireLock with a short TTL"))
		must.True(t, lock1 != nil, clause("short-TTL AcquireLock returns a lock"))

		var lock2 *chat.Lock
		waitUntil(t, "AcquireLock succeeds after the prior lock expires", func() bool {
			var acqErr error
			lock2, acqErr = s.AcquireLock(ctx, "thread1", longTTL)
			must.NoError(t, acqErr, clause("AcquireLock while polling for expiry"))
			return lock2 != nil
		})
		must.NotEq(t, lock1.Token, lock2.Token, clause("a lock acquired after expiry has a new token"))
	})

	t.Run("should extend a lock", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		lock, err := s.AcquireLock(ctx, "thread1", 100*time.Millisecond)
		must.NoError(t, err, clause("AcquireLock before ExtendLock"))
		must.True(t, lock != nil, clause("AcquireLock before ExtendLock returns a lock"))
		extended, err := s.ExtendLock(ctx, lock, longTTL)
		must.NoError(t, err, clause("ExtendLock of a live lock"))
		must.True(t, extended, clause("ExtendLock of a live lock with the holding token returns true"))
		lock2, err := s.AcquireLock(ctx, "thread1", longTTL)
		must.NoError(t, err, clause("AcquireLock after ExtendLock"))
		must.True(t, lock2 == nil, clause("ExtendLock keeps the thread held"))
	})

	t.Run("should force-release a lock regardless of token", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		lock, err := s.AcquireLock(ctx, "thread1", longTTL)
		must.NoError(t, err, clause("AcquireLock before ForceReleaseLock"))
		must.True(t, lock != nil, clause("AcquireLock before ForceReleaseLock returns a lock"))
		must.NoError(t, s.ForceReleaseLock(ctx, "thread1"), clause("ForceReleaseLock ignores the token"))
		lock2, err := s.AcquireLock(ctx, "thread1", longTTL)
		must.NoError(t, err, clause("AcquireLock after ForceReleaseLock"))
		must.True(t, lock2 != nil, clause("ForceReleaseLock makes the thread acquirable"))
		must.NotEq(t, lock.Token, lock2.Token, clause("a lock acquired after ForceReleaseLock has a new token"))
	})

	t.Run("should no-op when force-releasing a non-existent lock", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		must.NoError(t, s.ForceReleaseLock(t.Context(), "nonexistent"), clause("ForceReleaseLock of a missing thread is a no-op"))
	})

	t.Run("should not extend an expired lock", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		lock, err := s.AcquireLock(ctx, "thread1", shortTTL)
		must.NoError(t, err, clause("AcquireLock with a short TTL before extend-after-expiry"))
		must.True(t, lock != nil, clause("short-TTL AcquireLock returns a lock"))

		// Do not poll ExtendLock — a live extend would refresh TTL and never expire.
		waitElapsed(t, 2*shortTTL)
		extended, err := s.ExtendLock(ctx, lock, longTTL)
		must.NoError(t, err, clause("ExtendLock of an expired lock"))
		must.False(t, extended, clause("ExtendLock never resurrects an expired lock"))

		lock2, err := s.AcquireLock(ctx, "thread1", longTTL)
		must.NoError(t, err, clause("AcquireLock after failed extend-after-expiry"))
		must.True(t, lock2 != nil, clause("failed ExtendLock must leave the thread free (never resurrects)"))
	})
}

func testKV(t *testing.T, factory func(t *testing.T) chat.StateAdapter) {
	t.Run("should set and get a value", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		want := rawJSON(t, "value1")
		must.NoError(t, s.Set(ctx, "key1", want, 0), clause("Set of a new key succeeds"))
		got, err := s.Get(ctx, "key1")
		must.NoError(t, err, clause("Get after Set"))
		must.Eq(t, want, got, clause("Get after Set returns the stored bytes"))
	})

	t.Run("should return null for a missing key", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		got, err := s.Get(t.Context(), "missing")
		must.NoError(t, err, clause("Get of a missing key"))
		must.Nil(t, got, clause("Get of a missing key returns (nil, nil)"))
	})

	t.Run("should overwrite an existing key", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		must.NoError(t, s.Set(ctx, "key1", rawJSON(t, "first"), 0), clause("Set first value"))
		must.NoError(t, s.Set(ctx, "key1", rawJSON(t, "second"), 0), clause("Set overwrites the same key"))
		got, err := s.Get(ctx, "key1")
		must.NoError(t, err, clause("Get after overwrite"))
		must.Eq(t, rawJSON(t, "second"), got, clause("Set replaces the previous value"))
	})

	t.Run("should delete a value", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		must.NoError(t, s.Set(ctx, "key1", rawJSON(t, "value1"), 0), clause("Set before Delete"))
		must.NoError(t, s.Delete(ctx, "key1"), clause("Delete of an existing key succeeds"))
		got, err := s.Get(ctx, "key1")
		must.NoError(t, err, clause("Get after Delete"))
		must.Nil(t, got, clause("Get after Delete returns missing"))
	})

	t.Run("should no-op when deleting a missing key", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		must.NoError(t, s.Delete(t.Context(), "missing"), clause("Delete of a missing key is a no-op"))
	})

	t.Run("should expire a set value after TTL", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		want := rawJSON(t, "value")
		must.NoError(t, s.Set(ctx, "key1", want, shortTTL), clause("Set with TTL"))
		got, err := s.Get(ctx, "key1")
		must.NoError(t, err, clause("Get before TTL expiry"))
		must.Eq(t, want, got, clause("Get returns the value before TTL expiry"))
		waitUntil(t, "Get returns nil after Set TTL expiry", func() bool {
			got, err = s.Get(ctx, "key1")
			must.NoError(t, err, clause("Get while polling Set TTL expiry"))
			return got == nil
		})
	})

	t.Run("should not alias stored KV bytes", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		original := json.RawMessage(`{"n":1}`)
		must.NoError(t, s.Set(ctx, "key1", original, 0), clause("Set before aliasing check"))
		got, err := s.Get(ctx, "key1")
		must.NoError(t, err, clause("Get before mutating returned bytes"))
		must.Eq(t, original, got, clause("Get returns the stored bytes"))
		must.True(t, len(got) > 0, clause("Get returned bytes to mutate"))
		got[0] = 'x'
		reread, err := s.Get(ctx, "key1")
		must.NoError(t, err, clause("Get after mutating the previous result"))
		must.Eq(t, original, reread, clause("Get result must not alias the stored value; mutating it leaves the store unchanged"))
	})
}

func testSetIfNotExists(t *testing.T, factory func(t *testing.T) chat.StateAdapter) {
	t.Run("should set a value when key does not exist", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		ok, err := s.SetIfNotExists(ctx, "key1", rawJSON(t, "value1"), 0)
		must.NoError(t, err, clause("SetIfNotExists of a missing key"))
		must.True(t, ok, clause("SetIfNotExists returns true when the key is missing"))
		got, err := s.Get(ctx, "key1")
		must.NoError(t, err, clause("Get after SetIfNotExists"))
		must.Eq(t, rawJSON(t, "value1"), got, clause("SetIfNotExists stores the value"))
	})

	t.Run("should not overwrite an existing key", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		_, err := s.SetIfNotExists(ctx, "key1", rawJSON(t, "first"), 0)
		must.NoError(t, err, clause("first SetIfNotExists"))
		ok, err := s.SetIfNotExists(ctx, "key1", rawJSON(t, "second"), 0)
		must.NoError(t, err, clause("SetIfNotExists of a live key"))
		must.False(t, ok, clause("SetIfNotExists returns false when the key exists"))
		got, err := s.Get(ctx, "key1")
		must.NoError(t, err, clause("Get after rejected SetIfNotExists"))
		must.Eq(t, rawJSON(t, "first"), got, clause("SetIfNotExists must not overwrite a live key"))
	})

	t.Run("should allow setting after TTL expiry", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		_, err := s.SetIfNotExists(ctx, "key1", rawJSON(t, "first"), shortTTL)
		must.NoError(t, err, clause("SetIfNotExists with TTL"))
		waitUntil(t, "SetIfNotExists succeeds after the prior value expires", func() bool {
			ok, err := s.SetIfNotExists(ctx, "key1", rawJSON(t, "second"), 0)
			must.NoError(t, err, clause("SetIfNotExists while polling TTL expiry"))
			return ok
		})
		got, err := s.Get(ctx, "key1")
		must.NoError(t, err, clause("Get after SetIfNotExists replaced an expired key"))
		must.Eq(t, rawJSON(t, "second"), got, clause("SetIfNotExists may replace an expired key"))
	})

	t.Run("should respect TTL on the new value", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		_, err := s.SetIfNotExists(ctx, "key1", rawJSON(t, "value"), shortTTL)
		must.NoError(t, err, clause("SetIfNotExists with TTL"))
		waitUntil(t, "Get returns nil after SetIfNotExists TTL expiry", func() bool {
			got, err := s.Get(ctx, "key1")
			must.NoError(t, err, clause("Get while polling SetIfNotExists TTL expiry"))
			return got == nil
		})
	})
}

func testLists(t *testing.T, factory func(t *testing.T) chat.StateAdapter) {
	t.Run("should append and retrieve list items", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		must.NoError(t, s.AppendToList(ctx, "list1", json.RawMessage(`{"id":1}`), 0, 0), clause("first AppendToList"))
		must.NoError(t, s.AppendToList(ctx, "list1", json.RawMessage(`{"id":2}`), 0, 0), clause("second AppendToList"))
		got, err := s.GetList(ctx, "list1")
		must.NoError(t, err, clause("GetList after appends"))
		must.Eq(t, []json.RawMessage{json.RawMessage(`{"id":1}`), json.RawMessage(`{"id":2}`)}, got, clause("GetList returns items in append order"))
	})

	t.Run("should return empty array for non-existent list", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		got, err := s.GetList(t.Context(), "nonexistent")
		must.NoError(t, err, clause("GetList of a missing key"))
		// statepg-friendly: nil or empty both mean a missing list.
		must.True(t, len(got) == 0, clause("GetList of a missing key returns an empty list"))
	})

	t.Run("should trim to maxLength, keeping newest", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		for i := 1; i <= 5; i++ {
			item, err := json.Marshal(map[string]int{"id": i})
			must.NoError(t, err)
			must.NoError(t, s.AppendToList(ctx, "list1", item, 3, 0), clause("AppendToList with maxLength"))
		}
		got, err := s.GetList(ctx, "list1")
		must.NoError(t, err, clause("GetList after maxLength trim"))
		must.Eq(t, []json.RawMessage{
			json.RawMessage(`{"id":3}`),
			json.RawMessage(`{"id":4}`),
			json.RawMessage(`{"id":5}`),
		}, got, clause("AppendToList maxLength keeps the newest items"))
	})

	t.Run("should respect TTL on lists", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		must.NoError(t, s.AppendToList(ctx, "list1", json.RawMessage(`{"id":1}`), 0, shortTTL), clause("AppendToList with TTL"))
		waitUntil(t, "GetList is empty after list TTL expiry", func() bool {
			got, err := s.GetList(ctx, "list1")
			must.NoError(t, err, clause("GetList while polling list TTL expiry"))
			return len(got) == 0
		})
	})

	t.Run("should refresh TTL on subsequent appends", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		ttl := 80 * time.Millisecond
		must.NoError(t, s.AppendToList(ctx, "list1", json.RawMessage(`{"id":1}`), 0, ttl), clause("first AppendToList with TTL"))
		waitElapsed(t, 20*time.Millisecond)
		must.NoError(t, s.AppendToList(ctx, "list1", json.RawMessage(`{"id":2}`), 0, ttl), clause("AppendToList refreshes list TTL"))
		got, err := s.GetList(ctx, "list1")
		must.NoError(t, err, clause("GetList after TTL refresh"))
		must.Eq(t, []json.RawMessage{json.RawMessage(`{"id":1}`), json.RawMessage(`{"id":2}`)}, got, clause("a subsequent AppendToList with TTL keeps prior items"))
	})

	t.Run("should keep lists isolated by key", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		must.NoError(t, s.AppendToList(ctx, "list-a", rawJSON(t, "a"), 0, 0), clause("AppendToList list-a"))
		must.NoError(t, s.AppendToList(ctx, "list-b", rawJSON(t, "b"), 0, 0), clause("AppendToList list-b"))
		a, err := s.GetList(ctx, "list-a")
		must.NoError(t, err, clause("GetList list-a"))
		must.Eq(t, []json.RawMessage{rawJSON(t, "a")}, a, clause("lists are isolated by key"))
		b, err := s.GetList(ctx, "list-b")
		must.NoError(t, err, clause("GetList list-b"))
		must.Eq(t, []json.RawMessage{rawJSON(t, "b")}, b, clause("lists are isolated by key"))
	})

	t.Run("should start fresh after expired list", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		must.NoError(t, s.AppendToList(ctx, "list1", json.RawMessage(`{"id":1}`), 0, shortTTL), clause("AppendToList with TTL"))
		waitUntil(t, "GetList is empty after list TTL expiry", func() bool {
			got, err := s.GetList(ctx, "list1")
			must.NoError(t, err, clause("GetList while polling list TTL expiry"))
			return len(got) == 0
		})
		must.NoError(t, s.AppendToList(ctx, "list1", json.RawMessage(`{"id":2}`), 0, 0), clause("AppendToList after expiry"))
		got, err := s.GetList(ctx, "list1")
		must.NoError(t, err, clause("GetList after append onto an expired list"))
		must.Eq(t, []json.RawMessage{json.RawMessage(`{"id":2}`)}, got, clause("an expired list is replaced, not appended to"))
	})
}

func testQueue(t *testing.T, factory func(t *testing.T) chat.StateAdapter) {
	t.Run("should enqueue and dequeue a single entry", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		now := time.Now()
		entry := chat.QueueEntry{
			Message:    &chat.Message{ID: "m1", Text: "hello"},
			EnqueuedAt: now,
			ExpiresAt:  now.Add(queueTTL),
		}
		depth, err := s.Enqueue(ctx, "thread1", entry, 10)
		must.NoError(t, err, clause("Enqueue of a single entry"))
		must.Eq(t, 1, depth, clause("Enqueue returns depth 1 for the first entry"))
		got, err := s.Dequeue(ctx, "thread1")
		must.NoError(t, err, clause("Dequeue after single Enqueue"))
		must.True(t, got != nil && got.Message != nil, clause("Dequeue returns the enqueued entry"))
		must.Eq(t, "m1", got.Message.ID, clause("Dequeue returns the enqueued message id"))
		must.Eq(t, "hello", got.Message.Text, clause("Dequeue returns the enqueued message text"))
		assertTimeMS(t, entry.EnqueuedAt, got.EnqueuedAt, "Dequeue preserves EnqueuedAt")
		assertTimeMS(t, entry.ExpiresAt, got.ExpiresAt, "Dequeue preserves ExpiresAt")
	})

	t.Run("should return null when dequeuing from empty queue", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		got, err := s.Dequeue(t.Context(), "thread1")
		must.NoError(t, err, clause("Dequeue of an empty queue"))
		must.Nil(t, got, clause("Dequeue of an empty queue returns nil"))
	})

	t.Run("should return null when dequeuing from nonexistent thread", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		got, err := s.Dequeue(t.Context(), "nonexistent")
		must.NoError(t, err, clause("Dequeue of a missing thread"))
		must.Nil(t, got, clause("Dequeue of a missing thread returns nil"))
	})

	t.Run("should return 0 depth for empty queue", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		depth, err := s.QueueDepth(t.Context(), "thread1")
		must.NoError(t, err, clause("QueueDepth of an empty queue"))
		must.Eq(t, 0, depth, clause("QueueDepth of an empty queue is 0"))
	})

	t.Run("should dequeue in FIFO order", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		expires := time.Now().Add(queueTTL)
		must.NoError(t, enqueue(t, s, "thread1", "m1", 1000, expires, 10))
		must.NoError(t, enqueue(t, s, "thread1", "m2", 2000, expires, 10))
		must.NoError(t, enqueue(t, s, "thread1", "m3", 3000, expires, 10))
		depth, err := s.QueueDepth(ctx, "thread1")
		must.NoError(t, err, clause("QueueDepth after three enqueues"))
		must.Eq(t, 3, depth, clause("QueueDepth counts enqueued entries"))
		must.Eq(t, "m1", dequeueID(t, s, "thread1"), clause("Dequeue is FIFO (first)"))
		must.Eq(t, "m2", dequeueID(t, s, "thread1"), clause("Dequeue is FIFO (second)"))
		must.Eq(t, "m3", dequeueID(t, s, "thread1"), clause("Dequeue is FIFO (third)"))
		got, err := s.Dequeue(ctx, "thread1")
		must.NoError(t, err, clause("Dequeue after draining"))
		must.Nil(t, got, clause("Dequeue after draining returns nil"))
		depth, err = s.QueueDepth(ctx, "thread1")
		must.NoError(t, err, clause("QueueDepth after draining"))
		must.Eq(t, 0, depth, clause("QueueDepth after draining is 0"))
	})

	t.Run("should trim to maxSize keeping newest entries", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		expires := time.Now().Add(queueTTL)
		for i := 1; i <= 5; i++ {
			must.NoError(t, enqueue(t, s, "thread1", "m"+strconv.Itoa(i), int64(i*1000), expires, 3))
		}
		depth, err := s.QueueDepth(ctx, "thread1")
		must.NoError(t, err, clause("QueueDepth after maxSize trim"))
		must.Eq(t, 3, depth, clause("Enqueue maxSize keeps the newest entries"))
		must.Eq(t, "m3", dequeueID(t, s, "thread1"), clause("trimmed queue dequeues the oldest kept entry first"))
		must.Eq(t, "m4", dequeueID(t, s, "thread1"), clause("trimmed queue FIFO (second)"))
		must.Eq(t, "m5", dequeueID(t, s, "thread1"), clause("trimmed queue FIFO (newest)"))
	})

	t.Run("should handle maxSize of 1 (debounce behavior)", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		expires := time.Now().Add(queueTTL)
		must.NoError(t, enqueue(t, s, "thread1", "m1", 1000, expires, 1))
		must.NoError(t, enqueue(t, s, "thread1", "m2", 2000, expires, 1))
		must.NoError(t, enqueue(t, s, "thread1", "m3", 3000, expires, 1))
		depth, err := s.QueueDepth(ctx, "thread1")
		must.NoError(t, err, clause("QueueDepth with maxSize 1"))
		must.Eq(t, 1, depth, clause("maxSize 1 keeps only the newest entry"))
		must.Eq(t, "m3", dequeueID(t, s, "thread1"), clause("maxSize 1 debounce keeps the last enqueued message"))
	})

	t.Run("should keep queues isolated by thread", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		expires := time.Now().Add(queueTTL)
		must.NoError(t, enqueue(t, s, "thread-a", "a1", 1000, expires, 10))
		must.NoError(t, enqueue(t, s, "thread-b", "b1", 1000, expires, 10))
		da, err := s.QueueDepth(ctx, "thread-a")
		must.NoError(t, err, clause("QueueDepth thread-a"))
		must.Eq(t, 1, da, clause("queues are isolated by thread"))
		db, err := s.QueueDepth(ctx, "thread-b")
		must.NoError(t, err, clause("QueueDepth thread-b"))
		must.Eq(t, 1, db, clause("queues are isolated by thread"))
		must.Eq(t, "a1", dequeueID(t, s, "thread-a"), clause("dequeue thread-a"))
		must.Eq(t, "b1", dequeueID(t, s, "thread-b"), clause("dequeue thread-b"))
	})

	t.Run("should skip stale entries on dequeue", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		now := time.Now()
		must.NoError(t, enqueue(t, s, "thread1", "stale", 1000, now.Add(shortTTL), 10))
		must.NoError(t, enqueue(t, s, "thread1", "fresh", 2000, now.Add(queueTTL), 10))
		// Do not poll Dequeue — a live head would be returned before it goes stale.
		waitElapsed(t, 2*shortTTL)
		must.Eq(t, "fresh", dequeueID(t, s, "thread1"), clause("Dequeue discards stale entries and returns the next live one"))
		got, err := s.Dequeue(ctx, "thread1")
		must.NoError(t, err, clause("Dequeue after stale-skip drained the live entries"))
		must.Nil(t, got, clause("only the fresh entry remains after stale-skip"))
	})
}

func testConnection(t *testing.T, factory func(t *testing.T) chat.StateAdapter) {
	t.Run("should throw when not connected", func(t *testing.T) {
		t.Parallel()
		lock := &chat.Lock{ThreadID: "thread1", Token: "tok", ExpiresAt: time.Now().Add(longTTL)}
		entry := chat.QueueEntry{
			Message:    &chat.Message{ID: "m1"},
			EnqueuedAt: time.Now(),
			ExpiresAt:  time.Now().Add(queueTTL),
		}
		ops := []struct {
			name string
			fn   func(chat.StateAdapter) error
		}{
			{"subscribe", func(s chat.StateAdapter) error { return s.Subscribe(t.Context(), "test") }},
			{"unsubscribe", func(s chat.StateAdapter) error { return s.Unsubscribe(t.Context(), "test") }},
			{"isSubscribed", func(s chat.StateAdapter) error { _, err := s.IsSubscribed(t.Context(), "test"); return err }},
			{"acquireLock", func(s chat.StateAdapter) error { _, err := s.AcquireLock(t.Context(), "thread1", longTTL); return err }},
			{"releaseLock", func(s chat.StateAdapter) error { return s.ReleaseLock(t.Context(), lock) }},
			{"extendLock", func(s chat.StateAdapter) error { _, err := s.ExtendLock(t.Context(), lock, longTTL); return err }},
			{"forceReleaseLock", func(s chat.StateAdapter) error { return s.ForceReleaseLock(t.Context(), "thread1") }},
			{"get", func(s chat.StateAdapter) error { _, err := s.Get(t.Context(), "key"); return err }},
			{"set", func(s chat.StateAdapter) error { return s.Set(t.Context(), "key", rawJSON(t, "v"), 0) }},
			{"setIfNotExists", func(s chat.StateAdapter) error {
				_, err := s.SetIfNotExists(t.Context(), "key", rawJSON(t, "v"), 0)
				return err
			}},
			{"delete", func(s chat.StateAdapter) error { return s.Delete(t.Context(), "key") }},
			{"getList", func(s chat.StateAdapter) error { _, err := s.GetList(t.Context(), "list"); return err }},
			{"appendToList", func(s chat.StateAdapter) error {
				return s.AppendToList(t.Context(), "list", rawJSON(t, "v"), 0, 0)
			}},
			{"enqueue", func(s chat.StateAdapter) error { _, err := s.Enqueue(t.Context(), "thread1", entry, 10); return err }},
			{"dequeue", func(s chat.StateAdapter) error { _, err := s.Dequeue(t.Context(), "thread1"); return err }},
			{"queueDepth", func(s chat.StateAdapter) error { _, err := s.QueueDepth(t.Context(), "thread1"); return err }},
		}
		for _, tc := range ops {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				s := factory(t)
				err := tc.fn(s)
				must.ErrorContains(t, err, "not connected", clause(tc.name+" before Connect returns a not-connected error"))
			})
		}
	})

	t.Run("should be idempotent on connect", func(t *testing.T) {
		t.Parallel()
		s := factory(t)
		ctx := t.Context()
		must.NoError(t, s.Connect(ctx), clause("first Connect"))
		must.NoError(t, s.Connect(ctx), clause("Connect is idempotent"))
		t.Cleanup(func() {
			c, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = s.Disconnect(c)
		})
	})

	t.Run("should be idempotent on disconnect", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		must.NoError(t, s.Disconnect(ctx), clause("first Disconnect"))
		must.NoError(t, s.Disconnect(ctx), clause("Disconnect is idempotent"))
	})

	t.Run("should throw after disconnect", func(t *testing.T) {
		t.Parallel()
		s := connected(t, factory)
		ctx := t.Context()
		must.NoError(t, s.Disconnect(ctx), clause("Disconnect before a post-disconnect call"))
		err := s.Subscribe(ctx, "test")
		must.ErrorContains(t, err, "not connected", clause("Subscribe after Disconnect returns a not-connected error"))
	})
}

func connected(t *testing.T, factory func(t *testing.T) chat.StateAdapter) chat.StateAdapter {
	t.Helper()
	s := factory(t)
	must.NoError(t, s.Connect(t.Context()), clause("Connect before use"))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = s.Disconnect(ctx)
	})
	return s
}

func clause(s string) must.Setting {
	return must.Sprint("contract: " + s)
}

func rawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	must.NoError(t, err)
	return b
}

func waitUntil(t *testing.T, name string, ready func() bool) {
	t.Helper()
	if ready() {
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), pollLimit)
	defer cancel()
	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("contract: %s: timed out after %s", name, pollLimit)
		case <-ticker.C:
			if ready() {
				return
			}
		}
	}
}

func waitElapsed(t *testing.T, d time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), d+pollLimit)
	defer cancel()
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		t.Fatalf("contract: timed out waiting %s to elapse", d)
	case <-timer.C:
	}
}

func enqueue(t *testing.T, s chat.StateAdapter, threadID, id string, enqueuedAtMs int64, expiresAt time.Time, maxSize int) error {
	t.Helper()
	_, err := s.Enqueue(t.Context(), threadID, chat.QueueEntry{
		Message:    &chat.Message{ID: id},
		EnqueuedAt: time.UnixMilli(enqueuedAtMs),
		ExpiresAt:  expiresAt,
	}, maxSize)
	return err
}

func dequeueID(t *testing.T, s chat.StateAdapter, threadID string) string {
	t.Helper()
	got, err := s.Dequeue(t.Context(), threadID)
	must.NoError(t, err, clause("Dequeue"))
	must.True(t, got != nil && got.Message != nil, clause("Dequeue returns a live entry with a message"))
	return got.Message.ID
}

func assertTimeMS(t *testing.T, want, got time.Time, name string) {
	t.Helper()
	must.Eq(t, want.UnixMilli(), got.UnixMilli(), clause(name+" (millisecond)"))
}
