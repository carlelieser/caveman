import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import type { FakeUpstream } from './fake-upstream.js';
import { startFakeUpstream } from './fake-upstream.js';
import { createApp } from '../src/http/server.js';

const app = createApp();

let upstream: FakeUpstream;

const SAMPLE_BODY = {
  model: 'claude-sonnet-4-5',
  max_tokens: 1024,
  system: 'You are terse.',
  messages: [
    { role: 'user', content: 'hello' },
    {
      role: 'assistant',
      content: [
        { type: 'text', text: 'hi there' },
        { type: 'tool_use', id: 'tu_1', name: 'search', input: { query: 'x' } },
      ],
    },
    {
      role: 'user',
      content: [
        { type: 'tool_result', tool_use_id: 'tu_1', content: 'result text' },
        { type: 'text', text: 'and then?', cache_control: { type: 'ephemeral' } },
      ],
    },
  ],
  tools: [{ name: 'search', description: 'searches', input_schema: { type: 'object' } }],
  temperature: 0.5,
  metadata: { user_id: 'abc' },
  future_unknown_field: { nested: [1, 2, 3] },
};

function proxyRequest(headers: Record<string, string>, body: unknown): Request {
  return new Request('http://caveman.test/v1/messages', {
    method: 'POST',
    headers: { 'content-type': 'application/json', ...headers },
    body: JSON.stringify(body),
  });
}

beforeEach(async () => {
  upstream = await startFakeUpstream();
  process.env['CAVEMAN_UPSTREAM_BASE_URL'] = upstream.baseUrl;
});

afterEach(async () => {
  delete process.env['CAVEMAN_UPSTREAM_BASE_URL'];
  await upstream.close();
});

describe('transparency', () => {
  it('forwards a body deep-equal to what the client sent when no Caveman headers are set', async () => {
    await app.fetch(proxyRequest({ 'x-api-key': 'sk-test' }, SAMPLE_BODY));

    expect(upstream.requests).toHaveLength(1);
    const received = JSON.parse(upstream.requests[0]!.body) as unknown;
    expect(received).toEqual(SAMPLE_BODY);
  });

  it('forwards to the /v1/messages path with POST', async () => {
    await app.fetch(proxyRequest({}, SAMPLE_BODY));

    expect(upstream.requests[0]!.url).toBe('/v1/messages');
    expect(upstream.requests[0]!.method).toBe('POST');
  });

  it('forwards a body deep-equal when Caveman headers request no compression', async () => {
    const headers = {
      'X-Caveman-Compress': '0',
      'X-Caveman-Scope': 'messages,system',
      'X-Caveman-Scorer': 'heuristic',
    };
    await app.fetch(proxyRequest(headers, SAMPLE_BODY));

    const received = JSON.parse(upstream.requests[0]!.body) as unknown;
    expect(received).toEqual(SAMPLE_BODY);
  });
});

describe('header handling', () => {
  it('forwards auth and version headers verbatim', async () => {
    const headers = {
      'x-api-key': 'sk-ant-secret',
      authorization: 'Bearer token-value',
      'anthropic-version': '2023-06-01',
      'anthropic-beta': 'prompt-caching-2024-07-31',
    };
    await app.fetch(proxyRequest(headers, SAMPLE_BODY));

    const received = upstream.requests[0]!.headers;
    expect(received['x-api-key']).toBe('sk-ant-secret');
    expect(received['authorization']).toBe('Bearer token-value');
    expect(received['anthropic-version']).toBe('2023-06-01');
    expect(received['anthropic-beta']).toBe('prompt-caching-2024-07-31');
  });

  it('strips every X-Caveman-* header before forwarding', async () => {
    const headers = {
      'x-api-key': 'sk-test',
      'X-Caveman-Compress': '0.3',
      'X-Caveman-Scope': 'messages',
      'X-Caveman-Scorer': 'heuristic',
    };
    await app.fetch(proxyRequest(headers, SAMPLE_BODY));

    const received = upstream.requests[0]!.headers;
    const cavemanNames = Object.keys(received).filter((name) =>
      name.toLowerCase().startsWith('x-caveman-'),
    );
    expect(cavemanNames).toEqual([]);
  });

  it('reaches upstream despite a client content-length that the forwarded body would contradict', async () => {
    const headers = {
      'x-api-key': 'sk-test',
      'content-length': '999999',
      host: 'localhost:8787',
    };
    const response = await app.fetch(proxyRequest(headers, SAMPLE_BODY));

    expect(response.status).toBe(200);
    expect(upstream.requests).toHaveLength(1);
    const received = upstream.requests[0]!.headers;
    expect(received['host']).toBe(upstream.baseUrl.replace('http://', ''));
    expect(Number(received['content-length'])).toBe(
      Buffer.byteLength(upstream.requests[0]!.body),
    );
  });

  it('does not forward accept-encoding, so upstream never frames a compressed stream', async () => {
    const headers = { 'x-api-key': 'sk-test', 'accept-encoding': 'gzip, br' };
    await app.fetch(proxyRequest(headers, SAMPLE_BODY));

    const forwardedEncoding = upstream.requests[0]!.headers['accept-encoding'];
    expect(forwardedEncoding).not.toContain('br');
  });
});

