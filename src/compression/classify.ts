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
  /** An adjective carrying its clause's assertion, as in `connection refused`. */
  | 'predicate'
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

const VERB_TAG = 'Verb';
const ADJECTIVE_TAG = 'Adjective';

/** Added by `markPredicates`; compromise never emits it. */
const PREDICATE_TAG = 'CavemanPredicate';

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
  [PREDICATE_TAG, 'predicate'],
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

/**
 * Compromise splits a contraction into one term per part it expands to, and
 * only the first carries text: `doesn't` is an `Auxiliary` term followed by a
 * text-less `Negative` one. A term with no text has no offset in the source, so
 * its tags belong to the word that produced it.
 */
function mergeTaglessTerms(terms: readonly Term[]): Term[] {
  const merged: Term[] = [];
  for (const term of terms) {
    const previous = merged[merged.length - 1];
    if (term.text === '' && previous !== undefined) {
      merged[merged.length - 1] = {
        ...previous,
        post: previous.post + term.pre + term.post,
        tags: [...previous.tags, ...term.tags],
      };
      continue;
    }
    merged.push(term);
  }
  return merged;
}

/**
 * Compromise tags a past participle `Adjective`, which is right for `an
 * abandoned building` and wrong for `50 requests abandoned`, where the
 * participle is the whole predication and the copula has been left out. A
 * sentence carrying no verb has nothing else to predicate with, so an adjective
 * in one is holding the assertion rather than describing a noun.
 */
function markPredicates(terms: readonly Term[]): Term[] {
  if (terms.some((term) => term.tags.includes(VERB_TAG))) return [...terms];
  return terms.map((term, index) =>
    term.tags.includes(ADJECTIVE_TAG) && followsANoun(terms, index)
      ? { ...term, tags: [...term.tags, PREDICATE_TAG] }
      : term,
  );
}

/**
 * An attributive adjective precedes what it describes and a predicative one
 * follows its subject, which is the only thing separating `a very large dog`
 * from `connection refused` once neither has a verb.
 */
function followsANoun(terms: readonly Term[], index: number): boolean {
  return terms.slice(0, index).some((term) => classOf(term.tags) === 'noun');
}

function parseTerms(text: string): Term[] {
  const sentences = nlp(text).json({ terms: { tags: true } }) as SentenceJson[];
  return sentences.flatMap((sentence) =>
    markPredicates(mergeTaglessTerms((sentence.terms ?? []).map(toTerm))),
  );
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
