import { describe, expect, it } from 'vitest';
import { CAVEMAN_HEADER_NAMES, parseCompressionPolicy } from '../src/policy/headers.js';

function headersWith(entries: Record<string, string>): Headers {
  return new Headers(entries);
}

describe('parseCompressionPolicy defaults', () => {
  it('defaults compress to 0 when X-Caveman-Compress is absent', () => {
    const result = parseCompressionPolicy(headersWith({}));
    expect(result.ok).toBe(true);
    if (result.ok) expect(result.policy.compress).toBe(0);
  });

  it('defaults scope to messages only when X-Caveman-Scope is absent', () => {
    const result = parseCompressionPolicy(headersWith({}));
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.policy.scope).toEqual({
        messages: true,
        system: false,
        tool_results: false,
      });
    }
  });

  it('defaults scorer to "heuristic" when X-Caveman-Scorer is absent', () => {
    const result = parseCompressionPolicy(headersWith({}));
    expect(result.ok).toBe(true);
    if (result.ok) expect(result.policy.scorer).toBe('heuristic');
  });
});

describe('X-Caveman-Compress boundary values', () => {
  it('accepts 0 as the lower boundary (inclusive)', () => {
    const result = parseCompressionPolicy(headersWith({ 'X-Caveman-Compress': '0' }));
    expect(result.ok).toBe(true);
    if (result.ok) expect(result.policy.compress).toBe(0);
  });

  it('accepts 0.9 as the upper boundary (inclusive)', () => {
    const result = parseCompressionPolicy(headersWith({ 'X-Caveman-Compress': '0.9' }));
    expect(result.ok).toBe(true);
    if (result.ok) expect(result.policy.compress).toBe(0.9);
  });

  it('rejects a value just over the upper boundary', () => {
    const result = parseCompressionPolicy(
      headersWith({ 'X-Caveman-Compress': '0.90001' }),
    );
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.header).toBe('X-Caveman-Compress');
      expect(result.value).toBe('0.90001');
    }
  });

  it('rejects a value just under the lower boundary', () => {
    const result = parseCompressionPolicy(
      headersWith({ 'X-Caveman-Compress': '-0.00001' }),
    );
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.header).toBe('X-Caveman-Compress');
  });
});

describe('X-Caveman-Compress rejections', () => {
  it('rejects an empty string', () => {
    const result = parseCompressionPolicy(headersWith({ 'X-Caveman-Compress': '' }));
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.header).toBe('X-Caveman-Compress');
  });

  it('rejects a non-numeric string ("abc")', () => {
    const result = parseCompressionPolicy(headersWith({ 'X-Caveman-Compress': 'abc' }));
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.header).toBe('X-Caveman-Compress');
      expect(result.value).toBe('abc');
    }
  });

  it('rejects a value above 0.9 ("1.5")', () => {
    const result = parseCompressionPolicy(headersWith({ 'X-Caveman-Compress': '1.5' }));
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.header).toBe('X-Caveman-Compress');
  });

  it('rejects a negative value ("-0.1")', () => {
    const result = parseCompressionPolicy(headersWith({ 'X-Caveman-Compress': '-0.1' }));
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.header).toBe('X-Caveman-Compress');
  });

  it('rejects the literal string "NaN"', () => {
    const result = parseCompressionPolicy(headersWith({ 'X-Caveman-Compress': 'NaN' }));
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.header).toBe('X-Caveman-Compress');
  });

  it('rejects the literal string "Infinity"', () => {
    const result = parseCompressionPolicy(
      headersWith({ 'X-Caveman-Compress': 'Infinity' }),
    );
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.header).toBe('X-Caveman-Compress');
  });

  it('accepts scientific notation ("1e-1" parses as 0.1, within range)', () => {
    const result = parseCompressionPolicy(headersWith({ 'X-Caveman-Compress': '1e-1' }));
    expect(result.ok).toBe(true);
    if (result.ok) expect(result.policy.compress).toBeCloseTo(0.1);
  });

  it('accepts a value with surrounding whitespace (" 0.3 " is trimmed)', () => {
    const result = parseCompressionPolicy(headersWith({ 'X-Caveman-Compress': ' 0.3 ' }));
    expect(result.ok).toBe(true);
    if (result.ok) expect(result.policy.compress).toBeCloseTo(0.3);
  });

  it('rejects a whitespace-only value', () => {
    const result = parseCompressionPolicy(headersWith({ 'X-Caveman-Compress': '   ' }));
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.header).toBe('X-Caveman-Compress');
  });
});

describe('X-Caveman-Scope parsing', () => {
  it('accepts a single valid member', () => {
    const result = parseCompressionPolicy(headersWith({ 'X-Caveman-Scope': 'system' }));
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.policy.scope).toEqual({
        messages: false,
        system: true,
        tool_results: false,
      });
    }
  });

  it('accepts multiple comma-separated members', () => {
    const result = parseCompressionPolicy(
      headersWith({ 'X-Caveman-Scope': 'messages,system,tool_results' }),
    );
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.policy.scope).toEqual({
        messages: true,
        system: true,
        tool_results: true,
      });
    }
  });

  it('trims whitespace around each member ("messages, system")', () => {
    const result = parseCompressionPolicy(
      headersWith({ 'X-Caveman-Scope': 'messages, system' }),
    );
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.policy.scope).toEqual({
        messages: true,
        system: true,
        tool_results: false,
      });
    }
  });

  it('rejects an empty string', () => {
    const result = parseCompressionPolicy(headersWith({ 'X-Caveman-Scope': '' }));
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.header).toBe('X-Caveman-Scope');
  });

  it('rejects a trailing comma producing an empty member ("messages,")', () => {
    const result = parseCompressionPolicy(
      headersWith({ 'X-Caveman-Scope': 'messages,' }),
    );
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.header).toBe('X-Caveman-Scope');
  });

  it('rejects an unknown member', () => {
    const result = parseCompressionPolicy(
      headersWith({ 'X-Caveman-Scope': 'messages,bogus' }),
    );
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.header).toBe('X-Caveman-Scope');
      expect(result.value).toBe('messages,bogus');
    }
  });

  it('rejects a duplicate member ("messages,messages")', () => {
    const result = parseCompressionPolicy(
      headersWith({ 'X-Caveman-Scope': 'messages,messages' }),
    );
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.header).toBe('X-Caveman-Scope');
  });
});

describe('X-Caveman-Scorer parsing', () => {
  it('accepts a custom scorer name', () => {
    const result = parseCompressionPolicy(
      headersWith({ 'X-Caveman-Scorer': 'my-scorer' }),
    );
    expect(result.ok).toBe(true);
    if (result.ok) expect(result.policy.scorer).toBe('my-scorer');
  });

  it('rejects an empty string', () => {
    const result = parseCompressionPolicy(headersWith({ 'X-Caveman-Scorer': '' }));
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.header).toBe('X-Caveman-Scorer');
  });

  it('rejects a whitespace-only value', () => {
    const result = parseCompressionPolicy(headersWith({ 'X-Caveman-Scorer': '   ' }));
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.header).toBe('X-Caveman-Scorer');
  });
});

describe('CAVEMAN_HEADER_NAMES', () => {
  it('lists exactly the Caveman header names for stripping upstream', () => {
    expect(CAVEMAN_HEADER_NAMES).toEqual([
      'X-Caveman-Compress',
      'X-Caveman-Scope',
      'X-Caveman-Scorer',
      'X-Caveman-Claude-Mode',
    ]);
  });
});
