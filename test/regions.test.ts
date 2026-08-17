import { describe, expect, it } from 'vitest';
import type { Region } from '../src/compression/regions.js';
import { classifyRegions } from '../src/compression/regions.js';

const SAMPLES: readonly string[] = [
  '',
  '   ',
  '\n\n\t ',
  'plain prose with no structure at all',
  'The quick brown fox.',
  '```ts\nconst value = compute(1, 2);\n```',
  '~~~\nraw block\n~~~',
  'text before\n```js\ncode()\n```\ntext after',
  'an unterminated ```fence\nand its body',
  '    indented code line\nand prose after',
  '\ttab indented code',
  'inline `value` code',
  'https://api.example.com/v1/x?y=1&z=2',
  'see https://x.example/a?b=1&c=2 for details',
  'the path src/compression/compress.ts is here',
  './a/b and ../c/d and /etc/hosts',
  'C:\\Users\\me\\file.txt on windows',
  '{"user_id": 4}',
  'a json blob {"a": 1, "b": 2} inline',
  '<Component prop="x" />',
  'html <div class="a">text</div> here',
  'at Foo.bar (app.js:10:5)',
  'File "x.py", line 3',
  '0xDEADBEEF and 550e8400-e29b-41d4-a716-446655440000',
  'version v2.1.0-rc3 and ^14.16.0 and ~1.2.3',
  '| col | col |\n| --- | --- |\n| a | b |',
  '- bullet one\n- bullet two',
  '1. first\n2. second',
  '# Header\n## Subheader\n\nbody text',
  '> quoted line\n> more quote',
  'emoji 👨‍👩‍👧‍👦 and 🇯🇵 flags',
  '日本語のテキストです',
  'café naïve résumé',
  'a\n\nb\n\nc',
  'trailing whitespace   ',
  '   leading whitespace',
  'x'.repeat(3000),
  'mixed\n```\ncode\n```\n| t | t |\nat Foo (a.js:1:2)\nhttps://x.io/a?b=1\nprose the end.',
];

function reconstruct(text: string, regions: readonly Region[]): string {
  return regions.map((region) => text.slice(region.start, region.end)).join('');
}

describe('classifyRegions tiling', () => {
  it('reconstructs the original text exactly from its regions', () => {
    for (const sample of SAMPLES) {
      expect(reconstruct(sample, classifyRegions(sample))).toBe(sample);
    }
  });

  it('starts at 0 and ends at text.length', () => {
    for (const sample of SAMPLES) {
      const regions = classifyRegions(sample);
      if (sample === '') {
        expect(regions).toEqual([]);
        continue;
      }
      expect(regions[0]?.start).toBe(0);
      expect(regions[regions.length - 1]?.end).toBe(sample.length);
    }
  });

  it('tiles without gaps or overlaps', () => {
    for (const sample of SAMPLES) {
      const regions = classifyRegions(sample);
      for (let index = 1; index < regions.length; index += 1) {
        expect(regions[index]?.start).toBe(regions[index - 1]?.end);
      }
    }
  });

  it('emits no empty region', () => {
    for (const sample of SAMPLES) {
      for (const region of classifyRegions(sample)) {
        expect(region.end).toBeGreaterThan(region.start);
      }
    }
  });

  it('is a pure function of its input', () => {
    for (const sample of SAMPLES) {
      expect(classifyRegions(sample)).toEqual(classifyRegions(sample));
    }
  });
});

function protectedText(text: string): string[] {
  return classifyRegions(text)
    .filter((region) => region.kind === 'protected')
    .map((region) => text.slice(region.start, region.end));
}

function isProtected(text: string, needle: string): boolean {
  return protectedText(text).some((span) => span.includes(needle));
}

