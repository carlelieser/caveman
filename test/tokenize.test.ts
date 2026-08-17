import { describe, expect, it } from 'vitest';
import { reconstruct, tokenize } from '../src/compression/tokenize.js';

const SAMPLES: readonly string[] = [
  '',
  '   ',
  '\n\n\t ',
  'hello world',
  'The quick brown fox.',
  "don't stop believing",
  'a\n\nb\nc',
  'trailing whitespace   ',
  '   leading whitespace',
  '日本語のテキストです',
  '中文测试 mixed with English',
  'emoji 👨‍👩‍👧‍👦 family and 🇯🇵 flag',
  'combining é mark',
  'café naïve résumé',
  '```ts\nconst value = compute(1, 2);\n```',
  'punctuation!!! ??? ...',
  'snake_case camelCase kebab-case dotted.path',
  'https://example.com/path?query=1',
  'x'.repeat(5000),
  'multiple    internal     spaces',
];

describe('tokenize', () => {
  it('reconstructs the original text exactly from spans and their gaps', () => {
    for (const sample of SAMPLES) {
      expect(reconstruct(sample, tokenize(sample))).toBe(sample);
    }
  });

  it('reports offsets that slice back to the span text', () => {
    for (const sample of SAMPLES) {
      for (const span of tokenize(sample)) {
        expect(sample.slice(span.start, span.end)).toBe(span.text);
      }
    }
  });

  it('produces spans in ascending, non-overlapping offset order', () => {
    for (const sample of SAMPLES) {
      const spans = tokenize(sample);
      for (let index = 1; index < spans.length; index += 1) {
        const previous = spans[index - 1];
        const current = spans[index];
        expect(previous && current && previous.end <= current.start).toBe(true);
      }
    }
  });

  it('returns no spans for empty or whitespace-only text', () => {
    expect(tokenize('')).toEqual([]);
    expect(tokenize('   ')).toEqual([]);
    expect(tokenize('\n\t\r\n')).toEqual([]);
  });

  it('covers words and leaves whitespace to the gaps between spans', () => {
    expect(tokenize('hello world').map((span) => span.text)).toEqual(['hello', 'world']);
  });

  it('leaves trailing sentence punctuation out of the span', () => {
    expect(tokenize('end. next').map((span) => span.text)).toEqual(['end', 'next']);
    expect(tokenize('really?!').map((span) => span.text)).toEqual(['really']);
  });

  it('keeps intraword punctuation inside a word', () => {
    expect(tokenize("don't").map((span) => span.text)).toEqual(["don't"]);
    expect(tokenize('foo_bar.baz').map((span) => span.text)).toEqual(['foo_bar.baz']);
  });

  it('emits standalone punctuation runs as gaps, not spans', () => {
    expect(tokenize('--- ***').map((span) => span.text)).toEqual([]);
  });

  it('splits unspaced scripts per grapheme so CJK text yields tokens', () => {
    expect(tokenize('日本語').map((span) => span.text)).toEqual(['日', '本', '語']);
  });

  it('never splits an emoji sequence mid-codepoint', () => {
    const spans = tokenize('👨‍👩‍👧‍👦');
    expect(spans.map((span) => span.text)).toEqual(['👨‍👩‍👧‍👦']);
    expect([...(spans[0]?.text ?? '')].length).toBeGreaterThan(1);
  });

  it('keeps a combining mark attached to its base character', () => {
    const spans = tokenize('éclair');
    expect(spans.map((span) => span.text)).toEqual(['éclair']);
  });

  it('treats newlines as gaps and keeps code block content as spans', () => {
    const source = '```ts\nconst value = 1;\n```';
    const spans = tokenize(source);
    expect(spans.map((span) => span.text)).toEqual(['ts', 'const', 'value', '1']);
    expect(reconstruct(source, spans)).toBe(source);
  });

  it('keeps a very long token as a single span', () => {
    const long = 'y'.repeat(10000);
    const spans = tokenize(long);
    expect(spans).toHaveLength(1);
    expect(spans[0]?.text).toBe(long);
  });

  it('is a pure function of its input', () => {
    const source = 'repeatable tokenization of text';
    expect(tokenize(source)).toEqual(tokenize(source));
  });
});
