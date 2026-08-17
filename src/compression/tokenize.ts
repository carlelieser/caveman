export type Span = {
  text: string;
  start: number;
  end: number;
};

/**
 * Grapheme granularity is defined by UAX #29 independently of locale, unlike
 * word granularity. Passing `undefined` as the locale keeps the host default
 * from reaching a segmentation that varies between machines.
 */
const GRAPHEME_SEGMENTER = new Intl.Segmenter(undefined, { granularity: 'grapheme' });

/** Matches whole clusters, since CRLF is one grapheme of two characters. */
const WHITESPACE_PATTERN = /^\s+$/u;

/** Scripts written without spaces, where each grapheme is its own token. */
const UNSPACED_SCRIPT_PATTERN =
  /^[\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Hangul}\p{Script=Thai}]/u;

/** Pictographs carry meaning alone and never merge into a neighbouring word. */
const STANDALONE_PATTERN = /^\p{Extended_Pictographic}/u;

/**
 * Punctuation and symbols split words, but only between word characters —
 * `don't` and `foo_bar` stay whole while `end.` sheds its period.
 */
const PUNCTUATION_PATTERN = /^[\p{P}\p{S}]/u;

const INTRAWORD_PUNCTUATION = new Set(["'", '’', '-', '_', '.', '/', ':']);

type GraphemeClass = 'whitespace' | 'standalone' | 'punctuation' | 'word';

function classifyGrapheme(grapheme: string): GraphemeClass {
  if (WHITESPACE_PATTERN.test(grapheme)) {
    return 'whitespace';
  }
  if (STANDALONE_PATTERN.test(grapheme) || UNSPACED_SCRIPT_PATTERN.test(grapheme)) {
    return 'standalone';
  }
  if (PUNCTUATION_PATTERN.test(grapheme)) {
    return 'punctuation';
  }
  return 'word';
}

type Accumulator = {
  spans: Span[];
  pending: string;
  pendingStart: number;
};

function flush(accumulator: Accumulator): void {
  if (accumulator.pending === '') {
    return;
  }
  accumulator.spans.push({
    text: accumulator.pending,
    start: accumulator.pendingStart,
    end: accumulator.pendingStart + accumulator.pending.length,
  });
  accumulator.pending = '';
}

function append(accumulator: Accumulator, grapheme: string, offset: number): void {
  if (accumulator.pending === '') {
    accumulator.pendingStart = offset;
  }
  accumulator.pending += grapheme;
}

function emitStandalone(
  accumulator: Accumulator,
  grapheme: string,
  offset: number,
): void {
  flush(accumulator);
  accumulator.spans.push({
    text: grapheme,
    start: offset,
    end: offset + grapheme.length,
  });
}

/**
 * Trailing intraword punctuation (`end.`) belongs to the gap, not the word, so
 * it is only kept once a word character follows it.
 */
function isJoinable(accumulator: Accumulator, grapheme: string): boolean {
  return accumulator.pending !== '' && INTRAWORD_PUNCTUATION.has(grapheme);
}

function consume(accumulator: Accumulator, grapheme: string, offset: number): void {
  const graphemeClass = classifyGrapheme(grapheme);
  if (graphemeClass === 'whitespace') {
    flush(accumulator);
    return;
  }
  if (graphemeClass === 'standalone') {
    emitStandalone(accumulator, grapheme, offset);
    return;
  }
  if (graphemeClass === 'punctuation') {
    consumePunctuation(accumulator, grapheme, offset);
    return;
  }
  append(accumulator, grapheme, offset);
}

function consumePunctuation(
  accumulator: Accumulator,
  grapheme: string,
  offset: number,
): void {
  if (isJoinable(accumulator, grapheme)) {
    append(accumulator, grapheme, offset);
    return;
  }
  flush(accumulator);
}

/**
 * Punctuation joined optimistically (`end.`) is released back into the gap when
 * no word character follows it.
 */
function trimTrailingPunctuation(spans: Span[]): Span[] {
  return spans.map((span) => shrinkSpan(span));
}

function shrinkSpan(span: Span): Span {
  let length = span.text.length;
  while (length > 0 && INTRAWORD_PUNCTUATION.has(span.text.charAt(length - 1))) {
    length -= 1;
  }
  if (length === span.text.length) {
    return span;
  }
  return {
    text: span.text.slice(0, length),
    start: span.start,
    end: span.start + length,
  };
}

/**
 * Splits text into word spans carrying offsets into the original string. Gaps
 * between spans hold the whitespace and punctuation that separate them, so the
 * input is reconstructible from the spans plus the original text.
 */
export function tokenize(text: string): Span[] {
  const accumulator: Accumulator = { spans: [], pending: '', pendingStart: 0 };
  let offset = 0;
  for (const { segment } of GRAPHEME_SEGMENTER.segment(text)) {
    consume(accumulator, segment, offset);
    offset += segment.length;
  }
  flush(accumulator);
  return trimTrailingPunctuation(accumulator.spans).filter((span) => span.text !== '');
}

/** Rebuilds the original text from spans, restoring the gaps between them. */
export function reconstruct(text: string, spans: Span[]): string {
  const parts: string[] = [];
  let cursor = 0;
  for (const span of spans) {
    parts.push(text.slice(cursor, span.start), span.text);
    cursor = span.end;
  }
  parts.push(text.slice(cursor));
  return parts.join('');
}
