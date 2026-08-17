import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import type { FakeUpstream } from './fake-upstream.js';
import { startFakeUpstream } from './fake-upstream.js';
import { createApp } from '../src/http/server.js';

const VERBOSE_SYSTEM =
  'You are a very helpful assistant that is able to answer all of the questions ' +
  'that the user might possibly want to ask you about the current weather.';

const VERBOSE_MESSAGE =
  'Could you please go ahead and tell me what the weather is like in the city of ' +
  'San Francisco on this particular day, if that is something you can do?';

function requestBody(): Record<string, unknown> {
  return {
    model: 'claude-sonnet-4-5',
    max_tokens: 1024,
    system: VERBOSE_SYSTEM,
    messages: [{ role: 'user', content: VERBOSE_MESSAGE }],
  };
}

function post(headers: Record<string, string>): Request {
  return new Request('http://caveman.test/v1/messages', {
    method: 'POST',
    headers: { 'content-type': 'application/json', ...headers },
    body: JSON.stringify(requestBody()),
  });
}

function forwardedBody(upstream: FakeUpstream): Record<string, unknown> {
  const recorded = upstream.requests.at(-1);
  if (recorded === undefined) throw new Error('no request reached the fake upstream');
  return JSON.parse(recorded.body) as Record<string, unknown>;
}

describe('compression over HTTP', () => {
  let upstream: FakeUpstream;
  let app: ReturnType<typeof createApp>;

  beforeEach(async () => {
    upstream = await startFakeUpstream();
    process.env['CAVEMAN_UPSTREAM_BASE_URL'] = upstream.baseUrl;
    app = createApp();
  });

  afterEach(async () => {
    delete process.env['CAVEMAN_UPSTREAM_BASE_URL'];
    await upstream.close();
  });

  it('forwards the body untouched when no compression header is present', async () => {
    await app.fetch(post({}));
    expect(forwardedBody(upstream)).toEqual(requestBody());
  });

  it('forwards the body untouched when the level is off', async () => {
    await app.fetch(post({ 'X-Caveman-Compress': 'off' }));
    expect(forwardedBody(upstream)).toEqual(requestBody());
  });

  it('shortens message text when compression is on', async () => {
    await app.fetch(post({ 'X-Caveman-Compress': 'moderate' }));
    const forwarded = forwardedBody(upstream);
    const messages = forwarded['messages'] as { content: string }[];
    expect(messages[0]?.content.length).toBeLessThan(VERBOSE_MESSAGE.length);
  });

  it('compresses the system prompt under the default scope', async () => {
    await app.fetch(post({ 'X-Caveman-Compress': 'moderate' }));
    const forwarded = forwardedBody(upstream);
    expect(String(forwarded['system']).length).toBeLessThan(VERBOSE_SYSTEM.length);
  });

  it('leaves the system prompt alone when the scope names only messages', async () => {
    await app.fetch(
      post({ 'X-Caveman-Compress': 'moderate', 'X-Caveman-Scope': 'messages' }),
    );
    expect(forwardedBody(upstream)['system']).toBe(VERBOSE_SYSTEM);
  });

  it('compresses the system prompt when the scope includes it', async () => {
    await app.fetch(
      post({ 'X-Caveman-Compress': 'moderate', 'X-Caveman-Scope': 'messages,system' }),
    );
    const forwarded = forwardedBody(upstream);
    expect(String(forwarded['system']).length).toBeLessThan(VERBOSE_SYSTEM.length);
  });

  it('reports accounting headers when compression ran', async () => {
    const response = await app.fetch(post({ 'X-Caveman-Compress': 'moderate' }));
    const before = Number(response.headers.get('X-Caveman-Tokens-Before'));
    const after = Number(response.headers.get('X-Caveman-Tokens-After'));
    expect(after).toBeLessThan(before);
    expect(Number(response.headers.get('X-Caveman-Ratio'))).toBeGreaterThan(0);
    expect(response.headers.get('X-Caveman-Level')).toBe('moderate');
  });

  it('omits accounting headers when compression is off', async () => {
    const response = await app.fetch(post({}));
    expect(response.headers.get('X-Caveman-Ratio')).toBeNull();
    expect(response.headers.get('X-Caveman-Tokens-Before')).toBeNull();
  });

  it('attaches accounting headers to an upstream error so it stays attributable', async () => {
    upstream.reply((_request, response) => {
      response.writeHead(400, { 'content-type': 'application/json' });
      response.end(JSON.stringify({ type: 'error' }));
    });
    const response = await app.fetch(post({ 'X-Caveman-Compress': 'moderate' }));
    expect(response.status).toBe(400);
    expect(response.headers.get('X-Caveman-Ratio')).not.toBeNull();
  });

  it('rejects a fractional compress value with 400 naming the header', async () => {
    const response = await app.fetch(post({ 'X-Caveman-Compress': '0.5' }));
    expect(response.status).toBe(400);
    const body = (await response.json()) as { error: { message: string } };
    expect(body.error.message).toContain('X-Caveman-Compress');
    expect(upstream.requests).toHaveLength(0);
  });

  it('rejects an unknown level with 400 naming the header', async () => {
    const response = await app.fetch(post({ 'X-Caveman-Compress': 'aggressive' }));
    expect(response.status).toBe(400);
    const body = (await response.json()) as { error: { message: string } };
    expect(body.error.message).toContain('X-Caveman-Compress');
    expect(upstream.requests).toHaveLength(0);
  });

  it('never forwards Caveman control headers upstream', async () => {
    await app.fetch(
      post({ 'X-Caveman-Compress': 'moderate', 'X-Caveman-Scope': 'messages' }),
    );
    const recorded = upstream.requests.at(-1);
    expect(recorded?.headers['x-caveman-compress']).toBeUndefined();
    expect(recorded?.headers['x-caveman-scope']).toBeUndefined();
  });

  it('sends a byte-identical body when the same request is compressed twice', async () => {
    await app.fetch(post({ 'X-Caveman-Compress': 'moderate' }));
    await app.fetch(post({ 'X-Caveman-Compress': 'moderate' }));
    const [first, second] = upstream.requests;
    expect(first?.body).toBe(second?.body);
  });
});
