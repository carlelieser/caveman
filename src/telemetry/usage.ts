/**
 * Token counts as the provider billed them, read from the upstream response.
 *
 * Caveman's own accounting estimates characters locally; these are the numbers
 * the invoice is built from. The cache fields are the ones that say whether a
 * forwarded prefix still matched: a read is billed at a fraction of the base
 * rate, a creation at a premium, so a prefix Caveman rewrote shows up here as
 * creations replacing reads.
 */
export type UpstreamUsage = {
  inputTokens: number | null;
  outputTokens: number | null;
  cacheReadTokens: number | null;
  cacheCreationTokens: number | null;
};

const USAGE_KEYS = {
  input: 'input_tokens',
  output: 'output_tokens',
  cacheRead: 'cache_read_input_tokens',
  cacheCreation: 'cache_creation_input_tokens',
} as const;

export function emptyUsage(): UpstreamUsage {
  return {
    inputTokens: null,
    outputTokens: null,
    cacheReadTokens: null,
    cacheCreationTokens: null,
  };
}

export function hasUsage(usage: UpstreamUsage): boolean {
  return (
    usage.inputTokens !== null ||
    usage.outputTokens !== null ||
    usage.cacheReadTokens !== null ||
    usage.cacheCreationTokens !== null
  );
}

function readCount(source: Record<string, unknown>, key: string): number | null {
  const value = source[key];
  return typeof value === 'number' && Number.isFinite(value) ? value : null;
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return null;
  return value as Record<string, unknown>;
}

/**
 * Merges a `usage` object into what has been seen so far. A streamed response
 * reports usage twice — `message_start` carries the input and cache counts with
 * `output_tokens` still at its initial value, and `message_delta` carries the
 * final output count and nothing else — so a later field only overwrites an
 * earlier one when it is actually present.
 */
function mergeUsage(into: UpstreamUsage, source: Record<string, unknown>): UpstreamUsage {
  const input = readCount(source, USAGE_KEYS.input);
  const output = readCount(source, USAGE_KEYS.output);
  const cacheRead = readCount(source, USAGE_KEYS.cacheRead);
  const cacheCreation = readCount(source, USAGE_KEYS.cacheCreation);
  return {
    inputTokens: input ?? into.inputTokens,
    outputTokens: output ?? into.outputTokens,
    cacheReadTokens: cacheRead ?? into.cacheReadTokens,
    cacheCreationTokens: cacheCreation ?? into.cacheCreationTokens,
  };
}

/**
 * Reads `usage` out of one parsed event or response body, wherever it sits:
 * top-level for a non-streamed message, and under `message` for the
 * `message_start` event that opens a stream.
 */
export function usageFrom(parsed: unknown, current: UpstreamUsage): UpstreamUsage {
  const root = asRecord(parsed);
  if (root === null) return current;
  let usage = current;
  const nested = asRecord(root['message']);
  if (nested !== null) {
    const nestedUsage = asRecord(nested['usage']);
    if (nestedUsage !== null) usage = mergeUsage(usage, nestedUsage);
  }
  const direct = asRecord(root['usage']);
  if (direct !== null) usage = mergeUsage(usage, direct);
  return usage;
}

const DATA_PREFIX = 'data:';

/**
 * Accumulates SSE text and yields the JSON payload of each complete `data:`
 * line. Events are separated by a blank line and a chunk boundary can fall
 * anywhere, so the tail is held back until its newline arrives.
 */
export function createEventParser(): (chunk: string) => string[] {
  let pending = '';
  return (chunk) => {
    pending += chunk;
    const lines = pending.split('\n');
    // The final element is whatever followed the last newline: an incomplete
    // line that the next chunk continues.
    pending = lines.pop() ?? '';
    const payloads: string[] = [];
    for (const line of lines) {
      const trimmed = line.trim();
      if (!trimmed.startsWith(DATA_PREFIX)) continue;
      const payload = trimmed.slice(DATA_PREFIX.length).trim();
      if (payload !== '') payloads.push(payload);
    }
    return payloads;
  };
}

export type UsageObserver = {
  /** Feeds one decoded chunk of the response body. */
  push(chunk: string): void;
  /** Usage as it stands, callable at any point. */
  current(): UpstreamUsage;
};

/**
 * Watches a response body go past and picks out the token counts, parsing only
 * what it recognizes. It never holds the body: whatever it is fed has already
 * been forwarded, so a stream stays a stream.
 *
 * Both encodings are read by the same observer. A non-streamed body arrives as
 * one JSON document with no `data:` lines, so it is parsed whole once the
 * stream ends; a streamed one arrives as `data:` lines that are parsed as they
 * complete.
 */
export function createUsageObserver(): UsageObserver {
  const parseEvents = createEventParser();
  let usage = emptyUsage();
  let plain = '';
  let sawEvent = false;
  return {
    push(chunk) {
      for (const payload of parseEvents(chunk)) {
        sawEvent = true;
        try {
          usage = usageFrom(JSON.parse(payload), usage);
        } catch {
          // A payload that is not JSON carries no usage; the body still went
          // through untouched.
        }
      }
      if (!sawEvent) plain += chunk;
    },
    current() {
      if (sawEvent || plain === '') return usage;
      try {
        return usageFrom(JSON.parse(plain), usage);
      } catch {
        return usage;
      }
    },
  };
}
