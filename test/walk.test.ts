import { describe, expect, it } from 'vitest';
import { toIR } from '../src/adapters/anthropic/to-ir.js';
import { fromIR } from '../src/adapters/anthropic/from-ir.js';
import { ALL_SCOPES, collectTextNodes, mapTextNodes } from '../src/ir/walk.js';
import type { WalkScope } from '../src/ir/walk.js';

const BODY = {
  model: 'claude-sonnet-4-5',
  max_tokens: 1024,
  system: [
    { type: 'text', text: 'system one' },
    { type: 'text', text: 'system two', cache_control: { type: 'ephemeral' } },
  ],
  messages: [
    {
      role: 'user',
      content: [
        { type: 'text', text: 'user one' },
        { type: 'image', source: { type: 'url', url: 'https://example.com/a.png' } },
      ],
    },
    {
      role: 'assistant',
      content: [
        { type: 'text', text: 'assistant one' },
        {
          type: 'tool_use',
          id: 'toolu_1',
          name: 'search',
          input: { query: 'never touched' },
        },
        { type: 'thinking', thinking: 'never touched', signature: 'sig' },
      ],
    },
    {
      role: 'user',
      content: [
        {
          type: 'tool_result',
          tool_use_id: 'toolu_1',
          content: [
            { type: 'text', text: 'result one' },
            { type: 'text', text: 'result two' },
          ],
        },
      ],
    },
  ],
};

function textsFor(scopes: readonly WalkScope[]): string[] {
  return collectTextNodes(toIR(BODY), scopes).map((node) => node.text);
}

describe('scoping', () => {
  it('collects only message text under the messages scope', () => {
    expect(textsFor(['messages'])).toEqual(['user one', 'assistant one']);
  });

  it('collects only system text under the system scope', () => {
    expect(textsFor(['system'])).toEqual(['system one', 'system two']);
  });

  it('reaches text nested in tool_result under the tool_results scope', () => {
    expect(textsFor(['tool_results'])).toEqual(['result one', 'result two']);
  });

  it('collects every compressible node in document order across all scopes', () => {
    expect(textsFor(ALL_SCOPES)).toEqual([
      'system one',
      'system two',
      'user one',
      'assistant one',
      'result one',
      'result two',
    ]);
  });

  it('collects nothing for an empty scope list', () => {
    expect(textsFor([])).toEqual([]);
  });
});

describe('scoring context', () => {
  it('reports the role that owns each node', () => {
    const nodes = collectTextNodes(toIR(BODY), ALL_SCOPES);
    expect(nodes.map((node) => node.role)).toEqual([
      'system',
      'system',
      'user',
      'assistant',
      'user',
      'user',
    ]);
  });

  it('reports cache_control presence', () => {
    const nodes = collectTextNodes(toIR(BODY), ['system']);
    expect(nodes.map((node) => node.hasCacheControl)).toEqual([false, true]);
  });

  it('addresses a nested tool_result node by message, block, and nested index', () => {
    const nodes = collectTextNodes(toIR(BODY), ['tool_results']);
    expect(nodes[1]?.path).toEqual({
      scope: 'tool_results',
      messageIndex: 2,
      blockIndex: 0,
      toolResultIndex: 1,
    });
  });

  it('leaves toolResultIndex null for a direct message node', () => {
    const nodes = collectTextNodes(toIR(BODY), ['messages']);
    expect(nodes[0]?.path).toEqual({
      scope: 'messages',
      messageIndex: 0,
      blockIndex: 0,
      toolResultIndex: null,
    });
  });
});

describe('mapping', () => {
  it('returns a new request without mutating the original', () => {
    const request = toIR(BODY);
    const mapped = mapTextNodes(request, ALL_SCOPES, (node) => node.text.toUpperCase());
    expect(mapped).not.toBe(request);
    expect(collectTextNodes(request, ALL_SCOPES).map((node) => node.text)).toEqual([
      'system one',
      'system two',
      'user one',
      'assistant one',
      'result one',
      'result two',
    ]);
  });

  it('applies the mapper to in-scope nodes only', () => {
    const mapped = mapTextNodes(toIR(BODY), ['system'], (node) =>
      node.text.toUpperCase(),
    );
    expect(collectTextNodes(mapped, ALL_SCOPES).map((node) => node.text)).toEqual([
      'SYSTEM ONE',
      'SYSTEM TWO',
      'user one',
      'assistant one',
      'result one',
      'result two',
    ]);
  });

  it('rewrites nested tool_result text in place', () => {
    const mapped = mapTextNodes(toIR(BODY), ['tool_results'], () => 'compressed');
    const body = fromIR(mapped) as Record<string, unknown>;
    const messages = body['messages'] as Array<Record<string, unknown>>;
    const blocks = messages[2]?.['content'] as Array<Record<string, unknown>>;
    expect(blocks[0]?.['content']).toEqual([
      { type: 'text', text: 'compressed' },
      { type: 'text', text: 'compressed' },
    ]);
  });

  it('never exposes tool_use input, thinking, or opaque blocks as text nodes', () => {
    const texts = textsFor(ALL_SCOPES);
    expect(texts).not.toContain('never touched');
  });

  it('leaves non-text blocks byte-identical after an identity map', () => {
    const mapped = mapTextNodes(toIR(BODY), ALL_SCOPES, (node) => node.text);
    expect(fromIR(mapped)).toEqual(BODY);
  });

  it('preserves cache_control on a rewritten block', () => {
    const mapped = mapTextNodes(toIR(BODY), ['system'], () => 'short');
    const system = (fromIR(mapped) as Record<string, unknown>)['system'];
    expect(system).toEqual([
      { type: 'text', text: 'short' },
      { type: 'text', text: 'short', cache_control: { type: 'ephemeral' } },
    ]);
  });
});

describe('string-form content survives mapping', () => {
  it('re-emits a mapped string-form message as a string', () => {
    const request = toIR({
      model: 'm',
      max_tokens: 1,
      messages: [{ role: 'user', content: 'original text' }],
    });
    const mapped = mapTextNodes(request, ['messages'], () => 'mapped text');
    const messages = (fromIR(mapped) as Record<string, unknown>)['messages'];
    expect(messages).toEqual([{ role: 'user', content: 'mapped text' }]);
  });

  it('re-emits a mapped string-form tool_result as a string', () => {
    const request = toIR({
      model: 'm',
      max_tokens: 1,
      messages: [
        {
          role: 'user',
          content: [{ type: 'tool_result', tool_use_id: 'toolu_1', content: 'original' }],
        },
      ],
    });
    const mapped = mapTextNodes(request, ['tool_results'], () => 'mapped');
    const messages = (fromIR(mapped) as Record<string, unknown>)['messages'] as Array<
      Record<string, unknown>
    >;
    const blocks = messages[0]?.['content'] as Array<Record<string, unknown>>;
    expect(blocks[0]?.['content']).toBe('mapped');
  });
});
