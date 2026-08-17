import { describe, expect, it } from 'vitest';
import { CAVEMAN_HEADER_NAMES, parseCompressionPolicy } from '../src/policy/headers.js';

function headersWith(entries: Record<string, string>): Headers {
  return new Headers(entries);
}

describe('parseCompressionPolicy defaults', () => {
  it('defaults the level to null (off) when X-Caveman-Compress is absent', () => {
    const result = parseCompressionPolicy(headersWith({}));
    expect(result.ok).toBe(true);
    if (result.ok) expect(result.policy.level).toBeNull();
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
});

describe('X-Caveman-Compress levels', () => {
  for (const level of ['light', 'moderate', 'caveman'] as const) {
    it(`accepts "${level}"`, () => {
      const result = parseCompressionPolicy(headersWith({ 'X-Caveman-Compress': level }));
      expect(result.ok).toBe(true);
      if (result.ok) expect(result.policy.level).toBe(level);
    });
  }

  it('accepts "off" as an explicit way to disable compression', () => {
    const result = parseCompressionPolicy(headersWith({ 'X-Caveman-Compress': 'off' }));
    expect(result.ok).toBe(true);
    if (result.ok) expect(result.policy.level).toBeNull();
  });

  it('accepts a level in any case', () => {
    const result = parseCompressionPolicy(
      headersWith({ 'X-Caveman-Compress': 'CAVEMAN' }),
    );
    expect(result.ok).toBe(true);
    if (result.ok) expect(result.policy.level).toBe('caveman');
  });

  it('accepts a level with surrounding whitespace', () => {
    const result = parseCompressionPolicy(
      headersWith({ 'X-Caveman-Compress': '  light  ' }),
    );
    expect(result.ok).toBe(true);
    if (result.ok) expect(result.policy.level).toBe('light');
  });
});

describe('X-Caveman-Compress rejections', () => {
  it('rejects a fraction, naming the header and listing the levels', () => {
    const result = parseCompressionPolicy(headersWith({ 'X-Caveman-Compress': '0.5' }));
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.header).toBe('X-Caveman-Compress');
      expect(result.value).toBe('0.5');
      expect(result.reason).toContain('light');
      expect(result.reason).toContain('caveman');
    }
  });

  it('rejects "0", which used to mean off', () => {
    const result = parseCompressionPolicy(headersWith({ 'X-Caveman-Compress': '0' }));
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.header).toBe('X-Caveman-Compress');
  });

  it('rejects an empty string', () => {
    const result = parseCompressionPolicy(headersWith({ 'X-Caveman-Compress': '' }));
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.header).toBe('X-Caveman-Compress');
  });

  it('rejects a whitespace-only value', () => {
    const result = parseCompressionPolicy(headersWith({ 'X-Caveman-Compress': '   ' }));
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.header).toBe('X-Caveman-Compress');
  });

  it('rejects an unknown level name', () => {
    const result = parseCompressionPolicy(
      headersWith({ 'X-Caveman-Compress': 'aggressive' }),
    );
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.header).toBe('X-Caveman-Compress');
      expect(result.value).toBe('aggressive');
    }
  });

  it('rejects a former scorer name', () => {
    const result = parseCompressionPolicy(
      headersWith({ 'X-Caveman-Compress': 'heuristic' }),
    );
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

describe('CAVEMAN_HEADER_NAMES', () => {
  it('lists exactly the Caveman header names for stripping upstream', () => {
    expect(CAVEMAN_HEADER_NAMES).toEqual(['X-Caveman-Compress', 'X-Caveman-Scope']);
  });
});
