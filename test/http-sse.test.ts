import { serve } from '@hono/node-server';
import type { ServerType } from '@hono/node-server';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import type { AddressInfo } from 'node:net';
import type { FakeUpstream } from './fake-upstream.js';
import { startFakeUpstream, writeDelayedEvents } from './fake-upstream.js';
import { createApp } from '../src/http/server.js';

const EVENT_DELAY_MS = 120;

const SSE_EVENTS = [
  'event: message_start\ndata: {"type":"message_start"}\n\n',
  'event: content_block_delta\ndata: {"type":"content_block_delta","index":0}\n\n',
  'event: message_stop\ndata: {"type":"message_stop"}\n\n',
];

const STREAM_BODY = {
  model: 'claude-sonnet-4-5',
  max_tokens: 64,
  messages: [{ role: 'user', content: 'stream please' }],
  stream: true,
};

type ChunkArrival = { text: string; elapsedMs: number };

let upstream: FakeUpstream;
let proxy: ServerType;
let proxyUrl: string;

async function startProxy(): Promise<void> {
  proxy = serve({ fetch: createApp().fetch, port: 0, hostname: '127.0.0.1' });
  await new Promise<void>((resolve) => proxy.once('listening', resolve));
  const address = proxy.address() as AddressInfo;
  proxyUrl = `http://127.0.0.1:${address.port}/v1/messages`;
}

async function readWithTimestamps(response: Response): Promise<ChunkArrival[]> {
  const body = response.body;
  if (body === null) throw new Error('reading SSE response failed: body was null');
  const reader = body.getReader();
  const decoder = new TextDecoder();
  const startedAt = Date.now();
  const arrivals: ChunkArrival[] = [];
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    arrivals.push({
      text: decoder.decode(value, { stream: true }),
      elapsedMs: Date.now() - startedAt,
    });
  }
  return arrivals;
}

beforeEach(async () => {
  upstream = await startFakeUpstream();
  process.env['CAVEMAN_UPSTREAM_BASE_URL'] = upstream.baseUrl;
  upstream.reply((_request, response) =>
    writeDelayedEvents(response, SSE_EVENTS, EVENT_DELAY_MS),
  );
  await startProxy();
});

afterEach(async () => {
  delete process.env['CAVEMAN_UPSTREAM_BASE_URL'];
  await new Promise<void>((resolve) => proxy.close(() => resolve()));
  await upstream.close();
});

describe('SSE passthrough', () => {
  it('delivers chunks incrementally rather than as one buffered body', async () => {
    const response = await fetch(proxyUrl, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(STREAM_BODY),
    });
    const arrivals = await readWithTimestamps(response);

    expect(arrivals.length).toBeGreaterThan(1);

    // Upstream holds each event back by one delay, so a proxy that buffered the
    // body would deliver every chunk at once at the end of the stream. Arrivals
    // spread across at least one delay can only happen if chunks were piped.
    const firstArrival = arrivals[0]!;
    const lastArrival = arrivals[arrivals.length - 1]!;
    const spreadMs = lastArrival.elapsedMs - firstArrival.elapsedMs;
    expect(spreadMs).toBeGreaterThan(EVENT_DELAY_MS);
    expect(firstArrival.elapsedMs).toBeLessThan(spreadMs);
  });

  it('preserves the text/event-stream content type', async () => {
    const response = await fetch(proxyUrl, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(STREAM_BODY),
    });

    expect(response.headers.get('content-type')).toContain('text/event-stream');
    expect(response.headers.get('x-accel-buffering')).toBe('no');
    await response.body?.cancel();
  });

  it('delivers every event in order with content intact', async () => {
    const response = await fetch(proxyUrl, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(STREAM_BODY),
    });
    const arrivals = await readWithTimestamps(response);

    expect(arrivals.map((arrival) => arrival.text).join('')).toBe(SSE_EVENTS.join(''));
  });

  it('forwards the stream request body transparently', async () => {
    const response = await fetch(proxyUrl, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(STREAM_BODY),
    });
    await response.body?.cancel();

    expect(JSON.parse(upstream.requests[0]!.body)).toEqual(STREAM_BODY);
  });
});
