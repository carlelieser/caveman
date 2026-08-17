export type RegionKind = 'prose' | 'protected';

export type Region = {
  kind: RegionKind;
  start: number;
  end: number;
};

type Span = { start: number; end: number };

/**
 * A line of the source plus its offset, so line-oriented detection can report
 * absolute positions without re-scanning.
 */
type Line = {
  start: number;
  end: number;
  text: string;
};

const LINE_PATTERN = /[^\n]*\n?/gy;

function splitLines(text: string): Line[] {
  const lines: Line[] = [];
  LINE_PATTERN.lastIndex = 0;
  while (LINE_PATTERN.lastIndex < text.length) {
    const start = LINE_PATTERN.lastIndex;
    const match = LINE_PATTERN.exec(text);
    if (match === null || match[0] === '') break;
    lines.push({ start, end: start + match[0].length, text: match[0] });
  }
  return lines;
}

const FENCE_PATTERN = /^\s{0,3}(?:`{3,}|~{3,})/u;
const INDENTED_CODE_PATTERN = /^(?: {4}|\t)\S/u;
const TABLE_ROW_PATTERN = /^\s*\|.*\|\s*$/u;
const STACK_TRACE_PATTERN =
  /^\s*(?:at\s|File\s"|Caused by:|\.{3}\s\d+\smore|Traceback\s\(most recent call last\))/u;

/**
 * A blank line inside an indented run keeps the run alive — a code block may
 * contain empty lines — but a blank line is only protected once indented code
 * resumes after it, so it is buffered rather than emitted eagerly.
 */
const BLANK_LINE_PATTERN = /^\s*$/u;

type LineScan = {
  spans: Span[];
  /** Indented-code lines held back until a further indented line confirms them. */
  pending: Span[];
  inFence: boolean;
};

function pushSpan(spans: Span[], span: Span): void {
  if (span.end > span.start) spans.push(span);
}

function flushPending(scan: LineScan): void {
  scan.pending = [];
}

function commitPending(scan: LineScan): void {
  for (const span of scan.pending) pushSpan(scan.spans, span);
  scan.pending = [];
}

function scanFenceLine(scan: LineScan, line: Line): void {
  commitPending(scan);
  pushSpan(scan.spans, line);
  scan.inFence = !scan.inFence;
}

function scanIndentedLine(scan: LineScan, line: Line): void {
  commitPending(scan);
  pushSpan(scan.spans, line);
}

/**
 * Line-oriented protection. Fences own every line between them including the
 * closers, so a URL or table row inside a fenced block never fragments it.
 */
function scanLines(text: string): Span[] {
  const scan: LineScan = { spans: [], pending: [], inFence: false };
  for (const line of splitLines(text)) {
    if (FENCE_PATTERN.test(line.text)) {
      scanFenceLine(scan, line);
      continue;
    }
    if (scan.inFence) {
      pushSpan(scan.spans, line);
      continue;
    }
    if (INDENTED_CODE_PATTERN.test(line.text)) {
      scanIndentedLine(scan, line);
      continue;
    }
    if (BLANK_LINE_PATTERN.test(line.text)) {
      scan.pending.push(line);
      continue;
    }
    if (TABLE_ROW_PATTERN.test(line.text) || STACK_TRACE_PATTERN.test(line.text)) {
      flushPending(scan);
      pushSpan(scan.spans, line);
      continue;
    }
    flushPending(scan);
  }
  // An unterminated fence protects everything after it; its lines are already in.
  return scan.spans;
}

/**
 * Inline constructs, each anchored tightly enough that ordinary prose does not
 * match. Order is irrelevant because every match is merged by offset, not by
 * precedence — longest coverage wins through the union, not through the list.
 */
const INLINE_PATTERNS: readonly RegExp[] = [
  // Inline code, backtick-delimited, non-greedy so adjacent spans stay separate.
  /`[^`\n]+`/gu,
  // URLs, including the query string and fragment.
  /\b[a-z][a-z0-9+.-]*:\/\/[^\s<>()[\]{}"']+/giu,
  // Bare hosts that read as URLs without a scheme.
  /\bwww\.[^\s<>()[\]{}"']+/giu,
  // Windows paths.
  /\b[A-Za-z]:\\[^\s"'<>|]+/gu,
  // POSIX-ish paths: a segment followed by at least one slash-joined segment,
  // anchored on a leading `/`, `./`, `../`, or a dotted filename.
  /(?:\.{1,2}\/|\/)[A-Za-z0-9_.-]+(?:\/[A-Za-z0-9_.-]+)*\/?/gu,
  /\b[A-Za-z0-9_-]+(?:\/[A-Za-z0-9_-]+)+\.[A-Za-z0-9]+\b/gu,
  // A dotted filename with a known-shaped extension.
  /\b[A-Za-z0-9_-]+\.(?:[A-Za-z]{1,5}[A-Za-z0-9]{0,3})\b(?=[\s:,)\]}]|$)/gu,
  // JSON or JS object/array literals, one nesting level of braces.
  /\{[^{}\n]*[:,][^{}\n]*\}/gu,
  /\[[^[\]\n]*[:,"][^[\]\n]*\]/gu,
  // One line of a pretty-printed object, whose braces sit on other lines.
  /^[ \t]*"(?:[^"\\\n]|\\.)*"[ \t]*:.*$/gmu,
  // A double-quoted string anywhere else; its contents are read back verbatim.
  /"(?:[^"\\\n]|\\.)*"/gu,
  // XML and JSX elements.
  /<\/?[A-Za-z][A-Za-z0-9.:-]*(?:\s[^<>]*)?\/?>/gu,
  // Stack frame fragments appearing mid-line.
  /\([^\s()]+:\d+(?::\d+)?\)/gu,
  /\b[A-Za-z0-9_.-]+:\d+(?::\d+)?\b/gu,
  // Hex literals, UUIDs, long digit and hash runs.
  /\b0[xX][0-9a-fA-F]+\b/gu,
  /\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b/gu,
  /\b[0-9a-fA-F]{16,}\b/gu,
  /\b\d{7,}\b/gu,
  // Version strings, with or without a leading `v`, `^` or `~`.
  /(?:^|(?<=[\s(=@]))[v^~><=]{0,2}\d+\.\d+(?:\.\d+)?(?:[-+][A-Za-z0-9.]+)?/gu,
  // Markdown structural markers. The marker only — the sentence after a bullet
  // or a header is prose and stays compressible.
  /^[ \t]*(?:[-*+]|\d+[.)])[ \t]+/gmu,
  /^[ \t]*#{1,6}[ \t]+/gmu,
  /^[ \t]*>[ \t]?/gmu,
  // Identifiers that are code by shape rather than by delimiter.
  /\b[A-Za-z_$][A-Za-z0-9_$]*\([^()\n]*\)/gu,
  /\b[a-z][a-z0-9]*(?:_[a-z0-9]+)+\b/giu,
  /\$[A-Za-z_{][A-Za-z0-9_}]*/gu,
  /--?[A-Za-z][A-Za-z0-9-]*=\S+/gu,
];

