import { describe, expect, it } from 'vitest';
import type { WordClass } from '../src/compression/classify.js';
import { classifyWords } from '../src/compression/classify.js';
import { classifyRegions } from '../src/compression/regions.js';

const SAMPLES: readonly string[] = [
  '',
  '   ',
  'hello world',
  'The man has quickly gone to the very large store.',
  'I need you to book a flight, and the book is on the table.',
  'She is running because they were tired and he could not sleep.',
  'John visited Paris in March 2024 with 3 friends.',
  "Don't stop believing! It's fine.",
  'emoji 👨‍👩‍👧‍👦 survives compression 🇯🇵 intact here',
  '日本語のテキストです これはテストです',
  '中文测试 mixed with English words',
  'café naïve résumé combining marks',
  'á combining mark before the noun',
  'multi\n\nparagraph\n\ntext with several lines',
  'a — b and x…y and “quoted” text',
  'The value is 42 and the id is abc-def.',
  'Here is code:\n```ts\nconst a = the(b);\n```\nand the rest of the prose.',
  'Check src/a/b.ts and https://x.example/a?b=1 for the details.',
  'trailing whitespace   ',
  '   leading whitespace',
  'the the the the the',
  'x'.repeat(2000),
];

function classify(text: string) {
  return classifyWords(text, classifyRegions(text));
}

describe('classifyWords offsets', () => {
  it('slices back to the word text on every sample', () => {
    for (const sample of SAMPLES) {
      for (const word of classify(sample)) {
        expect(sample.slice(word.start, word.end)).toBe(word.text);
      }
    }
  });

  it('never emits an empty word', () => {
    for (const sample of SAMPLES) {
      for (const word of classify(sample)) {
        expect(word.text).not.toBe('');
        expect(word.end).toBeGreaterThan(word.start);
      }
    }
  });

  it('produces words in ascending, non-overlapping offset order', () => {
    for (const sample of SAMPLES) {
      const words = classify(sample);
      for (let index = 1; index < words.length; index += 1) {
        const previous = words[index - 1];
        const current = words[index];
        expect(previous && current && previous.end <= current.start).toBe(true);
      }
    }
  });

  it('keeps offsets inside the text', () => {
    for (const sample of SAMPLES) {
      for (const word of classify(sample)) {
        expect(word.start).toBeGreaterThanOrEqual(0);
        expect(word.end).toBeLessThanOrEqual(sample.length);
      }
    }
  });

  it('never splits a grapheme cluster', () => {
    const text = 'keep 👨‍👩‍👧‍👦 the family and 🇯🇵 and á mark';
    const segmenter = new Intl.Segmenter(undefined, { granularity: 'grapheme' });
    const boundaries = new Set<number>([0, text.length]);
    let offset = 0;
    for (const { segment } of segmenter.segment(text)) {
      offset += segment.length;
      boundaries.add(offset);
    }
    for (const word of classify(text)) {
      expect(boundaries.has(word.start)).toBe(true);
      expect(boundaries.has(word.end)).toBe(true);
    }
  });

  it('is a pure function of its input', () => {
    for (const sample of SAMPLES) {
      expect(classify(sample)).toEqual(classify(sample));
    }
  });
});

function classOf(text: string, word: string): WordClass | undefined {
  return classify(text).find((entry) => entry.text === word)?.wordClass;
}

describe('classifyWords classes', () => {
  it('tags determiners', () => {
    expect(classOf('The man went to the store.', 'The')).toBe('determiner');
  });

  it('tags prepositions', () => {
    expect(classOf('The dog sat on the mat.', 'on')).toBe('preposition');
  });

  it('tags conjunctions', () => {
    expect(classOf('bread and butter', 'and')).toBe('conjunction');
  });

  it('tags copulas', () => {
    expect(classOf('The book is here.', 'is')).toBe('copula');
  });

  it('tags auxiliaries', () => {
    expect(classOf('She has gone home.', 'has')).toBe('auxiliary');
  });

  it('tags pronouns rather than nouns', () => {
    expect(classOf('She went home.', 'She')).toBe('pronoun');
  });

  it('tags adverbs', () => {
    expect(classOf('He quickly left.', 'quickly')).toBe('adverb');
  });

  it('tags adjectives', () => {
    expect(classOf('a very large dog', 'large')).toBe('adjective');
  });

  it('tags numbers', () => {
    expect(classOf('He sent 42 reports.', '42')).toBe('number');
  });

  it('tags proper nouns rather than nouns', () => {
    expect(classOf('John visited Paris.', 'Paris')).toBe('proper');
  });

  it('tags a negation as other so no level can remove it', () => {
    expect(classOf('It did not work.', 'not')).toBe('other');
    expect(classOf('We never saw that.', 'never')).toBe('other');
  });

  it('tags a contracted negation as other, not as the auxiliary it expands to', () => {
    expect(classOf("It doesn't work.", "doesn't")).toBe('other');
    expect(classOf("I can't reproduce it.", "can't")).toBe('other');
    expect(classOf("That won't happen.", "won't")).toBe('other');
    expect(classOf("I haven't tried.", "haven't")).toBe('other');
  });

  it('tags a participle as a predicate when its sentence has no verb', () => {
    expect(classOf('50 requests abandoned', 'abandoned')).toBe('predicate');
    expect(classOf('the build failed', 'failed')).toBe('predicate');
  });

  it('tags the same participle as an adjective when the sentence has a verb', () => {
    expect(classOf('an abandoned building was sold', 'abandoned')).toBe('adjective');
  });

  it('tags an adjective before its noun as an adjective, verb or not', () => {
    expect(classOf('a very large dog', 'large')).toBe('adjective');
    expect(classOf('the broken pipe', 'broken')).toBe('adjective');
  });

  it('still tags an uncontracted auxiliary as an auxiliary', () => {
    expect(classOf('It does work.', 'does')).toBe('auxiliary');
    expect(classOf('I have tried.', 'have')).toBe('auxiliary');
  });

  it('disambiguates "book" by sentence context', () => {
    const text = 'I need you to book a flight, and the book is on the table.';
    const books = classify(text).filter((word) => word.text === 'book');
    expect(books).toHaveLength(2);
    expect(books[0]?.wordClass).toBe('verb');
    expect(books[1]?.wordClass).toBe('noun');
  });

  it('classifies no word inside a protected region', () => {
    const text = 'Here is code:\n```ts\nconst the = a(b);\n```\nand the rest.';
    const regions = classifyRegions(text);
    const protectedRegions = regions.filter((region) => region.kind === 'protected');
    for (const word of classifyWords(text, regions)) {
      for (const region of protectedRegions) {
        const overlaps = word.start < region.end && word.end > region.start;
        expect(overlaps).toBe(false);
      }
    }
  });

  it('returns nothing when every region is protected', () => {
    const text = '```ts\nconst value = compute(1, 2);\n```';
    expect(classifyWords(text, classifyRegions(text))).toEqual([]);
  });
});
