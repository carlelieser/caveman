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
caveman claude
```

That starts the proxy if it is not already up and launches `claude` against it.
Arguments pass through, so `caveman claude --resume` works.

Run `install.sh` from inside a clone and it links that clone, so your edits are
what runs.

## Commands

| Command                  | Does                                                  |
| ------------------------ | ----------------------------------------------------- |
| `caveman up`             | Start the proxy in the background                     |
| `caveman down`           | Stop it, reporting the session savings                |
| `caveman status`         | Say whether it is running, and on which port          |
| `caveman <client> [...]` | Start it if needed, then launch a client              |

Logs and the pid file live in `run/` under the install root. A client is a file
in `bin/clients/`, so adding one is adding a file.

### Running without the CLI

The proxy is an ordinary server, and the CLI only sets these for you:

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
  lib/                  paths, port, health, daemon, client
  clients/              one file per client
```

## Tests

```sh
npm test
```
