import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { compressText } from '../src/compression/compress.js';
import type { CompressContext, CompressRequest } from '../src/compression/compress.js';
import type { Level } from '../src/compression/levels.js';

const PROJECT_ROOT = fileURLToPath(new URL('..', import.meta.url));

const CONTEXT: CompressContext = { role: 'user', kind: 'text' };

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
  'The man has quickly gone to the very large store.',
  'Check src/compression/compress.ts and https://x.example/a?b=1 for the rest.',
];

const LEVELS: readonly Level[] = ['light', 'moderate', 'caveman'];

function requestFor(text: string, level: Level): CompressRequest {
  return { text, level, context: CONTEXT };
}

function compress(text: string, level: Level): string {
  return compressText(requestFor(text, level)).text;
}

describe('compressText invariants', () => {
  it('never returns output longer than the input', () => {
    for (const sample of SAMPLES) {
      for (const level of LEVELS) {
        expect(compress(sample, level).length).toBeLessThanOrEqual(sample.length);
      }
    }
  });

  /**
   * Levels nest, so a rising level can only remove more — except where the
   * higher level would remove every word in the block and the empty-block guard
   * returns the original instead. That fallback is the one case where output
   * grows with the level, and it is the safety rule winning over the saving.
   */
  it('never yields longer output as the level rises, unless the guard fired', () => {
    for (const sample of SAMPLES) {
      const results = LEVELS.map((level) => compressText(requestFor(sample, level)));
      for (let index = 1; index < results.length; index += 1) {
        const current = results[index];
        const previous = results[index - 1];
        if (current === undefined || previous === undefined) continue;
        if (current.stats.isUncompressed) continue;
        expect(current.text.length).toBeLessThanOrEqual(previous.text.length);
      }
    }
  });

  it('removes at least as many classes as the level below, on ordinary prose', () => {
    // No level empties this block, so nesting shows through directly.
    const lengths = LEVELS.map((level) => compress(PROSE, level).length);
    for (let index = 1; index < lengths.length; index += 1) {
      expect(lengths[index] ?? 0).toBeLessThanOrEqual(lengths[index - 1] ?? 0);
    }
  });

  it('never produces whitespace-only output from non-empty input', () => {
    for (const sample of SAMPLES) {
      for (const level of LEVELS) {
        expect(compress(sample, level).trim()).not.toBe('');
      }
    }
  });

  it('passes whitespace-only and empty input through unchanged', () => {
    for (const level of LEVELS) {
      expect(compress('', level)).toBe('');
      expect(compress('   \n ', level)).toBe('   \n ');
    }
  });

  it('returns a block of only removable words unchanged', () => {
    // At moderate and above every word here is removable, so removing the class
    // would empty the block; the API rejects an empty text block. At light only
    // the determiner goes, which is an ordinary compression.
    for (const level of ['moderate', 'caveman'] as const) {
      expect(compress('to the', level)).toBe('to the');
      expect(compress('of the', level)).toBe('of the');
    }
    expect(compress('the the the', 'light')).toBe('the the the');
  });

  it('actually drops content at every level', () => {
    for (const level of LEVELS) {
      const compressed = compress(PROSE, level);
      expect(compressed.length).toBeLessThan(PROSE.length);
      expect(compressed).not.toBe(PROSE);
    }
  });

  it('collapses whitespace left by adjacent drops', () => {
    for (const level of LEVELS) {
      expect(compress(PROSE, level)).not.toMatch(/ {2,}/);
    }
  });

  it('does not indent a block whose opening word was dropped', () => {
    expect(compress('The man went to the store.', 'light')).toBe('man went to store.');
  });

  it('reports stats a caller can compute a ratio from', () => {
    const result = compressText(requestFor(PROSE, 'caveman'));
    expect(result.stats.wordsOut).toBeLessThan(result.stats.wordsIn);
    expect(result.stats.charsIn).toBe(PROSE.length);
    expect(result.stats.charsOut).toBe(result.text.length);
    expect(result.stats.isUncompressed).toBe(false);
  });
});

describe('compressText grammatical selection', () => {
  it('removes determiners at every level', () => {
    for (const level of LEVELS) {
      expect(compress('The man went to the store.', level)).not.toContain(' the ');
    }
  });

  it('keeps prepositions and pronouns at light but drops them at moderate', () => {
    const text = 'She went to the store with him.';
    expect(compress(text, 'light')).toContain('to');
    expect(compress(text, 'moderate')).not.toMatch(/\bto\b/);
  });

  it('keeps adjectives and adverbs until caveman', () => {
    const text = 'The very large dog quickly ate the food.';
    expect(compress(text, 'moderate')).toContain('large');
    expect(compress(text, 'moderate')).toContain('quickly');
    expect(compress(text, 'caveman')).not.toContain('large');
    expect(compress(text, 'caveman')).not.toContain('quickly');
  });

  it('keeps "book" as a noun and drops nothing of it as a verb, in one text', () => {
    // Both occurrences are content words, so neither is removable; what the
    // classifier must get right is that they are tagged differently.
    const text = 'I need you to book a flight, and the book is on the table.';
    const compressed = compress(text, 'caveman');
    expect(compressed.match(/\bbook\b/gu)).toHaveLength(2);
  });

  it('never removes a negation', () => {
    const text = 'The change did not break the very large test suite.';
    for (const level of LEVELS) {
      expect(compress(text, level)).toContain('not');
    }
  });

  it('keeps nouns, verbs, numbers and proper nouns at every level', () => {
    const text = 'John quickly sent the 42 large reports to Paris on Tuesday.';
    for (const level of LEVELS) {
      const compressed = compress(text, level);
      expect(compressed).toContain('John');
      expect(compressed).toContain('Paris');
      expect(compressed).toContain('42');
      expect(compressed).toContain('reports');
    }
  });

  it('preserves terminal sentence punctuation when the last word is dropped', () => {
    const compressed = compress('The dog sat on the mat.', 'moderate');
    expect(compressed.endsWith('.')).toBe(true);
  });

  it('keeps sentence boundaries across a multi-sentence block', () => {
    const compressed = compress(
      'The man went to the store. The dog sat on the mat.',
      'moderate',
    );
    expect(compressed.match(/\./gu)).toHaveLength(2);
  });
});

