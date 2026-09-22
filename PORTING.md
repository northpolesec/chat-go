# PORTING.md

This file is the ledger. The steps are in `.claude/skills/porting/SKILL.md`.

An old row can name a task number. That number is history. New work updates the row and adds a bump entry. Do not add a task table.

## Pins

| Pin | Upstream | Version | Go |
| --- | --- | --- | --- |
| chat | [vercel/chat](https://github.com/vercel/chat) `6adca3617e3b4f4bdda553c7afba0529ab1fec4c` | v4.40.0 | `chat`, `internal/shared`, `slack`, `github`, `linear`, `statememory`, `chattest` |
| remend | [vercel/streamdown](https://github.com/vercel/streamdown) `packages/remend` | v1.2.1 | `internal/remend` |

These chat paths are in scope: `packages/chat`, `packages/adapter-shared`, `packages/adapter-slack`, `packages/adapter-github`, and `packages/adapter-linear`.

Also in scope: `packages/state-memory`, `packages/state-pg` (behavior only; the Go adapter is `statepg`), and `packages/tests`. Any other `@chat-adapter/*` package is not ported.

## Dropped

- Slack socket mode and socket forwarding
- Slack `WebClient` and `webClientOptions` (HTTP uses `slack/api.Client`)
- GitHub multi-tenant App mode (one `InstallationID`)
- GitHub Octokit escape hatch
- Linear comments mode
- Linear multi-tenant installs, the OAuth callback, and AES token encryption
- Vercel Connect as its own auth mode (`TokenSource` holds one token)
- Subclass APIs

## Bump log

### 2026-09-12 v4.40.0 (initial, `6adca36`)

- Pin: chat and remend
- Packages: `chat`, `adapter-shared`, `adapter-slack`, `adapter-github`, `adapter-linear`, `state-memory`, remend v1.2.1
- Not ported: every other upstream adapter
- Gotchas re-checked: 1, 2, 3
- Live checks:
  - [x] Slack: stop mid-turn (gotcha 1), and a tool card with streamed text (gotcha 2). Fixed on a real turn
  - [ ] Slack error path
  - [ ] GitHub: stop mid-turn, tool card with streamed text, error path
  - [ ] Linear: stop mid-turn, tool card with streamed text, error path
  - [ ] Linear: does a retry send a fresh `Linear-Timestamp`? Does `agentSession.activities` include the trigger comment? Does a prompted event include `user.email`?

`statepg` came later. It is `chat.StateAdapter` on a `*pgxpool.Pool` that you supply. The DDL is `statepg.Schema`. This package does not create tables. There is no `key_prefix`. The store serves one bot.

Queue depth counts rows with `expires_at` after now. Dequeue deletes stale rows first. Disconnect keeps the rows. Disconnect does not close the pool. The conformance test skips when `TEST_DATABASE_URL` is unset. `chattest` uses `shortTTL` 100ms, `pollLimit` 2s, and `pollEvery` 5s.

One Postgres round-trip does not fail the test. `statememory` did not change.

## Global divergences

- JSX→structs
- Logger→slog
- optional methods→capability interfaces
- default-true booleans→DisableX
- sync token resolver→TokenSource
- socket mode dropped
- WebClient dropped
- remark→goldmark
- remend vendored

## Gotchas

The three gotchas are in `.claude/skills/porting/SKILL.md`. On every bump, check them again. Record that check in the bump log. Do not copy the rules here.

## Phase A — Foundation

| Upstream file | Go file | Tests | Divergences |
| --- | --- | --- | --- |
| `packages/chat/src/types.ts` (listed subset + referenced plain data) | `chat/types.go` | `chat/types_test.go` | `Message` completed in `message.go` (Task 5); `EmojiValue` completed in `emoji.go` (Task 3); `SuggestedPrompts` from `adapter-slack` `SlackSuggestedPromptsOptions` (not in `types.ts`); process* flattened to `*Input` structs; `Message \| (() => Promise<Message>)` → `*Message`; `Author.isBot` (`boolean \| "unknown"`) → `*bool` (nil = unknown); `StreamOptions.signal` omitted (use `ctx`); `PostableAst.AST` is `any` (mdast `Root`); `Card` moved to `cards.go` (Task 10); `Modal` moved to `modals.go` (Task 11); Modal `children` are `[]any`; TS string postable variant is `PostableText`; `fetchSubject?` → `SubjectFetcher`; `FormattedContent` is `any`; `Attachment.FetchData` is `func() ([]byte, error)`; `ThreadInfo` and `UserInfo` promoted to `chat` (GitHub adapter returns them too); `slack` keeps aliases |

### Capability-interface mapping

TS optional method → Go interface, with one-line `packages/chat/src` call-site evidence. Slack-only methods have no core `adapter.` check.

| TS method | Go interface | Call-site evidence |
| --- | --- | --- |
| `postEphemeral?` | `EphemeralPoster` | `thread.ts:568` `if (this.adapter.postEphemeral)` |
| `startTyping` (required) | `Adapter` | `types.ts:577` required (no `?`); `thread.ts:885` unconditional `this.adapter.startTyping(...)` |
| `endTyping?` | `TypingNotifier` | `types.ts:297` optional; `thread.ts:900` `this.adapter.endTyping?.(...)` |
| `postObject?` | `ObjectPoster` | `postable-object.ts:80` `adapter.postObject` |
| `editObject?` | `ObjectPoster` | `plan.ts:140` `!!adapter.postObject && !!adapter.editObject` |
| `stream?` | `Streamer` | `thread.ts:765` `if (this.adapter.stream)` |
| `scheduleMessage?` | `MessageScheduler` | `thread.ts:721` `if (!this.adapter.scheduleMessage)` |
| `openDM?` | `DMOpener` | `thread.ts:579` `if (this.adapter.openDM)` |
| `fetchMessage?` | `MessageFetcher` | `chat.ts:1829` `event.adapter.fetchMessage` |
| `fetchChannelInfo?` | `ChannelReader` | `channel.ts:255` `if (this.adapter.fetchChannelInfo)` |
| `fetchChannelMessages?` | `ChannelReader` | `history/channel.ts:74` `if (adapter.fetchChannelMessages)` |
| `listThreads?` | `ChannelReader` | `history/channel.ts:113` `if (!adapter.listThreads)` |
| `postChannelMessage?` | `ChannelReader` | `channel.ts:317` `this.adapter.postChannelMessage` |
| `openModal?` | `ModalOpener` | `chat.ts:1579` `event.adapter.openModal` |
| `updateModal` (Slack adapter; not on core `Adapter`) | `ModalOpener` | no `packages/chat/src` `adapter.updateModal` check; `adapter-slack/src/index.ts:5135` |
| `publishHomeView` (Slack adapter) | `AgentViewPublisher` | no `packages/chat/src` `adapter.` check; `adapter-slack/src/index.ts:3969` |
| `setSuggestedPrompts` (Slack adapter) | `AgentViewPublisher` | no `packages/chat/src` `adapter.` check; `adapter-slack/src/index.ts:3985` |
| `setAssistantStatus` (Slack adapter) | `AgentViewPublisher` | no `packages/chat/src` `adapter.` check; `adapter-slack/src/index.ts:4099` |
| `setSessionStatus` (Slack adapter) | `AgentViewPublisher` | no `packages/chat/src` `adapter.` check; `adapter-slack/src/index.ts:4131` |
| `setAssistantTitle` (Slack adapter) | `AgentViewPublisher` | no `packages/chat/src` `adapter.` check; `adapter-slack/src/index.ts:4169` |
| `disconnect?` | `Disconnecter` | `chat.ts:529` `if (!adapter.disconnect)` |
| `PostError` (Go addition) | `ErrorPoster` | Go addition for a failed turn |
| `AbortTurn` (Go addition) | `TurnAborter` | slack `handleAgentSessionStopped`, linear `handleStop` |
| `fetchSubject?` | `SubjectFetcher` | `types.ts:363` optional; `message.ts:187` `if (!adapter?.fetchSubject)` |

Grouping adjusted in this task: `startTyping` is required (`types.ts:577`) so it lives on `Adapter` (`status string`, `TypingOptions`; zero = unset). `TypingNotifier` is `EndTyping` only (`types.ts:297` optional, `AgentSessionStatus`; zero = unset). `ChannelReader.ListThreads` returns `ListThreadsResult` (`Threads []ThreadSummary`), not `FetchResult`.

## Phase B — Core leaves

| Upstream file | Go file | Tests | Divergences |
| --- | --- | --- | --- |
| `packages/chat/src/emoji.ts` | `chat/emoji.go` | `chat/emoji_test.go` (43/43 `it` cases) | `EmojiValue` comparable values not `===` identity; no singleton registry; `EmojiFormats` slices not `string \| string[]`; `CustomEmojiMap` is `map[string]EmojiFormats`; helper is `Emoji.Get`/`Custom` not dynamic fields; `DefaultEmojiResolver` mutex-guarded; `ConvertEmojiPlaceholders` 2-arg (always default resolver); `Extend` rebuilds reverse maps (TS can leave stale keys) |
| `packages/chat/src/markdown.ts` | `chat/markdown.go`, `chat/format.go` | `chat/markdown_test.go` (130/130 `it` cases) | remark/mdast → goldmark (`extension.GFM`+`Strikethrough`); helpers that read text take `src []byte`; `Emphasis.Level>=2` is strong; constructed text is `*ast.String`; constructed fence lang is a node attribute; `GetNodeChildren` hides code-span/fence children (mdast value-nodes); `IsTableRowNode` accepts `TableHeader`; `ToPlainText` trims goldmark's trailing fence newline; `GetNodeValue`/`ToPlainText` unescape CommonMark backslash-punctuations on non-raw `Text` (mdast stores unescaped values; Task 16); `StringifyMarkdown` emits strikethrough, GFM tables, and autolinks (Task 16; 52→77 lines); `StringifyOptions.Bullet` dropped as dead flexibility (list emission never landed upstream-test-driven); `BaseFormatConverter` is a struct + `FormatHooks` (no inheritance); `FromAst` takes `src []byte`; `RenderPostable` takes `any`; card element types moved to `cards.go` (Task 10); `TableElementToASCII` stays here |
| `packages/chat/src/message.ts` | `chat/message.go` | `chat/message_test.go` (22/22 `it` cases) | dates → `time.Time`, JSON ISO-8601 ms strings; `raw`/`Formatted`/`MessageSubject.Raw` are `any`; WeakMap adapter → unexported field; `subject` Promise → `Subject(ctx)` (errors → nil); `fetchSubject?` → `SubjectFetcher`; no `@workflow/serde` (`WorkflowSerialize`/`WorkflowDeserialize`); goldmark `Formatted` is not mdast JSON (`encoding/json` emits `{}`); `IsMention` is `*bool`; `Author.IsSystem` omitted from JSON when false |

### Task 4 adjusted tests (remark vs goldmark)

No ported case changed its expected plain-text / ASCII / stringify value. Fenced-code trailing newline (`"const x = 1;\n"` vs remark `"const x = 1;"`) is absorbed in `GetNodeValue` / `ToPlainText`, not in the test.

Task 16: goldmark `Text.Value` keeps CommonMark escape backslashes (`\>` stays `\>`). remark's `text.value` is unescaped (`>`). Absorbed in `GetNodeValue` / `ToPlainText` via `util.UnescapePunctuations` on non-raw `Text`. `StringifyMarkdown` still emits the raw segment. No Task 16 expected string was changed.

`BaseFormatConverter` (23 `it` cases) ported in `chat/format.go` (split so `markdown.go` stays under ~700 lines).

## Phase C — remend and streaming markdown

| Upstream file | Go file | Tests | Divergences |
| --- | --- | --- | --- |
| `packages/remend/src/index.ts` | `internal/remend/remend.go` | `internal/remend/remend_test.go` (21/22; 1 permanently deferred) | `Mend` / `MendWithOptions`; `Options` is only `InlineKatex` (opt-in). Default-true flags + custom handlers live on unexported `remendOptions` / `customHandler`. Pipeline is `builtInHandlers()` with setext (15), links+`withLinkMode` (20), katex (70), inlineKatex (75). Non-string TS inputs cannot be passed. |
| `packages/remend/src/utils.ts` + `patterns.ts` | `internal/remend/utils.go` | `internal/remend/utils_test.go` (10/10 + coverage-gaps utils) | Positions are byte indices. `isWordChar` uses first rune + `unicode.IsLetter`/`IsNumber`. `linkImagePattern`, `incompleteLinkUrlPattern`, `doubleUnderscoreGlobalPattern` unused by any remend handler upstream; omitted. |
| `packages/remend/src/code-block-utils.ts` | `internal/remend/codeblock.go` | `internal/remend/codeblock_test.go` (6/6) | Byte-index lookup. Single-entry cache is mutex-guarded. |
| `packages/remend/src/inline-code-handler.ts` | `internal/remend/inlinecode.go` | `internal/remend/inlinecode_test.go` (14/14 + `code-blocks.test.ts` 25/25) | None beyond byte-index ops (ASCII fixtures). |
| `packages/remend/src/strikethrough-handler.ts` | `internal/remend/strikethrough.go` | `internal/remend/strikethrough_test.go` (5/5) | None beyond byte-index `LastIndex` (ASCII fixtures). |
| `packages/remend/src/single-tilde-handler.ts` | `internal/remend/tilde.go` | `internal/remend/tilde_test.go` (13/13) | No JS lookahead; rune walk. `isInsideCodeBlock` gets byte offset of `~`. |
| `packages/remend/src/comparison-operator-handler.ts` | `internal/remend/comparison.go` | `internal/remend/comparison_test.go` (17/17) | `FindAllStringSubmatchIndex` instead of replace callback offset. `(?m)` for JS `m`. Upstream file has 17 `it` cases (not 18). |
| `packages/remend/src/html-tag-handler.ts` | `internal/remend/htmltag.go` | `internal/remend/htmltag_test.go` (12/12) | `trimEnd` → `TrimRightFunc(..., unicode.IsSpace)`. Byte match index. |
| `packages/remend/src/emphasis-handlers.ts` | `internal/remend/emphasis.go` | `internal/remend/emphasis_test.go` | Scanner positions are bytes. prev/next + `isWordChar` use runes (café, 기울임, 中文). QF1001 De Morgan on one guard. |
| `packages/remend/src/link-image-handler.ts` | `internal/remend/link.go` | `internal/remend/link_test.go` (links 14/14, images 5/5) | Byte indices (ASCII fixtures). Incomplete images keep `streamdown:incomplete-image` (same as upstream remend; not stripped). |
| `packages/remend/src/katex-handler.ts` | `internal/remend/katex.go` | `internal/remend/katex_test.go` (50/50) | Byte indices (ASCII fixtures). Inline `$...$` is opt-in via `Options.InlineKatex`. |
| `packages/remend/src/setext-heading-handler.ts` | `internal/remend/setext.go` | `internal/remend/setext_test.go` (19/19) | Empty string is the Go stand-in for TS `!text` / non-string. `\s` is Go ASCII whitespace. |

The remend suite ports 488 of 489 `it` cases. One case stays deferred: input that is not a string. `Mend` takes a string. The comparison file has 17 `it` cases, not 18.

## Phase D — Core composites and state

| Upstream file | Go file | Tests | Divergences |
| --- | --- | --- | --- |
| `packages/chat/src/streaming-markdown.ts` | `chat/streamingmd.go` | `chat/streamingmd_test.go` (64/64 `it` cases) | `wrapTablesForAppend` default-true → `DisableTableWrapForAppend`; `getCommittableText`/`getText` → `CommittableText`/`Text`; not goroutine-safe (single consumer, matching upstream); JS string indices / `.length` → Go bytes / `len` (ASCII fixtures); `trimStart`/`trim` → `TrimLeftFunc(unicode.IsSpace)`/`TrimSpace`; `[\s:]` is Go-regexp ASCII `\s` vs JS Unicode `\s` (ASCII fixtures); modernize `SplitSeq` / `slices.Backward` |
| `packages/chat/src/cards.ts` | `chat/cards.go` | `chat/cards_test.go` (37/37 `it` cases) | JSX/`fromReactElement` not ported (struct literals); `Text` builder is `CardText` (`markdown.go` owns `Text`); `Card` moved here from `types.go`; Task 4 subset types moved here from `format.go` and expanded (one definition each); `TableElementToASCII` stays in `markdown.go`; `ChartElementToFallbackText` lives here; standalone `CardChildToFallbackText` is the cards.ts fields/link version (`BaseFormatConverter.CardChildToFallbackText` keeps the markdown.ts `**label**: value` form); no `Header` element upstream; Select/RadioSelect builders live in `modals.go` (4 validation cases un-deferred in Task 11) |
| `packages/chat/src/modals.ts` | `chat/modals.go` | `chat/modals_test.go` (24/36 `it` cases) + 4 cards Select/RadioSelect | JSX/`fromReactModalElement` not ported (struct literals); `Modal` moved from `types.go`; children stay `[]any`; Element-suffix types + ctor names (`Select` → `SelectElement` / `Select()`); empty Select/RadioSelect options panic with the upstream `Error` message; `console.warn` → `slog.Warn`; `ToModalElement` is jsx-runtime `toModalElement` without JSX resolution (`*Modal` or nil); `NumberInput` min/max/initialValue are `*float64` (nil = unset); `Float` is a Go-divergence helper for literals |
| `packages/adapter-shared/src/code-fences.ts` | `internal/shared/codefences.go` | `internal/shared/codefences_test.go` (11/11 `it` cases) | byte indices (ASCII fixtures); RE2 has no lookahead so `BLOCK_MARKER` / ordered-list are hand-checked; `trimStart` → `unicode.IsSpace` |
| `packages/adapter-shared/src/card-utils.ts` | `internal/shared/cardutils.go` | `internal/shared/cardutils_test.go` (39/39 `it` cases) | undefined style/platform → `""`; `Card` is `chat.Card` (struct literal); default child fallback is `chat.CardChildToFallbackText` |
| `packages/adapter-shared/src/download.ts` | `internal/shared/download.go` | `internal/shared/download_test.go` (26/26 `it`, 39/39 incl. `it.each` rows) + 1 Go-divergence (`TestDownloadAdvertisesOnlyDecodableEncodings`) | `DownloadAttachment(ctx, client HTTPDoer, url, opts)`; hops issued manually (`CheckRedirect=ErrUseLastResponse` on a package-owned client, never `http.DefaultClient`); `AttachmentTransport` still replaces a hop; `net/url` is WHATWG-IPv4-canonicalized + hostname-lowercased (Node URL does both; Go does neither); timeout is `ctx` + `opts.Timeout` (default 30s); upstream advertises+decodes br, this port advertises only `"gzip, deflate"` (what it decodes; omitting Accept-Encoding would enable Transport auto-gzip); `NetworkError` lives in `errors.go` (Task 12b); messages keep upstream wording; `CreateResolver` is ctx-aware. Auth headers are not auto-stripped (function form decides per hop, same as upstream). Redirect tests use `httptest.Server` behind a rewrite transport so the hop loop still validates public HTTPS URLs (localhost IPs are blocked). |
| `packages/adapter-shared/src/errors.ts` | `internal/shared/errors.go` | `internal/shared/errors_test.go` (24/24 `it` cases) | TS `Error` subclasses → pointer structs with `Error()`; `errors.As` matches the hierarchy (specialized types `Unwrap` to `*AdapterError`; `NetworkError.Unwrap` returns `[]error` so the cause is still matchable). Optional fields are zero values (`""` / `0` / `nil`). Messages keep the upstream wording the tests pin. `NetworkError` moved here from `download.go`. |
| `packages/adapter-shared/src/adapter-utils.ts` | `internal/shared/adapterutils.go` | `internal/shared/adapterutils_test.go` (37/37 `it` cases) | Message is `any` (TS `AdapterPostableMessage` plus the null/string cases the tests pass). Card / PostableCard are `chat` types. Missing files/attachments return empty slices, not nil. `FileUpload.Data` is `[]byte` (Blob / ArrayBuffer identity cases use `[]byte`). |
| `packages/adapter-shared/src/buffer-utils.ts` | `internal/shared/bufferutils.go` | `internal/shared/bufferutils_test.go` (11/16 `it` cases; 5 permanently deferred) | Node `Buffer` → `[]byte` (returned as-is). ArrayBuffer and Blob have no Go analog (5 conversion tests deferred). `ToBuffer` is synchronous for the same reason. `throwOnUnsupported` default-true → `DisableThrowOnUnsupported`. Unsupported types return `(nil, *ValidationError)` or `(nil, nil)`. |
| `packages/adapter-shared/src/mentions.ts` | `internal/shared/mentions.go` | `internal/shared/mentions_test.go` (17/17 `it` cases) | Byte indices (ASCII fixtures); `isBoundary` uses `unicode.IsSpace`; index 0 does not read `text[-1]`. Slack `linkBareMentionNames` calls `shared.ReplaceBareMentions` (Task 16 inlined copy removed). |
| `packages/state-memory/src/index.ts` | `statememory/statememory.go` | `statememory/statememory_test.go` (33/33 `it` cases) | byte-storing (`json.RawMessage` clone on write/read); injectable `now` (lazy TTL, no timers); crypto/rand tokens; stale queue entries discarded on dequeue; `QueueEntry.Message` stored by pointer (`Message` contains `sync.Once`); no production `console.warn`; no `_getSubscriptionCount`/`_getLockCount`; not-connected error drops trailing period (ST1005) |
| `packages/state-memory/src/index.test.ts` + `packages/state-pg/src/index.test.ts` (behavior union; not a port) | `chattest/state.go` | `statememory/conformance_test.go` via `RunStateAdapterTests` | shared `chat.StateAdapter` contract. Memory-wins recorded but **not asserted** where a pg impl cannot follow: disconnect wipe (memory-only); `QueueDepth` counting stale (pg counts live). Go additions: KV no-aliasing, stale-dequeue, Delete. TTL uses a short real wait. There is no clock to inject. |

`RunStateAdapterTests` covers behavior that both memory and Postgres can do. It is not a line-for-line port. Constructor tests, SQL mocks, and `state-pg` integration tests are out of scope.

The test skips memory behavior that a Postgres adapter cannot match. Disconnect does not erase rows. Queue depth counts live rows (`expires_at` after now), not stale rows. The test compares messages by exported fields, not by pointer. TTL cases use a short real TTL and a deadline. There is no clock to inject.

## Phase E — Slack leaves

| Upstream file | Go file | Tests | Divergences |
| --- | --- | --- | --- |
| `packages/adapter-slack/src/crypto.ts` (re-export) + `packages/adapter-shared/src/crypto.ts` | `slack/crypto.go` | `slack/crypto_test.go` (14/14 `it` cases) + 2 Go-added (Node fixed vector, JSON wire layout) | stay unexported (Task 30 store uses them internally); encrypt/decrypt return errors; `isEncryptedTokenData` accepts the struct, `*struct`, or `map[string]any`; Node `Buffer` base64 is more lenient than `encoding/base64` |
| `packages/adapter-slack/src/file.ts` | `slack/file.go` (alias) + `slack/api` `IsSlackAuthURL` | `slack/file_test.go` + `slack/api/file_test.go` (0 upstream `it`; Go-added origin cases) | **Task 21 move:** canonical `isSlackAuthUrl` is `api.IsSlackAuthURL` (exported). `api/` must not import the parent `slack` package (upstream `api/boundary.test.ts`; would be an import cycle). Parent `file.go` keeps an unexported alias. optional `apiUrl` is `""` (unset); origin is WHATWG-style (lowercase scheme/host, omit default ports) because `net/url` does neither |
| `packages/adapter-slack/src/markdown.ts` | `slack/markdown.go` | `slack/markdown_test.go` (46/46 `it` cases) | unexported until Task 30; `replaceBareMentions` is `shared.ReplaceBareMentions` (Task 12b); `slackMrkdwnToMarkdown` extracted to `format.go`; `ToMarkdown`/`ToPlainText` override the base so goldmark reads converted markdown; lookbehind emphasis is a byte walk; mention/fence scanners are byte indices (ASCII fixtures); string postable is `string` or `chat.PostableText`. No expected-value changes. |
| `packages/adapter-slack/src/format/index.ts` | `slack/format.go` | `slack/format_test.go` (29/29 `it` cases) | unexported until Task 30; TypeError → panic; `Date \| number` → `any` (`int`/`int64`/`time.Time`); optional emoji/verbatim are `*bool`; optional link/label are `""`; lookbehind mention/emphasis are a byte walk (RE2); lengths are Go bytes (ASCII fixtures). `format/boundary.test.ts` not ported (compile-time: `format.go` imports only stdlib). `replaceBareMentions` is `internal/shared/mentions.go`; format owns `linkBareSlackMentions`. |
| `packages/adapter-slack/src/blocks/{types,limits,errors,input,index}.ts` | `slack/blocks/{types,limits,errors,input,blocks}.go` | `slack/blocks/blocks_test.go` (28/28 `it` cases) | published `blocks` package; SlackBlock is a marshal-only fat struct (TS index signature); card/actions children are interfaces; chart union is one struct; PageSize is `*int` (0 ≠ unset); chart values are float64; input selected/value are `*string`; `markdownBoldToSlackMrkdwn` copied (no parent import — later cycle); JS `.length` → bytes (ASCII fixtures); fenced fallback byte-truncates so `…` stays within 3000; `blocks/boundary.test.ts` not ported (compile-time: stdlib only) |
| `packages/adapter-slack/src/cards.ts` | `slack/cards.go` | `slack/cards_test.go` (50/50 `it` cases) | **Layering:** independent of `blocks/` — neither wraps the other. Adapter `index.ts` re-exports from `./cards` (`chat.Card`); `blocks/index.ts` aliases `cardToBlockKit = cardToSlackBlocks` (`SlackCardElement`). This file ports `cards.ts` only (does not call Task 18 builders). `SlackBlock` aliases `blocks.SlackBlock`; output elements reuse Task 18 structs; `CardToBlockKit` returns `[]SlackBlock` (no error). `jsonEq`/`jsonVal`/`jsonMap` copied into `cards_test.go` (unexported in `blocks`; not a cycle). chat-side `cardToFallbackText` (cards.ts:921) not added — this file uses `shared.CardToFallbackText`. JS `.length` → bytes (ASCII fixtures); 3,000-char ASCII-fallback pin is rune count (BMP + one `…`). ConvertTextToBlock / ConvertFieldsToBlock exported for Task 20 modals. |
| `packages/adapter-slack/src/modals.ts` | `slack/modals.go` | `slack/modals_test.go` (38/38 `it` cases + 1 Go-divergence-closing) | `console.warn` → `slog.Warn`; JS `undefined` → `""`; title `slice(0, 24)` is Go bytes (ASCII fixtures); `NumberInput` bounds emit only when the `*float64` is non-nil (mirrors `modals.ts:244–252`); `SlackBlock.Optional` is `*bool` (fat-struct field) so `optional:false` marshals; view-submission `payload.view.state.values` walking is `index.ts` `handleViewSubmission` (Phase G), not an export of this file. Reuses `ConvertTextToBlock` / `ConvertFieldsToBlock`. Does not call Task 18 `blocks/input` builders (upstream does not import them). Go-divergence-closing: `omits number_input bounds that were not provided` (upstream has no bare-`NumberInput` pin). |

### adapter-shared package accounting (complete)

Every `packages/adapter-shared/src` file. Task 12b closed the mentions/errors/adapter-utils/buffer-utils gap.

| Upstream file | Go file | Reason if not a 1:1 port |
| --- | --- | --- |
| `adapter-utils.ts` | `internal/shared/adapterutils.go` | |
| `buffer-utils.ts` | `internal/shared/bufferutils.go` | Node Buffer → `[]byte`; 5 ArrayBuffer/Blob tests deferred |
| `card-utils.ts` | `internal/shared/cardutils.go` | Task 12 |
| `code-fences.ts` | `internal/shared/codefences.go` | Task 12 |
| `crypto.ts` | `slack/crypto.go` | Task 15; upstream slack re-exports it. Not duplicated in `internal/shared`. |
| `download.ts` | `internal/shared/download.go` | Task 12; `NetworkError` now in `errors.go` |
| `errors.ts` | `internal/shared/errors.go` | Task 12b |
| `mentions.ts` | `internal/shared/mentions.go` | Task 12b; Slack no longer inlines it |
| `index.ts` | — | Re-exports only; no Go file |

## Phase F — Transport

| Upstream file | Go file | Tests | Divergences |
| --- | --- | --- | --- |
| `packages/adapter-slack/src/api/{client,extra,index}.ts` | `slack/api/{client,extra}.go` | `slack/api/client_test.go` (17/17 `it` + 4 `it.each` rows) + `file_test.go` (moved origin cases) | free functions + per-call `fetch`/`token`/`apiUrl` → `Client` (`HTTPClient`, `Token` `TokenSource`, `APIURL`); `SlackBotToken` string\|func → `TokenSource` + `StaticToken`; `isSlackAuthUrl` moved here as `IsSlackAuthURL` (parent aliases); no retries; nil `HTTPClient` is a package-owned client, never `http.DefaultClient`; `FetchThreadReplies`/`OpenView` return `Response` (messages/view on `Raw`); `UploadFiles` returns `[]UploadedFile`; `OpenView` is `(triggerID, view)` (no `interactivityPointer` on the signature); `api/boundary.test.ts` not ported (compile-time: stdlib only, no parent/`chat`/`shared` import) |
| `packages/adapter-slack/src/webhook/verify.ts` | `slack/webhook/verify.go` | `slack/webhook/verify_test.go` (6/20 `index.test.ts` `it` + 6 Go-added) | `verifySlackSignature` → `Verify(signingSecret, header, body, now)`; `now` is `time.Time` not `now() ms`; skew default 300 only (no `maxSkewSeconds` option); sentinels not `SlackWebhookVerificationError`; no Web Crypto check; `webhookVerifier` / `verifySlackRequest` / `readSlackWebhook` not in this file (Task 23). Test file split: this task took `verifySlackSignature` (5) + `verifySlackRequest` "returns the verified body" |
| `packages/adapter-slack/src/webhook/{types,parse,utils}.ts` + `index.ts` re-exports | `slack/webhook/{types,parse,utils}.go` | `slack/webhook/parse_test.go` (14/20 remaining `index.test.ts` `it`) | `parseSlackWebhookBody` → `Parse(contentType, body, header)` (header is flattened `SlackParseOptions.headers` for retry + content-type fallback); `SlackWebhookPayload` union → `Event` interface (variants drop the `Slack` prefix; `Kind` is a field); `json.RawMessage` then `map[string]any` + switch on `type` (the one dynamic-unmarshal site); form-urlencoded and `payload=<json>` dispatch matches `isFormBody`/`parseFormBody`; `URLSearchParams` → `url.ParseQuery` (fromEntries last-wins); optional strings/numbers are zero values, optional bools `*bool`; `SlackWebhookParseError` → `ErrInvalidJSON`; `readSlackWebhook` → `Read`; `verifySlackRequest` + `webhookVerifier` → `VerifyRequest` + `WebhookVerifier` (string/`[]byte` replaces the body, other truthy keeps it, falsy → `ErrVerifierRejected`); `webhook/boundary.test.ts` not ported (compile-time: stdlib only, no parent/`chat`/`shared`) |
| `packages/adapter-slack/src/agent-context.ts` | `slack/agentcontext.go` | `slack/agentcontext_test.go` (10/10 `it` + 1 Go-added JSON-raw) | exported `GetAppContext` / `NormalizeAppContextEntities` (`index.ts` re-exports them); `SlackAppContext \| undefined` is `*SlackAppContext`; `Message.Raw` is `any` so `app_context` is read from `map[string]any` (typed `SlackAppContext` or a JSON object — TS is a duck-typed assertion); non-string values on typed tokens become `""` (TS `as string` is compile-only); empty results are a non-nil slice. `chat.AppContextEntity` is a flat struct (not a union); all upstream kinds fit. |

## Phase G — The SlackAdapter

| Upstream file | Go file | Tests | Divergences |
| --- | --- | --- | --- |
| `packages/adapter-slack/src/index.ts` (constructor, thread IDs, `parseMessage`) | `slack/slack.go` + `slack/parse.go` | `slack/slack_test.go` (Task 25 describe blocks) | `createSlackAdapter` → `New`; `botToken` → `api.TokenSource`; inversion only for default-true flags → `DisableNativeStreaming` (`agentView` is `AgentView bool`, zero = off = `?? false`); `logger` → `*slog.Logger` (nil → discard); `webhookVerifier` is `func(http.Header, []byte) error`; socket fields dropped; `webClientOptions` dropped; `Message.Raw` is the inner event; `Formatted` is goldmark (shape helper in tests); `rehydrateAttachment` landed in Task 31; constructor derives `supportsTurnCancellation = agentView` and `sessionTitle = config.sessionTitle ?? agentView`; `botUserIDFrom` shadows the static `botUserID` when the request ctx has one (upstream OR-chains `rc \|\| static` — deliberate, more tenant-correct; unpinned edge) |
| `packages/adapter-slack/src/index.ts` (core messaging, mentions, ephemeral IDs, initialize) | `slack/slack_messaging.go` + `slack/slack_users.go` (+ `slack.go`/`parse.go` seams) | `slack/slack_post_test.go`, `slack_fetch_test.go`, `slack_mentions_test.go`, `slack_ephemeral_test.go` (102/102 owned `it` + 1 Go-added) | Slack calls go through Task 21 `api.Client` (`httptest` + hook-rewrite transport); cards via `slack.CardToBlockKit` not `blocks.CardToBlockKit`; files via `api.Client.UploadFiles` not `files.uploadV2`; `RawMessage.Channel` is the adapter thread ID; `PostEphemeral` has no `usedFallback` (always false upstream); `StartTyping` `""` is unset (`processing` in agent view; JS `""`→`active` cannot be expressed); `OpenView` uses `Call("views.open")` when `interactivityPointer` is needed; webhook-dispatch `it`s call `parseSlackMessage`/`resolveInlineMentions` (Task 28 owns `handleWebhook`); `subclass extensibility` is same-package field access; `state()` no-ops when `Chat.State()` is nil (Task 25 `stubChat`); `user_change` → Task 28, `rehydrateAttachment` → Task 31; `UserInfo.Tz` (IANA zone from `users.info` `tz`): Go addition, absent upstream. |
| `packages/adapter-slack/src/index.ts` (`stream`, rotation, fallback, feedbackButtons) | `slack/slack_stream.go` | `slack/slack_stream_test.go` (44/45 owned `it`; 1 handleWebhook click landed in Task 28; +2 Go-added contract tests) | `chatStream` helper → `chat.startStream`/`appendStream`/`stopStream` via `api.Client.Call` (JSON; always `chunks`, text wrapped as a `markdown_text` chunk, as `ChatStreamer` sends it — a bare `markdown_text` after a card is `streaming_mode_mismatch`, gotcha 2); ChatStreamer 256-char buffer is `streamBufferSize` (tests that mocked flush-every-append set it to 1); `Date.now` → injectable `now` (no `time.Sleep`); `nativeStreamingBroken` is `atomic.Bool` (no mutex across network); Infinity max-age → `time.Duration(math.MaxInt64)` (Duration has no NaN — the 0/−5 table covers the ≤0 rewrite); `*url.Error` wrapping `context.Canceled`/`DeadlineExceeded` is unwrapped to the bare sentinel; a canceled ctx still finalizes the stream (open task cards → `error`, `chat.stopStream`) under `context.WithoutCancel` + `streamFinalizeTimeout`, returning `(msg, context.Canceled)` (gotcha 1; upstream's un-signaled fetches get this for free); **adjudicated:** upstream propagates an iterator throw immediately (dangling native stream, no finish); Go finalizes the native stream and returns `(*RawMessage, error)` — strictly safer, pinned by `TestStreamProducerTerminalErrorFinalizes`; `RawMessage.Channel` is the adapter thread ID; `feedbackButtons: true` is `&FeedbackButtonsOptions{}`; `BuildFeedbackButtonsBlock` exported |
| `packages/adapter-slack/src/index.ts` (`handleWebhook`, `processEventPayload`, interactive/slash) | `slack/slack_webhook.go` (+ `slack.go`/`parse.go`/`chat.ChatInstance` seams) | `slack/slack_webhook_test.go` (69/70 owned `it`; socket-dedupe `it` DROPPED; +2 un-deferred: `user_change`, feedback-button `onAction`; +1 Go-added concurrent token-isolation test) | `HandleWebhook(w, r)` writes the response (upstream returns `Response`); the adapter unmarshals the JSON envelope itself and switches `payload.event.type` — dispatch does **not** go through `webhook.Parse` (which only first-classes url_verification / app_mention / im-message); `Message.Raw` is the inner event; `process*` factories flatten to `*Input`; `ProcessMessageDeleted` is `(threadID, messageID)` — Go drops upstream `deletedAt` / `previousMessage` / `raw` (nothing the tests pin is lost); `waitUntil` → `pending` + `waitPending` (tests); request context is `context.WithValue` (not an adapter-wide field); spawned work + options-load leftover use `context.WithoutCancel`; **adjudicated ack:** upstream does not await `processMessage` / `processSlashCommand` / `processAction` before acking (`index.ts:3352` / `2516` / `2593`) because the TS Chat class returns promptly — Go awaits those calls, so `ChatInstance.Process*` implementations must return promptly; the async split lives in the Chat core; `Config.WebhookVerifier` wraps Task 22 `webhook.VerifyRequest`; options-load timeout is injectable `optionsLoadTimeout` (tests use 1ms, no `time.Sleep`); `SetInstallation` / token resolve is a Task 30 seam; `app_context_changed` + `processAppHomeInput.Entities` landed in Task 29; `ProcessAppHomeOpened` / `ProcessAppContextChanged` / session-stop / title-changed / `abortTurn` are slack-local type-asserts, not `ChatInstance` |
| `packages/adapter-slack/src/index.ts` (agent view: home, prompts, statuses, titles) | `slack/slack_agentview.go` (+ webhook glue) | `slack/slack_agentview_test.go` (41/41 owned `it`; StartTyping `""`→`active` row kept as `processing`) | `AgentViewPublisher` methods take encoded thread IDs; Slack channel+`thread_ts` helpers are unexported (tests call them). TS `suggestedPrompts` / `sessionTitle` unions → typed pairs (`SuggestedPrompts`+`SuggestedPromptsResolver`, `SessionTitle *bool`+`SessionTitleResolver`); both-set is a constructor validation error. `setSuggestedPrompts` always sends `prompts` (empty → `[]`). `views.publish` / `assistant.threads.setSuggestedPrompts` use `EncodingJSON`; `setStatus` / `setTitle` / `rename` use `slackCall` (form). No new typed `api.Client` methods. `buildFeedbackButtonsBlock` already exported from Task 27. StartTyping `""` stays unset → `processing` (JS explicit `""`→`active` cannot be expressed; Task 26 pin). |
| `packages/adapter-slack/src/index.ts` (OAuth, installations, token crypto, withBotToken, client/webClient) | `slack/installations.go` (+ `slack.go` constructor/config, `parse.go` enterprise fetchMetadata, `api.Client.Extra` Grid extras on every Call) | `slack/installations_test.go` (74/82 owned `it`; 4 rehydrate landed in Task 31; 4 WebClient-only skipped) | `handleOAuthCallback` → `HandleOAuthCallback(ctx, *http.Request, *SlackOAuthCallbackOptions)`; `oauth.v2.access` via `api.Client` + empty `StaticToken` (empty token omits `Authorization`); request context stays ctx values (`WithBotToken` is a ctx callback); `WebClient`/`client` → `WebClient`/`Client` returning cached `*api.Client`; `botToken` func → `api.TokenSource`; encrypted `botToken` wire is still Task 15 `encryptedTokenData` (unexported); missing/undecryptable install is an error (fail-closed), never another tenant's token. `installationProvider` is read-only (writes stay on the state store). Grid extras (`team_id` / `client_context_team_id`) ride `api.Client.Extra` so every Call path gets them once when the ctx request context carries Grid flags (`withToken` stays the unit-testable map helper). `enrichLinks` returns `(links, err)` — upstream swallows state errors (`index.ts:4481-4487`); no production caller yet. |
| `packages/adapter-slack/src/index.ts` (files, schedule, channel ops, postObject) | `slack/slack_files.go` + `slack/slack_channels.go` (+ `parse.go` token snapshot / `reply_count`) | `slack/slack_files_test.go` + `slack/slack_channels_test.go` (20/20 owned `it`) | `RehydrateAttachment` re-binds `FetchData` through `resolveTokenForTeam` (provider, else state). `FetchData` snapshots the request-ctx token string at parse (`ctxToken ?? getToken()`, ALS → ctx via `parseSlackMessageSync` + `WithBotToken`); single-workspace re-resolves `TokenSource` per fetch. Auth headers attach only when `api.IsSlackAuthURL` (token resolver is not invoked off Slack). Bytes go through `shared.DownloadAttachment` (SSRF policy; buffers like upstream `downloadAttachment`). `ScheduleMessage` → `chat.scheduleMessage` (`*RawMessage`; no cancel — `MessageScheduler` has none); empty `thread_ts` omitted. `ChannelReader` via `conversations.info` / `conversations.history` (`slackCall` form). `PostChannelMessage` posts with empty thread then rewrites `Channel` from `ts`. `ObjectPoster` posts/edits Block Kit `plan`/`task_card` (unsupported kind → `[kind]` text). Pins/usergroups have no `index.test.ts` thin-client coverage (WebClient escape hatch only — not ported). Unfurl cache via `chat.StateKV` already landed in Tasks 28/30. |

### Config field mapping (upstream `SlackAdapterConfig` → Go)

| Upstream | Go | Notes |
| --- | --- | --- |
| `botToken` string\|func | `Token api.TokenSource` | `api.StaticToken` / custom; env `SLACK_BOT_TOKEN` only in zero-config |
| `signingSecret` | `SigningSecret` | env `SLACK_SIGNING_SECRET` unless `WebhookVerifier` is set |
| `botUserId` | `BotUserID` | |
| `userName` | `UserName` | default `"bot"` |
| `apiUrl` | `APIURL` | env `SLACK_API_URL` |
| `logger` | `Logger *slog.Logger` | nil → `slog.New(slog.DiscardHandler)` |
| `clientId` / `clientSecret` | `ClientID` / `ClientSecret` | env fallback only in zero-config (`SLACK_CLIENT_ID` / `SLACK_CLIENT_SECRET`) |
| `encryptionKey` | `EncryptionKey` | env fallback always (`config.encryptionKey ?? SLACK_ENCRYPTION_KEY`); hex or base64 → 32-byte key |
| `installationKeyPrefix` | `InstallationKeyPrefix` | default `slack:installation` |
| `installationProvider` | `InstallationProvider` | interface; lookup is Task 30 |
| `nativeStreaming` default true | `DisableNativeStreaming` | inverted (only default-true flags invert) |
| `agentView` default false | `AgentView bool` | plain; zero = off. Task-25 brief wrongly used `DisableAgentView` / default-true. `supportsTurnCancellation = AgentView`; `sessionTitle = config.sessionTitle ?? agentView` |
| `feedbackButtons` | `*FeedbackButtonsOptions` | nil = off |
| `loadingMessages` | `LoadingMessages` | |
| `sessionTitle` | `SessionTitle *bool` + `SessionTitleResolver` | TS `boolean \| resolver` → typed pair (nil bool = unset → `agentView`); both-set is a validation error |
| `suggestedPrompts` | `SuggestedPrompts *chat.SuggestedPrompts` + `SuggestedPromptsResolver` | TS static-or-resolver union → typed pair; both-set is a validation error |
| `streamSegmentMaxAgeMs` | `StreamSegmentMaxAge time.Duration` | 0 → 240s |
| `webhookVerifier` | `WebhookVerifier func(http.Header, []byte) error` | nil → built-in verify (Task 28) |
| `appToken` | dropped | socket mode cut |
| `socketForwardingSecret` | dropped | socket mode cut |
| `mode` `"socket"` / socket variants | dropped | webhook only |
| `webClientOptions` | dropped | `WebClient` replaced by `api.Client` |
| fetch / HTTP | `HTTPClient api.HTTPDoer` | |

`Config.LogValue` redacts `Token`, `SigningSecret`, `ClientSecret`, `EncryptionKey`.

`botUserIDFrom` (used by `isMessageFromSelf` / mention checks): if the request ctx carries a `botUserID`, that value wins and the adapter-level static `botUserID` is not consulted. Upstream OR-chains `requestContext.botUserId || this.botUserId`. The shadow is deliberate and more tenant-correct in multi-workspace mode (another tenant's static identity must not leak onto a resolved install). No upstream `it` pins the OR-chain; unpinned edge.

Three items stay out: socket-mode text, four WebClient-only tests, and the StartTyping row from `""` to `active`. That row stays `processing`. Crypto helpers and `isSlackAuthURL` stay unexported. Callers use `GetInstallation`, `SetInstallation`, `DeleteInstallation`, and `HandleOAuthCallback`. Tokens go in as plaintext and come out as plaintext.

## Phase H — Contracts and smoke

Shared runners live in `chattest` (`RunConnectContract` / `RunThreadIDContract` / `RunSelfMessageContract`). Each takes `factory func(*testing.T) chat.Adapter`. Capabilities beyond core `chat.Adapter` are type-asserted inside the suite and skipped with a reason when absent (`ConnectBuilder`, `ThreadIDCaser`+`Named`, `DMChecker`+`DMFixtures`, `SelfMessageRequests`+`DispatchWatcher`, optional `createAdapterWithSecretAndVerifier` / `DispatchHandler`).

`slack/contracts_test.go` is the Slack wire-up: `httptest` + `statememory` + `chattest.MockChat` at `Initialize`. Slack implements Connect + thread-id hooks. Slack does **not** implement `SelfMessageRequests`: upstream Slack never calls `selfMessageContract` (isMe filtering is Chat-core, `index.ts:3225-3239`); a first run with hooks confirmed `processMessage` still fires for the bot's own events. The fake in `chattest/contracts_test.go` proves the self-message runner.

**Reconciliation:** Task 25's `TestThreadIDContract` (encode/decode/isDM cases) was deleted. Canonical cases are `chattest.RunThreadIDContract` with Slack's pinned ids (`slack:C12345:1234567890.123456`, `slack:C12345:`) and DM fixtures. `TestDecodeThreadIdEdgeCases` / `TestIsDMEdgeCases` stay Slack-local.

| Upstream file | Go file | Tests | Divergences |
| --- | --- | --- | --- |
| `packages/tests/src/connect-contract.ts` | `chattest/contracts.go` `RunConnectContract` | `chattest/contracts_test.go` (fake, 6/6) + `slack/contracts_test.go` (6/6) | factory + `ConnectBuilder`; `HandleWebhook(w, r)` + `httptest`; verifier is `(req, body string) (any, error)` (throw → error, falsy → `IsFalsy`); Slack `Config.WebhookVerifier` is `(header, body) error` so the wire-up adapts and reconstructs a `*http.Request` (contract only asserts `any(Request)` + raw body) |
| `packages/tests/src/thread-id-contract.ts` | `chattest/contracts.go` `RunThreadIDContract` | fake 4/4 + Slack 4/4 (moved from `TestThreadIDContract`) | factory + `ThreadIDCaser` / `Named` / optional `isDM`; Slack cases are the Task 25 pins |
| `packages/tests/src/self-message-contract.ts` | `chattest/contracts.go` `RunSelfMessageContract` | fake 2/2; Slack skip (upstream Slack does not call this contract) | factory + `SelfMessageRequests` + `DispatchWatcher` (`MockChat`); default handler `processMessage` |
| `packages/tests/src/factories.ts` (`createMockChatInstance`) | `chattest/factories.go` | used by contract fakes + Slack `Initialize` | process* record dispatch names; no `vi.fn` |
| `packages/tests/src/matchers.ts` (`toHaveDispatched`) | `chattest.MockChat.Dispatched` | in-suite | no vitest matcher registration |
| — (Go-added smoke) | `slack/smoke_test.go` | `TestAdapterSmoke` | signed `event_callback` → `ProcessMessage` (Raw = inner event) → native `Stream` + `PostMessage` card; exact API method order |
| `packages/tests/src/{app-context-matcher,index,smoke}.test.ts` + `setup.ts` | — | — | vitest matcher infrastructure / out-of-scope Chat orchestrator |
| `packages/adapter-slack/src/types.ts` | — | — | covered by the Config field-mapping table |

## Phase I — GitHub adapter

This is `@chat-adapter/github` v4.40.0 (`packages/adapter-github`). Multi-tenant App mode is dropped. That mode is a client per install, a repo-to-install map, and `getInstallationId`. Go keeps one `InstallationID`. `installationToken` is a `TokenSource`.

| Upstream file | Go file | Tests | Divergences |
| --- | --- | --- | --- |
| `types.ts` | `github/types.go` | `github/types_test.go` | TS union `GitHubRawMessage` → one `RawMessage{Type,...}`; `GitHubIssueComment`/`GitHubReviewComment` → one `Comment` superset; `AuthorAssociation` added (not in the upstream types); config union → one `Config` (this table) |
| `index.ts` (constructor auth) | `github/auth.go` | `github/auth_test.go` | `@octokit/auth-app` → stdlib RS256 JWT + token exchange; `Token(ctx)` caches with a 5 min refresh margin |
| `index.ts` (Octokit) | `github/api.go` | `github/api_test.go` | hand-rolled REST client; error mapping per go-github `CheckResponse`/`parseRate` (reference, not a dependency) |
| `index.ts` (`verifySignature`) | `github/verify.go` | `github/verify_test.go` | `timingSafeEqual` → `hmac.Equal` |
| `chat.ts detectMention` | `github/mention.go` | `github/mention_test.go` | Go-divergence: adapters set `Message.IsMention` (no Chat core); unexported until a second adapter needs it |
| `index.ts` (`handleWebhook`) | `github/webhook.go` | `github/webhook_test.go` | `HandleWebhook(w, r)` writes the response (upstream returns `Response`); verifier is `func(*http.Request, []byte) error` and wins over `WebhookSecret` at `New` (secret is cleared); 25 MiB cap → 413; raw payload is never logged; multi-tenant no-repo / per-installation lookup DROPPED; `ProcessMessage` is awaited under `context.WithoutCancel` |
| `index.ts` (`parseMessage`) | `github/parse.go` | `github/parse_test.go` | `Raw` is a reconstructed `RawMessage` (`repoRef` zeros `ID`, sets `FullName`); `Formatted` is goldmark; `IsMention` set here (no Chat core); `IsMe` is `id == BotUserID && id != "0"`; `FullName` is login |
| `index.ts` (post/edit/delete, reactions, fetch, users, threads, channels, subject) | `github/messaging.go` | `github/messaging_test.go` | Octokit methods → `client.do`; cards via `CardToGitHubMarkdown`; `GetUser` / `FetchSubject` API errors → `(nil, nil)`; `FetchSubject` logs Debug on a bad `FullName` the same way as an API error; `RenderFormatted` is comma-ok `content.(ast.Node)` (nil node if the type is wrong); `ThreadInfo` has no `IsDM`; `Probe` is Go-added (App JWT `GET /app` + `GET /installation/repositories`) |
| `index.ts` (`stream`) | `github/messaging.go` `Stream` | `github/messaging_test.go` `TestStream` | accumulate `MarkdownTextChunk`s and `PostMessage` once; iterator error or canceled ctx posts nothing; empty text returns `(nil, nil)` (upstream posts `{markdown: ""}`); task-card chunks are ignored |
| `markdown.ts` | `internal/shared/markdown.go` (`github/markdown.go` delegates) | `internal/shared/markdown_test.go` (19/19) | `PostableMarkdown` is returned verbatim (GitHub speaks markdown natively and `FromAst` is lossy — `chat.StringifyMarkdown` is inline-only and panics on a `ParseMarkdown` tree with nil `src`) |
| `cards.ts` | `internal/shared/markdowncards.go` (`github/cards.go` delegates) | `internal/shared/markdowncards_test.go` (12/12) | `CardToMarkdown` / `CardToPlainText` over `chat.Card` element types; `ChartElement` uses `chat.CardChildToFallbackText`; `escapeMarkdown` order is `\`, `*`, `_`, `[`, `]` |
| `index.test.ts` (shared contracts) | `github/contracts_test.go` | `TestAdapterContracts` (Connect 6/6, thread-id 3/3 + DM skip, self-message 2/2) | factory + `githubContract` (`*Adapter` + `ghMock` + `MockChat`); `DecodeThreadID` returns a `ThreadID` value so `ThreadIDCases` `Decoded` are values; GitHub has no `IsDM` |
| — (Go-added smoke) | `github/smoke_test.go` | `TestAdapterSmoke` | App config → signed `issue_comment` → `ProcessMessage` (`Raw` is `RawMessage`, `*IsMention`, `Author.IsMe == false`) → `Stream` two chunks → one comment POST |

### Config field mapping (upstream `GitHubAdapterConfig` → Go)

| Upstream | Go | Notes |
| --- | --- | --- |
| `token` string\|func | `Token TokenSource` | `StaticToken` / any resolver; env `GITHUB_TOKEN` only in zero-config |
| `appId` | `AppID` | env `GITHUB_APP_ID` only in zero-config |
| `privateKey` | `PrivateKey []byte` | PEM; env `GITHUB_PRIVATE_KEY` only in zero-config |
| `installationId` | `InstallationID int64` | env `GITHUB_INSTALLATION_ID` only in zero-config; required with App fields (no multi-tenant map) |
| `installationToken` | `Token` | any `TokenSource`; Vercel Connect resolver is a `TokenSource`, not a third auth mode |
| `webhookSecret` | `WebhookSecret` | env `GITHUB_WEBHOOK_SECRET` unless `WebhookVerifier` is set |
| `webhookVerifier` | `WebhookVerifier func(*http.Request, []byte) error` | wins over `WebhookSecret` (`New` clears the secret) |
| `userName` | `UserName` | default `"github-bot"`; env `GITHUB_BOT_USERNAME` |
| `botUserId` number | `BotUserID string` | numeric id as a string; env `GITHUB_BOT_USER_ID`; skips detection when set |
| `apiUrl` | `APIURL` | env `GITHUB_API_URL`; default `https://api.github.com` |
| `logger` | `Logger *slog.Logger` | nil → `slog.New(slog.DiscardHandler)` |
| per-installation clients / `getInstallationId` / repo→installation map | dropped | multi-tenant App mode is not implemented |

`Config.LogValue` redacts `Token`, `PrivateKey`, `WebhookSecret`.

## Phase II — Linear adapter

`@chat-adapter/linear` v4.40.0 is the `linear` package. Only agent-session mode is ported.

| Upstream file | Go file | Tests | Divergences |
| --- | --- | --- | --- |
| `index.ts` (constructor, initialize) | `linear/linear.go`, `linear/auth.go` | `linear_test.go`, `auth_test.go` | apiKey/accessToken/multi-tenant/Connect/clientCredentials-config flavours → `Token` or `ClientID`+`ClientSecret` (client_credentials); scopes are a constant; `Initialize` fails when the viewer query fails; token cache re-mints inside 1 h of expiry and on `Invalidate()` |
| `index.ts` (handleWebhook, onAgentSessionEvent) | `linear/webhook.go`, `linear/verify.go` | `webhook_test.go`, `verify_test.go` | `Linear-Timestamp` header first, body second, both absent rejected; delivery claim on session/activity id via the state adapter (Go addition); `stop` signal → `chat.TurnAborter` + one `response` (upstream treats stop as a prompt); null-issue and self drops; 500 on dispatch failure; `Comment`, `Reaction`, `OAuthApp` events dropped with comments mode |
| `index.ts` (parseMessage) | `linear/parse.go` | `parse_test.go` | delegation without a comment → short `Text`, `promptContext` on `Raw`; `guidance`/`previousComments` not parsed |
| `index.ts` (postMessage, startTyping, reactions) | `linear/messaging.go` | `messaging_test.go` | `PostError` (Go addition, `chat.ErrorPoster`); `RawMessage.ID` falls back to the activity id when `sourceComment` is null; `AddReaction` rejects synthetic ids |
| `index.ts` (streamInAgentSession) | `linear/messaging.go` `Stream` | `stream_test.go` | task error → `action` with `result: "failed"` (upstream: `error` activity); empty final text → `response` "Done."; first failed post stops posting and drains; `plan_update` ignored; `ActivityContent.Parameter` is `*string` so non-action activities omit it (action always sends `""`); thoughts use `DisableTableWrapForAppend: true` so a GFM table is not rewritten into the final response |
| `index.ts` (fetchMessages, fetchThread, getUser, fetchSubject) | `linear/messaging.go` | `fetch_test.go` | activities filtered to prompt/response/error/elicitation and `ephemeral` skipped (upstream renders every activity); backward uses `last`/`before`; `FetchThread.ChannelID` is `linear:{issueId}` (upstream: bare id); comment-thread and issue-level fetches dropped with comments mode |
| `markdown.ts`, `cards.ts` | `internal/shared/markdown.go`, `internal/shared/markdowncards.go` | moved GitHub tests | identical to GitHub's upstream; one implementation |
| `utils.ts` | `linear/parse.go` `userName` | `parse_test.go` | — |
| `types.ts` | `linear/types.go` | `types_test.go` | comments mode thread shapes decode to a `ValidationError` naming the mode; `ActivityContent.Parameter` is `*string` (`omitempty`) so thought/response/error omit the key |

Dropped: comments mode (`linear:{issueId}`, `:c:` threads, post-then-edit streaming), multi-tenant installations and `handleOAuthCallback`, Vercel Connect, AES token encryption, `LINEAR_API_KEY`/`LINEAR_ACCESS_TOKEN`/`LINEAR_CLIENT_CREDENTIALS_*` env flavours, `plan_update` → `agentSessionUpdate`.

## Tests

Run `make fmt`. Then run `go test -shuffle=on -race ./...`.

Contracts: `go test -shuffle=on -race ./chattest ./slack ./github ./linear -run 'TestConnectContractFake|TestThreadIDContractFake|TestSelfMessageContractFake|TestAdapterContracts'`.

Smoke: `go test -shuffle=on -race ./slack ./github ./linear -run TestAdapterSmoke`.
