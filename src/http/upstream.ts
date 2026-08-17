import type { ProviderAdapter } from '../adapters/provider.js';
import { CAVEMAN_HEADER_NAMES } from '../policy/headers.js';

const GLOBAL_OVERRIDE_VARIABLE = 'CAVEMAN_UPSTREAM_BASE_URL';

/**
 * Headers that describe the client's connection to Caveman rather than the
 * request itself. Forwarding them breaks the upstream call: a stale
 * `content-length` disagrees with the re-serialized body, `host` points at
 * Caveman, and hop-by-hop headers belong to the finished connection.
 */
const CONNECTION_HEADER_NAMES: readonly string[] = [
  'host',
  'content-length',
  'connection',
  'keep-alive',
  'proxy-authorization',
  'proxy-connection',
  'te',
  'trailer',
  'transfer-encoding',
  'upgrade',
  'expect',
];

/**
 * Decoding is left to undici so the SSE stream arrives as plain text chunks
 * rather than a compressed frame that would batch tokens.
 */
const NEGOTIATION_HEADER_NAMES: readonly string[] = ['accept-encoding'];

const STRIPPED_REQUEST_HEADER_NAMES: ReadonlySet<string> = new Set([
  ...CONNECTION_HEADER_NAMES,
  ...NEGOTIATION_HEADER_NAMES,
  ...CAVEMAN_HEADER_NAMES.map((name) => name.toLowerCase()),
]);

/**
 * Response headers describing upstream's transfer encoding, which no longer
 * apply once the body has been decoded and re-framed to the client.
 */
const STRIPPED_RESPONSE_HEADER_NAMES: ReadonlySet<string> = new Set([
  'content-encoding',
  'content-length',
  'connection',
  'keep-alive',
  'transfer-encoding',
]);

/** Per-provider override, e.g. `CAVEMAN_ANTHROPIC_BASE_URL`. */
function overrideVariableName(providerName: string): string {
  return `CAVEMAN_${providerName.toUpperCase().replace(/[^A-Z0-9]+/g, '_')}_BASE_URL`;
}

function readOverride(name: string): string | null {
  const configured = process.env[name];
  if (configured === undefined || configured.trim() === '') return null;
  return configured.trim().replace(/\/+$/, '');
}

/**
 * The adapter's own base URL, unless an env override redirects it. The
 * provider-wide override applies to every adapter and exists so a test or a
 * local run can point all traffic at one host.
 */
export function upstreamBaseUrl(adapter: ProviderAdapter): string {
  const specific = readOverride(overrideVariableName(adapter.name));
  if (specific !== null) return specific;
  return readOverride(GLOBAL_OVERRIDE_VARIABLE) ?? adapter.baseUrl;
}

/**
 * Copies the client's headers verbatim except those that would misdescribe the
 * new request. Credentials pass through untouched and are never read.
 */
export function forwardableRequestHeaders(incoming: Headers): Headers {
  const outgoing = new Headers();
  incoming.forEach((value, name) => {
    if (STRIPPED_REQUEST_HEADER_NAMES.has(name.toLowerCase())) return;
    outgoing.append(name, value);
  });
  return outgoing;
}

export function forwardableResponseHeaders(incoming: Headers): Headers {
  const outgoing = new Headers();
  incoming.forEach((value, name) => {
    if (STRIPPED_RESPONSE_HEADER_NAMES.has(name.toLowerCase())) return;
    outgoing.append(name, value);
  });
  return outgoing;
}

export type UpstreamRequest = {
  adapter: ProviderAdapter;
  headers: Headers;
  body: string;
  /** The incoming query string, including its leading `?`, when there was one. */
  search?: string;
  signal?: AbortSignal;
};

export async function sendUpstream(request: UpstreamRequest): Promise<Response> {
  const url = `${upstreamBaseUrl(request.adapter)}${request.adapter.path}${request.search ?? ''}`;
  try {
    return await fetch(url, {
      method: 'POST',
      headers: request.headers,
      body: request.body,
    });
  } catch (cause) {
    throw new Error(`upstream request to ${url} failed`, { cause });
  }
}

/**
 * Rebuilds the client-facing response around the upstream body stream. The
 * stream is handed over unread so the first SSE chunk reaches the client as
 * soon as upstream emits it.
 */
export type ResponseDecorator = (headers: Headers) => void;

/**
 * Called with each decoded chunk as it passes, and once at end of stream. It
 * observes bytes that have already been forwarded, so it can neither delay nor
 * alter them.
 */
export type BodyObserver = {
  push(chunk: string): void;
  finish(): void;
};

/**
 * Copies the body through while handing each chunk to the observer. Every chunk
 * is enqueued before it is observed, so watching costs the stream nothing: a
 * slow or throwing observer cannot hold a chunk back from the client.
 */
function observeBody(
  body: ReadableStream<Uint8Array>,
  observer: BodyObserver,
): ReadableStream<Uint8Array> {
  const decoder = new TextDecoder();
  return body.pipeThrough(
    new TransformStream<Uint8Array, Uint8Array>({
      transform(chunk, controller) {
        controller.enqueue(chunk);
        try {
          observer.push(decoder.decode(chunk, { stream: true }));
        } catch {
          // Telemetry never breaks a response body.
        }
      },
      flush() {
        try {
          observer.finish();
        } catch {
          // As above: the body has already been delivered in full.
        }
      },
    }),
  );
}

export function passthroughResponse(
  upstream: Response,
  decorate?: ResponseDecorator,
  observer?: BodyObserver,
): Response {
  const headers = forwardableResponseHeaders(upstream.headers);
  if (isEventStream(upstream.headers)) {
    headers.set('cache-control', 'no-cache, no-transform');
    headers.set('x-accel-buffering', 'no');
  }
  decorate?.(headers);
  const body =
    observer !== undefined && upstream.body !== null
      ? observeBody(upstream.body, observer)
      : upstream.body;
  return new Response(body, { status: upstream.status, headers });
}

function isEventStream(headers: Headers): boolean {
  const contentType = headers.get('content-type');
  return contentType !== null && contentType.includes('text/event-stream');
}
