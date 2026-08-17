import type { WordClass } from './classify.js';

/** How much grammar a request is willing to lose. */
export type Level = 'light' | 'moderate' | 'caveman';

export const LEVEL_NAMES = ['light', 'moderate', 'caveman'] as const;

/**
 * Classes each level may drop, stated as a total enumeration rather than as a
 * delta from the level below. Nouns, verbs, numbers, proper nouns and `other`
 * appear in none of them, so no level can remove them.
 *
 * The sets are nested — `light ⊂ moderate ⊂ caveman` — which is what makes
 * output length non-increasing as the level rises. Changing any membership
 * changes output bytes for the same input.
 */
export const REMOVABLE: Readonly<Record<Level, ReadonlySet<WordClass>>> = {
  light: new Set<WordClass>(['determiner']),
  moderate: new Set<WordClass>([
    'determiner',
    'preposition',
    'conjunction',
    'auxiliary',
    'copula',
    'pronoun',
  ]),
  caveman: new Set<WordClass>([
    'determiner',
    'preposition',
    'conjunction',
    'auxiliary',
    'copula',
    'pronoun',
    'adverb',
    'adjective',
  ]),
};

export function isLevel(value: string): value is Level {
  return (LEVEL_NAMES as readonly string[]).includes(value);
}

/** True when `level` permits dropping a word of this class. */
export function isRemovable(level: Level, wordClass: WordClass): boolean {
  return REMOVABLE[level].has(wordClass);
}