function scanInline(text: string): Span[] {
  const spans: Span[] = [];
  for (const pattern of INLINE_PATTERNS) {
    pattern.lastIndex = 0;
    for (const match of text.matchAll(pattern)) {
      const start = match.index;
      if (start === undefined || match[0] === '') continue;
      spans.push({ start, end: start + match[0].length });
    }
  }
  return spans;
}

/**
 * Sorted by start, then by end descending, so a longer span at the same offset
 * is merged first. The comparator is total, so the order never depends on the
 * engine's sort stability.
 */
function compareSpans(left: Span, right: Span): number {
  if (left.start !== right.start) return left.start - right.start;
  return right.end - left.end;
}

/** Overlapping and touching spans collapse into one, so protection never fragments. */
function mergeSpans(spans: readonly Span[]): Span[] {
  const ordered = [...spans].sort(compareSpans);
  const merged: Span[] = [];
  for (const span of ordered) {
    const last = merged[merged.length - 1];
    if (last !== undefined && span.start <= last.end) {
      last.end = Math.max(last.end, span.end);
      continue;
    }
    merged.push({ start: span.start, end: span.end });
  }
  return merged;
}

function clampSpans(spans: readonly Span[], length: number): Span[] {
  const clamped: Span[] = [];
  for (const span of spans) {
    const start = Math.max(0, Math.min(span.start, length));
    const end = Math.max(start, Math.min(span.end, length));
    if (end > start) clamped.push({ start, end });
  }
  return clamped;
}

function fillGaps(protectedSpans: readonly Span[], length: number): Region[] {
  const regions: Region[] = [];
  let cursor = 0;
  for (const span of protectedSpans) {
    if (span.start > cursor) {
      regions.push({ kind: 'prose', start: cursor, end: span.start });
    }
    regions.push({ kind: 'protected', start: span.start, end: span.end });
    cursor = span.end;
  }
  if (cursor < length) {
    regions.push({ kind: 'prose', start: cursor, end: length });
  }
  return regions;
}

/**
 * Splits text into regions that tile it exactly: the first starts at 0, each
 * region's end is the next one's start, and the last ends at `text.length`.
 * Reconstructing the slices in order yields the input back.
 *
 * Anything matching a code-shaped pattern resolves to `protected`, because a
 * false positive costs only savings while a false negative corrupts a code
 * block, a path or a stack trace that the model needs verbatim.
 */
export function classifyRegions(text: string): Region[] {
  if (text === '') return [];
  const found = [...scanLines(text), ...scanInline(text)];
  const merged = mergeSpans(clampSpans(found, text.length));
  return fillGaps(merged, text.length);
}
