# Caveman

A transparent normalizing compression proxy. It converts a provider-native
request into one internal representation, drops the lowest-scoring tokens from
eligible text, converts back to the provider's wire format, and forwards it
upstream.

The representation is provider-neutral. Provider-specific code lives in adapters;
the compression pipeline has no wire format. Two adapters ship today: `anthropic`
forwards to the API, and `claude` runs the request through the local `claude`
CLI. Both speak the Anthropic wire format, so a client moves between them by
changing the path.

Compression is off unless you ask for it. With no Caveman headers, a request is
normalized, denormalized, and forwarded unchanged.

## Running

```sh
npm install
npm run dev          # tsx watch, listens on PORT (default 8787)
```

Each HTTP adapter has its own upstream host. `CAVEMAN_<PROVIDER>_BASE_URL`
redirects one provider (`CAVEMAN_ANTHROPIC_BASE_URL`);
`CAVEMAN_UPSTREAM_BASE_URL` redirects all of them. The per-provider variable
wins.

## Routes

| Route                 | Upstream                                |
| --------------------- | --------------------------------------- |
| `/v1/messages`        | `https://api.anthropic.com`, over HTTP  |
| `/claude/v1/messages` | the local `claude` CLI, as a subprocess |

## Headers

| Header                  | Meaning                                            | Default     |
| ----------------------- | -------------------------------------------------- | ----------- |
| `X-Caveman-Compress`    | Fraction of eligible tokens to drop, `0`–`0.9`     | `0` (off)   |
| `X-Caveman-Scope`       | Comma list of `messages`, `system`, `tool_results` | `messages`  |
| `X-Caveman-Scorer`      | Scorer name                                        | `heuristic` |
| `X-Caveman-Claude-Mode` | `proxy` or `agent`, on the `claude` route only     | `proxy`     |

A malformed value returns 400 naming the header. It is never coerced to a
default. Caveman headers are stripped before forwarding; auth headers pass
through untouched and are never read or logged.

When compression runs, the response carries `X-Caveman-Tokens-Before`,
`X-Caveman-Tokens-After`, `X-Caveman-Ratio`, and `X-Caveman-Scorer`. Estimation
is local and character-based; the billed counts are in the upstream response's
`usage`.

## The claude adapter

`/claude/v1/messages` runs each request through the local `claude` CLI instead
of the API, so it bills against whatever that CLI is logged in to rather than an
API key. `CAVEMAN_CLAUDE_BIN` overrides the binary (default `claude`).

The CLI is an agent, not a bare model, and the mode header chooses how much of
that shows through:

- `proxy` replaces the CLI's own agent prompt with the request's `system` and
  denies it tools, so the call behaves as much like a plain model call as a CLI
  session can.
- `agent` keeps the CLI's prompt and tools and appends the request's `system`.
  Responses may then contain `tool_use` blocks for tools the client never
  declared.

Either way the CLI still runs as an agent process, so some prompt overhead
remains and is visible in the response's `usage`.

Three request fields do not survive the trip, because the CLI has nowhere to put
them:

- **Conversation history is flattened into one prompt.** The CLI reads only the
  first message it is given, so a multi-turn request is rendered as a labelled
  `Human:` / `Assistant:` transcript. Non-text blocks become short placeholders
  (`[tool_use: name]`, `[image]`) rather than disappearing; `thinking` blocks are
  dropped, since their signatures cannot be replayed.
- **`max_tokens` is accepted and ignored.** The CLI has no equivalent flag.
- **`tools` are not forwarded.** Tool definitions round-trip through the IR but
  the CLI has no way to accept them.

Responses are assembled to match the route the client asked for: `stream: true`
gets Anthropic SSE, anything else gets a single Messages body. The `usage` is
the CLI's own billed count, not a local estimate.

## What is never compressed

Tool definitions, `tool_use.input`, `thinking` and `redacted_thinking` blocks,
images, and documents. A block type the adapter does not recognize round-trips
verbatim as passthrough.

Compression is deterministic: the same text, ratio, and scorer version produce
identical bytes. A compressed prefix marked with `cache_control` stays cacheable.

## Layout

```
src/
  ir/                   provider-neutral representation and its walk
  adapters/anthropic/   wire format ↔ IR
  adapters/claude/      the same wire format, over the local CLI
  compression/          tokenize, score, compress, pipeline
  policy/               header parsing
  http/                 Hono server, handler, upstream, SSE passthrough
  telemetry/            token accounting and response headers
```

To add a provider, write a `ProviderAdapter` — a route, an upstream host, `toIR`,
`fromIR`, an error envelope — and add it to `REGISTERED_ADAPTERS` in
`src/http/server.ts`. The handler, the pipeline, and the IR are untouched. Every
registered adapter serves on its own route.

A provider that is not an HTTP host supplies a `transport` as well, and owns the
trip upstream. Absent one, the request is posted to `baseUrl + path`, which is
what every HTTP adapter wants. The `claude` adapter is the worked example: it
reuses the Anthropic `toIR`/`fromIR` unchanged and differs only in transport.

Dependencies point one direction: `http → pipeline → ir` and
`http → adapters → ir`. Adapters and compression never import each other.

## Tests

```sh
npm test
```

Round-trip identity: `fromIR(toIR(x))` deep-equals `x` across recorded request
shapes. Determinism: identical output across separate processes. Structural
validity: every fixture compressed at several ratios still has parseable
`tool_use` inputs, unchanged `thinking` blocks, and no empty text block.

The `claude` adapter runs against a fake CLI (`test/fixtures/fake-claude.mjs`)
pointed at by `CAVEMAN_CLAUDE_BIN`, so the suite exercises a real subprocess
without reaching the network or spending tokens.
