package statepg_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shoenig/test/must"

	"github.com/northpolesec/chat-go/chat"
	"github.com/northpolesec/chat-go/chattest"
	"github.com/northpolesec/chat-go/statepg"
)

// freshDB creates a uniquely named database off the TEST_DATABASE_URL
// maintenance connection, executes statepg.Schema, and returns a pool
// (dropped in t.Cleanup) — each conformance subtest is fully isolated.
func freshDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL not set; statepg conformance needs Postgres " +
			"(run: TEST_DATABASE_URL=postgres://chatgo:chatgo@127.0.0.1:5432/postgres?sslmode=disable make test)")
	}
	var b [6]byte
	_, _ = rand.Read(b[:])
	name := "chatgo_statepg_" + hex.EncodeToString(b[:])
	ctx := context.Background() //nolint:usetesting // Cleanup outlives t.Context
	admin, err := pgx.Connect(ctx, base)
	must.NoError(t, err)
	_, err = admin.Exec(ctx, "CREATE DATABASE "+name)
	must.NoError(t, err)
	t.Cleanup(func() {
		_, err := admin.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)")
		must.NoError(t, err)
		must.NoError(t, admin.Close(ctx))
	})
	u, err := url.Parse(base)
	must.NoError(t, err)
	u.Path = "/" + name
	pool, err := pgxpool.New(t.Context(), u.String())
	must.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = pool.Exec(t.Context(), statepg.Schema)
	must.NoError(t, err)
	return pool
}

func TestStateAdapterConformance(t *testing.T) {
	t.Parallel()
	chattest.RunStateAdapterTests(t, func(t *testing.T) chat.StateAdapter {
		return statepg.New(freshDB(t))
	})
}

// Disconnect persists data and leaves the pool usable (pg divergence from
// statememory, PORTING.md Phase D): reconnecting sees the same state.
func TestDisconnectPersistsData(t *testing.T) {
	t.Parallel()
	pool := freshDB(t)
	s := statepg.New(pool)
	must.NoError(t, s.Connect(t.Context()))
	must.NoError(t, s.Subscribe(t.Context(), "slack:C1:1.1"))
	must.NoError(t, s.Disconnect(t.Context()))

	s2 := statepg.New(pool)
	must.NoError(t, s2.Connect(t.Context()))
	sub, err := s2.IsSubscribed(t.Context(), "slack:C1:1.1")
	must.NoError(t, err)
	must.True(t, sub)
}

// Schema is idempotent: executing it twice is safe (the consumer runs it on
// every startup).
func TestSchemaIdempotent(t *testing.T) {
	t.Parallel()
	pool := freshDB(t)
	_, err := pool.Exec(t.Context(), statepg.Schema)
	must.NoError(t, err)
}
