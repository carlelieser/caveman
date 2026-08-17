import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { compressText } from '../src/compression/compress.js';
import type { CompressRequest } from '../src/compression/compress.js';
import { heuristicScorer } from '../src/compression/heuristic-scorer.js';
import type { ScoreContext, Scorer } from '../src/compression/scorer.js';
import { tokenize } from '../src/compression/tokenize.js';

const PROJECT_ROOT = fileURLToPath(new URL('..', import.meta.url));

const CONTEXT: ScoreContext = { role: 'user', kind: 'text', blockText: '' };

const PROSE = [
  'The compression proxy sits between a client and the API, so that callers',
  'do not pay for tokens that contribute little to the output. It scores and',
  'drops the lowest ranked tokens from eligible text, then forwards the request',
  'upstream without any change to the client beyond one header.',
].join(' ');

const SAMPLES: readonly string[] = [
  PROSE,
  'hello world',
  'a b c d e f g h i j',
  '日本語のテキストです これはテストです',
  'emoji 👨‍👩‍👧‍👦 survives compression 🇯🇵 intact',
  '```ts\nconst value = compute(alpha, beta);\nreturn value;\n```',
  'multi\n\nparagraph\n\ntext with several lines',
  'café naïve résumé combining marks',
];

const RATIOS: readonly number[] = [0, 0.1, 0.25, 0.5, 0.75, 0.9];

function requestFor(text: string, ratio: number): CompressRequest {
  return {
    text,
    ratio,
    scorer: heuristicScorer,
    context: { ...CONTEXT, blockText: text },
  };
}

function compress(text: string, ratio: number): string {
  return compressText(requestFor(text, ratio)).text;
}

describe('compressText invariants', () => {
  it('is the byte-for-byte identity function at ratio 0', () => {
    for (const sample of SAMPLES) {
      expect(compress(sample, 0)).toBe(sample);
    }
  });

  it('leaves text untouched at ratio 0 even when tokenize would alter it', () => {
    const spaced = '  odd   spacing\t\tand punctuation!!!  ';
    expect(compress(spaced, 0)).toBe(spaced);
  });

  it('never returns output longer than the input', () => {
    for (const sample of SAMPLES) {
      for (const ratio of RATIOS) {
        expect(compress(sample, ratio).length).toBeLessThanOrEqual(sample.length);
      }
    }
  });

  it('never yields longer output as the ratio increases', () => {
    for (const sample of SAMPLES) {
      const lengths = RATIOS.map((ratio) => compress(sample, ratio).length);
      for (let index = 1; index < lengths.length; index += 1) {
        expect(lengths[index] ?? 0).toBeLessThanOrEqual(lengths[index - 1] ?? 0);
      }
    }
  });

  it('survives a scorer that returns no scores at all', () => {
    const silentScorer: Scorer = {
      name: 'silent',
      version: '1.0.0',
      score: () => [],
    };
    const text = 'alpha beta gamma delta';
    const result = compressText({ ...requestFor(text, 0.5), scorer: silentScorer });
    expect(result.text.trim()).not.toBe('');
    expect(result.text.length).toBeLessThanOrEqual(text.length);
  });

  it('always keeps at least one span, whatever the ratio and span count', () => {
    const ratios = [0.5, 0.9, 0.99, 0.999];
    for (let spanCount = 1; spanCount <= 6; spanCount += 1) {
      const text = Array.from({ length: spanCount }, (_u, index) => `w${index}`).join(
        ' ',
      );
      for (const ratio of ratios) {
        const result = compressText(requestFor(text, ratio));
        expect(result.stats.spansOut).toBeGreaterThanOrEqual(1);
        expect(result.text.trim()).not.toBe('');
      }
    }
  });

  it('never produces whitespace-only output from non-empty input', () => {
    for (const sample of SAMPLES) {
      for (const ratio of RATIOS) {
        expect(compress(sample, ratio).trim()).not.toBe('');
      }
    }
  });

  it('passes whitespace-only and empty input through unchanged', () => {
    expect(compress('', 0.5)).toBe('');
    expect(compress('   \n ', 0.5)).toBe('   \n ');
  });

  it('rejects a ratio outside [0, 1) naming the operation and the value', () => {
    expect(() => compress('text', 1)).toThrow(/compressText.*received 1/);
    expect(() => compress('text', -0.1)).toThrow(/compressText/);
    expect(() => compress('text', Number.NaN)).toThrow(/compressText/);
  });

  it('actually drops content at a meaningful ratio', () => {
    const compressed = compress(PROSE, 0.5);
    expect(compressed.length).toBeLessThan(PROSE.length);
    expect(compressed).not.toBe(PROSE);
  });

  it('collapses whitespace left by adjacent drops', () => {
    const compressed = compress(PROSE, 0.5);
    expect(compressed).not.toMatch(/ {2,}/);
  });

  it('reports stats a caller can compute a ratio from', () => {
    const result = compressText(requestFor(PROSE, 0.5));
    expect(result.stats.spansIn).toBe(tokenize(PROSE).length);
    expect(result.stats.spansOut).toBeLessThan(result.stats.spansIn);
    expect(result.stats.charsIn).toBe(PROSE.length);
    expect(result.stats.charsOut).toBe(result.text.length);
    expect(result.stats.isUncompressed).toBe(false);
  });
});

