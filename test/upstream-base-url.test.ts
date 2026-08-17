import { afterEach, describe, expect, it } from 'vitest';
import type { ProviderAdapter } from '../src/adapters/provider.js';
import { anthropicAdapter } from '../src/adapters/anthropic/adapter.js';
import { upstreamBaseUrl } from '../src/http/upstream.js';

const VARIABLES = [
  'CAVEMAN_UPSTREAM_BASE_URL',
  'CAVEMAN_ANTHROPIC_BASE_URL',
  'CAVEMAN_FAKE_BASE_URL',
] as const;

const fakeAdapter: ProviderAdapter = {
  name: 'fake',
  path: '/v2/chat',
  baseUrl: 'https://api.fake-provider.test',
  toIR: () => {
    throw new Error('unused');
  },
  fromIR: () => {
    throw new Error('unused');
  },
  errorEnvelope: (message) => ({ fault: message }),
};

afterEach(() => {
  for (const variable of VARIABLES) delete process.env[variable];
});

describe('upstream base url resolution', () => {
  it('uses the adapter’s own base url when nothing overrides it', () => {
    expect(upstreamBaseUrl(anthropicAdapter)).toBe('https://api.anthropic.com');
    expect(upstreamBaseUrl(fakeAdapter)).toBe('https://api.fake-provider.test');
  });

  it('gives each adapter a different host by default', () => {
    expect(upstreamBaseUrl(anthropicAdapter)).not.toBe(upstreamBaseUrl(fakeAdapter));
  });

  it('applies the global override to every adapter', () => {
    process.env['CAVEMAN_UPSTREAM_BASE_URL'] = 'http://localhost:9999';
    expect(upstreamBaseUrl(anthropicAdapter)).toBe('http://localhost:9999');
    expect(upstreamBaseUrl(fakeAdapter)).toBe('http://localhost:9999');
  });

  it('prefers the provider override over the global one', () => {
    process.env['CAVEMAN_UPSTREAM_BASE_URL'] = 'http://localhost:9999';
    process.env['CAVEMAN_FAKE_BASE_URL'] = 'http://localhost:8888';
    expect(upstreamBaseUrl(fakeAdapter)).toBe('http://localhost:8888');
    expect(upstreamBaseUrl(anthropicAdapter)).toBe('http://localhost:9999');
  });

  it('derives the override name from the provider name', () => {
    process.env['CAVEMAN_ANTHROPIC_BASE_URL'] = 'http://localhost:7777';
    expect(upstreamBaseUrl(anthropicAdapter)).toBe('http://localhost:7777');
  });

  it('trims trailing slashes so the path is not doubled', () => {
    process.env['CAVEMAN_FAKE_BASE_URL'] = 'http://localhost:8888///';
    expect(upstreamBaseUrl(fakeAdapter)).toBe('http://localhost:8888');
  });

  it('ignores an empty override rather than forwarding to a bare path', () => {
    process.env['CAVEMAN_FAKE_BASE_URL'] = '   ';
    expect(upstreamBaseUrl(fakeAdapter)).toBe('https://api.fake-provider.test');
  });
});