describe('policy failures', () => {
  it('returns 400 naming X-Caveman-Compress and never calls upstream', async () => {
    const response = await app.fetch(
      proxyRequest({ 'X-Caveman-Compress': 'not-a-number' }, SAMPLE_BODY),
    );

    expect(response.status).toBe(400);
    expect(upstream.requests).toHaveLength(0);
    const payload = (await response.json()) as {
      type: string;
      error: { type: string; message: string };
    };
    expect(payload.type).toBe('error');
    expect(payload.error.type).toBe('invalid_request_error');
    expect(payload.error.message).toContain('X-Caveman-Compress');
  });

  it('returns 400 naming X-Caveman-Scope for an unknown scope member', async () => {
    const response = await app.fetch(
      proxyRequest({ 'X-Caveman-Scope': 'messages,nonsense' }, SAMPLE_BODY),
    );

    expect(response.status).toBe(400);
    expect(upstream.requests).toHaveLength(0);
    const payload = (await response.json()) as { error: { message: string } };
    expect(payload.error.message).toContain('X-Caveman-Scope');
  });

  it('returns 400 for a body that is not valid JSON without calling upstream', async () => {
    const response = await app.fetch(
      new Request('http://caveman.test/v1/messages', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: '{ not json',
      }),
    );

    expect(response.status).toBe(400);
    expect(upstream.requests).toHaveLength(0);
    const payload = (await response.json()) as { error: { type: string } };
    expect(payload.error.type).toBe('invalid_request_error');
  });
});

describe('upstream error passthrough', () => {
  it('forwards a 429 with status and body intact', async () => {
    const errorBody = {
      type: 'error',
      error: { type: 'rate_limit_error', message: 'slow down' },
    };
    upstream.reply((_request, response) => {
      response.writeHead(429, { 'content-type': 'application/json' });
      response.end(JSON.stringify(errorBody));
    });

    const response = await app.fetch(proxyRequest({}, SAMPLE_BODY));

    expect(response.status).toBe(429);
    expect(await response.json()).toEqual(errorBody);
  });

  it('forwards a 500 with status and body intact', async () => {
    const errorBody = {
      type: 'error',
      error: { type: 'api_error', message: 'upstream exploded' },
    };
    upstream.reply((_request, response) => {
      response.writeHead(500, { 'content-type': 'application/json' });
      response.end(JSON.stringify(errorBody));
    });

    const response = await app.fetch(proxyRequest({}, SAMPLE_BODY));

    expect(response.status).toBe(500);
    expect(await response.json()).toEqual(errorBody);
  });

  it('forwards a non-error 200 body and content-type unchanged', async () => {
    const okBody = {
      type: 'message',
      id: 'msg_1',
      content: [{ type: 'text', text: 'ok' }],
    };
    upstream.reply((_request, response) => {
      response.writeHead(200, { 'content-type': 'application/json' });
      response.end(JSON.stringify(okBody));
    });

    const response = await app.fetch(proxyRequest({}, SAMPLE_BODY));

    expect(response.status).toBe(200);
    expect(response.headers.get('content-type')).toContain('application/json');
    expect(await response.json()).toEqual(okBody);
  });
});
