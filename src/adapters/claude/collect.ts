import type { ProviderRequestBody } from '../provider.js';
import type { CliEnvelope } from './envelopes.js';
import { isPlainObject, readEnvelopes, resultError } from './envelopes.js';

/**
 * The CLI has no single-shot Messages response to forward, so one is assembled:
 * the last `assistant` envelope carries the content and model, and the `result`
 * envelope carries the billed usage.
 */
type Collected = {
  assistant: Record<string, unknown> | null;
  result: CliEnvelope | null;
  error: string | null;
};

function assistantMessage(envelope: CliEnvelope): Record<string, unknown> | null {
  if (envelope['type'] !== 'assistant') return null;
  const message = envelope['message'];
  return isPlainObject(message) ? message : null;
}

async function collect(stream: AsyncIterable<Uint8Array>): Promise<Collected> {
  const state: Collected = { assistant: null, result: null, error: null };
  for await (const envelope of readEnvelopes(stream)) {
    const message = assistantMessage(envelope);
    if (message !== null) state.assistant = message;
    if (envelope['type'] === 'result') state.result = envelope;
    const failure = resultError(envelope);
    if (failure !== null) state.error = failure;
  }
  return state;
}

function usageFrom(result: CliEnvelope | null): unknown {
  if (result === null) return { input_tokens: 0, output_tokens: 0 };
  const usage = result['usage'];
  return isPlainObject(usage) ? usage : { input_tokens: 0, output_tokens: 0 };
}

/**
 * Falls back to the result envelope's plain text when no assistant message was
 * seen, so a run that produced an answer still returns one.
 */
function contentFrom(state: Collected): unknown[] {
  const blocks = state.assistant?.['content'];
  if (Array.isArray(blocks)) return blocks;
  const text = state.result?.['result'];
  if (typeof text === 'string' && text !== '') return [{ type: 'text', text }];
  return [];
}

function stringField(source: Record<string, unknown> | null, key: string): string | null {
  const value = source?.[key];
  return typeof value === 'string' ? value : null;
}

export type CollectedResponse = {
  body: ProviderRequestBody;
  error: string | null;
};

export async function collectMessage(
  stream: AsyncIterable<Uint8Array>,
  fallbackModel: string,
): Promise<CollectedResponse> {
  const state = await collect(stream);
  const body: ProviderRequestBody = {
    id: stringField(state.assistant, 'id') ?? 'msg_caveman_claude',
    type: 'message',
    role: 'assistant',
    model: stringField(state.assistant, 'model') ?? fallbackModel,
    content: contentFrom(state),
    stop_reason: stringField(state.result, 'stop_reason') ?? 'end_turn',
    stop_sequence: null,
    usage: usageFrom(state.result),
  };
  return { body, error: state.error };
}
