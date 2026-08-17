import { describe, expect, it } from 'vitest';
import type { WordClass } from '../src/compression/classify.js';
import {
  LEVEL_NAMES,
  REMOVABLE,
  isLevel,
  isRemovable,
} from '../src/compression/levels.js';

const NEVER_REMOVABLE: readonly WordClass[] = [
  'noun',
  'verb',
  'number',
  'proper',
  'predicate',
  'other',
];

describe('REMOVABLE', () => {
  it('nests light inside moderate inside caveman', () => {
    for (const wordClass of REMOVABLE.light) {
      expect(REMOVABLE.moderate.has(wordClass)).toBe(true);
    }
    for (const wordClass of REMOVABLE.moderate) {
      expect(REMOVABLE.caveman.has(wordClass)).toBe(true);
    }
  });

  it('grows strictly at each level', () => {
    expect(REMOVABLE.light.size).toBeLessThan(REMOVABLE.moderate.size);
    expect(REMOVABLE.moderate.size).toBeLessThan(REMOVABLE.caveman.size);
  });

  it('removes only determiners at light', () => {
    expect([...REMOVABLE.light]).toEqual(['determiner']);
  });

  it('never removes a content class at any level', () => {
    for (const level of LEVEL_NAMES) {
      for (const wordClass of NEVER_REMOVABLE) {
        expect(isRemovable(level, wordClass)).toBe(false);
      }
    }
  });
});

describe('isLevel', () => {
  it('accepts every named level', () => {
    for (const level of LEVEL_NAMES) {
      expect(isLevel(level)).toBe(true);
    }
  });

  it('rejects a fraction, an unknown name and an empty string', () => {
    expect(isLevel('0.5')).toBe(false);
    expect(isLevel('heuristic')).toBe(false);
    expect(isLevel('')).toBe(false);
    expect(isLevel('LIGHT')).toBe(false);
  });
});
