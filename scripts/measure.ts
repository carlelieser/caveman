/**
 * Prints what compression saves over the recorded prompts, per level and per
 * request. The corpus is the one the tests use, so these are the numbers the
 * README quotes.
 */
import type { Level } from '../src/compression/levels.js';
import { LEVEL_NAMES } from '../src/compression/levels.js';
import { ALL_SCOPES } from '../src/ir/walk.js';
import { toIR } from '../src/adapters/anthropic/to-ir.js';
import { runPipeline } from '../src/compression/pipeline.js';
import { accountFor } from '../src/telemetry/accounting.js';
import { PROMPT_FIXTURES } from '../test/fixtures/prompts.js';

type Measurement = {
  name: string;
  tokensBefore: number;
  tokensAfter: number;
  ratio: number;
  proseShare: number;
};

function measure(body: Record<string, unknown>, level: Level): Measurement {
  const result = runPipeline({
    request: toIR(body),
    level,
    scopes: ALL_SCOPES,
    cacheMode: 'ignore',
  });
  const accounting = accountFor(result.stats);
  return {
    name: '',
    tokensBefore: accounting.tokensBefore,
    tokensAfter: accounting.tokensAfter,
    ratio: accounting.ratio,
    proseShare: result.stats.charsProse / result.stats.charsBefore,
  };
}

function percent(value: number): string {
  return `${(value * 100).toFixed(1)}%`;
}

function count(value: number): string {
  return value.toLocaleString('en-US');
}

function printCorpusTotals(): void {
  console.log('corpus');
  for (const level of LEVEL_NAMES) {
    let before = 0;
    let after = 0;
    for (const fixture of PROMPT_FIXTURES) {
      const measurement = measure(fixture.body, level);
      before += measurement.tokensBefore;
      after += measurement.tokensAfter;
    }
    const saved = percent((after - before) / before);
    console.log(`  ${level.padEnd(9)} ${count(before)} → ${count(after)} tok  ${saved}`);
  }
}

function printPerRequest(level: Level): void {
  console.log(`\nby request, at ${level} levels`);
  const rows = PROMPT_FIXTURES.map((fixture) => ({
    ...measure(fixture.body, level),
    name: fixture.name,
  })).sort((left, right) => right.proseShare - left.proseShare);

  for (const row of rows) {
    const prose = `${(row.proseShare * 100).toFixed(0)}%`.padStart(4);
    const saved = percent(-row.ratio).padStart(7);
    console.log(`  ${row.name.slice(0, 48).padEnd(48)} ${prose} prose ${saved}`);
  }
}

printCorpusTotals();
printPerRequest('caveman');
