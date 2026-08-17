import { describe, expect, it } from 'vitest';
import { toIR } from '../src/adapters/anthropic/to-ir.js';
import { fromIR } from '../src/adapters/anthropic/from-ir.js';
import { REQUEST_FIXTURES } from './fixtures/requests.js';

function roundTrip(body: Record<string, unknown>): Record<string, unknown> {
  return fromIR(toIR(body));
}

describe('anthropic adapter round-trip identity', () => {
  for (const fixture of REQUEST_FIXTURES) {
    it(`preserves ${fixture.name}`, () => {
      expect(roundTrip(fixture.body)).toEqual(fixture.body);
    });
  }

  it('does not mutate the input body', () => {
    const body = structuredClone(REQUEST_FIXTURES[5]?.body ?? {});
    const before = structuredClone(body);
    roundTrip(body);
    expect(body).toEqual(before);
  });

  it('is idempotent across repeated round-trips', () => {
    for (const fixture of REQUEST_FIXTURES) {
      expect(roundTrip(roundTrip(fixture.body))).toEqual(fixture.body);
    }
  });
});

/**
 * Prompt cache lookup matches on the serialized request prefix, so a body that
 * is structurally equal but serializes to different bytes misses the cache and
 * re-writes every cached segment. `toEqual` ignores key order and cannot see
 * this; only string equality can.
 */
describe('anthropic adapter serialization is byte-stable', () => {
  for (const fixture of REQUEST_FIXTURES) {
    it(`re-serializes ${fixture.name} to identical bytes`, () => {
      expect(JSON.stringify(roundTrip(fixture.body))).toBe(JSON.stringify(fixture.body));
    });
  }
});

describe('absent optional fields stay absent', () => {
  it('omits system when the request had none', () => {
    const body = {
      model: 'claude-sonnet-4-5',
      max_tokens: 100,
      messages: [{ role: 'user', content: 'hi' }],
    };
    expect('system' in roundTrip(body)).toBe(false);
  });

  it('omits tools when the request had none', () => {
    const body = {
      model: 'claude-sonnet-4-5',
      max_tokens: 100,
      messages: [{ role: 'user', content: 'hi' }],
    };
    expect('tools' in roundTrip(body)).toBe(false);
  });

  it('omits is_error and cache_control when the block had none', () => {
    const body = {
      model: 'claude-sonnet-4-5',
      max_tokens: 100,
      messages: [
        {
          role: 'user',
          content: [{ type: 'tool_result', tool_use_id: 'toolu_1', content: 'ok' }],
        },
      ],
    };
    const messages = roundTrip(body)['messages'] as Array<Record<string, unknown>>;
    const blocks = messages[0]?.['content'] as Array<Record<string, unknown>>;
    const block = blocks[0] ?? {};
    expect('is_error' in block).toBe(false);
    expect('cache_control' in block).toBe(false);
  });
});

describe('unknown block types degrade to opaque', () => {
  it('keeps an unrecognized block verbatim rather than dropping it', () => {
    const block = { type: 'quantum_foo', bar: 1, deep: { list: [1, 2, 3] } };
    const request = toIR({
      model: 'm',
      max_tokens: 1,
      messages: [{ role: 'user', content: [block] }],
    });
    const content = request.messages[0]?.content ?? [];
    expect(content[0]).toEqual({ kind: 'opaque', raw: block });
  });

  it('classifies thinking blocks as byte-preserved thinking nodes', () => {
    const block = { type: 'thinking', thinking: 'reasoning', signature: 'sig' };
    const request = toIR({
      model: 'm',
      max_tokens: 1,
      messages: [{ role: 'assistant', content: [block] }],
    });
    const content = request.messages[0]?.content ?? [];
    expect(content[0]).toEqual({ kind: 'thinking', raw: block });
  });
});
