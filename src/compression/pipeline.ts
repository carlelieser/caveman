import type { IrRequest } from '../ir/types.js';
import type { TextNode, WalkScope } from '../ir/walk.js';
import type { ScoreContext, ScoreKind, ScoreRole, Scorer } from './scorer.js';
import { mapTextNodes } from '../ir/walk.js';
import { compressText } from './compress.js';

export type PipelineStats = {
  scorer: string;
  ratio: number;
  nodesSeen: number;
  nodesCompressed: number;
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
  charsBefore: number;
  charsAfter: number;
};

function newTally(): Tally {
  return { nodesSeen: 0, nodesCompressed: 0, charsBefore: 0, charsAfter: 0 };
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
 */
export function runPipeline(request: PipelineRequest): PipelineResult {
  const tally = newTally();
  const compressed = mapTextNodes(request.request, request.scopes, (node) =>
    compressNode(node, request, tally),
  );
  return {
    request: compressed,
    stats: {
      scorer: request.scorer.name,
      ratio: request.ratio,
      nodesSeen: tally.nodesSeen,
      nodesCompressed: tally.nodesCompressed,
      charsBefore: tally.charsBefore,
      charsAfter: tally.charsAfter,
    },
  };
}
