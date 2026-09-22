// Ported from packages/state-memory/src/index.ts @ 6adca36 (chat v4.40.0).
// Divergences: values are stored as copied json.RawMessage bytes (clone on
// write, clone on read) so this backend cannot alias like the TS reference
// store; a `now func() time.Time` field defaulting to time.Now is the TTL
// test seam (no timers or background expiry); lock tokens use crypto/rand
// rather than Date.now+Math.random; stale queue entries (ExpiresAt <= now)
// are discarded on dequeue (types.ts QueueEntry contract; memory upstream
// left that to chat.ts); QueueEntry.Message is stored by pointer (Message
// contains sync.Once and cannot be copied); no NODE_ENV production
// console.warn; test-only _getSubscriptionCount/_getLockCount omitted;
// not-connected error drops the trailing period (ST1005).
package statememory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/northpolesec/chat-go/chat"
)

var errNotConnected = errors.New("MemoryStateAdapter is not connected. Call connect() first")

var _ chat.StateAdapter = (*State)(nil)

// State is an in-memory StateAdapter for development and tests.
type State struct {
	mu            sync.Mutex
	now           func() time.Time
	connected     bool
	cache         map[string]cachedValue
	locks         map[string]chat.Lock
	queues        map[string][]chat.QueueEntry
	subscriptions map[string]struct{}
}

type cachedValue struct {
	value     json.RawMessage
	expiresAt time.Time // zero = no expiry
}

// New returns an empty in-memory state adapter. Call Connect before use.
func New() *State {
	return &State{
		now:           time.Now,
		cache:         make(map[string]cachedValue),
		locks:         make(map[string]chat.Lock),
		queues:        make(map[string][]chat.QueueEntry),
		subscriptions: make(map[string]struct{}),
	}
}

func (s *State) currentTime() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}

func (s *State) ensureConnected() error {
	if !s.connected {
		return errNotConnected
	}
	return nil
}

func (s *State) Connect(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connected = true
	return nil
}

func (s *State) Disconnect(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connected = false
	clear(s.subscriptions)
	clear(s.locks)
	clear(s.queues)
	return nil
}

func (s *State) Subscribe(_ context.Context, threadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureConnected(); err != nil {
		return err
	}
	s.subscriptions[threadID] = struct{}{}
	return nil
}

func (s *State) Unsubscribe(_ context.Context, threadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureConnected(); err != nil {
		return err
	}
	delete(s.subscriptions, threadID)
	return nil
}

func (s *State) IsSubscribed(_ context.Context, threadID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureConnected(); err != nil {
		return false, err
	}
	_, ok := s.subscriptions[threadID]
	return ok, nil
}

func (s *State) AcquireLock(_ context.Context, threadID string, ttl time.Duration) (*chat.Lock, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureConnected(); err != nil {
		return nil, err
	}
	s.cleanExpiredLocks()
	now := s.currentTime()
	if existing, ok := s.locks[threadID]; ok && existing.ExpiresAt.After(now) {
		return nil, nil
	}
	lock := chat.Lock{
		ThreadID:  threadID,
		Token:     generateToken(),
		ExpiresAt: now.Add(ttl),
	}
	s.locks[threadID] = lock
	cp := lock
	return &cp, nil
}

func (s *State) ForceReleaseLock(_ context.Context, threadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureConnected(); err != nil {
		return err
	}
	delete(s.locks, threadID)
	return nil
}

func (s *State) ReleaseLock(_ context.Context, lock *chat.Lock) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureConnected(); err != nil {
		return err
	}
	if lock == nil {
		return nil
	}
	existing, ok := s.locks[lock.ThreadID]
	if ok && existing.Token == lock.Token {
		delete(s.locks, lock.ThreadID)
	}
	return nil
}

func (s *State) ExtendLock(_ context.Context, lock *chat.Lock, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureConnected(); err != nil {
		return false, err
	}
	if lock == nil {
		return false, nil
	}
	existing, ok := s.locks[lock.ThreadID]
	if !ok || existing.Token != lock.Token {
		return false, nil
	}
	now := s.currentTime()
	if existing.ExpiresAt.Before(now) {
		delete(s.locks, lock.ThreadID)
		return false, nil
	}
	existing.ExpiresAt = now.Add(ttl)
	s.locks[lock.ThreadID] = existing
	return true, nil
}

