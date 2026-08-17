/**
 * Measures compression over the recorded prompts. `--savings` prints what it
 * saves, per level and per request; the corpus is the one the tests use, so
 * these are the numbers the README quotes. `--performance` prints how long it
 * takes. Savings is the default.
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

/**
 * Trials per timed subject. `compromise` tags on first use and the JIT needs a
 * few passes to settle, so a single run measures warmup rather than the work.
 */
const WARMUP_RUNS = 5;
const TIMED_RUNS = 20;

type Timing = {
  name: string;
  median: number;
  min: number;
  max: number;
  /** Prose characters the run had to classify, the driver of the cost. */
  charsProse: number;
};

/**
 * Times the pipeline alone. `toIR` runs once up front and its result is reused,
 * because parsing the wire format is the server's cost on every request whether
 * or not compression is on, and including it would understate the share
 * compression adds.
 */
function time(name: string, body: Record<string, unknown>, level: Level): Timing {
  const request = toIR(body);
  const run = (): number => {
    const started = performance.now();
    runPipeline({ request, level, scopes: ALL_SCOPES, cacheMode: 'ignore' });
    return performance.now() - started;
  };

  for (let index = 0; index < WARMUP_RUNS; index += 1) run();

  const samples: number[] = [];
  for (let index = 0; index < TIMED_RUNS; index += 1) samples.push(run());
  samples.sort((left, right) => left - right);

  const stats = runPipeline({ request, level, scopes: ALL_SCOPES, cacheMode: 'ignore' }).stats;
  return {
    name,
    // Median, not mean: a GC pause during one trial should not move the number.
    median: samples[Math.floor(samples.length / 2)] ?? 0,
    min: samples[0] ?? 0,
    max: samples[samples.length - 1] ?? 0,
    charsProse: stats.charsProse,
  };
}

function milliseconds(value: number): string {
  return `${value.toFixed(2)}ms`;
}

function printLevelTimings(): void {
  console.log(`corpus, ${TIMED_RUNS} runs each after ${WARMUP_RUNS} warmup`);
  for (const level of LEVEL_NAMES) {
    let median = 0;
    let charsProse = 0;
    for (const fixture of PROMPT_FIXTURES) {
      const timing = time(fixture.name, fixture.body, level);
      median += timing.median;
      charsProse += timing.charsProse;
    }
    // Throughput over prose characters, which is what the classifier walks.
    const perSecond = charsProse / (median / 1000);
    console.log(
      `  ${level.padEnd(9)} ${milliseconds(median).padStart(9)}` +
        `  ${count(Math.round(perSecond / 1000))}k prose chars/s`,
    );
  }
}

function printPerRequestTimings(level: Level): void {
  console.log(`\nby request, at ${level} levels`);
  const rows = PROMPT_FIXTURES.map((fixture) =>
    time(fixture.name, fixture.body, level),
  ).sort((left, right) => right.median - left.median);

  for (const row of rows) {
    const median = milliseconds(row.median).padStart(9);
    const spread = `${milliseconds(row.min)}–${milliseconds(row.max)}`.padStart(18);
    console.log(`  ${row.name.slice(0, 48).padEnd(48)} ${median} ${spread}`);
  }
}

const MODES = ['savings', 'performance'] as const;
type Mode = (typeof MODES)[number];

function parseMode(argv: readonly string[]): Mode {
  const flags = argv.filter((argument) => argument.startsWith('--'));
  if (flags.length === 0) return 'savings';
  if (flags.length > 1) {
    console.error(`measure: give one of ${MODES.map((mode) => `--${mode}`).join(', ')}`);
    process.exit(1);
  }
  const requested = flags[0]?.slice(2);
  const mode = MODES.find((candidate) => candidate === requested);
  if (mode === undefined) {
    console.error(
      `measure: unknown flag "${flags[0]}", expected ${MODES.map((name) => `--${name}`).join(' or ')}`,
    );
    process.exit(1);
  }
  return mode;
}

if (parseMode(process.argv.slice(2)) === 'performance') {
  printLevelTimings();
  printPerRequestTimings('caveman');
} else {
  printCorpusTotals();
  printPerRequest('caveman');
}
