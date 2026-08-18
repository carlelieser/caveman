# Design

Caveman is a proxy. A request comes in, it deletes words from the prose, and forwards the rest
unchanged. Fewer words is fewer tokens, and tokens are the bill.

## What happens to a request

The body is parsed into a provider-neutral form, and every block of text in it is compressed one at
a time. For each block:

1. Split it into regions. Anything code-shaped is protected; the rest is prose.
2. Tag the prose for parts of speech.
3. Delete the words whose class the level allows.
4. Close the gaps, keeping sentence punctuation and paragraph breaks.

The body is then rebuilt and sent on. Nothing outside the prose is touched.

## Regions

Code blocks, JSON, stack traces, file paths, URLs, table rows and markdown markers are found by
pattern and copied through untouched (`compress/regions.go`). Anything code-shaped is protected,
because a wrong guess here costs some savings while the opposite corrupts a path or a trace the
model needs exactly as written.

This decides most of the outcome. A request that is mostly a pasted diff has almost no prose to
delete from and saves 7%; one that is all prose saves 46%. Same setting, same code.

## Levels

A level is the set of word classes it may delete — the table is in the README. The sets nest, so
raising the level never makes output longer (`compress/levels.go`). Nouns, verbs, numbers and
proper nouns are in no level's set. `caveman`, the most aggressive, reaches adjectives and adverbs
and stops.

## Words that look deletable but are not

Three rules override the class a word was tagged with (`compress/classify.go`).

`not` is tagged several ways at once, and the tag that wins is `Negative`, which no level deletes.
Deleting it would reverse the sentence.

`if`, `unless`, `because`, `before` and about fifteen others are matched by a word list rather than
by tag, because the tagger gives `before` the same tag in "proceed before the tests pass" as in
"the file before the directory". Delete it from the first and a condition becomes a claim: "do not
proceed if the tests fail" turns into "do not proceed, the tests fail". Keeping the harmless case
costs one word.

The tagger calls `abandoned` an adjective in both "an abandoned building" and "50 requests
abandoned", but in the second it is the entire assertion. An adjective following a noun in a
sentence with no verb is treated as the predicate and kept.

The same bias runs through the rest of the pipeline. A word whose position in the source cannot be
located exactly is left unclassified rather than guessed at. A block that compresses to nothing
keeps its original text. A result longer than its input is thrown away. Every uncertain case is
resolved by keeping the word, so the measured savings are a floor.

## Caching

Providers cache a prefix of the request and charge less when it matches, and the match is on bytes
being identical from one turn to the next. Compression looks at a block's text and the level, never
at where the block sits, so the same text always produces the same bytes. A block compressed on the
turn it first appears looks identical on every later turn, and the cached prefix settles in
compressed form (`compress/pipeline.go`). That is why compressing inside it is the default.

The alternative, `X-Caveman-Cache: respect`, skips blocks before the last cache breakpoint. That
makes a block's output depend on where the breakpoint is: when it moves past a block, the block
stops being compressed, its bytes change, and the prefix it was protecting is invalidated. It stays
for measurement.

## Fidelity

Every request is taken apart and rebuilt, including ones that are never compressed, so the rebuild
has to be byte-exact — key order, number literals, and fields Caveman does not model are all
preserved (`ir/orderedjson.go`). A rebuild that reordered keys would still be a valid request, and
would still miss the cache.

## Counting

Caveman counts what it saved before it forwards, running a real tokenizer over every block of text
it walked (`tokens/tokens.go`). Anthropic does not publish Claude's tokenizer, so the encoding is
cl100k_base, the BPE OpenAI ships — subword merges over UTF-8 bytes, common words as single tokens,
whitespace bound to the word after it. The tables are compiled in and loaded once, so no request
waits on a download. Counting adds about 7% to the walk: the corpus runs at 350k characters a
second without it and 325k with it.

Counting happens in the pipeline, not in accounting, because a token count needs the text and a
character count has already discarded it. Tokens also do not divide across a concatenation
boundary the way characters do, so each block is counted on its own and the totals are sums over
the same block boundaries on both sides — which is what makes before and after comparable.

The provider's own counts ride back in the response for free, and are logged on a second line. The
two will not match: the provider bills the whole serialized request, Claude's tokenizer is not
cl100k_base, and cache reads and cache writes are priced differently. A request can use fewer input
tokens and still cost more, if compressing it turned a cache read into a cache write.

## Extending it

A provider supplies its name, route, base URL, error shape, and conversions to and from the neutral
form (`adapters/provider.go`). Adding one is a registry entry; no handler knows a provider by name.
Part-of-speech tagging is a port of compromise.js 14.16.0, its lexicon and rules generated into Go
tables — about 1.8 MB, treated as a vendored dependency. Token counting brings a second one, the
cl100k_base BPE tables, loaded offline from `tiktoken-go`.

## Tests

`ir/deps_test.go` shells out to `go list -deps` and fails if the neutral form imports the
compressor, an adapter imports the tagger, or telemetry imports either. The invariants test holds
the rest: regions tile the input exactly, words never split a grapheme cluster, compression is
deterministic, output never grows, protected shapes come out verbatim. Golden corpora in
`testdata/golden/` turn any change in the algorithm into a visible diff.