func (s *State) Get(_ context.Context, key string) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureConnected(); err != nil {
		return nil, err
	}
	cached, ok := s.cache[key]
	if !ok {
		return nil, nil
	}
	if expired(cached.expiresAt, s.currentTime()) {
		delete(s.cache, key)
		return nil, nil
	}
	return cloneRaw(cached.value), nil
}

func (s *State) Set(_ context.Context, key string, value json.RawMessage, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureConnected(); err != nil {
		return err
	}
	s.cache[key] = cachedValue{
		value:     cloneRaw(value),
		expiresAt: expiry(s.currentTime(), ttl),
	}
	return nil
}

func (s *State) SetIfNotExists(_ context.Context, key string, value json.RawMessage, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureConnected(); err != nil {
		return false, err
	}
	if existing, ok := s.cache[key]; ok {
		if expired(existing.expiresAt, s.currentTime()) {
			delete(s.cache, key)
		} else {
			return false, nil
		}
	}
	s.cache[key] = cachedValue{
		value:     cloneRaw(value),
		expiresAt: expiry(s.currentTime(), ttl),
	}
	return true, nil
}

func (s *State) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureConnected(); err != nil {
		return err
	}
	delete(s.cache, key)
	return nil
}

func (s *State) AppendToList(_ context.Context, key string, value json.RawMessage, maxLength int, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureConnected(); err != nil {
		return err
	}
	now := s.currentTime()
	var list []json.RawMessage
	if cached, ok := s.cache[key]; ok && !expired(cached.expiresAt, now) {
		if err := json.Unmarshal(cached.value, &list); err != nil {
			list = nil
		}
	}
	list = append(list, cloneRaw(value))
	if maxLength > 0 && len(list) > maxLength {
		list = list[len(list)-maxLength:]
	}
	raw, err := json.Marshal(list)
	if err != nil {
		return fmt.Errorf("marshal list %q: %w", key, err)
	}
	s.cache[key] = cachedValue{
		value:     raw,
		expiresAt: expiry(now, ttl),
	}
	return nil
}

func (s *State) GetList(_ context.Context, key string) ([]json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureConnected(); err != nil {
		return nil, err
	}
	cached, ok := s.cache[key]
	if !ok {
		return []json.RawMessage{}, nil
	}
	if expired(cached.expiresAt, s.currentTime()) {
		delete(s.cache, key)
		return []json.RawMessage{}, nil
	}
	var list []json.RawMessage
	if err := json.Unmarshal(cached.value, &list); err != nil {
		return []json.RawMessage{}, nil
	}
	out := make([]json.RawMessage, len(list))
	for i, item := range list {
		out[i] = cloneRaw(item)
	}
	return out, nil
}

func (s *State) Enqueue(_ context.Context, threadID string, entry chat.QueueEntry, maxSize int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureConnected(); err != nil {
		return 0, err
	}
	q := append(s.queues[threadID], entry)
	if len(q) > maxSize {
		q = q[len(q)-maxSize:]
	}
	s.queues[threadID] = q
	return len(q), nil
}

func (s *State) Dequeue(_ context.Context, threadID string) (*chat.QueueEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureConnected(); err != nil {
		return nil, err
	}
	now := s.currentTime()
	for {
		q := s.queues[threadID]
		if len(q) == 0 {
			delete(s.queues, threadID)
			return nil, nil
		}
		entry := q[0]
		s.queues[threadID] = q[1:]
		if len(s.queues[threadID]) == 0 {
			delete(s.queues, threadID)
		}
		if entry.ExpiresAt.After(now) {
			cp := entry
			return &cp, nil
		}
	}
}

func (s *State) QueueDepth(_ context.Context, threadID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureConnected(); err != nil {
		return 0, err
	}
	return len(s.queues[threadID]), nil
}

func (s *State) cleanExpiredLocks() {
	now := s.currentTime()
	for id, lock := range s.locks {
		if !lock.ExpiresAt.After(now) {
			delete(s.locks, id)
		}
	}
}

func expired(expiresAt, now time.Time) bool {
	return !expiresAt.IsZero() && !expiresAt.After(now)
}

func expiry(now time.Time, ttl time.Duration) time.Time {
	if ttl <= 0 {
		return time.Time{}
	}
	return now.Add(ttl)
}

func cloneRaw(v json.RawMessage) json.RawMessage {
	if v == nil {
		return nil
	}
	out := make(json.RawMessage, len(v))
	copy(out, v)
	return out
}

func generateToken() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("mem_%d", time.Now().UnixNano())
	}
	return "mem_" + hex.EncodeToString(b[:])
}
