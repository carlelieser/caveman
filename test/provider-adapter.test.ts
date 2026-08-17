import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import type { ProviderAdapter, ProviderRequestBody } from '../src/adapters/provider.js';
import type { FakeUpstream } from './fake-upstream.js';
import { startFakeUpstream } from './fake-upstream.js';
import { anthropicAdapter } from '../src/adapters/anthropic/adapter.js';
import { createApp } from '../src/http/server.js';

/**
 * A second provider with a different route, a different wire format, and a
 * different error shape. It exists to prove the HTTP layer holds no knowledge
 * of any one provider.
 */
const fakeProviderAdapter: ProviderAdapter = {
  name: 'fake',
  path: '/v2/chat',
  baseUrl: 'https://api.fake-provider.test',
  toIR(body) {
    const prompt = String(body['prompt'] ?? '');
    return {
      model: String(body['model'] ?? ''),
      maxTokens: Number(body['limit'] ?? 0),
      system: null,
      // No isContentString: this provider has no string/array duality to
      // remember, and the neutral IR must not require one.
      messages: [
        { role: 'user', content: [{ kind: 'text', text: prompt, compressible: true }] },
      ],
      tools: [],
      extensions: {},
      passthrough: {},
    };
  },
  fromIR(request) {
    const first = request.messages[0]?.content[0];
    const prompt = first?.kind === 'text' ? first.text : '';
    return { model: request.model, limit: request.maxTokens, prompt };
  },
  errorEnvelope(message): ProviderRequestBody {
    return { fault: message };
  },
};

const VERBOSE_PROMPT =
  'Could you please go ahead and tell me what the weather is like in the city of ' +
  'San Francisco on this particular day, if that is something you can do?';

function post(path: string, body: unknown, headers: Record<string, string>): Request {
  return new Request(`http://caveman.test${path}`, {
    method: 'POST',
    headers: { 'content-type': 'application/json', ...headers },
    body: JSON.stringify(body),
  });
}

function fakeBody(): Record<string, unknown> {
  return { model: 'fake-1', limit: 256, prompt: VERBOSE_PROMPT };
}

describe('provider adapter seam', () => {
  let upstream: FakeUpstream;

  beforeEach(async () => {
    upstream = await startFakeUpstream();
    process.env['CAVEMAN_UPSTREAM_BASE_URL'] = upstream.baseUrl;
  });

  afterEach(async () => {
    delete process.env['CAVEMAN_UPSTREAM_BASE_URL'];
    await upstream.close();
  });

  it('serves a second provider on its own route with no handler changes', async () => {
    const app = createApp(undefined, [fakeProviderAdapter]);
    const response = await app.fetch(post('/v2/chat', fakeBody(), {}));
    expect(response.status).toBe(200);
    expect(upstream.requests.at(-1)?.url).toBe('/v2/chat');
  });

  it('compresses a second provider through the same pipeline', async () => {
    const app = createApp(undefined, [fakeProviderAdapter]);
    await app.fetch(post('/v2/chat', fakeBody(), { 'X-Caveman-Compress': '0.4' }));
    const forwarded = JSON.parse(upstream.requests.at(-1)?.body ?? '{}') as {
      prompt: string;
    };
    expect(forwarded.prompt.length).toBeLessThan(VERBOSE_PROMPT.length);
  });

  it('reports errors in the provider’s own envelope shape', async () => {
    const app = createApp(undefined, [fakeProviderAdapter]);
    const response = await app.fetch(
      post('/v2/chat', fakeBody(), { 'X-Caveman-Compress': '1.5' }),
    );
    expect(response.status).toBe(400);
    const body = (await response.json()) as { fault?: string; error?: unknown };
    expect(body.fault).toContain('X-Caveman-Compress');
    expect(body.error).toBeUndefined();
  });

  it('serves several providers side by side', async () => {
    const app = createApp(undefined, [anthropicAdapter, fakeProviderAdapter]);
    const anthropic = await app.fetch(
      post(
        '/v1/messages',
        {
          model: 'claude-sonnet-4-5',
          max_tokens: 16,
          messages: [{ role: 'user', content: 'hi' }],
        },
        {},
      ),
    );
    const fake = await app.fetch(post('/v2/chat', fakeBody(), {}));
    expect(anthropic.status).toBe(200);
    expect(fake.status).toBe(200);
    expect(upstream.requests.map((request) => request.url)).toEqual([
      '/v1/messages',
      '/v2/chat',
    ]);
  });

  it('treats an omitted isContentString as the block-array form', () => {
    const request = anthropicAdapter.toIR({
      model: 'claude-sonnet-4-5',
      max_tokens: 16,
      messages: [{ role: 'user', content: [{ type: 'text', text: 'hi' }] }],
    });
    const [message] = request.messages;
    if (message === undefined) throw new Error('expected one message');
    const { isContentString: _omitted, ...withoutFlag } = message;
    const emitted = anthropicAdapter.fromIR({ ...request, messages: [withoutFlag] });
    const messages = emitted['messages'] as { content: unknown }[];
    expect(Array.isArray(messages[0]?.content)).toBe(true);
  });

  it('does not serve a route no registered adapter claims', async () => {
    const app = createApp(undefined, [fakeProviderAdapter]);
    const response = await app.fetch(post('/v1/messages', fakeBody(), {}));
    expect(response.status).toBe(404);
    expect(upstream.requests).toHaveLength(0);
  });

  it('routes each provider to its own upstream host', async () => {
    const second = await startFakeUpstream();
    try {
      process.env['CAVEMAN_ANTHROPIC_BASE_URL'] = upstream.baseUrl;
      process.env['CAVEMAN_FAKE_BASE_URL'] = second.baseUrl;
      delete process.env['CAVEMAN_UPSTREAM_BASE_URL'];

      const app = createApp(undefined, [anthropicAdapter, fakeProviderAdapter]);
      await app.fetch(
        post(
          '/v1/messages',
          {
            model: 'claude-sonnet-4-5',
            max_tokens: 16,
            messages: [{ role: 'user', content: 'hi' }],
          },
          {},
        ),
      );
      await app.fetch(post('/v2/chat', fakeBody(), {}));

      expect(upstream.requests.map((request) => request.url)).toEqual(['/v1/messages']);
      expect(second.requests.map((request) => request.url)).toEqual(['/v2/chat']);
    } finally {
      delete process.env['CAVEMAN_ANTHROPIC_BASE_URL'];
      delete process.env['CAVEMAN_FAKE_BASE_URL'];
      await second.close();
    }
  });

  it('prefers a provider override over the global one', async () => {
    const second = await startFakeUpstream();
    try {
      process.env['CAVEMAN_FAKE_BASE_URL'] = second.baseUrl;
      const app = createApp(undefined, [fakeProviderAdapter]);
      await app.fetch(post('/v2/chat', fakeBody(), {}));

      expect(second.requests).toHaveLength(1);
      expect(upstream.requests).toHaveLength(0);
    } finally {
      delete process.env['CAVEMAN_FAKE_BASE_URL'];
      await second.close();
    }
  });

  it('keeps the anthropic error envelope on the anthropic route', async () => {
    const app = createApp(undefined, [anthropicAdapter]);
    const response = await app.fetch(
      post('/v1/messages', {}, { 'X-Caveman-Compress': '1.5' }),
    );
    const body = (await response.json()) as { error: { type: string } };
    expect(body.error.type).toBe('invalid_request_error');
  });
});
