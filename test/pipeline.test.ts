import { describe, expect, it } from 'vitest';
import type { IrRequest } from '../src/ir/types.js';
import { ALL_SCOPES, forEachTextNode } from '../src/ir/walk.js';
import { fromIR } from '../src/adapters/anthropic/from-ir.js';
import { toIR } from '../src/adapters/anthropic/to-ir.js';
import type { Level } from '../src/compression/levels.js';
import { runPipeline } from '../src/compression/pipeline.js';
import { REQUEST_FIXTURES } from './fixtures/requests.js';

const LEVELS: readonly Level[] = ['light', 'moderate', 'caveman'];

function compress(request: IrRequest, level: Level) {
  return runPipeline({ request, level, scopes: ALL_SCOPES });
}

function blocksOf(body: Record<string, unknown>): Record<string, unknown>[] {
  const messages = (body['messages'] ?? []) as Record<string, unknown>[];
  return messages.flatMap((message) => {
    const content = message['content'];
    return Array.isArray(content) ? (content as Record<string, unknown>[]) : [];
  });
}

function blocksOfKind(body: Record<string, unknown>, type: string) {
  return blocksOf(body).filter((block) => block['type'] === type);
}

describe('pipeline structural validity', () => {
  for (const fixture of REQUEST_FIXTURES) {
    for (const level of LEVELS) {
      it(`keeps ${fixture.name} structurally valid at ${level}`, () => {
        const compressed = fromIR(compress(toIR(fixture.body), level).request);
        const original = fixture.body;

        expect(blocksOfKind(compressed, 'tool_use')).toEqual(
          blocksOfKind(original, 'tool_use'),
        );
        expect(blocksOfKind(compressed, 'thinking')).toEqual(
          blocksOfKind(original, 'thinking'),
        );
        expect(blocksOfKind(compressed, 'redacted_thinking')).toEqual(
          blocksOfKind(original, 'redacted_thinking'),
        );
        expect(blocksOfKind(compressed, 'image')).toEqual(
          blocksOfKind(original, 'image'),
        );
        expect(compressed['tools']).toEqual(original['tools']);
      });
    }
  }

  it('never emits an empty text block at any level', () => {
    for (const fixture of REQUEST_FIXTURES) {
      for (const level of LEVELS) {
        const compressed = fromIR(compress(toIR(fixture.body), level).request);
        for (const block of blocksOfKind(compressed, 'text')) {
          expect(String(block['text']).trim()).not.toBe('');
        }
      }
    }
  });

  it('preserves every tool_use_id and its pairing with the originating tool_use', () => {
    for (const fixture of REQUEST_FIXTURES) {
      const compressed = fromIR(compress(toIR(fixture.body), 'caveman').request);
      const resultIds = blocksOfKind(compressed, 'tool_result').map(
        (block) => block['tool_use_id'],
      );
      const originalIds = blocksOfKind(fixture.body, 'tool_result').map(
        (block) => block['tool_use_id'],
      );
      expect(resultIds).toEqual(originalIds);

      // A tool_use in the same body must still be answered by its result; an
      // unmatched result is a valid shape, since its tool_use may be in an
      // earlier turn the request does not carry.
      const issuedIds = blocksOfKind(compressed, 'tool_use').map((block) => block['id']);
      const answeredIds = issuedIds.filter((id) => resultIds.includes(id));
      const originallyAnswered = blocksOfKind(fixture.body, 'tool_use')
        .map((block) => block['id'])
        .filter((id) => originalIds.includes(id));
      expect(answeredIds).toEqual(originallyAnswered);
    }
  });

  it('never offers a non-text block to the compressor', () => {
    const offered: string[] = [];
    for (const fixture of REQUEST_FIXTURES) {
      forEachTextNode(toIR(fixture.body), ALL_SCOPES, (node) => {
        offered.push(node.text);
      });
    }
    // A tool_use input or a thinking signature reaching the compressor means an
    // opaque block became compressible.
    for (const blockText of offered) {
      expect(blockText).not.toContain('toolu_');
      expect(blockText).not.toContain('signature');
      expect(blockText).not.toContain('San Francisco, CA');
    }
  });

  it('keeps every tool_use input parseable as JSON', () => {
    for (const fixture of REQUEST_FIXTURES) {
      const compressed = fromIR(compress(toIR(fixture.body), 'caveman').request);
      for (const block of blocksOfKind(compressed, 'tool_use')) {
        expect(() => JSON.parse(JSON.stringify(block['input']))).not.toThrow();
      }
    }
  });

  it('preserves cache_control markers on their original blocks', () => {
    for (const fixture of REQUEST_FIXTURES) {
      const compressed = fromIR(compress(toIR(fixture.body), 'moderate').request);
      const marked = blocksOf(compressed).filter(
        (block) => block['cache_control'] !== undefined,
      );
      const originallyMarked = blocksOf(fixture.body).filter(
        (block) => block['cache_control'] !== undefined,
      );
      expect(marked.length).toBe(originallyMarked.length);
    }
  });

  it('leaves a block of nothing but protected regions byte-identical', () => {
    const body = {
      model: 'claude-sonnet-4-5',
      max_tokens: 1024,
      messages: [
        {
          role: 'user',
          content: '```ts\nconst value = compute(alpha, beta);\nreturn value;\n```',
        },
      ],
    };
    for (const level of LEVELS) {
      expect(fromIR(compress(toIR(body), level).request)).toEqual(body);
    }
  });

  it('produces byte-identical output across repeated runs', () => {
    for (const fixture of REQUEST_FIXTURES) {
      const first = JSON.stringify(
        fromIR(compress(toIR(fixture.body), 'moderate').request),
      );
      const second = JSON.stringify(
        fromIR(compress(toIR(fixture.body), 'moderate').request),
      );
      expect(first).toBe(second);
    }
  });

  it('reports stats that account for every walked node', () => {
    const body = {
      model: 'claude-sonnet-4-5',
      max_tokens: 1024,
      system: 'You are a careful assistant that answers questions about the weather.',
      messages: [
        {
          role: 'user',
          content:
            'Could you please tell me what the weather is like in San Francisco today?',
        },
      ],
    };
    const result = compress(toIR(body), 'moderate');
    expect(result.stats.nodesSeen).toBe(2);
    expect(result.stats.nodesCompressed).toBeGreaterThan(0);
    expect(result.stats.charsAfter).toBeLessThan(result.stats.charsBefore);
    expect(result.stats.level).toBe('moderate');
  });

  it('only touches the scopes the policy enables', () => {
    const body = {
      model: 'claude-sonnet-4-5',
      max_tokens: 1024,
      system: 'You are a careful assistant that answers questions about the weather.',
      messages: [
        {
          role: 'user',
          content:
            'Could you please tell me what the weather is like in San Francisco today?',
        },
      ],
    };
    const systemOnly = runPipeline({
      request: toIR(body),
      level: 'moderate',
      scopes: ['system'],
    });
    const emitted = fromIR(systemOnly.request);
    expect(emitted['system']).not.toBe(body.system);
    expect(emitted['messages']).toEqual(body.messages);
    expect(systemOnly.stats.nodesSeen).toBe(1);
  });
});

