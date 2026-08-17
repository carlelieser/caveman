import type { ClassifiedWord } from './classify.js';
import type { Level } from './levels.js';
import type { Region } from './regions.js';
import { classifyWords } from './classify.js';
import { isRemovable } from './levels.js';
import { classifyRegions } from './regions.js';

/** Where the text came from, so removal can be scoped by origin later. */
export type CompressRole = 'user' | 'assistant' | 'system';

/** Which kind of IR node the text was taken from. */
export type CompressKind = 'text' | 'tool_result';

export type CompressContext = {
  role: CompressRole;
  kind: CompressKind;
};

export type CompressionStats = {
  wordsIn: number;
  wordsOut: number;
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
  level: Level;
  context: CompressContext;
};

const WHITESPACE_ONLY_PATTERN = /^\s*$/u;

function identityResult(text: string, wordsIn: number): CompressionResult {
  return {
    text,
    stats: {
      wordsIn,
      wordsOut: wordsIn,
      charsIn: text.length,
      charsOut: text.length,
      isUncompressed: true,
    },
  };
}

const WHITESPACE_RUN_PATTERN = /\s+/gu;
/**
 * Punctuation and whitespace a dropped word left behind. Pictographs are
 * excluded even though they are symbols: an emoji carries meaning on its own,
 * so a word dropped beside one must not take it along. `tokenize` draws the
 * same line for the same reason.
 */
const LEADING_PUNCTUATION_PATTERN = /^(?:(?!\p{Extended_Pictographic})[\p{P}\p{S}\s])+/u;

/**
 * Sentence-final punctuation, kept even when the word it was attached to is
 * dropped. A compressed block runs several sentences together without it, and
 * the reader — the model — loses the boundaries entirely.
 */
const TERMINAL_PUNCTUATION_PATTERN = /[.!?;:,](?=\s|$)/u;

/**
 * A gap accumulated across dropped words holds the punctuation those words were
 * attached to. Keeping it would strand commas and periods against unrelated
 * words, so only the separator itself survives — unless the gap ends a
 * sentence, in which case that mark is put back in front of the separator.
 */
function joinGap(gap: string, hasDropped: boolean): string {
  if (!hasDropped) return collapseWhitespace(gap);
  const stripped = collapseWhitespace(stripOrphans(gap));
  const terminal = terminalMarkOf(gap);
  if (terminal === null) return stripped;
  return terminal + stripped;
}

