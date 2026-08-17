import { describe, expect, it } from 'vitest';
import { tokenize } from '../src/compression/tokenize.js';
import { createScorerRegistry } from '../src/compression/scorer.js';
import type { ScoreContext, Scorer } from '../src/compression/scorer.js';
import {
  defaultScorerRegistry,
  heuristicScorer,
} from '../src/compression/heuristic-scorer.js';

const CONTEXT: ScoreContext = {
  role: 'user',
  kind: 'text',
  blockText: '',
};

function contextFor(text: string): ScoreContext {
  return { ...CONTEXT, blockText: text };
}

function scoreOf(text: string, word: string): number {
  const spans = tokenize(text);
  const scores = heuristicScorer.score(spans, contextFor(text));
  const index = spans.findIndex((span) => span.text === word);
  expect(index).toBeGreaterThanOrEqual(0);
  return scores[index] ?? Number.NaN;
}

describe('heuristicScorer', () => {
  it('identifies itself with a name and an explicit version', () => {
    expect(heuristicScorer.name).toBe('heuristic');
    expect(heuristicScorer.version).toMatch(/^\d+\.\d+\.\d+$/);
  });

  it('returns one score per span', () => {
    const text = 'the quick brown fox jumps';
    const spans = tokenize(text);
    expect(heuristicScorer.score(spans, contextFor(text))).toHaveLength(spans.length);
  });

  it('returns no scores for text with no spans', () => {
    expect(heuristicScorer.score(tokenize('   '), contextFor('   '))).toEqual([]);
  });

  it('scores stopwords below content words', () => {
    const text = 'the compression algorithm';
    expect(scoreOf(text, 'the')).toBeLessThan(scoreOf(text, 'compression'));
  });

  it('scores short tokens below long tokens', () => {
    const text = 'ab extraordinarily';
    expect(scoreOf(text, 'ab')).toBeLessThan(scoreOf(text, 'extraordinarily'));
  });

  it('scores a repeated token below a token appearing once', () => {
    const text = 'signal noise noise noise noise';
    expect(scoreOf(text, 'noise')).toBeLessThan(scoreOf(text, 'signal'));
  });

  it('applies a mild positional decay so later text scores higher', () => {
    const text = 'alpha beta gamma alpha';
    const spans = tokenize(text);
    const scores = heuristicScorer.score(spans, contextFor(text));
    expect(scores[3] ?? 0).toBeGreaterThan(scores[0] ?? 0);
  });

  it('weighs the same span differently by role', () => {
    const text = 'deterministic scoring behaviour';
    const spans = tokenize(text);
    const asSystem = heuristicScorer.score(spans, {
      ...contextFor(text),
      role: 'system',
    });
    const asAssistant = heuristicScorer.score(spans, {
      ...contextFor(text),
      role: 'assistant',
    });
    expect(asSystem[0] ?? 0).toBeGreaterThan(asAssistant[0] ?? 0);
  });

  it('produces identical scores across repeated calls', () => {
    const text = 'determinism is the property that preserves prompt caching';
    const spans = tokenize(text);
    const first = heuristicScorer.score(spans, contextFor(text));
    const second = heuristicScorer.score(spans, contextFor(text));
    expect(first).toEqual(second);
  });

  it('produces finite scores for unicode text', () => {
    const text = '日本語 mixed 👨‍👩‍👧 with café';
    const scores = heuristicScorer.score(tokenize(text), contextFor(text));
    expect(scores.every((score) => Number.isFinite(score))).toBe(true);
  });

  it('does not mutate the spans it is given', () => {
    const text = 'immutability of scorer input';
    const spans = tokenize(text);
    const snapshot = structuredClone(spans);
    heuristicScorer.score(spans, contextFor(text));
    expect(spans).toEqual(snapshot);
  });
});

const STUB_SCORER: Scorer = {
  name: 'stub',
  version: '0.0.1',
  score: (spans) => spans.map(() => 0),
};

describe('createScorerRegistry', () => {
  it('resolves a registered scorer by name', () => {
    const registry = createScorerRegistry([heuristicScorer, STUB_SCORER]);
    const result = registry.find('heuristic');
    expect(result.ok && result.scorer).toBe(heuristicScorer);
  });

  it('reports an unknown name as a typed failure listing what is available', () => {
    const registry = createScorerRegistry([heuristicScorer]);
    const result = registry.find('nonexistent');
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.requested).toBe('nonexistent');
      expect(result.available).toEqual(['heuristic']);
    }
  });

  it('throws naming the operation and the requested scorer', () => {
    const registry = createScorerRegistry([heuristicScorer]);
    expect(() => registry.require('nope')).toThrow(/resolveScorer.*"nope"/);
  });

  it('rejects a registry built with duplicate names', () => {
    expect(() => createScorerRegistry([heuristicScorer, heuristicScorer])).toThrow(
      /duplicate scorer name "heuristic"/,
    );
  });

  it('lists names in a stable sorted order', () => {
    const registry = createScorerRegistry([STUB_SCORER, heuristicScorer]);
    expect(registry.names()).toEqual(['heuristic', 'stub']);
  });

  it('exposes the heuristic scorer in the default registry', () => {
    expect(defaultScorerRegistry.require('heuristic')).toBe(heuristicScorer);
  });
});
