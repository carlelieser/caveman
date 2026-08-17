import type { Span } from './tokenize.js';

/** Where the text came from, so a scorer can weigh roles differently. */
export type ScoreRole = 'user' | 'assistant' | 'system';

/** Which kind of IR node the text was taken from. */
export type ScoreKind = 'text' | 'tool_result';

/**
 * Everything a scorer may read beyond the spans themselves. Kept minimal and
 * additive: new optional fields extend it without breaking existing scorers.
 */
export type ScoreContext = {
  role: ScoreRole;
  kind: ScoreKind;
  /** The full block text the spans index into. */
  blockText: string;
};

export type Scorer = {
  name: string;
  version: string;
  /** One score per span, in span order. Higher means keep. */
  score(spans: Span[], context: ScoreContext): number[];
};

export type ScorerLookupFailure = {
  ok: false;
  requested: string;
  available: readonly string[];
};

export type ScorerLookupResult = { ok: true; scorer: Scorer } | ScorerLookupFailure;

export type ScorerRegistry = {
  find(name: string): ScorerLookupResult;
  require(name: string): Scorer;
  names(): readonly string[];
};

function sortedNames(scorers: readonly Scorer[]): readonly string[] {
  return scorers.map((scorer) => scorer.name).sort();
}

function findDuplicate(scorers: readonly Scorer[]): string | null {
  const seen = new Set<string>();
  for (const scorer of scorers) {
    if (seen.has(scorer.name)) {
      return scorer.name;
    }
    seen.add(scorer.name);
  }
  return null;
}

/**
 * Builds a registry over the given scorers. Lookup order never depends on Map
 * iteration, so the same name resolves identically in every process.
 */
export function createScorerRegistry(scorers: readonly Scorer[]): ScorerRegistry {
  const duplicate = findDuplicate(scorers);
  if (duplicate !== null) {
    throw new Error(
      `createScorerRegistry: duplicate scorer name "${duplicate}" in registry`,
    );
  }
  const available = sortedNames(scorers);
  const byName = new Map(scorers.map((scorer) => [scorer.name, scorer]));

  function find(name: string): ScorerLookupResult {
    const scorer = byName.get(name);
    if (scorer === undefined) {
      return { ok: false, requested: name, available };
    }
    return { ok: true, scorer };
  }

  function require(name: string): Scorer {
    const result = find(name);
    if (!result.ok) {
      throw new Error(
        `resolveScorer: unknown scorer "${name}"; available: ${available.join(', ')}`,
      );
    }
    return result.scorer;
  }

  return { find, require, names: () => available };
}
