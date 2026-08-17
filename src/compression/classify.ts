import nlp from 'compromise';
import type { Region } from './regions.js';

export type WordClass =
  | 'determiner'
  | 'preposition'
  | 'conjunction'
  | 'auxiliary'
  | 'copula'
  | 'pronoun'
  | 'adverb'
  | 'adjective'
  | 'noun'
  | 'verb'
  | 'number'
  | 'proper'
  | 'other';

export type ClassifiedWord = {
  text: string;
  start: number;
  end: number;
  wordClass: WordClass;
};

/**
 * Compromise's tags, mapped in priority order. Order is what makes the mapping
 * well-defined: its tags co-occur freely — a pronoun also carries `Noun`, a
 * copula also carries `Verb` and often `Auxiliary` — so the first match wins
 * and the more specific tag has to come first.
 *
 * `Negative` is listed above everything it co-occurs with because dropping
 * `not` inverts the sentence. It resolves to `other`, which no level removes.
 */
const TAG_PRIORITY: readonly (readonly [string, WordClass])[] = [
  ['Negative', 'other'],
  ['QuestionWord', 'other'],
  ['Expression', 'other'],
  ['Emoji', 'other'],
  ['Acronym', 'other'],
  ['Abbreviation', 'other'],
  ['ProperNoun', 'proper'],
  ['Value', 'number'],
  ['Pronoun', 'pronoun'],
  ['Determiner', 'determiner'],
  ['Copula', 'copula'],
  ['Auxiliary', 'auxiliary'],
  ['Modal', 'auxiliary'],
  ['Preposition', 'preposition'],
  ['Conjunction', 'conjunction'],
  ['Adverb', 'adverb'],
  ['Adjective', 'adjective'],
  ['Verb', 'verb'],
  ['Noun', 'noun'],
] as const;

/**
 * How far past the cursor a term's text may be found. Compromise reports the
 * separator it consumed in `pre`, so the text should sit immediately after it;
 * the slack absorbs a separator it rewrote (an unspaced em-dash becomes a
 * hyphen) without letting a repeat of the same word further along be claimed.
 */
const OFFSET_SLACK = 8;

/**
 * Grapheme granularity is defined by UAX #29 independently of locale. Passing
 * `undefined` as the locale keeps the host default from reaching a segmentation
 * that varies between machines, exactly as `tokenize` does.
 */
const GRAPHEME_SEGMENTER = new Intl.Segmenter(undefined, { granularity: 'grapheme' });

type Term = {
  text: string;
  pre: string;
  post: string;
  tags: readonly string[];
};

type TermJson = {
  text?: string;
  pre?: string;
  post?: string;
  tags?: string[];
};

type SentenceJson = {
  terms?: TermJson[];
};

function toTerm(term: TermJson): Term {
  return {
    text: term.text ?? '',
    pre: term.pre ?? '',
    post: term.post ?? '',
    tags: term.tags ?? [],
  };
}

/**
 * Tags are read as a plain array in the order compromise emits them, never
 * through a Set, so nothing here depends on iteration order.
 */
function classOf(tags: readonly string[]): WordClass {
  for (const [tag, wordClass] of TAG_PRIORITY) {
    if (tags.includes(tag)) return wordClass;
  }
  return 'other';
}

function parseTerms(text: string): Term[] {
  const sentences = nlp(text).json({ terms: { tags: true } }) as SentenceJson[];
  return sentences.flatMap((sentence) => (sentence.terms ?? []).map(toTerm));
}

type Cursor = { position: number };

/**
 * Locates a term's text in the original string at or after the cursor.
 *
 * Compromise normalizes as it parses — it collapses non-breaking spaces and
 * rewrites some dashes — so neither its own offsets nor `pre.length` arithmetic
 * can be trusted to land on the right character. Only an exact `indexOf` of the
 * term text counts as a location; anything else returns null and the word is
 * left unclassified rather than sliced at a guessed offset.
 */
function locate(text: string, term: Term, cursor: Cursor): number | null {
  const limit = cursor.position + term.pre.length + term.text.length + OFFSET_SLACK;
  const found = text.indexOf(term.text, cursor.position);
  if (found === -1 || found > limit) return null;
  return found;
}

function advancePast(cursor: Cursor, term: Term): void {
  cursor.position += term.pre.length + term.text.length + term.post.length;
}

/**
 * Offsets of every grapheme-cluster boundary in the text, including 0 and the
 * end. Compromise splits on codepoints, so it can report a word starting in the
 * middle of a ZWJ emoji sequence; a word that begins or ends mid-cluster would
 * leave a fragment behind when its neighbour is dropped.
 */
function graphemeBoundaries(text: string): Set<number> {
  const boundaries = new Set<number>([0, text.length]);
  let offset = 0;
  for (const { segment } of GRAPHEME_SEGMENTER.segment(text)) {
    offset += segment.length;
    boundaries.add(offset);
  }
  return boundaries;
}

/**
 * Keeps only words whose both ends fall on a grapheme boundary. Widening one
 * instead would change its text and break the slice invariant, so a word that
 * straddles a cluster is discarded and its characters stay in the gap, where
 * they are copied through verbatim.
 */
function onGraphemeBoundaries(
  words: readonly ClassifiedWord[],
  boundaries: ReadonlySet<number>,
): ClassifiedWord[] {
  return words.filter((word) => boundaries.has(word.start) && boundaries.has(word.end));
}

/**
 * Classifies one span of prose. Terms whose offset cannot be recovered exactly
 * are dropped from the result, which leaves their text in the assembled output
 * untouched — a lost classification costs savings, never meaning.
 */
function classifyRegion(text: string, region: Region): ClassifiedWord[] {
  const source = text.slice(region.start, region.end);
  const words: ClassifiedWord[] = [];
  const cursor: Cursor = { position: 0 };
  for (const term of parseTerms(source)) {
    if (term.text === '') {
      cursor.position += term.pre.length + term.post.length;
      continue;
    }
    const found = locate(source, term, cursor);
    if (found === null) {
      advancePast(cursor, term);
      continue;
    }
    words.push({
      text: term.text,
      start: region.start + found,
      end: region.start + found + term.text.length,
      wordClass: classOf(term.tags),
    });
    cursor.position = found + term.text.length + term.post.length;
  }
  return onGraphemeBoundaries(words, graphemeBoundaries(text));
}

/**
 * Classifies every word inside the prose regions, in ascending offset order.
 * Protected regions contribute nothing, so no word inside one can ever be
 * selected for removal.
 *
 * Each region is parsed on its own so compromise sees whole sentences and can
 * disambiguate by context — `book` is a verb in "book a flight" and a noun in
 * "the book is here" — while offsets stay relative to a slice whose position in
 * the original string is known.
 *
 * Every returned word satisfies `text.slice(word.start, word.end) === word.text`.
 */
export function classifyWords(
  text: string,
  regions: readonly Region[],
): ClassifiedWord[] {
  const words: ClassifiedWord[] = [];
  for (const region of regions) {
    if (region.kind !== 'prose') continue;
    for (const word of classifyRegion(text, region)) words.push(word);
  }
  return words;
}
