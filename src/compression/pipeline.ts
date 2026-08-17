import type { IrRequest } from '../ir/types.js';
import type { TextNode, WalkScope } from '../ir/walk.js';
import type { ScoreContext, ScoreKind, ScoreRole, Scorer } from './scorer.js';
import { collectTextNodes, mapTextNodes } from '../ir/walk.js';
import { compressText } from './compress.js';

export type PipelineStats = {
  scorer: string;
  ratio: number;
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
  ratio: number;
  scorer: Scorer;
  scopes: readonly WalkScope[];
};

const DEFAULT_SCORE_ROLE: ScoreRole = 'user';

function scoreKindOf(node: TextNode): ScoreKind {
  return node.path.scope === 'tool_results' ? 'tool_result' : 'text';
}

function scoreContextOf(node: TextNode): ScoreContext {
  return {
    role: node.role ?? DEFAULT_SCORE_ROLE,
    kind: scoreKindOf(node),
    blockText: node.text,
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

function compressNode(node: TextNode, request: PipelineRequest, tally: Tally): string {
  const result = compressText({
    text: node.text,
    ratio: request.ratio,
    scorer: request.scorer,
    context: scoreContextOf(node),
  });
  tally.nodesSeen += 1;
  tally.charsBefore += result.stats.charsIn;
  tally.charsAfter += result.stats.charsOut;
  if (!result.stats.isUncompressed) {
    tally.nodesCompressed += 1;
  }
  return result.text;
}

/**
 * Compresses every in-scope text node and reports what it cost. A ratio of 0 is
 * still walked so the stats describe the same nodes a compressed run would
 * touch, which is what makes an off-by-default request comparable to a
 * compressed one.
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
      scorer: request.scorer.name,
      ratio: request.ratio,
      nodesSeen: tally.nodesSeen,
      nodesCompressed: tally.nodesCompressed,
      nodesSkipped: tally.nodesSkipped,
      charsBefore: tally.charsBefore,
      charsAfter: tally.charsAfter,
    },
  };
}
