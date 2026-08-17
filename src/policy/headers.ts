import type { CacheMode } from '../compression/pipeline.js';
import type { Level } from '../compression/levels.js';
import { LEVEL_NAMES, isLevel } from '../compression/levels.js';

const COMPRESS_HEADER = 'X-Caveman-Compress';
const SCOPE_HEADER = 'X-Caveman-Scope';
const CACHE_HEADER = 'X-Caveman-Cache';

const OFF_VALUE = 'off';

const CACHE_MODE_NAMES = ['ignore', 'respect'] as const;

/**
 * Compressing a cached prefix is the default. The compressed bytes are stable
 * across turns and a growing conversation rewrites its tail anyway, so the
 * alternative buys an unchanged prefix at the price of never compressing the
 * largest part of the request.
 */
const DEFAULT_CACHE_MODE: CacheMode = 'ignore';

const SCOPE_NAMES = ['messages', 'system', 'tool_results'] as const;

type ScopeName = (typeof SCOPE_NAMES)[number];

/**
 * Every scope. A system prompt and its tool results are routinely larger than
 * the conversation they frame, so a default of `messages` alone leaves most of
 * a request untouched. Narrowing is done by naming the scopes to keep.
 */
const DEFAULT_SCOPE: readonly ScopeName[] = ['messages', 'system', 'tool_results'];

export const CAVEMAN_HEADER_NAMES = [
  COMPRESS_HEADER,
  SCOPE_HEADER,
  CACHE_HEADER,
] as const;

export type CompressionScope = Readonly<Record<ScopeName, boolean>>;

export type CompressionPolicy = {
  /** Null when compression is off, which is the default. */
  level: Level | null;
  scope: CompressionScope;
  cacheMode: CacheMode;
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

function isCacheMode(value: string): value is CacheMode {
  return (CACHE_MODE_NAMES as readonly string[]).includes(value);
}

function parseCacheMode(rawValue: string | null): CacheMode | PolicyFailure {
  if (rawValue === null) {
    return DEFAULT_CACHE_MODE;
  }
  const trimmed = rawValue.trim();
  if (trimmed === '') {
    return failure(CACHE_HEADER, rawValue, 'must not be empty');
  }
  const normalized = trimmed.toLowerCase();
  if (!isCacheMode(normalized)) {
    return failure(
      CACHE_HEADER,
      rawValue,
      `must be one of ${CACHE_MODE_NAMES.join(', ')}`,
    );
  }
  return normalized;
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

  const cacheMode = parseCacheMode(headers.get(CACHE_HEADER));
  if (isFailure(cacheMode)) {
    return cacheMode;
  }

  return {
    ok: true,
    policy: { level, scope: buildScope(scopeNames), cacheMode },
  };
}
