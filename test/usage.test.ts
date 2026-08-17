import { describe, expect, it } from 'vitest';
import {
  createEventParser,
  createUsageObserver,
  emptyUsage,
  hasUsage,
} from '../src/telemetry/usage.js';

const MESSAGE_START = {
  type: 'message_start',
  message: {
    type: 'message',
    usage: {
      input_tokens: 5710,
      output_tokens: 1,
      cache_read_input_tokens: 4200,
      cache_creation_input_tokens: 300,
    },
  },
};

const MESSAGE_DELTA = {
  type: 'message_delta',
  usage: { output_tokens: 412 },
};

function sseEvent(payload: unknown, name: string): string {
  return `event: ${name}\ndata: ${JSON.stringify(payload)}\n\n`;
}

function observe(chunks: readonly string[]) {
  const observer = createUsageObserver();
  for (const chunk of chunks) observer.push(chunk);
  return observer.current();
}

describe('usage from a streamed response', () => {
  it('reads the counts message_start carries', () => {
    const usage = observe([sseEvent(MESSAGE_START, 'message_start')]);
    expect(usage.inputTokens).toBe(5710);
    expect(usage.cacheReadTokens).toBe(4200);
    expect(usage.cacheCreationTokens).toBe(300);
  });

  it('takes the final output count from message_delta', () => {
    const usage = observe([
      sseEvent(MESSAGE_START, 'message_start'),
      sseEvent(MESSAGE_DELTA, 'message_delta'),
    ]);
    expect(usage.outputTokens).toBe(412);
  });

  it('keeps the input counts message_delta does not repeat', () => {
    const usage = observe([
      sseEvent(MESSAGE_START, 'message_start'),
      sseEvent(MESSAGE_DELTA, 'message_delta'),
    ]);
    expect(usage.inputTokens).toBe(5710);
    expect(usage.cacheReadTokens).toBe(4200);
  });

  it('reads an event split across chunk boundaries', () => {
    const whole = sseEvent(MESSAGE_START, 'message_start');
    for (const cut of [1, 7, 20, whole.length - 3]) {
      const usage = observe([whole.slice(0, cut), whole.slice(cut)]);
      expect(usage.inputTokens).toBe(5710);
    }
  });

  it('reads an event split one character at a time', () => {
    const whole = sseEvent(MESSAGE_START, 'message_start');
    const usage = observe([...whole]);
    expect(usage.cacheReadTokens).toBe(4200);
  });

  it('ignores events that carry no usage', () => {
    const usage = observe([
      sseEvent({ type: 'content_block_delta', index: 0 }, 'content_block_delta'),
      sseEvent({ type: 'ping' }, 'ping'),
    ]);
    expect(hasUsage(usage)).toBe(false);
  });

  it('survives a data line that is not JSON', () => {
    const usage = observe(['data: not json at all\n\n', sseEvent(MESSAGE_START, 'x')]);
    expect(usage.inputTokens).toBe(5710);
  });
});

describe('usage from a non-streamed response', () => {
  it('reads the counts from a whole JSON body', () => {
    const body = {
      type: 'message',
      usage: {
        input_tokens: 120,
        output_tokens: 40,
        cache_read_input_tokens: 0,
        cache_creation_input_tokens: 90,
      },
    };
    const usage = observe([JSON.stringify(body)]);
    expect(usage.inputTokens).toBe(120);
    expect(usage.outputTokens).toBe(40);
    expect(usage.cacheReadTokens).toBe(0);
    expect(usage.cacheCreationTokens).toBe(90);
  });

  it('reads a body delivered in several chunks', () => {
    const body = JSON.stringify({ usage: { input_tokens: 7 } });
    const middle = Math.floor(body.length / 2);
    const usage = observe([body.slice(0, middle), body.slice(middle)]);
    expect(usage.inputTokens).toBe(7);
  });

  it('reports nothing for a body that is not JSON', () => {
    expect(hasUsage(observe(['<html>error</html>']))).toBe(false);
  });

  it('reports nothing for an empty body', () => {
    expect(hasUsage(observe([]))).toBe(false);
  });

  it('distinguishes a zero count from an absent one', () => {
    const usage = observe([JSON.stringify({ usage: { cache_read_input_tokens: 0 } })]);
    expect(usage.cacheReadTokens).toBe(0);
    expect(usage.inputTokens).toBeNull();
  });
});

describe('emptyUsage', () => {
  it('reports no usage', () => {
    expect(hasUsage(emptyUsage())).toBe(false);
  });
});

describe('event parser', () => {
  it('holds back a line until its newline arrives', () => {
    const parse = createEventParser();
    expect(parse('data: {"a":1}')).toEqual([]);
    expect(parse('\n')).toEqual(['{"a":1}']);
  });

  it('returns several payloads from one chunk', () => {
    const parse = createEventParser();
    expect(parse('data: 1\ndata: 2\n')).toEqual(['1', '2']);
  });

  it('skips lines that are not data lines', () => {
    const parse = createEventParser();
    expect(parse('event: message_start\n: comment\n\n')).toEqual([]);
  });
});
