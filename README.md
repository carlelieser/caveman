# Caveman

An LLM compression proxy. Uses part-of-speech natural language processing to identify and remove unnecessary words, effectively reducing token usage by up to 46%.

See [DESIGN.md](docs/DESIGN.md) for a more detailed overview of the pipeline.

## Quickstart

```sh
curl -fsSL https://raw.githubusercontent.com/carlelieser/caveman/main/install.sh | bash
```

```sh
caveman claude
```

## Commands

| Command                  | Does                                         |
| ------------------------ | -------------------------------------------- |
| `caveman up`             | Start the proxy in the background            |
| `caveman down`           | Stop it, reporting the session savings       |
| `caveman status`         | Say whether it is running, and on which port |
| `caveman <client> [...]` | Start it if needed, then launch a client     |

Logs live in `run/` under the install root (`~/.caveman`).

### Compression level

`-l` (or `--level`) takes `off`, `light`, `moderate`, or `caveman` — the
default, and the most aggressive. Give it to `up` and every client inherits it;
give it to a client and that launch alone uses it.

```sh
caveman up -l moderate    # every client from now on
caveman claude            # moderate, inherited
caveman claude -l light   # this launch only
caveman claude -l off     # uncompressed, for a baseline
```

At `off` the CLI sends no Caveman header, so the request forwards
byte-identical — the same thing the server does for any client that doesn't
ask. Everything after `--` goes to the client untouched, which is how you pass
a flag the CLI would otherwise read as its own:
`caveman claude -- -l debug.log`

### Running without the CLI

The proxy is an ordinary server, so run it and point a client at it yourself:

```sh
npm install
npm start # (or `npm run dev` for development)
```

```sh
ANTHROPIC_BASE_URL=http://localhost:8787 \
ANTHROPIC_CUSTOM_HEADERS="X-Caveman-Compress: caveman" \
ENABLE_TOOL_SEARCH=true \
claude
```

Those three variables are exactly what `caveman claude` sets; the CLI adds
starting the server and filling in the configured port. See
[Headers](#headers) for the rest of what a request can ask for.

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

## Headers

The CLI sets `X-Caveman-Compress` from `-l` and leaves the other two alone.
Send them yourself to reach what the CLI does not expose.

| Header               | Meaning                                            | Default   |
| -------------------- | -------------------------------------------------- | --------- |
| `X-Caveman-Compress` | `off`, `light`, `moderate`, `caveman`              | `off`     |
| `X-Caveman-Scope`    | Comma list of `messages`, `system`, `tool_results` | `messages, system, tool_results` |
| `X-Caveman-Cache`    | `ignore` or `respect`                              | `ignore`  |

Values are case-insensitive and trimmed. A malformed value returns a 400 naming
the header and accepted values; the request never reaches upstream.
Caveman headers are stripped before forwarding.

### Caching

With `X-Caveman-Cache: ignore` Caveman compresses every node the same way no
matter where breakpoints sit, so a node has one compressed form and the cached
prefix keeps hitting once it settles. `respect` skips nodes before the last
`cache_control` breakpoint, which ties a node's output to where that breakpoint
is: when it advances past a node, that node's text flips back to the original
and the prefix misses. `respect` exists to measure the default (see
[DESIGN.md](docs/DESIGN.md#caching)).

## Levels

| Level      | Removes                                                      | "The man has quickly gone to the very large store" |
| ---------- | ------------------------------------------------------------ | -------------------------------------------------- |
| `light`    | determiners                                                  | "man has quickly gone to very large store"         |
| `moderate` | `light` + prepositions, conjunctions, auxiliaries, copulas, pronouns | "man quickly gone very large store"                |
| `caveman`  | `moderate` + adverbs, adjectives                                        | "man gone store"                                   |

Nouns, verbs, numbers, proper nouns, negations and subordinators (`if`,
`unless`, `because`) are never removed at any level.

## Results

| Level      | Tokens        | Saved  |
| ---------- | ------------- | ------ |
| `light`    | 6,287 → 5,880 | -6.5%  |
| `moderate` | 6,287 → 4,935 | -21.5% |
| `caveman`  | 6,287 → 4,442 | -29.3% |

Savings depend on how much of the request is prose, since code, JSON, and
log lines pass through untouched. At `caveman` level:

| Request                         | Prose | Saved  |
| ------------------------------- | ----- | ------ |
| Rambling bug report             | 99%   | -46.0% |
| Dense prose, no code            | 100%  | -39.3% |
| Six-turn debugging conversation | 98%   | -36.9% |
| Bug report with a stack trace   | 70%   | -27.8% |
| Mostly a pasted diff            | 32%   | -12.8% |
| Terse expert question           | 31%   | -7.2%  |

`npm run measure` to test against corpus and per request.

## Telemetry

When compression runs, the response carries `X-Caveman-Tokens-Before`,
`X-Caveman-Tokens-After`, `X-Caveman-Ratio` (the reduction achieved, not the one
the level asked for), and `X-Caveman-Level`.

```
caveman  1,204 → 892 tok  -25.9%  moderate  14 nodes, 9 compressed  71% prose  —  session 3,110 saved
caveman  billed  5,710 in  412 out  4,200 cache read  0 cache write
```

## What is never compressed

Tool definitions, `tool_use.input`, `thinking` and `redacted_thinking` blocks,
images, documents, and any block type the adapter does not recognize.

Inside a text block: fenced and indented code blocks, inline code, URLs, file
paths, JSON and XML/JSX spans, quoted strings, stack trace lines, hex literals,
UUIDs, long digit runs, version strings, markdown table rows, and markdown
structural markers.

## Layout

```
src/
  ir/                   provider-neutral representation and its walk
  adapters/anthropic/   wire format ↔ IR
  compression/          regions, classify, levels, compress, pipeline
  policy/               header parsing
  http/                 Hono server, handler, upstream, SSE passthrough, health
  telemetry/            accounting, response headers, savings log, usage
  config/               .env loading
bin/
  caveman               the CLI entry point
  lib/                  paths, port, level, health, daemon, client
  clients/              one file per client
scripts/
  measure.ts            return token savings per level
```

## Tests

```sh
npm test
```
