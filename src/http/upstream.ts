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
};

export async function sendUpstream(request: UpstreamRequest): Promise<Response> {
  const url = `${upstreamBaseUrl(request.adapter)}${request.adapter.path}`;
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

export function passthroughResponse(
  upstream: Response,
  decorate?: ResponseDecorator,
): Response {
  const headers = forwardableResponseHeaders(upstream.headers);
  if (isEventStream(upstream.headers)) {
    headers.set('cache-control', 'no-cache, no-transform');
    headers.set('x-accel-buffering', 'no');
  }
  decorate?.(headers);
  return new Response(upstream.body, { status: upstream.status, headers });
}

function isEventStream(headers: Headers): boolean {
  const contentType = headers.get('content-type');
  return contentType !== null && contentType.includes('text/event-stream');
}
