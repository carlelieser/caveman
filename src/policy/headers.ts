import type { Level } from '../compression/levels.js';
import { LEVEL_NAMES, isLevel } from '../compression/levels.js';

const COMPRESS_HEADER = 'X-Caveman-Compress';
const SCOPE_HEADER = 'X-Caveman-Scope';

const OFF_VALUE = 'off';

const SCOPE_NAMES = ['messages', 'system', 'tool_results'] as const;

type ScopeName = (typeof SCOPE_NAMES)[number];

const DEFAULT_SCOPE: readonly ScopeName[] = ['messages'];

export const CAVEMAN_HEADER_NAMES = [COMPRESS_HEADER, SCOPE_HEADER] as const;

export type CompressionScope = Readonly<Record<ScopeName, boolean>>;

export type CompressionPolicy = {
  /** Null when compression is off, which is the default. */
  level: Level | null;
  scope: CompressionScope;
};

export type PolicyFailure = {
  ok: false;
  header: string;
  value: string;
  reason: string;
};

export type PolicyResult = { ok: true; policy: CompressionPolicy } | PolicyFailure;

function buildScope(names: readonly ScopeName[]): CompressionScope {
  const scope = {} as Record<ScopeName, boolean>;
  for (const name of SCOPE_NAMES) {
    scope[name] = names.includes(name);
  }
  return scope;
}

function isScopeName(value: string): value is ScopeName {
  return (SCOPE_NAMES as readonly string[]).includes(value);
}

function failure(header: string, value: string, reason: string): PolicyFailure {
  return { ok: false, header, value, reason };
}

const ACCEPTED_VALUES = [OFF_VALUE, ...LEVEL_NAMES].join(', ');

/**
 * Levels are named, never numeric. A fraction used to mean "drop this share of
 * the tokens"; nothing removes a share of a class, so a number is now a
 * malformed value rather than a legacy spelling to be mapped onto a level.
 */
function parseCompress(rawValue: string | null): Level | null | PolicyFailure {
  if (rawValue === null) {
    return null;
  }
  const trimmed = rawValue.trim();
  if (trimmed === '') {
    return failure(COMPRESS_HEADER, rawValue, 'must not be empty');
  }
  const normalized = trimmed.toLowerCase();
  if (normalized === OFF_VALUE) {
    return null;
  }
  if (!isLevel(normalized)) {
    return failure(COMPRESS_HEADER, rawValue, `must be one of ${ACCEPTED_VALUES}`);
  }
  return normalized;
}

function parseScope(rawValue: string | null): readonly ScopeName[] | PolicyFailure {
  if (rawValue === null) {
    return DEFAULT_SCOPE;
  }
  const trimmed = rawValue.trim();
  if (trimmed === '') {
    return failure(SCOPE_HEADER, rawValue, 'must not be empty');
  }
  const entries = trimmed.split(',').map((entry) => entry.trim());
  const seen = new Set<string>();
  const names: ScopeName[] = [];
  for (const entry of entries) {
    if (entry === '') {
      return failure(SCOPE_HEADER, rawValue, 'must not contain empty members');
    }
    if (!isScopeName(entry)) {
      return failure(SCOPE_HEADER, rawValue, `unknown scope member "${entry}"`);
    }
    if (seen.has(entry)) {
      return failure(SCOPE_HEADER, rawValue, `duplicate scope member "${entry}"`);
    }
    seen.add(entry);
    names.push(entry);
  }
  return names;
}

function isFailure(value: unknown): value is PolicyFailure {
  return typeof value === 'object' && value !== null && 'ok' in value;
}

export function parseCompressionPolicy(headers: Headers): PolicyResult {
  const level = parseCompress(headers.get(COMPRESS_HEADER));
  if (isFailure(level)) {
    return level;
  }

  const scopeNames = parseScope(headers.get(SCOPE_HEADER));
  if (isFailure(scopeNames)) {
    return scopeNames;
  }

  return {
    ok: true,
    policy: { level, scope: buildScope(scopeNames) },
  };
}