function terminalMarkOf(gap: string): string | null {
  const match = TERMINAL_PUNCTUATION_PATTERN.exec(gap);
  return match === null ? null : match[0];
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

function newAssembly(): Assembly {
  return { parts: [], pendingGap: '', hasEmitted: false, hasDroppedSinceEmit: false };
}

function absorbGap(assembly: Assembly, gap: string): void {
  assembly.pendingGap += gap;
}

function emitText(assembly: Assembly, text: string): void {
  assembly.parts.push(separatorFor(assembly));
  assembly.parts.push(text);
  assembly.pendingGap = '';
  assembly.hasEmitted = true;
  assembly.hasDroppedSinceEmit = false;
}

const TRAILING_WHITESPACE_PATTERN = /\s$/u;

/**
 * A protected region carries its own trailing whitespace — a list marker is
 * `"- "`, a table block ends in its newline — so the separator a drop would
 * otherwise contribute would double it. Emitting nothing keeps the region's
 * bytes exactly as they were written.
 */
function separatorFor(assembly: Assembly): string {
  const separator = gapBefore(assembly);
  if (separator !== ' ') return separator;
  const previous = assembly.parts[assembly.parts.length - 1] ?? '';
  return TRAILING_WHITESPACE_PATTERN.test(previous) ? '' : separator;
}

/**
 * The separator to put in front of the next emission. Nothing was dropped, so
 * the gap stands as written; a drop turns it into a joining separator instead.
 * Before the first emission there is nothing to join to, so a gap left by a
 * dropped opening word collapses away rather than indenting the block.
 */
function gapBefore(assembly: Assembly): string {
  if (!assembly.hasDroppedSinceEmit) return assembly.pendingGap;
  if (!assembly.hasEmitted) return leadingGap(assembly.pendingGap);
  return joinGap(assembly.pendingGap, true);
}

/**
 * A block that lost its first word keeps its original indentation but not the
 * space the word used to occupy, so the text still starts at the left margin
 * it started at before compression.
 */
function leadingGap(gap: string): string {
  const collapsed = collapseWhitespace(stripLeadingPunctuation(gap));
  return collapsed === ' ' ? '' : collapsed;
}

/**
 * A protected region is emitted whole and marks the boundary as clean, so a
 * word dropped just before it cannot pull punctuation out of the region that
 * follows — the region's own bytes are already committed to the output.
 */
function emitProtected(assembly: Assembly, text: string): void {
  if (text === '') return;
  emitText(assembly, text);
}

type AssemblyInput = {
  text: string;
  units: readonly Unit[];
  dropped: ReadonlySet<number>;
};

/**
 * One emittable piece of the source in offset order: either a protected region
 * that must survive byte-identically, or a classified word that may be dropped.
 */
type Unit = {
  start: number;
  end: number;
  text: string;
  isProtected: boolean;
};

/** Leading and trailing gaps are preserved so surrounding layout survives. */
function assemble(input: AssemblyInput): string {
  const assembly = newAssembly();
  let cursor = 0;
  input.units.forEach((unit, index) => {
    absorbGap(assembly, input.text.slice(cursor, unit.start));
    if (unit.isProtected) {
      emitProtected(assembly, unit.text);
    } else if (input.dropped.has(index)) {
      assembly.hasDroppedSinceEmit = true;
    } else {
      emitText(assembly, unit.text);
    }
    cursor = unit.end;
  });
  assembly.parts.push(trailingGap(assembly, input.text.slice(cursor)));
  return assembly.parts.join('');
}

/**
 * The gap after the last unit. A drop in it still strands punctuation, so it
 * gets the same treatment as any other gap, but it is never used to join two
 * words and so keeps whatever trailing layout the block had.
 */
function trailingGap(assembly: Assembly, tail: string): string {
  const gap = assembly.pendingGap + tail;
  if (!assembly.hasDroppedSinceEmit) return gap;
  const terminal = terminalMarkOf(gap);
  const collapsed = collapseWhitespace(stripLeadingPunctuation(gap));
  return terminal === null ? collapsed : terminal + collapsed;
}

function stripLeadingPunctuation(gap: string): string {
  return gap.replace(LEADING_PUNCTUATION_PATTERN, '');
}

function isDegenerate(candidate: string, original: string): boolean {
  const hasLostAllContent =
    WHITESPACE_ONLY_PATTERN.test(candidate) && !WHITESPACE_ONLY_PATTERN.test(original);
  return hasLostAllContent || candidate.length > original.length;
}

function buildUnits(
  text: string,
  regions: readonly Region[],
  words: readonly ClassifiedWord[],
): Unit[] {
  const units: Unit[] = [];
  for (const region of regions) {
    if (region.kind !== 'protected') continue;
    units.push({
      start: region.start,
      end: region.end,
      text: text.slice(region.start, region.end),
      isProtected: true,
    });
  }
  for (const word of words) {
    units.push({
      start: word.start,
      end: word.end,
      text: word.text,
      isProtected: false,
    });
  }
  return units.sort(compareUnits);
}

/**
 * Units never overlap — words come only from prose regions — so ordering by
 * start alone is total, with end as a tiebreak for the empty-span case.
 */
function compareUnits(left: Unit, right: Unit): number {
  if (left.start !== right.start) return left.start - right.start;
  return left.end - right.end;
}

function selectDropped(
  units: readonly Unit[],
  words: readonly ClassifiedWord[],
  level: Level,
): Set<number> {
  const classByOffset = new Map<number, ClassifiedWord['wordClass']>();
  for (const word of words) classByOffset.set(word.start, word.wordClass);
  const dropped = new Set<number>();
  units.forEach((unit, index) => {
    if (unit.isProtected) return;
    const wordClass = classByOffset.get(unit.start);
    if (wordClass !== undefined && isRemovable(level, wordClass)) dropped.add(index);
  });
  return dropped;
}

function countWords(units: readonly Unit[]): number {
  return units.filter((unit) => !unit.isProtected).length;
}

function buildResult(
  candidate: string,
  request: CompressRequest,
  wordsIn: number,
  droppedCount: number,
): CompressionResult {
  if (isDegenerate(candidate, request.text)) {
    return identityResult(request.text, wordsIn);
  }
  return {
    text: candidate,
    stats: {
      wordsIn,
      wordsOut: wordsIn - droppedCount,
      charsIn: request.text.length,
      charsOut: candidate.length,
      isUncompressed: false,
    },
  };
}

/**
 * Rewrites a block by removing whole grammatical classes. Text is split into
 * protected and prose regions first, so code, paths, JSON and stack traces are
 * copied through byte-identically; only words inside prose are classified, and
 * only the classes the level names are removed.
 *
 * Invariants are enforced here rather than assumed: output never grows, a
 * result that lost all content falls back to the original, and a block whose
 * every word is removable is returned unchanged — under class removal that is
 * a real case ("to the"), and the API rejects an empty text block.
 */
export function compressText(request: CompressRequest): CompressionResult {
  const regions = classifyRegions(request.text);
  const words = classifyWords(request.text, regions);
  const units = buildUnits(request.text, regions, words);
  const wordsIn = countWords(units);
  if (wordsIn === 0) {
    return identityResult(request.text, wordsIn);
  }

  const dropped = selectDropped(units, words, request.level);
  if (dropped.size === 0 || dropped.size === wordsIn) {
    return identityResult(request.text, wordsIn);
  }

  const candidate = assemble({ text: request.text, units, dropped });
  return buildResult(candidate, request, wordsIn, dropped.size);
}
