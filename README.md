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

| Header               | Meaning                                            | Default   |
| -------------------- | -------------------------------------------------- | --------- |
| `X-Caveman-Compress` | `off`, `light`, `moderate`, or `caveman`           | `off`     |
| `X-Caveman-Scope`    | Comma list of `messages`, `system`, `tool_results` | all three |
| `X-Caveman-Cache`    | `ignore` or `respect`                              | `ignore`  |

Levels name what they remove, and they nest — each one removes everything the
level below it does, plus more.

| Level      | Removes                                                      | "The man has quickly gone to the very large store" |
| ---------- | ------------------------------------------------------------ | -------------------------------------------------- |
| `light`    | determiners                                                  | "man has quickly gone to very large store"         |
| `moderate` | + prepositions, conjunctions, auxiliaries, copulas, pronouns | "man quickly gone very large store"                |
| `caveman`  | + adverbs, adjectives                                        | "man gone store"                                   |

Nouns, verbs, numbers, proper nouns, negations and subordinators are never
removed at any level. A word the classifier does not recognize is kept, so a
tagging miss costs savings rather than meaning.

A subordinator — `if`, `unless`, `when`, `before`, `because`, `although`,
`otherwise` and the rest of that closed class — is the word that relates one
clause to another. Drop it and both clauses remain, now asserted: "do not
proceed if the tests fail" becomes "do not proceed, the tests fail", a claim
that the tests failed. The token count records the saving and has no way to
show the loss.

These words are kept by an explicit list, not by their tag. The classifier gives
`before` the same tag in "proceed before the tests pass" and in "the file before
the directory". Keeping a non-subordinating use costs one short word; dropping a
subordinating one costs the meaning of the clause.

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

It also prints what the provider billed, read from the response as it streams
past:

```
caveman  billed  5,710 in  412 out  4,200 cache read  0 cache write
```

These are the counts the invoice is built from. A cache read is billed at a
fraction of the base rate and a cache write at a premium, so a prefix that
stopped matching shows up here as writes replacing reads. The line is printed
even when compression is off, so an uncompressed session gives a baseline. A
response that carries no counts prints no line.

Reading them costs the stream nothing. Each chunk is forwarded before it is
inspected, so the first token still reaches the client as soon as upstream
emits it.

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

## Caching

Text under a `cache_control` breakpoint is compressed like any other. A prompt
cache matches on the prefix bytes being identical from one turn to the next. The
compressor reads only a node's text and the level, never where the node sits, so
a node has one compressed form and produces it on every turn. The cached prefix
settles in compressed form and keeps hitting.

The turn a node first compresses costs one write of the segment it lies in. A
growing conversation writes that segment anyway: each turn extends the prefix and
re-writes its tail. From then on the written size is the compressed one, and
every later read is of a smaller prefix.

A positional rule breaks this. If nodes behind a breakpoint are skipped, a node's
output depends on where the breakpoint sits, and a rolling breakpoint that
advances past a node flips it from compressed text back to original. The prefix
changes and the cache misses on that turn.

`X-Caveman-Cache: respect` is the older behaviour: skip every node at or before
the last breakpoint. The cached prefix stays byte-identical to the one that
arrived and is never compressed — with a client that caches its system prompt
and tools, that is most of the request. It has the positional instability
described above, and exists to measure the default against.

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
