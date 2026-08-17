import { describe, expect, it } from 'vitest';
import type { PipelineStats } from '../src/compression/pipeline.js';
import { accountFor } from '../src/telemetry/accounting.js';

function statsFor(charsBefore: number, charsAfter: number): PipelineStats {
  return {
    scorer: 'heuristic',
    ratio: 0.4,
    nodesSeen: 1,
    nodesCompressed: 1,
    nodesSkipped: 0,
    charsBefore,
    charsAfter,
  };
}

describe('token accounting', () => {
  it('reports the saving as tokens before minus after', () => {
    const accounting = accountFor(statsFor(400, 240));
    expect(accounting.tokensBefore).toBe(100);
    expect(accounting.tokensAfter).toBe(60);
    expect(accounting.tokensSaved).toBe(40);
  });

  it('reports no saving when nothing was dropped', () => {
    expect(accountFor(statsFor(400, 400)).tokensSaved).toBe(0);
  });

  it('reports no saving for an empty request', () => {
    const accounting = accountFor(statsFor(0, 0));
    expect(accounting.tokensSaved).toBe(0);
    expect(accounting.ratio).toBe(0);
  });
});
