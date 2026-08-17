# Caveman

A transparent normalizing compression proxy. It converts a provider-native
request into one internal representation, removes whole grammatical classes
from eligible text, converts back to the provider's wire format, and forwards
it upstream.

Compression works in two passes. The first splits a block into regions and
marks which are safe to touch, so code, paths, JSON and stack traces are copied
through byte-identically. The second classifies each word in the remaining
prose by grammatical class and removes the classes the level names. "The man
went to the store" becomes "man went store" because determiners and
prepositions were removed, not because those words scored low.

The representation is provider-neutral. Provider-specific code lives in adapters;
the compression pipeline has no wire format. One adapter ships today:
`anthropic`, which forwards to the API.

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

| Route          | Upstream                               |
| -------------- | -------------------------------------- |
| `/v1/messages` | `https://api.anthropic.com`, over HTTP |

Auth headers pass through untouched, so a client authenticated by an OAuth
login reaches the API the same way an API key does. Point one at Caveman with
`ANTHROPIC_BASE_URL`.

## Headers

| Header               | Meaning                                            | Default    |
| -------------------- | -------------------------------------------------- | ---------- |
| `X-Caveman-Compress` | `off`, `light`, `moderate`, or `caveman`           | `off`      |
| `X-Caveman-Scope`    | Comma list of `messages`, `system`, `tool_results` | `messages` |

Levels name what they remove, and they nest — each one removes everything the
level below it does, plus more.

| Level      | Removes                                                      | "The man has quickly gone to the very large store" |
| ---------- | ------------------------------------------------------------ | -------------------------------------------------- |
| `light`    | determiners                                                  | "man has quickly gone to very large store"         |
| `moderate` | + prepositions, conjunctions, auxiliaries, copulas, pronouns | "man quickly gone very large store"                |
| `caveman`  | + adverbs, adjectives                                        | "man gone store"                                   |

Nouns, verbs, numbers, proper nouns and negations are never removed at any
level. A word the classifier does not recognize is kept, so a tagging miss
costs savings rather than meaning.

A malformed value returns 400 naming the header. It is never coerced to a
default — a fractional value like `0.5`, which earlier versions accepted, is
now an error rather than a legacy spelling. Caveman headers are stripped before
forwarding; auth headers pass through untouched and are never read or logged.

When compression runs, the response carries `X-Caveman-Tokens-Before`,
`X-Caveman-Tokens-After`, `X-Caveman-Ratio`, and `X-Caveman-Level`.
`X-Caveman-Ratio` reports the reduction actually achieved, not the level that
was asked for. Estimation is local and character-based; the billed counts are
in the upstream response's `usage`.

Caveman prints those counts to stdout as it forwards each compressed request,
with a running total for the session. Set `DISABLE_LOGS` to hide them.

## What is never compressed

Tool definitions, `tool_use.input`, `thinking` and `redacted_thinking` blocks,
images, and documents. A block type the adapter does not recognize round-trips
verbatim as passthrough.

Inside a text block, these regions are copied through byte-identically at every
level: fenced and indented code blocks, inline code, URLs, file paths, JSON and
XML/JSX spans, stack trace lines, hex literals, UUIDs, long digit runs, version
strings, markdown table rows, and markdown structural markers. An ambiguous
span resolves to protected, since a false positive costs only savings while a
false negative corrupts something the model needs verbatim. Markers are
protected but the prose after a bullet or a header is not — the marker
survives, the sentence compresses.

Compression is deterministic: the same text and level produce identical bytes,
in any process. The `compromise` dependency is pinned to an exact version
because its lexicon decides how a word is tagged.

Text at or before the last `cache_control` breakpoint is never compressed. The
prompt cache matches on the serialized request prefix, so rewriting a cached
block trades a small saving for re-billing the whole cached segment as a fresh
write. With a client that caches its system prompt and tools, this can leave
little compressible on the first turn; that is the intended trade.

For the same reason a forwarded request is byte-identical to the one that
arrived, not merely equal to it: key order is preserved at every level, since
JSON key order is insertion order and a reordered body misses the cache.

## Layout

```
src/
  ir/                   provider-neutral representation and its walk
  adapters/anthropic/   wire format ↔ IR
  compression/          regions, classify, levels, compress, pipeline
  policy/               header parsing
  http/                 Hono server, handler, upstream, SSE passthrough
  telemetry/            token accounting, response headers, savings log
```

To add a provider, write a `ProviderAdapter` — a route, an upstream host, `toIR`,
`fromIR`, an error envelope — and add it to `REGISTERED_ADAPTERS` in
`src/http/server.ts`. The handler, the pipeline, and the IR are untouched. Every
registered adapter serves on its own route.

Every provider is an HTTP host: the request is posted to `baseUrl + path`, with
the client's query string appended.

Dependencies point one direction: `http → pipeline → ir` and
`http → adapters → ir`. Adapters and compression never import each other.

## Tests

```sh
npm test
```

Round-trip identity: `fromIR(toIR(x))` re-serializes to the same bytes as `x`
across recorded request shapes, not merely deep-equals it. Determinism:
identical output across separate processes. Structural validity: every fixture
compressed at every level still has parseable `tool_use` inputs, unchanged
`thinking` blocks, and no empty text block.
