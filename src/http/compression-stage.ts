import type { WalkScope } from '../ir/walk.js';
import type { CompressionPolicy } from '../policy/headers.js';
import type { CompressionStage } from './messages.js';
import { ALL_SCOPES } from '../ir/walk.js';
import { runPipeline } from '../compression/pipeline.js';

function enabledScopes(policy: CompressionPolicy): WalkScope[] {
  return ALL_SCOPES.filter((scope) => policy.scope[scope]);
}

/** Builds the stage the handler injects. */
export function createCompressionStage(): CompressionStage {
  return (request, policy) => {
    if (policy.level === null) {
      return { request, stats: null };
    }
    const result = runPipeline({
      request,
      level: policy.level,
      scopes: enabledScopes(policy),
      cacheMode: policy.cacheMode,
    });
    return { request: result.request, stats: result.stats };
  };
}
