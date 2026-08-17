const COMPRESS_HEADER = 'X-Caveman-Compress';
const SCOPE_HEADER = 'X-Caveman-Scope';
const SCORER_HEADER = 'X-Caveman-Scorer';
const CLAUDE_MODE_HEADER = 'X-Caveman-Claude-Mode';

const COMPRESS_MIN = 0;
const COMPRESS_MAX = 0.9;

const SCOPE_NAMES = ['messages', 'system', 'tool_results'] as const;

type ScopeName = (typeof SCOPE_NAMES)[number];

const DEFAULT_SCOPE: readonly ScopeName[] = ['messages'];
const DEFAULT_SCORER = 'heuristic';

export const CAVEMAN_HEADER_NAMES = [
  COMPRESS_HEADER,
  SCOPE_HEADER,
  SCORER_HEADER,
  CLAUDE_MODE_HEADER,
] as const;

const CLAUDE_MODES = ['proxy', 'agent'] as const;

/**
 * `proxy` replaces the CLI's agent prompt with the request's system prompt and
 * denies it tools, so the call behaves as much like a plain model call as a
 * CLI session can. `agent` leaves the CLI's own prompt and tools in place.
 */
export type ClaudeMode = (typeof CLAUDE_MODES)[number];

export const DEFAULT_CLAUDE_MODE: ClaudeMode = 'proxy';

export type CompressionScope = Readonly<Record<ScopeName, boolean>>;

export type CompressionPolicy = {
  compress: number;
  scope: CompressionScope;
  scorer: string;
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

function parseCompress(rawValue: string | null): number | PolicyFailure {
  if (rawValue === null) {
    return 0;
  }
  const trimmed = rawValue.trim();
  if (trimmed === '') {
    return failure(COMPRESS_HEADER, rawValue, 'must not be empty');
  }
  const parsed = Number(trimmed);
  if (!Number.isFinite(parsed)) {
    return failure(COMPRESS_HEADER, rawValue, 'must be a finite number');
  }
  if (parsed < COMPRESS_MIN || parsed > COMPRESS_MAX) {
    return failure(
      COMPRESS_HEADER,
      rawValue,
      `must be between ${COMPRESS_MIN} and ${COMPRESS_MAX} inclusive`,
    );
  }
  return parsed;
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

function parseScorer(rawValue: string | null): string | PolicyFailure {
  if (rawValue === null) {
    return DEFAULT_SCORER;
  }
  const trimmed = rawValue.trim();
  if (trimmed === '') {
    return failure(SCORER_HEADER, rawValue, 'must not be empty');
  }
  return trimmed;
}

function isClaudeMode(value: string): value is ClaudeMode {
  return (CLAUDE_MODES as readonly string[]).includes(value);
}

export type ClaudeModeResult = { ok: true; mode: ClaudeMode } | PolicyFailure;

/**
 * Read separately from the compression policy: the mode selects how one adapter
 * runs the CLI, and the pipeline has no use for it.
 */
export function parseClaudeMode(headers: Headers): ClaudeModeResult {
  const rawValue = headers.get(CLAUDE_MODE_HEADER);
  if (rawValue === null) {
    return { ok: true, mode: DEFAULT_CLAUDE_MODE };
  }
  const trimmed = rawValue.trim();
  if (trimmed === '') {
    return failure(CLAUDE_MODE_HEADER, rawValue, 'must not be empty');
  }
  if (!isClaudeMode(trimmed)) {
    return failure(
      CLAUDE_MODE_HEADER,
      rawValue,
      `must be one of ${CLAUDE_MODES.join(', ')}`,
    );
  }
  return { ok: true, mode: trimmed };
}

export function parseCompressionPolicy(headers: Headers): PolicyResult {
  const compress = parseCompress(headers.get(COMPRESS_HEADER));
  if (typeof compress !== 'number') {
    return compress;
  }

  const scopeNames = parseScope(headers.get(SCOPE_HEADER));
  if ('ok' in scopeNames) {
    return scopeNames;
  }

  const scorer = parseScorer(headers.get(SCORER_HEADER));
  if (typeof scorer !== 'string') {
    return scorer;
  }

  return {
    ok: true,
    policy: { compress, scope: buildScope(scopeNames), scorer },
  };
}
