import { tokenize, type Span } from './tokenize.js';
import type { ScoreContext, Scorer } from './scorer.js';

export type CompressionStats = {
  spansIn: number;
  spansOut: number;
  charsIn: number;
  charsOut: number;
  /** True when an invariant forced the original text to be returned intact. */
  isUncompressed: boolean;
};

export type CompressionResult = {
  text: string;
  stats: CompressionStats;
};

export type CompressRequest = {
  text: string;
  ratio: number;
  scorer: Scorer;
  context: ScoreContext;
};

const RATIO_MIN = 0;
const RATIO_MAX = 1;

const WHITESPACE_ONLY_PATTERN = /^\s*$/u;

type RankedSpan = {
  index: number;
  score: number;
};

function identityResult(text: string, spansIn: number): CompressionResult {
  return {
    text,
    stats: {
      spansIn,
      spansOut: spansIn,
      charsIn: text.length,
      charsOut: text.length,
      isUncompressed: true,
    },
  };
}

/**
 * Total order over spans: score first, then span index. Ties never fall through
 * to the engine's sort, so the drop set is identical in every process.
 */
function compareForDropping(left: RankedSpan, right: RankedSpan): number {
  if (left.score !== right.score) {
    return left.score - right.score;
  }
  return left.index - right.index;
}

function rank(spans: readonly Span[], scores: readonly number[]): RankedSpan[] {
  return spans.map((_span, index) => ({ index, score: scores[index] ?? 0 }));
}

function selectDropped(ranked: RankedSpan[], dropCount: number): Set<number> {
  const ordered = [...ranked].sort(compareForDropping);
  const dropped = new Set<number>();
  for (let index = 0; index < dropCount; index += 1) {
    const candidate = ordered[index];
    if (candidate !== undefined) {
      dropped.add(candidate.index);
    }
  }
  return dropped;
}

const WHITESPACE_RUN_PATTERN = /\s+/gu;
const LEADING_PUNCTUATION_PATTERN = /^[\p{P}\p{S}\s]+/u;

/**
 * A gap accumulated across dropped spans holds the punctuation those spans were
 * attached to. Keeping it would strand commas and periods against unrelated
 * words, so only the separator itself survives.
 */
function joinGap(gap: string, hasDropped: boolean): string {
  const separator = hasDropped ? stripOrphans(gap) : gap;
  return collapseWhitespace(separator);
}

function stripOrphans(gap: string): string {
  const stripped = gap.replace(LEADING_PUNCTUATION_PATTERN, '');
  if (stripped === '') {
    return gap.includes('\n') ? '\n' : ' ';
  }
  return stripped;
}

/** Collapses runs of whitespace, keeping a line break where one existed. */
function collapseWhitespace(gap: string): string {
  return gap.replace(WHITESPACE_RUN_PATTERN, (run) => (run.includes('\n') ? '\n' : ' '));
}

type Assembly = {
  parts: string[];
  pendingGap: string;
  hasEmitted: boolean;
  hasDroppedSinceEmit: boolean;
};

function absorbGap(assembly: Assembly, gap: string): void {
  assembly.pendingGap += gap;
}

function emitSpan(assembly: Assembly, span: Span): void {
  if (assembly.hasEmitted) {
    assembly.parts.push(joinGap(assembly.pendingGap, assembly.hasDroppedSinceEmit));
  }
  assembly.parts.push(span.text);
  assembly.pendingGap = '';
  assembly.hasEmitted = true;
  assembly.hasDroppedSinceEmit = false;
}

type AssemblyInput = {
  text: string;
  spans: readonly Span[];
  dropped: ReadonlySet<number>;
};

function newAssembly(): Assembly {
  return { parts: [], pendingGap: '', hasEmitted: false, hasDroppedSinceEmit: false };
}

/** Leading and trailing gaps are preserved so surrounding layout survives. */
function assemble(input: AssemblyInput): string {
  const assembly = newAssembly();
  let cursor = 0;
  input.spans.forEach((span, index) => {
    absorbGap(assembly, input.text.slice(cursor, span.start));
    if (input.dropped.has(index)) {
      assembly.hasDroppedSinceEmit = true;
    } else {
      emitSpan(assembly, span);
    }
    cursor = span.end;
  });
  return assembly.parts.join('');
}

function isDegenerate(candidate: string, original: string): boolean {
  const hasLostAllContent =
    WHITESPACE_ONLY_PATTERN.test(candidate) && !WHITESPACE_ONLY_PATTERN.test(original);
  return hasLostAllContent || candidate.length > original.length;
}

/**
 * At least one span always survives, so a block can never be emptied by the
 * ratio alone. The API rejects an empty text block, so this is a hard bound
 * rather than a rounding artefact.
 */
function plannedDropCount(spanCount: number, ratio: number): number {
  const requested = Math.floor(spanCount * ratio);
  return Math.min(requested, spanCount - 1);
}

function validateRatio(ratio: number): void {
  const isInRange = ratio >= RATIO_MIN && ratio < RATIO_MAX;
  if (!Number.isFinite(ratio) || !isInRange) {
    throw new Error(
      `compressText: ratio must be in [${RATIO_MIN}, ${RATIO_MAX}); received ${ratio}`,
    );
  }
}

function buildResult(
  candidate: string,
  request: CompressRequest,
  spans: readonly Span[],
  dropped: ReadonlySet<number>,
): CompressionResult {
  if (isDegenerate(candidate, request.text)) {
    return identityResult(request.text, spans.length);
  }
  return {
    text: candidate,
    stats: {
      spansIn: spans.length,
      spansOut: spans.length - dropped.size,
      charsIn: request.text.length,
      charsOut: candidate.length,
      isUncompressed: false,
    },
  };
}

/**
 * Drops the lowest-scoring spans until the dropped fraction reaches `ratio`.
 * Invariants are enforced here rather than assumed: ratio 0 returns the input
 * untouched without a tokenize round-trip, output never grows, and a result
 * that lost all content falls back to the original text.
 */
export function compressText(request: CompressRequest): CompressionResult {
  validateRatio(request.ratio);
  if (request.ratio === RATIO_MIN) {
    return identityResult(request.text, tokenizeCount(request.text));
  }

  const spans = tokenize(request.text);
  if (spans.length === 0) {
    return identityResult(request.text, 0);
  }

  const scores = request.scorer.score([...spans], request.context);
  const dropCount = plannedDropCount(spans.length, request.ratio);
  if (dropCount === 0) {
    return identityResult(request.text, spans.length);
  }

  const dropped = selectDropped(rank(spans, scores), dropCount);
  const candidate = assemble({ text: request.text, spans, dropped });
  return buildResult(candidate, request, spans, dropped);
}

/** Ratio 0 must not round-trip through assembly, but stats still report spans. */
function tokenizeCount(text: string): number {
  return tokenize(text).length;
}
