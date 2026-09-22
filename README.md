# chat-go

chat-go is a Go port of [Vercel's Chat SDK](https://github.com/vercel/chat) v4.40.0. It holds the shared message types, three platform adapters, and two state stores. Your program owns the turn.

The module path is `github.com/northpolesec/chat-go`.

## How a message moves

A platform sends a webhook. The adapter checks the signature. Then it parses the event. Then it calls your `chat.ChatInstance`.

Your code reads and writes state. Then it posts or streams a reply through the same adapter.

```
platform webhook
       |
       v
    adapter -------- state (memory or Postgres)
       |
       v
 your ChatInstance
       |
       v
 adapter post or stream
       |
       v
  platform API
```

The adapter stores each platform thread as one string. `EncodeThreadID` and `DecodeThreadID` convert that string.

`chat.ChatInstance` is the surface your program implements. On Slack, `ProcessMessage` and the other `Process*` methods must return before the ack. The adapter waits for them, then acks the webhook. Slack sends the event again after about 3 seconds.

## Packages

```
chat/            messages, cards, markdown, adapter and state interfaces
slack/           Slack adapter (slack/api, slack/blocks, slack/webhook)
github/          GitHub adapter
linear/          Linear adapter
statememory/     in-memory state
statepg/         Postgres state
chattest/        contract tests for adapters and state
internal/shared/ helpers the adapters share
internal/remend/ repair for markdown that is still streaming
```

`internal/` is for this module only.

## State

`chat.StateAdapter` stores keys, lists, locks, a per-thread queue, and subscriptions.

`statememory` keeps that data in memory. `statepg` keeps it in Postgres. You supply a `*pgxpool.Pool`. You run `statepg.Schema` yourself. This package does not create tables. Disconnect keeps the rows. It does not close your pool.

## Platforms

Slack, GitHub, and Linear each implement `chat.Adapter`. Extra behavior is its own interface, such as `chat.Streamer`.

`HandleWebhook` checks the platform signature before it calls you. The Slack client refuses a `response_url` or a file URL on any other host.

GitHub uses one installation. Linear uses agent sessions. [PORTING.md](PORTING.md) lists what this port left out.

## Tests

```
go test ./...
```

The Postgres conformance test runs when `TEST_DATABASE_URL` is set. Otherwise that test skips.

## Porting

[PORTING.md](PORTING.md) lists the upstream pins and how this port differs.

If you bump `vercel/chat` or remend, or you add one named adapter, follow `.claude/skills/porting/SKILL.md`.

## License

Apache License 2.0. See [LICENSE](LICENSE).

The Chat SDK port is derived from MIT-licensed code. `internal/remend` is derived from Apache-2.0 code. Both notices are in [NOTICE](NOTICE).
