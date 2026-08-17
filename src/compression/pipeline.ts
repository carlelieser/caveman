import type { IrRequest } from '../ir/types.js';
import type { TextNode, WalkScope } from '../ir/walk.js';
import type { CompressContext, CompressKind, CompressRole } from './compress.js';
import type { Level } from './levels.js';
import { collectTextNodes, mapTextNodes } from '../ir/walk.js';
import { compressText } from './compress.js';

export type PipelineStats = {
  level: Level;
  nodesSeen: number;
  nodesCompressed: number;
  /** In-scope nodes left untouched to keep a cached prefix byte-stable. */
  nodesSkipped: number;
  charsBefore: number;
  charsAfter: number;
};

export type PipelineResult = {
  request: IrRequest;
  stats: PipelineStats;
};

export type PipelineRequest = {
  request: IrRequest;
  level: Level;
  scopes: readonly WalkScope[];
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
};

function newTally(): Tally {
  return {
    nodesSeen: 0,
    nodesCompressed: 0,
    nodesSkipped: 0,
    charsBefore: 0,
    charsAfter: 0,
  };
}

/**
 * Index of the last node carrying `cache_control`, or -1 when none does.
 * Everything up to and including that node is part of a cached prefix.
 */
function lastCacheBreakpoint(nodes: readonly TextNode[]): number {
  let last = -1;
  nodes.forEach((node, index) => {
    if (node.hasCacheControl) last = index;
  });
  return last;
}

/** A node inside the cached prefix is returned verbatim, but still counted. */
function skipNode(node: TextNode, tally: Tally): string {
  tally.nodesSeen += 1;
  tally.nodesSkipped += 1;
  tally.charsBefore += node.text.length;
  tally.charsAfter += node.text.length;
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
 * Text at or before the last `cache_control` breakpoint is left untouched.
 * Rewriting it would change the bytes the prompt cache matches on, so the whole
 * cached prefix — typically far larger than anything compression saves — would
 * be re-billed as a fresh write on every turn.
 */
export function runPipeline(request: PipelineRequest): PipelineResult {
  const tally = newTally();
  const cachedThrough = lastCacheBreakpoint(
    collectTextNodes(request.request, request.scopes),
  );
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
    },
  };
}
