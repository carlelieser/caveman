import type { PipelineStats } from '../compression/pipeline.js';
import type { TokenAccounting } from './accounting.js';
import type { LogSink } from './logging.js';
import { accountFor } from './accounting.js';

const PREFIX = 'caveman';

export type SavingsReporter = {
  record(stats: PipelineStats): void;
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

function nodes(stats: PipelineStats): string {
  return `${plural(stats.nodesSeen, 'node')}, ${count(stats.nodesSkipped)} cached`;
}

function requestLine(
  accounting: TokenAccounting,
  stats: PipelineStats,
  total: SessionTotal,
): string {
  const saving = [
    `${count(accounting.tokensBefore)} → ${count(accounting.tokensAfter)} tok`,
    percent(accounting.ratio),
    accounting.scorer,
    nodes(stats),
  ].join('  ');
  return `${PREFIX}  ${saving}  —  session ${count(total.tokensSaved)} saved`;
}

function summaryLine(total: SessionTotal): string {
  const requests = plural(total.requests, 'request');
  return `${PREFIX}  session  ${count(total.tokensSaved)} tok saved across ${requests}`;
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
    summary() {
      if (total.requests === 0) return null;
      return summaryLine(total);
    },
  };
}
