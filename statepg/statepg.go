// Package statepg is a Postgres chat.StateAdapter over an accepted pgx
// pool. It never creates tables: the consumer executes Schema (exported,
// embedded) in its own migration path, and the conformance tests execute
// it per test database.
//
// pg-specific semantics (recorded in PORTING.md Phase D when the suite was
// written): QueueDepth counts only live entries (expires_at > now());
// Dequeue purges stale rows before taking the head; Disconnect closes this
// adapter's gate but persists all data and does NOT close the accepted
// pool (the caller owns it).
//
// ponytail: no key_prefix column — single-bot. If a second bot ever
// shares the database, add the column and widen every primary key
// (upstream state-pg's shape).
package statepg

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/northpolesec/chat-go/chat"
)

// Schema is the chat_state_* DDL (idempotent, IF NOT EXISTS throughout).
// The consumer executes it in its own migration path; the conformance
// tests execute it per test database. statepg never runs it at runtime.
//
//go:embed schema.sql
var Schema string

var errNotConnected = errors.New("statepg: not connected. Call Connect first")

// State implements chat.StateAdapter over an accepted *pgxpool.Pool.
type State struct {
	pool      *pgxpool.Pool
	connected atomic.Bool
}

// New returns an unconnected adapter over pool. Call Connect before use.
func New(pool *pgxpool.Pool) *State { return &State{pool: pool} }

// Connect pings the database and opens the adapter's gate. Idempotent.
func (s *State) Connect(ctx context.Context) error {
	if s.connected.Load() {
		return nil
	}
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("statepg: connect: %w", err)
	}
	s.connected.Store(true)
	return nil
}

// Disconnect closes the gate. Data persists; the pool stays open (the
// caller owns it). Idempotent.
func (s *State) Disconnect(context.Context) error {
	s.connected.Store(false)
	return nil
}

func (s *State) check() error {
	if !s.connected.Load() {
		return errNotConnected
	}
	return nil
}

// expiresAt maps ttl to a nullable timestamptz (0 = never expires).
func expiresAt(ttl time.Duration) *time.Time {
	if ttl <= 0 {
		return nil
	}
	t := time.Now().Add(ttl)
	return &t
}

// --- StateKV ---

func (s *State) Get(ctx context.Context, key string) (json.RawMessage, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	var value string
	err := s.pool.QueryRow(ctx,
		`SELECT value FROM chat_state_kv WHERE key = $1 AND (expires_at IS NULL OR expires_at > now())`,
		key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(value), nil
}

func (s *State) Set(ctx context.Context, key string, value json.RawMessage, ttl time.Duration) error {
	if err := s.check(); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO chat_state_kv (key, value, expires_at) VALUES ($1, $2, $3)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value,
		   expires_at = EXCLUDED.expires_at`,
		key, string(value), expiresAt(ttl))
	return err
}

// SetIfNotExists atomically claims key: insert wins, and an expired row may
// be stolen (upstream lock-steal pattern; no read-then-write).
func (s *State) SetIfNotExists(ctx context.Context, key string, value json.RawMessage, ttl time.Duration) (bool, error) {
	if err := s.check(); err != nil {
		return false, err
	}
	tag, err := s.pool.Exec(ctx,
		`INSERT INTO chat_state_kv (key, value, expires_at) VALUES ($1, $2, $3)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value,
		   expires_at = EXCLUDED.expires_at
		 WHERE chat_state_kv.expires_at IS NOT NULL AND chat_state_kv.expires_at <= now()`,
		key, string(value), expiresAt(ttl))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s *State) Delete(ctx context.Context, key string) error {
	if err := s.check(); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM chat_state_kv WHERE key = $1`, key)
	return err
}

// --- StateLists ---

func (s *State) GetList(ctx context.Context, key string) ([]json.RawMessage, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT value FROM chat_state_lists
		 WHERE list_key = $1 AND (expires_at IS NULL OR expires_at > now())
		 ORDER BY seq`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []json.RawMessage
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(v))
	}
	return out, rows.Err()
}

// AppendToList appends in one transaction: expired rows die first (an
// expired list starts fresh, never resurrects), the whole list's TTL is
// refreshed, and the list is trimmed to maxLength keeping the newest.
func (s *State) AppendToList(ctx context.Context, key string, value json.RawMessage, maxLength int, ttl time.Duration) error {
	if err := s.check(); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx,
		`DELETE FROM chat_state_lists WHERE list_key = $1 AND expires_at IS NOT NULL AND expires_at <= now()`,
		key); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO chat_state_lists (list_key, value, expires_at) VALUES ($1, $2, $3)`,
		key, string(value), expiresAt(ttl)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE chat_state_lists SET expires_at = $2 WHERE list_key = $1`,
		key, expiresAt(ttl)); err != nil {
		return err
	}
	if maxLength > 0 {
		if _, err := tx.Exec(ctx,
			`DELETE FROM chat_state_lists WHERE list_key = $1 AND seq NOT IN (
			   SELECT seq FROM chat_state_lists WHERE list_key = $1 ORDER BY seq DESC LIMIT $2)`,
			key, maxLength); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// --- StateLocks ---

func newToken() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "pg_" + hex.EncodeToString(b[:])
}

// AcquireLock atomically inserts or steals an expired lock (upstream
// state-pg's exact ON CONFLICT ... WHERE expired pattern). (nil, nil) when
// the lock is live.
func (s *State) AcquireLock(ctx context.Context, threadID string, ttl time.Duration) (*chat.Lock, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	token := newToken()
	exp := time.Now().Add(ttl)
	var gotToken string
	var gotExp time.Time
	err := s.pool.QueryRow(ctx,
		`INSERT INTO chat_state_locks (thread_id, token, expires_at) VALUES ($1, $2, $3)
		 ON CONFLICT (thread_id) DO UPDATE
		   SET token = EXCLUDED.token, expires_at = EXCLUDED.expires_at
		   WHERE chat_state_locks.expires_at <= now()
		 RETURNING token, expires_at`,
		threadID, token, exp).Scan(&gotToken, &gotExp)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil // held by someone else
	}
	if err != nil {
		return nil, err
	}
	return &chat.Lock{ThreadID: threadID, Token: gotToken, ExpiresAt: gotExp}, nil
}

// ExtendLock refreshes a LIVE lock's TTL; an expired lock never
// resurrects (WHERE expires_at > now()).
func (s *State) ExtendLock(ctx context.Context, lock *chat.Lock, ttl time.Duration) (bool, error) {
	if err := s.check(); err != nil {
		return false, err
	}
	if lock == nil {
		return false, nil
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE chat_state_locks SET expires_at = $3
		 WHERE thread_id = $1 AND token = $2 AND expires_at > now()`,
		lock.ThreadID, lock.Token, time.Now().Add(ttl))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ReleaseLock deletes a token-matched lock; a wrong token is a no-op.
