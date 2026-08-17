/** Top-level request keys the IR models explicitly. Everything else is passthrough. */
export const MODELLED_REQUEST_KEYS: readonly string[] = [
  'model',
  'max_tokens',
  'system',
  'messages',
  'tools',
];

export const MODELLED_MESSAGE_KEYS: readonly string[] = ['role', 'content'];

export const MODELLED_TEXT_KEYS: readonly string[] = ['type', 'text', 'cache_control'];

export const MODELLED_TOOL_USE_KEYS: readonly string[] = [
  'type',
  'id',
  'name',
  'input',
  'cache_control',
];

export const MODELLED_TOOL_RESULT_KEYS: readonly string[] = [
  'type',
  'tool_use_id',
  'content',
  'is_error',
  'cache_control',
];

export function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

/**
 * Every own key of `source` not named in `modelled`. Returns undefined when
 * there are none, so an absent passthrough never materializes as an empty
 * object on the way back out.
 */
export function extractPassthrough(
  source: Record<string, unknown>,
  modelled: readonly string[],
): Record<string, unknown> | undefined {
  const rest: Record<string, unknown> = {};
  let hasRest = false;
  for (const [key, value] of Object.entries(source)) {
    if (modelled.includes(key)) continue;
    rest[key] = value;
    hasRest = true;
  }
  return hasRest ? rest : undefined;
}

/** Copies passthrough keys back onto a rebuilt wire object. */
export function applyPassthrough(
  target: Record<string, unknown>,
  passthrough: Record<string, unknown> | undefined,
): Record<string, unknown> {
  if (passthrough === undefined) return target;
  return Object.assign(target, passthrough);
}

/**
 * Re-emits a rebuilt wire object in the key order it arrived in. JSON key order
 * is insertion order, and prompt cache lookup matches on serialized bytes, so a
 * body reassembled in declaration order misses the cache despite being equal.
 *
 * Keys recorded but no longer present are skipped — a field the IR dropped must
 * not reappear as `undefined`. Keys present but never recorded are appended, so
 * a synthesized field still survives.
 */
export function inKeyOrder(
  built: Record<string, unknown>,
  keyOrder: readonly string[] | undefined,
): Record<string, unknown> {
  if (keyOrder === undefined) return built;
  const ordered: Record<string, unknown> = {};
  for (const key of keyOrder) {
    if (key in built) ordered[key] = built[key];
  }
  for (const [key, value] of Object.entries(built)) {
    if (!(key in ordered)) ordered[key] = value;
  }
  return ordered;
}

/**
 * Assigns `value` under `key` only when it is present, so an optional field
 * absent on the way in never reappears as an explicit `undefined`.
 */
export function assignIfPresent(
  target: Record<string, unknown>,
  key: string,
  value: unknown,
): void {
  if (value !== undefined) target[key] = value;
}
