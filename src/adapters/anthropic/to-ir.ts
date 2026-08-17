import type {
  IrContent,
  IrMessage,
  IrRequest,
  IrRole,
  IrTool,
  ProviderExtensions,
} from '../../ir/types.js';
import {
  MODELLED_MESSAGE_KEYS,
  MODELLED_REQUEST_KEYS,
  MODELLED_TEXT_KEYS,
  MODELLED_TOOL_RESULT_KEYS,
  MODELLED_TOOL_USE_KEYS,
  extractPassthrough,
  isPlainObject,
} from './passthrough.js';

/** An Anthropic `/v1/messages` request body, unvalidated. */
export type AnthropicRequestBody = Record<string, unknown>;

function toOpaque(raw: unknown): IrContent {
  return { kind: 'opaque', raw };
}

function toTextContent(block: Record<string, unknown>): IrContent {
  const content: IrContent = {
    kind: 'text',
    text: String(block['text'] ?? ''),
    compressible: true,
  };
  if ('cache_control' in block) content.cacheControl = block['cache_control'];
  const rest = extractPassthrough(block, MODELLED_TEXT_KEYS);
  if (rest !== undefined) content.passthrough = rest;
  return content;
}

function toToolUseContent(block: Record<string, unknown>): IrContent {
  const content: IrContent = {
    kind: 'tool_use',
    id: String(block['id'] ?? ''),
    name: String(block['name'] ?? ''),
    input: block['input'],
  };
  if ('cache_control' in block) content.cacheControl = block['cache_control'];
  const rest = extractPassthrough(block, MODELLED_TOOL_USE_KEYS);
  if (rest !== undefined) content.passthrough = rest;
  return content;
}

function toToolResultContent(block: Record<string, unknown>): IrContent {
  const raw = block['content'];
  const isContentString = typeof raw === 'string';
  const content: IrContent = {
    kind: 'tool_result',
    toolUseId: String(block['tool_use_id'] ?? ''),
    content: toContentArray(raw),
    isContentString,
  };
  if ('is_error' in block) content.isError = block['is_error'] === true;
  if ('cache_control' in block) content.cacheControl = block['cache_control'];
  const rest = extractPassthrough(block, MODELLED_TOOL_RESULT_KEYS);
  if (rest !== undefined) content.passthrough = rest;
  return content;
}

/**
 * `thinking` and `redacted_thinking` carry a signature the API validates, so
 * they are kept as raw wire values rather than being decomposed.
 */
const THINKING_TYPES: readonly string[] = ['thinking', 'redacted_thinking'];

function toContentBlock(block: unknown): IrContent {
  if (!isPlainObject(block)) return toOpaque(block);
  const type = block['type'];
  if (type === 'text') return toTextContent(block);
  if (type === 'tool_use') return toToolUseContent(block);
  if (type === 'tool_result') return toToolResultContent(block);
  if (typeof type === 'string' && THINKING_TYPES.includes(type)) {
    return { kind: 'thinking', raw: block };
  }
  return toOpaque(block);
}

/**
 * Normalizes the string-or-array content forms to an array. The original form
 * is recorded separately so `fromIR` can reproduce it.
 */
function toContentArray(raw: unknown): IrContent[] {
  if (raw === undefined || raw === null) return [];
  if (typeof raw === 'string') return [{ kind: 'text', text: raw, compressible: true }];
  if (!Array.isArray(raw)) return [toOpaque(raw)];
  return raw.map(toContentBlock);
}

function toMessage(raw: unknown): IrMessage {
  if (!isPlainObject(raw)) {
    return { role: 'user', content: [toOpaque(raw)], isContentString: false };
  }
  const content = raw['content'];
  const message: IrMessage = {
    role: raw['role'] as IrRole,
    content: toContentArray(content),
    isContentString: typeof content === 'string',
  };
  const rest = extractPassthrough(raw, MODELLED_MESSAGE_KEYS);
  if (rest !== undefined) message.passthrough = rest;
  return message;
}

/**
 * Only a string or block-array `system` is modelled; any other form (including
 * an explicit null) stays in passthrough so it round-trips as written.
 */
function isSystemModelled(body: AnthropicRequestBody): boolean {
  const system = body['system'];
  return typeof system === 'string' || Array.isArray(system);
}

function toSystem(raw: unknown): { system: IrContent[] | null; isSystemString: boolean } {
  if (typeof raw !== 'string' && !Array.isArray(raw)) {
    return { system: null, isSystemString: false };
  }
  return { system: toContentArray(raw), isSystemString: typeof raw === 'string' };
}

function toTools(raw: unknown): IrTool[] {
  if (!Array.isArray(raw)) return [];
  return raw.map((tool) => ({ raw: tool }));
}

/**
 * An empty `tools: []` is indistinguishable from an absent one once modelled as
 * an array, so it stays in passthrough and only a populated list is modelled.
 */
function isToolsModelled(body: AnthropicRequestBody): boolean {
  const tools = body['tools'];
  return Array.isArray(tools) && tools.length > 0;
}

function modelledRequestKeys(body: AnthropicRequestBody): readonly string[] {
  const dropped = new Set<string>();
  if (!isToolsModelled(body)) dropped.add('tools');
  if (!isSystemModelled(body)) dropped.add('system');
  return MODELLED_REQUEST_KEYS.filter((key) => !dropped.has(key));
}

/**
 * Converts an Anthropic request body into the IR. Unknown fields and unknown
 * block types are preserved rather than dropped, so an unrecognized future
 * shape degrades to passthrough instead of data loss.
 */
export function toIR(body: AnthropicRequestBody): IrRequest {
  const { system, isSystemString } = toSystem(body['system']);
  const messages = Array.isArray(body['messages']) ? body['messages'] : [];
  const extensions: ProviderExtensions = {};
  if (isSystemString) extensions.isSystemString = true;
  return {
    model: String(body['model'] ?? ''),
    maxTokens: Number(body['max_tokens'] ?? 0),
    system,
    messages: messages.map(toMessage),
    tools: isToolsModelled(body) ? toTools(body['tools']) : [],
    extensions,
    passthrough: extractPassthrough(body, modelledRequestKeys(body)) ?? {},
  };
}