describe('compressText unicode safety', () => {
  it('never emits a lone surrogate', () => {
    const text = 'emoji 👨‍👩‍👧‍👦 and 🇯🇵 and 日本語 mixed with words here';
    for (const ratio of RATIOS) {
      const compressed = compress(text, ratio);
      expect(compressed).toBe(compressed.normalize('NFC').normalize());
      expect(/[\uD800-\uDBFF](?![\uDC00-\uDFFF])/u.test(compressed)).toBe(false);
      expect(/(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(compressed)).toBe(false);
    }
  });

  it('keeps surviving emoji sequences whole', () => {
    const text = 'keep 👨‍👩‍👧‍👦 the family emoji intact when compressing this text';
    const compressed = compress(text, 0.3);
    const hasFamily = compressed.includes('👨‍👩‍👧‍👦');
    const hasPartial = compressed.includes('👨') && !hasFamily;
    expect(hasPartial).toBe(false);
  });

  it('keeps combining marks attached to their base character', () => {
    const text = 'café naïve résumé words that survive a compression pass here';
    const compressed = compress(text, 0.3);
    expect(/^[̀-ͯ]/u.test(compressed)).toBe(false);
  });
});

describe('compressText determinism', () => {
  it('produces byte-identical output across repeated calls', () => {
    for (const sample of SAMPLES) {
      for (const ratio of RATIOS) {
        const first = compress(sample, ratio);
        for (let attempt = 0; attempt < 5; attempt += 1) {
          expect(compress(sample, ratio)).toBe(first);
        }
      }
    }
  });

  it('resolves equal scores by span index rather than by sort order', () => {
    const flatScorer: Scorer = {
      name: 'flat',
      version: '1.0.0',
      score: (spans) => spans.map(() => 1),
    };
    const text = Array.from({ length: 40 }, (_unused, index) => `word${index}`).join(' ');
    const result = compressText({ ...requestFor(text, 0.5), scorer: flatScorer });
    // Equal scores drop the lowest indices first, so the tail survives intact.
    expect(result.text).toBe(
      Array.from({ length: 20 }, (_unused, index) => `word${index + 20}`).join(' '),
    );
  });

  it('produces byte-identical output in a separate Node process', () => {
    const local = SAMPLES.map((sample) => RATIOS.map((ratio) => compress(sample, ratio)));
    const script = [
      "import { compressText } from './src/compression/compress.ts';",
      "import { heuristicScorer } from './src/compression/heuristic-scorer.ts';",
      `const samples = ${JSON.stringify(SAMPLES)};`,
      `const ratios = ${JSON.stringify(RATIOS)};`,
      'const output = samples.map((text) =>',
      '  ratios.map((ratio) => compressText({',
      '    text, ratio, scorer: heuristicScorer,',
      "    context: { role: 'user', kind: 'text', blockText: text },",
      '  }).text),',
      ');',
      'process.stdout.write(JSON.stringify(output));',
    ].join('\n');

    const stdout = execFileSync(
      process.execPath,
      ['--import', 'tsx', '--input-type=module', '-e', script],
      { cwd: PROJECT_ROOT, encoding: 'utf8', env: { ...process.env, NODE_OPTIONS: '' } },
    );

    expect(JSON.parse(stdout)).toEqual(local);
  });
});
