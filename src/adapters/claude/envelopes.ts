/**
 * The CLI writes newline-delimited JSON. Only some of it is Anthropic wire
 * data: `stream_event` wraps a verbatim Messages SSE event, `result` closes the
 * run, and the rest (`system`, `rate_limit_event`, …) describes the session and
 * has no place in a Messages response.
 */
export type CliEnvelope = Record<string, unknown>;

export function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

/** A `stream_event` envelope's payload: an Anthropic SSE event, as-is. */
export function streamEvent(envelope: CliEnvelope): Record<string, unknown> | null {
  if (envelope['type'] !== 'stream_event') return null;
  const event = envelope['event'];
  return isPlainObject(event) ? event : null;
}

export function isResult(envelope: CliEnvelope): boolean {
  return envelope['type'] === 'result';
}

/** The CLI reports a failed run in the result envelope rather than by exiting. */
export function resultError(envelope: CliEnvelope): string | null {
  if (!isResult(envelope)) return null;
  if (envelope['is_error'] !== true) return null;
  const result = envelope['result'];
  return typeof result === 'string' && result !== ''
    ? result
    : 'claude CLI reported an error';
}

/**
 * Splits a byte stream into JSON values, one per line. Lines that are not JSON
 * are skipped: the CLI is free to write diagnostics to stdout, and a warning
 * should not abort a response that is otherwise complete.
 */
export async function* readEnvelopes(
  stream: AsyncIterable<Uint8Array>,
): AsyncGenerator<CliEnvelope> {
  const decoder = new TextDecoder();
  let buffer = '';
  for await (const chunk of stream) {
    buffer += decoder.decode(chunk, { stream: true });
    let newline = buffer.indexOf('\n');
    while (newline !== -1) {
      const line = buffer.slice(0, newline);
      buffer = buffer.slice(newline + 1);
      const parsed = parseLine(line);
      if (parsed !== null) yield parsed;
      newline = buffer.indexOf('\n');
    }
  }
  const parsed = parseLine(buffer);
  if (parsed !== null) yield parsed;
}

function parseLine(line: string): CliEnvelope | null {
  const trimmed = line.trim();
  if (trimmed === '') return null;
  try {
    const parsed: unknown = JSON.parse(trimmed);
    return isPlainObject(parsed) ? parsed : null;
  } catch {
    return null;
  }
}
