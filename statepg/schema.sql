-- statepg schema. Executed by the consumer (accept-a-pool, never
-- self-create at runtime) and by the conformance tests. Idempotent.
-- Only columns a query reads or writes exist; every expiry predicate is
-- secondary to a PK-leading lookup, so no expires_at indexes either
-- (a future global sweeper adds its index WITH its consumer).
CREATE TABLE IF NOT EXISTS chat_state_kv (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  expires_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS chat_state_lists (
  list_key TEXT NOT NULL,
  seq BIGINT GENERATED ALWAYS AS IDENTITY,
  value TEXT NOT NULL,
  expires_at TIMESTAMPTZ,
  PRIMARY KEY (list_key, seq)
);

CREATE TABLE IF NOT EXISTS chat_state_locks (
  thread_id TEXT PRIMARY KEY,
  token TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS chat_state_queues (
  thread_id TEXT NOT NULL,
  seq BIGINT GENERATED ALWAYS AS IDENTITY,
  enqueued_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  message TEXT NOT NULL,
  PRIMARY KEY (thread_id, seq)
);

CREATE TABLE IF NOT EXISTS chat_state_subscriptions (
  thread_id TEXT PRIMARY KEY
);
