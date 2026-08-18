# Caveman

<img src="docs/images/banner.png" alt="A caveman holding a torch before painted rocks in a cave" width="100%">

A compression proxy that uses part-of-speech tagging to remove word classes, cutting tokens by about 28% on a
mixed corpus and up to 52% on prose-heavy requests (see [Savings](#savings)).

How it works: [DESIGN.md](docs/DESIGN.md).

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/carlelieser/caveman/main/install.sh | bash
```

## Usage

```sh
caveman claude
```

### Commands

| Command                  | Does                                         |
| ------------------------ | -------------------------------------------- |
| `caveman up`             | Start the proxy in the background            |
| `caveman down`           | Stop it, reporting the session savings       |
| `caveman status`         | Say whether it is running, and on which port |
| `caveman <client> [...]` | Start it if needed, then launch a client     |
| `caveman measure`        | Report savings over the recorded corpus      |

### Levels

`-l` (or `--level`) takes `off`, `light`, `moderate`, or `caveman`, the
default and most aggressive. Give it to `up` and every client inherits it;
give it to a client and that launch alone uses it.

```sh
caveman up -l moderate    # every client from now on
caveman claude            # moderate, inherited
caveman claude -l light   # this launch only
caveman claude -l off     # uncompressed, for a baseline
```

`--count` reports the savings in real tokens rather than characters, which is
what you want when checking the numbers against a bill; it costs a tokenizer
pass per request, so it is off by default.

Everything after `--` goes to the client untouched, which is how you pass a flag
the CLI would otherwise read as its own: `caveman claude -- -l debug.log`

`claude` is built in. To add another, put an executable at
`$CAVEMAN_CLIENT_DIR/<name>` (default `$CAVEMAN_HOME/clients`); it runs with
`CAVEMAN_BASE_URL`, `CAVEMAN_LEVEL` and `CAVEMAN_COMPRESS_HEADER` set, and a
file there shadows a built-in of the same name.

| Level      | Removes                                                      | "The man has quickly gone to the very large store" |
| ---------- | ------------------------------------------------------------ | -------------------------------------------------- |
| `light`    | determiners                                                  | "man has quickly gone to very large store"         |
| `moderate` | `light` + prepositions, conjunctions, auxiliaries, copulas, pronouns | "man quickly gone very large store"                |
| `caveman`  | `moderate` + adverbs, adjectives                                        | "man gone store"                                   |

Nouns, verbs, numbers, proper nouns, negations and subordinators (`if`,
`unless`, `because`) are never removed at any level.

### Savings

| Level      | Tokens        | Saved  |
| ---------- | ------------- | ------ |
| `light`    | 6,353 → 5,930 | -6.7%  |
| `moderate` | 6,353 → 4,916 | -22.6% |
| `caveman`  | 6,353 → 4,609 | -27.5% |

Savings depend on how much of the request is prose, since code, JSON, and
log lines pass through untouched. At `caveman` level:

| Request                         | Prose | Saved  |
| ------------------------------- | ----- | ------ |
| Rambling bug report             | 99%   | -51.6% |
| Dense prose, no code            | 100%  | -47.1% |
| Six-turn debugging conversation | 98%   | -39.0% |
| Bug report with a stack trace   | 70%   | -26.2% |
| Mostly a pasted diff            | 32%   | -12.4% |
| Terse expert question           | 31%   | -5.9%  |

`caveman measure` to test against corpus.

### Performance

Compression runs at about 350k characters a second over the whole request,
protected regions included, or 325k with the token counting on top. A typical
request in this corpus takes between 0.5ms and 12ms.

`caveman measure --performance` to test pipeline latency.

## Running without the CLI

The proxy is an ordinary server, so run it and point a client at it yourself:

```sh
go build -o caveman ./cmd/caveman
CAVEMAN_SERVE=1 ./caveman
```

```sh
ANTHROPIC_BASE_URL=http://localhost:8787 \
ANTHROPIC_CUSTOM_HEADERS="X-Caveman-Compress: caveman" \
ENABLE_TOOL_SEARCH=true \
claude
```

## Headers

| Header               | Values                                            | Default   |
| -------------------- | -------------------------------------------------- | --------- |
| `X-Caveman-Compress` | `off`, `light`, `moderate`, `caveman`              | `off`     |
| `X-Caveman-Scope`    | Comma list of `messages`, `system`, `tool_results` | `messages, system, tool_results` |
| `X-Caveman-Cache`    | `ignore` or `respect`                              | `ignore`  |
| `X-Caveman-Count`    | `on` or `off`                                      | `off`     |

Compression is opt-in on the server, but CLI defaults to `caveman`.

Values are case-insensitive and trimmed. A malformed value returns a 400 naming
the header and accepted values; the request never reaches upstream.
Caveman headers are stripped before forwarding.

When compression runs, the response carries `X-Caveman-Chars-Before`,
`X-Caveman-Chars-After`, `X-Caveman-Ratio` and `X-Caveman-Level`. The token
headers, `X-Caveman-Tokens-Before` and `X-Caveman-Tokens-After`, appear only
when counting is enabled.

### Caching

With `X-Caveman-Cache: ignore` Caveman compresses every node the same way no
matter where breakpoints sit, so a node has one compressed form and the cached
prefix keeps hitting once it settles. `respect` skips nodes before the last
`cache_control` breakpoint, which ties a node's output to where that breakpoint
is: when it advances past a node, that node's text flips back to the original
and the prefix misses. `respect` exists to measure the default (see
[DESIGN.md](docs/DESIGN.md#caching)).

## Environment

| Variable                      | Meaning                                     |
| ----------------------------- | ------------------------------------------- |
| `PORT`                        | Listen port, default `8787`                 |
| `CAVEMAN_ANTHROPIC_BASE_URL`  | Redirect the anthropic adapter              |
| `CAVEMAN_UPSTREAM_BASE_URL`   | Redirect every adapter; provider var wins   |
| `DISABLE_LOGS`                | Silence stdout; `0` or `false` keeps it on  |

## Routes

| Route          | Upstream                                    |
| -------------- | ------------------------------------------- |
| `/v1/messages` | `https://api.anthropic.com`, POST over HTTP |
| `/health`      | none; answers `{"service":"caveman","status":"ok"}` |

The `service` name is how the CLI tells Caveman apart from an unrelated process
holding the port, so it will not start over one or stop it.

## Layout

```
cmd/caveman/     the entry point
internal/
  ir/            provider-neutral representation and its walk
  adapters/      wire format ↔ IR, one directory per provider
  compress/      region protection, word classification, the levels
  tagger/        part-of-speech tagging and its generated lexicon
  policy/        header parsing
  server/        request handler, upstream, SSE passthrough, health
  telemetry/     accounting, response headers, savings log, usage
  cli/           commands, daemon, clients, measure
testdata/golden/ the recorded corpus the tests gate against
```

## Tests

```sh
go test ./...
```

Compression is tested against JSON files in `testdata/golden/` to prevent regressions in the algorithm. If the algorithm improves, tests will fail, in which case goldens should be regenerated:

```sh
go test ./internal/compress/ -update
```
