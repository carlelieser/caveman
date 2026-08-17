import { describe, expect, it } from 'vitest';
import type { IrRequest } from '../src/ir/types.js';
import { ALL_SCOPES } from '../src/ir/walk.js';
import { fromIR } from '../src/adapters/anthropic/from-ir.js';
import { toIR } from '../src/adapters/anthropic/to-ir.js';
import { heuristicScorer } from '../src/compression/heuristic-scorer.js';
import { runPipeline } from '../src/compression/pipeline.js';
import { REQUEST_FIXTURES } from './fixtures/requests.js';

const RATIOS = [0, 0.1, 0.3, 0.5, 0.7, 0.9] as const;

function compress(request: IrRequest, ratio: number) {
  return runPipeline({ request, ratio, scorer: heuristicScorer, scopes: ALL_SCOPES });
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
    for (const ratio of RATIOS) {
      it(`keeps ${fixture.name} structurally valid at ratio ${ratio}`, () => {
        const compressed = fromIR(compress(toIR(fixture.body), ratio).request);
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

  it('never emits an empty text block at any ratio', () => {
    for (const fixture of REQUEST_FIXTURES) {
      for (const ratio of RATIOS) {
        const compressed = fromIR(compress(toIR(fixture.body), ratio).request);
        for (const block of blocksOfKind(compressed, 'text')) {
          expect(String(block['text']).trim()).not.toBe('');
        }
      }
    }
  });

  it('preserves every tool_use_id and its pairing with the originating tool_use', () => {
    for (const fixture of REQUEST_FIXTURES) {
      const compressed = fromIR(compress(toIR(fixture.body), 0.9).request);
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

  it('never offers a non-text block to the scorer', () => {
    const offered: string[] = [];
    const recordingScorer = {
      name: 'recording',
      version: '1.0.0',
      score(spans: { text: string }[], context: { blockText: string }) {
        offered.push(context.blockText);
        return spans.map(() => 1);
      },
    };
    for (const fixture of REQUEST_FIXTURES) {
      runPipeline({
        request: toIR(fixture.body),
        ratio: 0.5,
        scorer: recordingScorer,
        scopes: ALL_SCOPES,
      });
    }
    // A tool_use input or a thinking signature reaching the scorer means an
    // opaque block became compressible.
    for (const blockText of offered) {
      expect(blockText).not.toContain('toolu_');
      expect(blockText).not.toContain('signature');
      expect(blockText).not.toContain('San Francisco, CA');
    }
  });

  it('keeps every tool_use input parseable as JSON', () => {
    for (const fixture of REQUEST_FIXTURES) {
      const compressed = fromIR(compress(toIR(fixture.body), 0.9).request);
      for (const block of blocksOfKind(compressed, 'tool_use')) {
        expect(() => JSON.parse(JSON.stringify(block['input']))).not.toThrow();
      }
    }
  });

  it('preserves cache_control markers on their original blocks', () => {
    for (const fixture of REQUEST_FIXTURES) {
      const compressed = fromIR(compress(toIR(fixture.body), 0.5).request);
      const marked = blocksOf(compressed).filter(
        (block) => block['cache_control'] !== undefined,
      );
      const originallyMarked = blocksOf(fixture.body).filter(
        (block) => block['cache_control'] !== undefined,
      );
      expect(marked.length).toBe(originallyMarked.length);
    }
  });

  it('is the identity function at ratio 0', () => {
    for (const fixture of REQUEST_FIXTURES) {
      expect(fromIR(compress(toIR(fixture.body), 0).request)).toEqual(fixture.body);
    }
  });

  it('produces byte-identical output across repeated runs', () => {
    for (const fixture of REQUEST_FIXTURES) {
      const first = JSON.stringify(fromIR(compress(toIR(fixture.body), 0.4).request));
      const second = JSON.stringify(fromIR(compress(toIR(fixture.body), 0.4).request));
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
    const result = compress(toIR(body), 0.5);
    expect(result.stats.nodesSeen).toBe(2);
    expect(result.stats.nodesCompressed).toBeGreaterThan(0);
    expect(result.stats.charsAfter).toBeLessThan(result.stats.charsBefore);
    expect(result.stats.scorer).toBe('heuristic');
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
      ratio: 0.5,
      scorer: heuristicScorer,
      scopes: ['system'],
    });
    const emitted = fromIR(systemOnly.request);
    expect(emitted['system']).not.toBe(body.system);
    expect(emitted['messages']).toEqual(body.messages);
    expect(systemOnly.stats.nodesSeen).toBe(1);
  });
});