describe('classifyRegions protection', () => {
  const cases: readonly { name: string; text: string; needle: string }[] = [
    { name: 'a fenced code block', text: '```ts\ncode()\n```', needle: 'code()' },
    { name: 'a tilde fence', text: '~~~\ncode()\n~~~', needle: 'code()' },
    {
      name: 'an indented code block',
      text: 'prose\n\n    indented()\n',
      needle: 'indented()',
    },
    { name: 'inline code', text: 'the `value` here', needle: '`value`' },
    {
      name: 'a URL with a query string',
      text: 'see https://x.example/a?b=1&c=2 now',
      needle: 'https://x.example/a?b=1&c=2',
    },
    {
      name: 'a file path',
      text: 'open src/compression/compress.ts now',
      needle: 'src/compression/compress.ts',
    },
    { name: 'a relative path', text: 'see ./a/b/c.txt here', needle: './a/b/c.txt' },
    {
      name: 'a windows path',
      text: 'open C:\\Users\\me\\a.txt now',
      needle: 'C:\\Users\\me\\a.txt',
    },
    { name: 'a JSON object', text: 'body {"a": 1} sent', needle: '{"a": 1}' },
    {
      name: 'a JSX element',
      text: 'the <Foo bar="x" /> node',
      needle: '<Foo bar="x" />',
    },
    {
      name: 'a stack frame',
      text: 'at Foo.bar (app.js:10:5) failed',
      needle: '(app.js:10:5)',
    },
    { name: 'a python trace line', text: 'File "x.py", line 3', needle: 'File "x.py"' },
    { name: 'a hex literal', text: 'mask 0xDEADBEEF set', needle: '0xDEADBEEF' },
    {
      name: 'a UUID',
      text: 'id 550e8400-e29b-41d4-a716-446655440000 here',
      needle: '550e8400-e29b-41d4-a716-446655440000',
    },
    { name: 'a long digit run', text: 'ref 1234567890 seen', needle: '1234567890' },
    { name: 'a version string', text: 'needs ^14.16.0 exactly', needle: '^14.16.0' },
    { name: 'a table row', text: '| a | b |\n| - | - |', needle: '| a | b |' },
    { name: 'a list bullet', text: '- an item here', needle: '-' },
    { name: 'a header marker', text: '# A Header', needle: '#' },
    { name: 'a block quote marker', text: '> quoted text', needle: '>' },
    {
      name: 'a snake_case identifier',
      text: 'the user_id field',
      needle: 'user_id',
    },
    {
      name: 'a function call',
      text: 'call compute(a, b) now',
      needle: 'compute(a, b)',
    },
    {
      name: 'one line of a pretty-printed object',
      text: '{\n  "idempotency_key": "cart-7731-a",\n}',
      needle: '"idempotency_key": "cart-7731-a",',
    },
    {
      name: 'a string value holding words',
      text: '{\n  "status": "a pending order",\n}',
      needle: '"status": "a pending order",',
    },
    {
      name: 'a boolean value',
      text: '{\n  "has_more": false\n}',
      needle: '"has_more": false',
    },
    {
      name: 'a quoted string in prose',
      text: 'she said "the very large dog" loudly',
      needle: '"the very large dog"',
    },
  ];

  for (const { name, text, needle } of cases) {
    it(`protects ${name}`, () => {
      expect(isProtected(text, needle)).toBe(true);
    });
  }

  it('protects everything between a fence pair, including a URL inside it', () => {
    const text = 'before\n```\nhttps://x.io/a?b=1\n| a | b |\n```\nafter';
    expect(isProtected(text, 'https://x.io/a?b=1')).toBe(true);
    expect(isProtected(text, '| a | b |')).toBe(true);
  });

  it('leaves the prose after a list bullet compressible', () => {
    const regions = classifyRegions('- The first item here');
    const prose = regions
      .filter((region) => region.kind === 'prose')
      .map((region) => '- The first item here'.slice(region.start, region.end));
    expect(prose.join('')).toContain('The first item here');
  });

  it('leaves the prose after a header marker compressible', () => {
    const text = '# The Header';
    const prose = classifyRegions(text)
      .filter((region) => region.kind === 'prose')
      .map((region) => text.slice(region.start, region.end));
    expect(prose.join('')).toContain('The Header');
  });

  it('treats ordinary prose as prose', () => {
    const text = 'The man went to the store and bought some bread.';
    expect(classifyRegions(text)).toEqual([
      { kind: 'prose', start: 0, end: text.length },
    ]);
  });
});
