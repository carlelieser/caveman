import type { PipelineStats } from '../compression/pipeline.js';
import type { TokenAccounting } from './accounting.js';
import type { LogSink } from './logging.js';
import type { UpstreamUsage } from './usage.js';
import { accountFor } from './accounting.js';
import { hasUsage } from './usage.js';

const PREFIX = 'caveman';

export type SavingsReporter = {
  record(stats: PipelineStats): void;
  /**
   * Reports what the provider billed for one request. Separate from `record`
   * because it arrives later — the counts are in the response, which is still
   * streaming when the compression stats are already known.
   */
  recordUsage(usage: UpstreamUsage): void;
  /** Null until a request has been compressed, so an idle run stays quiet. */
  summary(): string | null;
};

type SessionTotal = {
  tokensSaved: number;
  requests: number;
};

function count(value: number): string {
  return value.toLocaleString('en-US');
}

/** The ratio actually achieved, not the one the header asked for. */
function percent(ratio: number): string {
  return `-${(ratio * 100).toFixed(1)}%`;
}

function plural(value: number, noun: string): string {
  return `${count(value)} ${noun}${value === 1 ? '' : 's'}`;
}

/**
 * Skipped nodes are only worth a column when something was skipped. Under the
 * default cache mode nothing is, so the count would read `0 cached` on every
 * line, spending width on a number that never changes.
 */
function nodes(stats: PipelineStats): string {
  const fields = [plural(stats.nodesSeen, 'node')];
  if (stats.nodesSkipped > 0) fields.push(`${count(stats.nodesSkipped)} cached`);
  fields.push(`${count(stats.nodesCompressed)} compressed`);
  return fields.join(', ');
}

/** Zero-length input has no prose share to report rather than a zero one. */
function prose(stats: PipelineStats): string {
  if (stats.charsBefore === 0) return '—';
  return `${((stats.charsProse / stats.charsBefore) * 100).toFixed(0)}% prose`;
}

function requestLine(
  accounting: TokenAccounting,
  stats: PipelineStats,
  total: SessionTotal,
): string {
  const saving = [
    `${count(accounting.tokensBefore)} → ${count(accounting.tokensAfter)} tok`,
    percent(accounting.ratio),
    accounting.level,
    nodes(stats),
    prose(stats),
  ].join('  ');
  return `${PREFIX}  ${saving}  —  session ${count(total.tokensSaved)} saved`;
}

function summaryLine(total: SessionTotal): string {
  const requests = plural(total.requests, 'request');
  return `${PREFIX}  session  ${count(total.tokensSaved)} tok saved across ${requests}`;
}

/** A count the response did not carry, rather than a zero it did. */
function billed(value: number | null): string {
  return value === null ? '—' : count(value);
}

/**
 * What the provider billed, as opposed to what Caveman estimated. A cache read
 * means a forwarded prefix still matched; a cache write means it was stored
 * fresh.
 */
function usageLine(usage: UpstreamUsage): string {
  const fields = [
    `${billed(usage.inputTokens)} in`,
    `${billed(usage.outputTokens)} out`,
    `${billed(usage.cacheReadTokens)} cache read`,
    `${billed(usage.cacheCreationTokens)} cache write`,
  ].join('  ');
  return `${PREFIX}  billed  ${fields}`;
}

export function createSavingsReporter(sink: LogSink): SavingsReporter {
  const total: SessionTotal = { tokensSaved: 0, requests: 0 };
  return {
    record(stats) {
      const accounting = accountFor(stats);
      total.tokensSaved += accounting.tokensSaved;
      total.requests += 1;
      sink(requestLine(accounting, stats, total));
    },
    recordUsage(usage) {
      if (!hasUsage(usage)) return;
      sink(usageLine(usage));
    },
    summary() {
      if (total.requests === 0) return null;
      return summaryLine(total);
    },
  };
}
