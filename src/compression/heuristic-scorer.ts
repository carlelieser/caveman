import type { Span } from './tokenize.js';
import { createScorerRegistry, type ScorerRegistry } from './scorer.js';
import type { ScoreContext, ScoreKind, ScoreRole, Scorer } from './scorer.js';

const SCORER_NAME = 'heuristic';
const SCORER_VERSION = '1.0.0';

/**
 * Weights are additive contributions to a span's score. Changing any of them
 * changes output bytes for the same input, so SCORER_VERSION must change too.
 */
const WEIGHTS = {
  stopword: -3,
  punctuation: -2.5,
  shortToken: -1.5,
  longToken: 1.25,
  rarity: 2,
  positionalDecay: 0.5,
  roleBias: 1,
  kindBias: 1,
} as const;

const LENGTH_THRESHOLDS = {
  short: 3,
  long: 8,
} as const;

/** Multiplies the role bias weight; higher keeps more of that role's text. */
const ROLE_BIAS: Readonly<Record<ScoreRole, number>> = {
  system: 0.6,
  user: 0.3,
  assistant: 0,
};

const KIND_BIAS: Readonly<Record<ScoreKind, number>> = {
  text: 0.2,
  tool_result: 0,
};

/** Space-delimited so the table reads as one block of data. */
const STOPWORD_TABLE = [
  'a about above after again all also am an and any are as at be because',
  'been before being below between both but by can did do does doing down',
  'during each few for from further had has have having he her here hers',
  'him his how i if in into is it its itself just me more most my no nor',
  'not now of off on once only or other our out over own same she should',
  'so some such than that the their them then there these they this those',
  'through to too under until up very was we were what when where which',
  'while who whom why will with would you your',
].join(' ');

const STOPWORD_SET = new Set(STOPWORD_TABLE.split(' '));

const PUNCTUATION_ONLY_PATTERN = /^[\p{P}\p{S}]+$/u;

/**
 * Lowercasing is pinned to the root locale: `toLowerCase` follows the host
 * locale and turns Turkish `I` into a dotless `ı`, which would make scores
 * differ between machines.
 */
function normalize(text: string): string {
  return text.toLocaleLowerCase('en-US');
}

function countOccurrences(spans: readonly Span[]): Map<string, number> {
  const counts = new Map<string, number>();
  for (const span of spans) {
    const key = normalize(span.text);
    counts.set(key, (counts.get(key) ?? 0) + 1);
  }
  return counts;
}

function lengthScore(text: string): number {
  if (text.length <= LENGTH_THRESHOLDS.short) {
    return WEIGHTS.shortToken;
  }
  if (text.length >= LENGTH_THRESHOLDS.long) {
    return WEIGHTS.longToken;
  }
  return 0;
}

/** Rare tokens carry more of the block's meaning than repeated ones. */
function rarityScore(occurrences: number, total: number): number {
  const frequency = occurrences / total;
  return WEIGHTS.rarity * (1 - frequency);
}

/** Later text scores slightly higher; recent context matters more. */
function positionalScore(index: number, total: number): number {
  if (total <= 1) {
    return WEIGHTS.positionalDecay;
  }
  return WEIGHTS.positionalDecay * (index / (total - 1));
}

function classScore(text: string): number {
  const normalized = normalize(text);
  if (PUNCTUATION_ONLY_PATTERN.test(text)) {
    return WEIGHTS.punctuation;
  }
  if (STOPWORD_SET.has(normalized)) {
    return WEIGHTS.stopword;
  }
  return 0;
}

function contextScore(context: ScoreContext): number {
  const roleBias = WEIGHTS.roleBias * (ROLE_BIAS[context.role] ?? 0);
  const kindBias = WEIGHTS.kindBias * (KIND_BIAS[context.kind] ?? 0);
  return roleBias + kindBias;
}

type SpanPosition = {
  index: number;
  total: number;
};

function scoreSpan(
  span: Span,
  position: SpanPosition,
  occurrences: Map<string, number>,
): number {
  const count = occurrences.get(normalize(span.text)) ?? 1;
  return (
    classScore(span.text) +
    lengthScore(span.text) +
    rarityScore(count, position.total) +
    positionalScore(position.index, position.total)
  );
}

/**
 * Deterministic by construction: no clock, no randomness, no locale-dependent
 * comparison, and no reliance on Map iteration order — the map is only read by
 * key, never enumerated.
 */
export const heuristicScorer: Scorer = {
  name: SCORER_NAME,
  version: SCORER_VERSION,
  score(spans: Span[], context: ScoreContext): number[] {
    const total = spans.length;
    if (total === 0) {
      return [];
    }
    const occurrences = countOccurrences(spans);
    const bias = contextScore(context);
    return spans.map(
      (span, index) => scoreSpan(span, { index, total }, occurrences) + bias,
    );
  },
};

/** The scorers available to the pipeline by name from `X-Caveman-Scorer`. */
export const defaultScorerRegistry: ScorerRegistry = createScorerRegistry([
  heuristicScorer,
]);