/**
 * A cached prefix is matched on its serialized bytes, so compressing anything
 * inside it trades a small saving for re-billing the entire cached segment.
 */
describe('cached prefixes are never compressed', () => {
  const cachedBody = {
    model: 'claude-sonnet-4-5',
    max_tokens: 1024,
    system: [
      { type: 'text', text: 'A long stable preamble that would compress well.' },
      {
        type: 'text',
        text: 'The final stable system block, marked as the cache breakpoint.',
        cache_control: { type: 'ephemeral' },
      },
    ],
    messages: [
      {
        role: 'user',
        content: [
          {
            type: 'text',
            text: 'An earlier turn that sits inside the cached prefix region.',
          },
        ],
      },
    ],
  };

  it('leaves the block carrying cache_control untouched', () => {
    const result = compress(toIR(cachedBody), 'moderate');
    const system = fromIR(result.request)['system'] as Record<string, unknown>[];
    expect(system[1]?.['text']).toBe(cachedBody.system[1]?.text);
  });

  it('leaves blocks before the breakpoint untouched', () => {
    const result = compress(toIR(cachedBody), 'moderate');
    const system = fromIR(result.request)['system'] as Record<string, unknown>[];
    expect(system[0]?.['text']).toBe(cachedBody.system[0]?.text);
  });

  it('counts skipped nodes so accounting stays honest', () => {
    const result = compress(toIR(cachedBody), 'moderate');
    // Both system blocks precede the breakpoint; the message follows it.
    expect(result.stats.nodesSkipped).toBe(2);
    expect(result.stats.nodesSeen).toBe(3);
  });

  it('still compresses text after the last breakpoint', () => {
    const result = compress(toIR(cachedBody), 'moderate');
    const messages = fromIR(result.request)['messages'] as Record<string, unknown>[];
    const blocks = messages[0]?.['content'] as Record<string, unknown>[];
    expect(blocks[0]?.['text']).not.toBe(cachedBody.messages[0]?.content[0]?.text);
  });

  it('compresses everything when no block is cached', () => {
    const uncached = structuredClone(cachedBody);
    delete (uncached.system[1] as Record<string, unknown>)['cache_control'];
    const result = compress(toIR(uncached), 'moderate');
    expect(result.stats.nodesSkipped).toBe(0);
    expect(result.stats.nodesCompressed).toBeGreaterThan(0);
  });

  it('keeps the cached prefix byte-identical through a full round-trip', () => {
    const result = compress(toIR(cachedBody), 'moderate');
    const emitted = fromIR(result.request);
    expect(JSON.stringify(emitted['system'])).toBe(JSON.stringify(cachedBody.system));
  });
});
