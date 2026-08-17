import type { WalkScope } from '../ir/walk.js';
import type { CompressionPolicy } from '../policy/headers.js';
import type { ScorerRegistry } from '../compression/scorer.js';
import type { CompressionStage } from './messages.js';
import { ALL_SCOPES } from '../ir/walk.js';
import { UnknownScorerError } from './unknown-scorer-error.js';
import { defaultScorerRegistry } from '../compression/heuristic-scorer.js';
import { runPipeline } from '../compression/pipeline.js';

function enabledScopes(policy: CompressionPolicy): WalkScope[] {
  return ALL_SCOPES.filter((scope) => policy.scope[scope]);
}

function isCompressionOff(policy: CompressionPolicy): boolean {
  return policy.compress === 0;
}

/** Builds the stage the handler injects. */
export function createCompressionStage(
  registry: ScorerRegistry = defaultScorerRegistry,
): CompressionStage {
  return (request, policy) => {
    if (isCompressionOff(policy)) {
      return { request, stats: null };
    }
    const lookup = registry.find(policy.scorer);
    if (!lookup.ok) {
      throw new UnknownScorerError(lookup.requested, lookup.available);
    }
    const result = runPipeline({
      request,
      ratio: policy.compress,
      scorer: lookup.scorer,
      scopes: enabledScopes(policy),
    });
    return { request: result.request, stats: result.stats };
  };
}
