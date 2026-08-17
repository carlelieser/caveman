import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import type { FakeUpstream } from './fake-upstream.js';
import { startFakeUpstream } from './fake-upstream.js';
import { createServedApp } from '../src/http/server.js';
import { createCompressionStage } from '../src/http/compression-stage.js';
import { createLogSink } from '../src/telemetry/logging.js';
import { createSavingsReporter } from '../src/telemetry/savings-log.js';
import type { SavingsReporter } from '../src/telemetry/savings-log.js';

const VERBOSE_MESSAGE =
  'Could you please go ahead and tell me what the weather is like in the city of ' +
  'San Francisco on this particular day, if that is something you can do?';

function requestBody(): Record<string, unknown> {
  return {
    model: 'claude-sonnet-4-5',
    max_tokens: 1024,
    messages: [{ role: 'user', content: VERBOSE_MESSAGE }],
  };
}

/** Every message block sits at or before the breakpoint, so none is compressed. */
function fullyCachedBody(): Record<string, unknown> {
  return {
    model: 'claude-sonnet-4-5',
    max_tokens: 1024,
    messages: [
      {
        role: 'user',
        content: [
          { type: 'text', text: VERBOSE_MESSAGE },
          {
            type: 'text',
            text: VERBOSE_MESSAGE,
            cache_control: { type: 'ephemeral' },
          },
        ],
      },
    ],
  };
}

function post(headers: Record<string, string>, body = requestBody()): Request {
  return new Request('http://caveman.test/v1/messages', {
    method: 'POST',
    headers: { 'content-type': 'application/json', ...headers },
    body: JSON.stringify(body),
  });
}

const COMPRESS = { 'X-Caveman-Compress': 'moderate' };

describe('savings logging', () => {
  let upstream: FakeUpstream;
  let lines: string[];
  let app: ReturnType<typeof createServedApp>['app'];
  let reporter: SavingsReporter;

  beforeEach(async () => {
    upstream = await startFakeUpstream();
    process.env['CAVEMAN_UPSTREAM_BASE_URL'] = upstream.baseUrl;
    lines = [];
    const served = createServedApp(
      createSavingsReporter((line) => lines.push(line)),
      createCompressionStage(),
    );
    app = served.app;
    reporter = served.reporter;
  });

  afterEach(async () => {
    delete process.env['CAVEMAN_UPSTREAM_BASE_URL'];
    await upstream.close();
  });

  it('writes one line naming tokens before, after, and the achieved ratio', async () => {
    await app.fetch(post(COMPRESS));
    expect(lines).toHaveLength(1);
    const [before, after] =
      String(lines[0])
        .match(/(\d+) → (\d+) tok/)
        ?.slice(1) ?? [];
    expect(Number(after)).toBeLessThan(Number(before));
    expect(lines[0]).toMatch(/-\d+\.\d%/);
    expect(lines[0]).toContain('moderate');
    expect(lines[0]).toContain('1 node, 1 compressed');
  });

  it('writes nothing when no compression header is present', async () => {
    await app.fetch(post({}));
    expect(lines).toEqual([]);
  });

  it('writes nothing when the level is off', async () => {
    await app.fetch(post({ 'X-Caveman-Compress': 'off' }));
    expect(lines).toEqual([]);
  });

  it('compresses a cached prefix by default and says so', async () => {
    await app.fetch(post(COMPRESS, fullyCachedBody()));
    expect(lines).toHaveLength(1);
    expect(lines[0]).toContain('2 nodes, 2 compressed');
    expect(lines[0]).not.toContain('cached');
  });

  it('names the skipped nodes when the cache mode is respect', async () => {
    await app.fetch(
      post({ ...COMPRESS, 'X-Caveman-Cache': 'respect' }, fullyCachedBody()),
    );
    expect(lines).toHaveLength(1);
    expect(lines[0]).toContain('-0.0%');
    expect(lines[0]).toContain('2 nodes, 2 cached, 0 compressed');
  });

  it('accumulates the session total across requests', async () => {
    await app.fetch(post(COMPRESS));
    await app.fetch(post(COMPRESS));
    const saved = lines.map((line) => Number(/session ([\d,]+) saved/.exec(line)?.[1]));
    expect(saved[0]).toBeGreaterThan(0);
    expect(saved[1]).toBe(Number(saved[0]) * 2);
  });

  it('writes nothing for a request rejected before it is forwarded', async () => {
    await app.fetch(post({ 'X-Caveman-Compress': '0.5' }));
    expect(lines).toEqual([]);
  });

  it('has no summary until a request has been compressed', () => {
    expect(reporter.summary()).toBeNull();
  });

  it('names the total and the request count in the summary', async () => {
    await app.fetch(post(COMPRESS));
    await app.fetch(post(COMPRESS));
    expect(reporter.summary()).toContain('2 requests');
    expect(reporter.summary()).toMatch(/[\d,]+ tok saved/);
  });

  it('says "1 request" when only one was compressed', async () => {
    await app.fetch(post(COMPRESS));
    expect(reporter.summary()).toContain('1 request');
  });
});

describe('the DISABLE_LOGS switch', () => {
  let upstream: FakeUpstream;
  let written: string[];

  beforeEach(async () => {
    upstream = await startFakeUpstream();
    process.env['CAVEMAN_UPSTREAM_BASE_URL'] = upstream.baseUrl;
    written = [];
  });

  afterEach(async () => {
    delete process.env['CAVEMAN_UPSTREAM_BASE_URL'];
    process.env['DISABLE_LOGS'] = '1';
    await upstream.close();
  });

  async function compressOnce(): Promise<void> {
    const sink = createLogSink();
    const served = createServedApp(
      createSavingsReporter((line) => sink(line)),
      createCompressionStage(),
    );
    const original = process.stdout.write.bind(process.stdout);
    process.stdout.write = (chunk: unknown): boolean => {
      written.push(String(chunk));
      return true;
    };
    try {
      await served.app.fetch(post(COMPRESS));
    } finally {
      process.stdout.write = original;
    }
  }

  it('writes to stdout when the variable is absent', async () => {
    delete process.env['DISABLE_LOGS'];
    await compressOnce();
    expect(written).toHaveLength(1);
    expect(written[0]).toContain('tok');
  });

  it('writes nothing when the variable is set', async () => {
    process.env['DISABLE_LOGS'] = '1';
    await compressOnce();
    expect(written).toEqual([]);
  });

  it('treats "0" as not disabled', async () => {
    process.env['DISABLE_LOGS'] = '0';
    await compressOnce();
    expect(written).toHaveLength(1);
  });
});
