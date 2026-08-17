# Design

## One representation

A provider-native request converts into one internal representation, gets
compressed, and converts back. Wire-format code lives in adapters; the
compression pipeline knows no wire format. One adapter ships today: `anthropic`.

Dependencies point one direction: `http → pipeline → ir` and
`http → adapters → ir`. Adapters and compression never import each other, so a
second provider is a new adapter and nothing else.

The IR remembers enough to reproduce the wire format exactly. It is not a model
of provider features — unmodelled fields ride along as passthrough, and key
order is recorded per object. That matters for caching, below.

## Two passes

The first pass splits each text block into regions and marks which ones to leave
alone. The second classifies every word in the remaining prose and removes the
classes the level names.

`moderate` names determiners and prepositions, so
"The man went to the store" loses both. The levels nest —
`light ⊂ moderate ⊂ caveman` — which makes output length non-increasing as the
level rises.

Word tagging comes from `compromise`. Its tags co-occur — a pronoun also carries
`Noun`, a copula also carries `Verb` — so `TAG_PRIORITY` in `classify.ts` maps
them in a fixed order and the first match wins. A word the classifier does not
recognize resolves to `other`, which no level removes. Every unresolved case
costs savings and never meaning.

The dependency is pinned to an exact version, no caret. Its lexicon decides how
a word is tagged, so a patch release changes which words are dropped and
therefore the bytes sent upstream.

## Subordinators

A subordinator — `if`, `unless`, `when`, `before`, `because`, `although`,
`otherwise` — is the word that relates one clause to another. Drop it and the
clauses remain, now asserted: "do not proceed if the tests fail" becomes "do not
proceed, the tests fail", which claims the tests failed. The token count records
a saving and has no way to show the loss.

These are kept by list, not by tag, because the tags do not separate the two
uses. `compromise` gives `before` the same `Conjunction` tag in "proceed before
the tests pass" as in "the file before the directory", and scatters the rest of
the class across `Conjunction`, `Preposition`, `Adverb` and `Determiner`. Only
`unless` and `lest` get a `Condition` tag.

Keeping a non-subordinating use costs one word. Dropping a subordinating one
costs the meaning of a clause. Every case resolves toward keeping.

## Predicate adjectives

`compromise` tags a past participle as `Adjective`, which is right for "the
abandoned building" and wrong for "50 requests abandoned" — where the participle
is the predication, with the copula left out. A sentence carrying no verb has
nothing else to predicate, so an adjective following a noun in a verbless
sentence is the one holding the assertion.

Those are tagged `predicate` and survive `caveman`, where adjectives are
otherwise removable. "connection refused" stays "connection refused".

## Region protection

Anything matching a protection pattern resolves to protected. Over-protecting
costs savings; under-protecting corrupts a code block, a path, or a stack trace
the model needs verbatim.

Whole blocks are excluded before any of this runs: tool definitions,
`tool_use.input`, `thinking` and `redacted_thinking`, images, documents, and any
block type the adapter does not recognize. What follows applies to the text
blocks that remain.

Line-level protection runs first: fenced blocks (fence lines included, so a
URL inside one never fragments), indented code, table rows, stack trace lines.
An unterminated fence protects everything after it. Then inline patterns:
backticked code, URLs, Windows and POSIX paths, dotted filenames, JSON object
and array literals, quoted strings, XML/JSX elements, stack frame fragments, hex
literals, UUIDs, long digit runs, version strings, snake_case identifiers,
`$VARS`, `--flags`, and call-shaped identifiers.

Markdown markers are protected as markers only. The prose after a bullet or
header survives as a sentence and compresses.

Overlapping spans merge before the gaps between them become prose regions, so
protection never fragments, and the regions tile: reconstructing them in order
yields the input back.

Each prose region is parsed on its own, so `compromise` sees whole sentences and
can disambiguate by context — `book` is a verb in "book a flight" and a noun in
"the book is here". Offsets stay anchored to the original string, and every
classified word satisfies `text.slice(word.start, word.end) === word.text`.

Words that fall outside grapheme cluster boundaries are dropped from the result
instead of sliced. Their characters stay in the gap, where they are copied
verbatim, so a ZWJ emoji sequence never splits.

## Two guards

A block that compresses to whitespace keeps its original text — the API rejects
an empty text block. A candidate longer than its input is discarded for the
original, since removal cannot lengthen text and a longer candidate means the
assembly is at fault.

Compression is deterministic: the same text at the same level produces the same
bytes, in one process and across processes.

## Caching

A prompt cache matches on prefix bytes being identical from one turn to the
next. Compressing a cached prefix does not break that, under the default. The
compressor reads a node's text and the level, never where the node sits, so each
node has one compressed form and produces it every turn. The cached prefix
settles in its compressed form and keeps hitting.

The turn a node first compresses costs one write for the segment it lies in.
Every conversation writes each segment once anyway: each turn extends the prefix
and re-writes its tail. That tail is smaller compressed, as is the prefix read
back.

Position-dependence is what would break it. If nodes under a breakpoint were
skipped, a node's output would depend on where the breakpoint sits, and a
breakpoint advancing past that node would flip its text back to the original —
prefix changes, cache misses.

`X-Caveman-Cache: respect` is exactly that: skip every node at or before the
last breakpoint. The cached prefix stays the one that arrived, which matches a
client that caches its system prompt and tools on every request. It carries the
instability above, and exists to measure against the default, not to be used.

For the same reason, a request that compresses to nothing new must serialize the
way it arrived. Key order is preserved at every level — top-level, per-message,
per-block — and string-form `content` is re-emitted as a string, never promoted
to a block array. JSON key order is insertion order, and a reordered
body misses the cache even when it carries identical values.

## Accounting

Response headers estimate at four characters to the token, because measuring
exactly would mean an upstream `count_tokens` call per request.
`X-Caveman-Ratio` reports the reduction achieved, not the one the level asked
for, and the headers attach even when upstream errors — so a compression-induced
4xx arrives with the ratio that caused it.

The billed counts are separate, read from the response body as it streams past:
`message_start` and `message_delta` for a stream, the whole document otherwise.
Those are the numbers the invoice is built from. Cache reads bill at a fraction
of the base rate and cache writes at a premium, so a prefix that stopped
matching shows up as writes replacing reads.

Reading costs the stream nothing. Each chunk is enqueued before the observer
sees it, and a throwing observer cannot hold a chunk back, so the first token
reaches the client as soon as upstream emits it.

## What the tests assert

436 tests across 17 files.

- **Round-trip identity.** `fromIR(toIR(x))` re-serializes to the same *bytes*
  as `x` for every fixture — string equality, not deep equality, so key order
  counts.
- **Transparency.** With no Caveman headers, the forwarded body is
  byte-identical to what the client sent.
- **Determinism.** Identical output across separate Node processes.
- **Structural validity.** Every fixture at every level keeps `tool_use` inputs
  parseable, `thinking` blocks unchanged, `tool_use_id` pairings intact, and
  emits no empty text block.
- **Region protection.** Regions tile with no gaps or overlaps, and each
  protected construct stays byte-identical at every level.
- **Unicode safety.** No lone surrogates, emoji sequences stay whole, combining
  marks stay attached.
- **Cache behaviour.** Both modes, including a breakpoint rolling forward across
  turns.
