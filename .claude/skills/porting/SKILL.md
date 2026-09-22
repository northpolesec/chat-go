---
name: porting
description: Port Vercel Chat SDK and remend changes into chat-go, or add one named adapter. Use when bumping vercel/chat or streamdown remend, porting a new @chat-adapter package, or updating PORTING.md.
---

# Porting

`PORTING.md` is the ledger. This file is how you update it. Do not copy these rules into the ledger. Do not name private consumers, hosts, or people.

## Pins

Move the two pins on their own.

A chat bump does not move remend. If the user includes remend, move it. If the chat diff needs the new remend, move it.

A remend bump does not change adapters.

The ledger names the chat packages in scope. If upstream adds another adapter, list it under "Not ported". Until the user names that package, do not start it.

## Bump

1. Read the pin, the dropped list, and the rows you will change.
2. Fetch upstream at the old commit and at the new tag. Use a scratch directory. Do not commit that checkout.
3. Diff only the paths in scope.
4. Port each changed file onto the Go file in its row. Keep the differences already on that row.
5. Do not port dropped scope. Do not edit a file the diff did not change. Do not add task numbers or `it()` tables.
6. Port the upstream tests for that change. Keep their wording.
7. If you cannot write a test in Go, mark that row permanently deferred.
8. Update those rows. Add one bump entry. Do not add a second row for the same file.
9. Run `make fmt`. Then run `go test -shuffle=on -race ./...` on the packages you changed.
10. If the tests pass, stop. Leave the live checks unchecked.

## New adapter

If the user names the package, start. Pin it at the current `vercel/chat` commit.

1. Add a section. Use the same row shape: upstream file, Go file, tests, differences.
2. Apply the global differences and the dropped list. That list has socket mode, the multi-tenant install map, the WebClient and Octokit escape hatches, and subclass APIs.
3. Add the `chattest` contracts this adapter can implement. Add `TestAdapterSmoke`.
4. Use the same test commands. Leave the live checks unchecked.

## Gotchas

On every bump, and for every new adapter, check these three again. The ledger records the check. It does not repeat the rules.

1. **`AbortSignal` becomes `ctx`.** Upstream `fetch` ignores `options.signal`, so code after an abort still runs. In Go the first call fails. The platform stays in the middle of the turn. After an abort, finish that work with `context.WithoutCancel` and a timeout. Slack has no cancelled task status. Close open cards as `error`.
2. **Keep the helper's wire shape.** If you inline a helper, send the same payloads it sends. Slack locks a stream to the mode from `startStream`. A bare `markdown_text` after a card is `streaming_mode_mismatch`. The helper always sends `chunks`.
3. **Live checks are not unit tests.** The two bugs above passed every ported test. On the bump entry, leave three checks open for a person. Those checks are a stop mid-turn, a tool card with streamed text, and the error path. Do not mark them done. Do not invent credentials.

## Bump entry

```
### YYYY-MM-DD <version> (<old>..<new>)
- Pin: chat or remend
- Files:
- Not ported:
- Gotchas re-checked: 1, 2, 3
- Live checks (unchecked): stop mid-turn; tool card + streamed text; error path
```

A row is `Upstream file | Go file | Tests | Divergences`. Update that row.