func (s *State) ReleaseLock(ctx context.Context, lock *chat.Lock) error {
	if err := s.check(); err != nil {
		return err
	}
	if lock == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`DELETE FROM chat_state_locks WHERE thread_id = $1 AND token = $2`,
		lock.ThreadID, lock.Token)
	return err
}

func (s *State) ForceReleaseLock(ctx context.Context, threadID string) error {
	if err := s.check(); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM chat_state_locks WHERE thread_id = $1`, threadID)
	return err
}

// --- StateQueue ---

// Enqueue inserts, trims to maxSize keeping the newest, and returns the
// live depth — one transaction.
func (s *State) Enqueue(ctx context.Context, threadID string, entry chat.QueueEntry, maxSize int) (int, error) {
	if err := s.check(); err != nil {
		return 0, err
	}
	// Message serialization rides chat-go's own wire shape: ToJSON returns
	// *SerializedMessage (no error, no bytes — message.go:160); the byte
	// encoding is plain encoding/json over it.
	var msg []byte
	if entry.Message != nil {
		var err error
		msg, err = json.Marshal(entry.Message.ToJSON())
		if err != nil {
			return 0, fmt.Errorf("statepg: encode queued message: %w", err)
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx,
		`DELETE FROM chat_state_queues WHERE thread_id = $1 AND expires_at <= now()`, threadID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO chat_state_queues (thread_id, enqueued_at, expires_at, message) VALUES ($1, $2, $3, $4)`,
		threadID, entry.EnqueuedAt, entry.ExpiresAt, string(msg)); err != nil {
		return 0, err
	}
	if maxSize > 0 {
		if _, err := tx.Exec(ctx,
			`DELETE FROM chat_state_queues WHERE thread_id = $1 AND seq NOT IN (
			   SELECT seq FROM chat_state_queues WHERE thread_id = $1 ORDER BY seq DESC LIMIT $2)`,
			threadID, maxSize); err != nil {
			return 0, err
		}
	}
	var depth int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM chat_state_queues WHERE thread_id = $1 AND expires_at > now()`,
		threadID).Scan(&depth); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return depth, nil
}

// Dequeue atomically takes the oldest live entry (purge-before-dequeue,
// then DELETE ... RETURNING on the head — no read-then-write window).
func (s *State) Dequeue(ctx context.Context, threadID string) (*chat.QueueEntry, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM chat_state_queues WHERE thread_id = $1 AND expires_at <= now()`, threadID); err != nil {
		return nil, err
	}
	var enq, exp time.Time
	var msg string
	err := s.pool.QueryRow(ctx,
		`DELETE FROM chat_state_queues WHERE thread_id = $1 AND seq = (
		   SELECT seq FROM chat_state_queues WHERE thread_id = $1 AND expires_at > now()
		   ORDER BY seq LIMIT 1 FOR UPDATE SKIP LOCKED)
		 RETURNING enqueued_at, expires_at, message`,
		threadID).Scan(&enq, &exp, &msg)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	entry := &chat.QueueEntry{EnqueuedAt: enq, ExpiresAt: exp}
	if msg != "" {
		// MessageFromJSON takes *SerializedMessage (message.go:229); a
		// corrupt row is a real error, never a silent nil message.
		var sm chat.SerializedMessage
		if err := json.Unmarshal([]byte(msg), &sm); err != nil {
			return nil, fmt.Errorf("statepg: decode queued message: %w", err)
		}
		entry.Message = chat.MessageFromJSON(&sm)
	}
	return entry, nil
}

func (s *State) QueueDepth(ctx context.Context, threadID string) (int, error) {
	if err := s.check(); err != nil {
		return 0, err
	}
	var depth int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM chat_state_queues WHERE thread_id = $1 AND expires_at > now()`,
		threadID).Scan(&depth)
	return depth, err
}

// --- StateSubscriptions ---

func (s *State) Subscribe(ctx context.Context, threadID string) error {
	if err := s.check(); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO chat_state_subscriptions (thread_id) VALUES ($1) ON CONFLICT (thread_id) DO NOTHING`,
		threadID)
	return err
}

func (s *State) Unsubscribe(ctx context.Context, threadID string) error {
	if err := s.check(); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM chat_state_subscriptions WHERE thread_id = $1`, threadID)
	return err
}

func (s *State) IsSubscribed(ctx context.Context, threadID string) (bool, error) {
	if err := s.check(); err != nil {
		return false, err
	}
	var sub bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM chat_state_subscriptions WHERE thread_id = $1)`, threadID).Scan(&sub)
	return sub, err
}

var _ chat.StateAdapter = (*State)(nil)
