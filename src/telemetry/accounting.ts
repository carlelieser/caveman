import type { Level } from '../compression/levels.js';
import type { PipelineStats } from '../compression/pipeline.js';

/**
 * Characters per token for English-weighted prose. Estimation is local because
 * an upstream count_tokens call per request would cost more than the saving it
 * measures; the billed counts arrive in the upstream response's `usage`.
 */
const CHARS_PER_TOKEN = 4;

export type TokenAccounting = {
  tokensBefore: number;
  tokensAfter: number;
  tokensSaved: number;
  ratio: number;
  level: Level;
};

export const ACCOUNTING_HEADER_NAMES = {
  tokensBefore: 'X-Caveman-Tokens-Before',
  tokensAfter: 'X-Caveman-Tokens-After',
  ratio: 'X-Caveman-Ratio',
  level: 'X-Caveman-Level',
} as const;

function estimateTokens(charCount: number): number {
  return Math.ceil(charCount / CHARS_PER_TOKEN);
}

/** Fraction of estimated tokens removed. Zero when there was nothing to drop. */
function reductionRatio(tokensBefore: number, tokensAfter: number): number {
  if (tokensBefore === 0) return 0;
  return (tokensBefore - tokensAfter) / tokensBefore;
}

function roundRatio(ratio: number): number {
  return Math.round(ratio * 10000) / 10000;
}

export function accountFor(stats: PipelineStats): TokenAccounting {
  const tokensBefore = estimateTokens(stats.charsBefore);
  const tokensAfter = estimateTokens(stats.charsAfter);
  return {
    tokensBefore,
    tokensAfter,
    tokensSaved: tokensBefore - tokensAfter,
    ratio: roundRatio(reductionRatio(tokensBefore, tokensAfter)),
    level: stats.level,
  };
}

/**
 * Stats are attached even when a request fails upstream, so a compression-induced
 * 4xx stays attributable to the ratio that caused it.
 */
export function applyAccountingHeaders(
  headers: Headers,
  accounting: TokenAccounting,
): void {
  headers.set(ACCOUNTING_HEADER_NAMES.tokensBefore, String(accounting.tokensBefore));
  headers.set(ACCOUNTING_HEADER_NAMES.tokensAfter, String(accounting.tokensAfter));
  headers.set(ACCOUNTING_HEADER_NAMES.ratio, accounting.ratio.toFixed(4));
  headers.set(ACCOUNTING_HEADER_NAMES.level, accounting.level);
}
