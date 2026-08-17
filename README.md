# Caveman

A compression proxy for LLM requests. It removes whole classes of function words
from the prose in a request — determiners, prepositions, adverbs — and forwards
the rest upstream unchanged.

Code, paths, JSON, stack traces, tool definitions and thinking blocks are copied
byte for byte. Only prose is touched.

Compression is off unless you ask for it. With no Caveman headers, a request is
forwarded byte-identical to what the client sent.

See [DESIGN.md](docs/DESIGN.md) for why compression removes what it does and why the
cache defaults are what they are.

## Quickstart

```sh
curl -fsSL https://raw.githubusercontent.com/carlelieser/caveman/main/install.sh | bash
```

```sh
caveman claude -l caveman
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

`-l` (or `--level`) takes `off`, `light`, `moderate`, or `caveman`. Give it to
`up` and every client inherits it; give it to a client and that launch alone
uses it. With no level anywhere the default is `off`, so nothing is compressed
until you ask.

```sh
caveman up -l moderate    # every client from now on
caveman claude            # moderate, inherited
caveman claude -l caveman # this launch only
```

At `off` the CLI sends no Caveman header at all, so the request forwards
byte-identical. Pass a client's own flags after `--` if they collide with `-l`.

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

The header takes `light`, `moderate`, or `caveman`. Omit it and the request
forwards byte-identical.

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
```

## Tests

```sh
npm test
```