describe('compressText protects non-prose', () => {
  const cases: readonly { name: string; text: string; kept: string }[] = [
    {
      name: 'a fenced code block',
      text: 'Here is the code:\n```ts\nconst a = the(b, c);\n```\nand the rest.',
      kept: '```ts\nconst a = the(b, c);\n```',
    },
    {
      name: 'unfenced JSON',
      text: 'The payload is {"user_id": 4, "the": 1} in the body.',
      kept: '{"user_id": 4, "the": 1}',
    },
    {
      name: 'a stack trace line',
      text: 'It failed:\n    at Foo.bar (app.js:10:5)\nand the cause is unclear.',
      kept: 'at Foo.bar (app.js:10:5)',
    },
    {
      name: 'a URL with a query string',
      text: 'Fetch https://api.example.com/v1/x?y=1&z=2 from the server.',
      kept: 'https://api.example.com/v1/x?y=1&z=2',
    },
    {
      name: 'a file path',
      text: 'Open the file src/compression/compress.ts in the editor.',
      kept: 'src/compression/compress.ts',
    },
    {
      name: 'a UUID',
      text: 'The record id is 550e8400-e29b-41d4-a716-446655440000 in the table.',
      kept: '550e8400-e29b-41d4-a716-446655440000',
    },
    {
      name: 'a markdown table row',
      text: '| the col | the other |\n| --- | --- |\nThe table is above.',
      kept: '| the col | the other |\n| --- | --- |',
    },
    {
      name: 'a hex literal',
      text: 'The mask is 0xDEADBEEF in the register.',
      kept: '0xDEADBEEF',
    },
    {
      name: 'a version string',
      text: 'It needs the version ^14.16.0 from the registry.',
      kept: '^14.16.0',
    },
    {
      name: 'a JSX element',
      text: 'Render the <Component prop="the value" /> in the tree.',
      kept: '<Component prop="the value" />',
    },
  ];

  for (const { name, text, kept } of cases) {
    it(`keeps ${name} byte-identical at every level`, () => {
      for (const level of LEVELS) {
        expect(compress(text, level)).toContain(kept);
      }
    });
  }

  it('leaves a block that is entirely a code fence untouched', () => {
    const fenced = '```ts\nconst value = compute(alpha, beta);\nreturn value;\n```';
    for (const level of LEVELS) {
      expect(compress(fenced, level)).toBe(fenced);
    }
  });
});

describe('compressText unicode safety', () => {
  it('never emits a lone surrogate', () => {
    const text = 'emoji 👨‍👩‍👧‍👦 and 🇯🇵 and 日本語 mixed with words here';
    for (const level of LEVELS) {
      const compressed = compress(text, level);
      expect(/[\uD800-\uDBFF](?![\uDC00-\uDFFF])/u.test(compressed)).toBe(false);
      expect(/(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(compressed)).toBe(false);
    }
  });

  it('keeps surviving emoji sequences whole', () => {
    const text = 'keep 👨‍👩‍👧‍👦 the family emoji intact when compressing this text';
    for (const level of LEVELS) {
      const compressed = compress(text, level);
      const hasFamily = compressed.includes('👨‍👩‍👧‍👦');
      const hasPartial = compressed.includes('👨') && !hasFamily;
      expect(hasPartial).toBe(false);
    }
  });

  it('keeps an emoji next to a dropped word', () => {
    // The emoji sits in the gap a dropped word leaves behind. Stripping that
    // gap as stranded punctuation would take the emoji with it: it is a symbol
    // by category but content by meaning.
    for (const level of LEVELS) {
      expect(compress('The 👨‍👩‍👧‍👦 family is here.', level)).toContain('👨‍👩‍👧‍👦');
      expect(compress('🇯🇵 is the flag.', level)).toContain('🇯🇵');
    }
  });

  it('keeps combining marks attached to their base character', () => {
    const text = 'café naïve résumé words that survive a compression pass here';
    for (const level of LEVELS) {
      expect(/^[̀-ͯ]/u.test(compress(text, level))).toBe(false);
    }
  });
});

describe('compressText determinism', () => {
  it('produces byte-identical output across repeated calls', () => {
    for (const sample of SAMPLES) {
      for (const level of LEVELS) {
        const first = compress(sample, level);
        for (let attempt = 0; attempt < 5; attempt += 1) {
          expect(compress(sample, level)).toBe(first);
        }
      }
    }
  });

  it('produces byte-identical output in a separate Node process', () => {
    const local = SAMPLES.map((sample) => LEVELS.map((level) => compress(sample, level)));
    const script = [
      "import { compressText } from './src/compression/compress.ts';",
      `const samples = ${JSON.stringify(SAMPLES)};`,
      `const levels = ${JSON.stringify(LEVELS)};`,
      'const output = samples.map((text) =>',
      '  levels.map((level) => compressText({',
      "    text, level, context: { role: 'user', kind: 'text' },",
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
