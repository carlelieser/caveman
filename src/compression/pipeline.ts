import type { IrRequest } from '../ir/types.js';
import type { TextNode, WalkScope } from '../ir/walk.js';
import type { CompressContext, CompressKind, CompressRole } from './compress.js';
import type { Level } from './levels.js';
import { collectTextNodes, mapTextNodes } from '../ir/walk.js';
import { compressText, proseLength } from './compress.js';

export type PipelineStats = {
  level: Level;
  nodesSeen: number;
  nodesCompressed: number;
  /** In-scope nodes left untouched to keep a cached prefix byte-stable. */
  nodesSkipped: number;
  charsBefore: number;
  charsAfter: number;
  /** Prose characters across every node seen, skipped ones included. */
  charsProse: number;
};

export type PipelineResult = {
  request: IrRequest;
  stats: PipelineStats;
};

/**
 * What to do about text a `cache_control` breakpoint covers.
 *
 * `ignore` compresses every in-scope node wherever it sits. The compressor
 * reads a node's text and the level, never its position, so a node has one
 * compressed form and produces it on every turn. The prefix the cache matches
 * on stays stable as the conversation grows.
 *
 * `respect` is the older rule: skip every node at or before the last
 * breakpoint. The cached prefix stays byte-identical to the one the client sent
 * and is never compressed. It is unstable across turns — a node compressed
 * while it sat in the tail is skipped once a rolling breakpoint advances past
 * it, so its bytes change and the prefix the mode protects is the one it
 * invalidates.
 */
export type CacheMode = 'ignore' | 'respect';

export type PipelineRequest = {
  request: IrRequest;
  level: Level;
  scopes: readonly WalkScope[];
  cacheMode: CacheMode;
};

const DEFAULT_ROLE: CompressRole = 'user';

function kindOf(node: TextNode): CompressKind {
  return node.path.scope === 'tool_results' ? 'tool_result' : 'text';
}

function contextOf(node: TextNode): CompressContext {
  return {
    role: node.role ?? DEFAULT_ROLE,
    kind: kindOf(node),
  };
}

type Tally = {
  nodesSeen: number;
  nodesCompressed: number;
  nodesSkipped: number;
  charsBefore: number;
  charsAfter: number;
  charsProse: number;
};

function newTally(): Tally {
  return {
    nodesSeen: 0,
    nodesCompressed: 0,
    nodesSkipped: 0,
    charsBefore: 0,
    charsAfter: 0,
    charsProse: 0,
  };
}

/**
 * Index of the last node carrying `cache_control`, or -1 when none does.
 * Everything up to and including that node is part of a cached prefix.
 *
 * Only consulted under `respect`. Note that a breakpoint on a non-text block —
 * a `tool_result` or a `tool_use` — is invisible here, because the walk yields
 * only text nodes.
 */
function lastCacheBreakpoint(nodes: readonly TextNode[]): number {
  let last = -1;
  nodes.forEach((node, index) => {
    if (node.hasCacheControl) last = index;
  });
  return last;
}

/** Nodes to leave verbatim, as an index bound. -1 leaves none. */
function cachedThroughIndex(request: PipelineRequest): number {
  if (request.cacheMode === 'ignore') return -1;
  return lastCacheBreakpoint(collectTextNodes(request.request, request.scopes));
}

/**
 * A node inside the cached prefix is returned verbatim, but still counted. It
 * is classified anyway, so the prose share covers the whole request rather than
 * only the compressible tail.
 */
function skipNode(node: TextNode, tally: Tally): string {
  tally.nodesSeen += 1;
  tally.nodesSkipped += 1;
  tally.charsBefore += node.text.length;
  tally.charsAfter += node.text.length;
  tally.charsProse += proseLength(node.text);
  return node.text;
}

const WHITESPACE_ONLY_PATTERN = /^\s*$/u;

/** The API rejects an empty text block, so an emptied node keeps its text. */
function hasEmptied(before: string, after: string): boolean {
  return WHITESPACE_ONLY_PATTERN.test(after) && !WHITESPACE_ONLY_PATTERN.test(before);
}

function compressNode(node: TextNode, request: PipelineRequest, tally: Tally): string {
  const result = compressText({
    text: node.text,
    level: request.level,
    context: contextOf(node),
  });
  const emptied = hasEmptied(node.text, result.text);
  const text = emptied ? node.text : result.text;
  tally.nodesSeen += 1;
  tally.charsBefore += result.stats.charsIn;
  tally.charsAfter += text.length;
  tally.charsProse += result.stats.charsProse;
  if (!emptied && !result.stats.isUncompressed) {
    tally.nodesCompressed += 1;
  }
  return text;
}

/**
 * Compresses every in-scope text node and reports what it cost. The walk runs
 * whatever the level, so the stats describe the same nodes at every setting,
 * which is what makes one level's result comparable to another's.
 *
 * Under the default `ignore` mode a breakpoint changes nothing. A cached prefix
 * matches on its bytes being identical from one turn to the next. Compression
 * is deterministic and reads nothing positional, so a node compressed on the
 * turn it first appears re-renders identically for the rest of the session and
 * the prefix settles in compressed form. The turn it first compresses costs one
 * write of the segment it lies in, which a growing conversation was going to
 * write anyway.
 */
export function runPipeline(request: PipelineRequest): PipelineResult {
  const tally = newTally();
  const cachedThrough = cachedThroughIndex(request);
  let index = -1;
  const compressed = mapTextNodes(request.request, request.scopes, (node) => {
    index += 1;
    if (index <= cachedThrough) return skipNode(node, tally);
    return compressNode(node, request, tally);
  });
  return {
    request: compressed,
    stats: {
      level: request.level,
      nodesSeen: tally.nodesSeen,
      nodesCompressed: tally.nodesCompressed,
      nodesSkipped: tally.nodesSkipped,
      charsBefore: tally.charsBefore,
      charsAfter: tally.charsAfter,
      charsProse: tally.charsProse,
    },
  };
}
