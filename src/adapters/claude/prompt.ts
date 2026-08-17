import type { ProviderRequestBody } from '../provider.js';

/**
 * The CLI honors only the first message handed to it on stdin, so a multi-turn
 * conversation has to arrive as one prompt. Turns are labelled only when there
 * is more than one, leaving the common single-turn request free of scaffolding.
 */
const HUMAN_LABEL = 'Human:';
const ASSISTANT_LABEL = 'Assistant:';

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

/**
 * Non-text blocks are named rather than dropped. A conversation whose tool call
 * vanished reads as though the model answered from nowhere, so the placeholder
 * keeps the shape of the exchange even though the payload cannot be replayed.
 */
function renderBlock(block: unknown): string {
  if (typeof block === 'string') return block;
  if (!isPlainObject(block)) return '';
  const type = block['type'];
  if (type === 'text') return String(block['text'] ?? '');
  if (type === 'tool_use') return `[tool_use: ${String(block['name'] ?? 'unknown')}]`;
  if (type === 'tool_result') return renderToolResult(block);
  if (type === 'thinking' || type === 'redacted_thinking') return '';
  if (typeof type === 'string') return `[${type}]`;
  return '';
}

function renderToolResult(block: Record<string, unknown>): string {
  const body = renderContent(block['content']);
  if (body === '') return '[tool_result]';
  return `[tool_result]\n${body}`;
}

function renderContent(content: unknown): string {
  if (typeof content === 'string') return content;
  if (!Array.isArray(content)) return '';
  return content
    .map(renderBlock)
    .filter((part) => part !== '')
    .join('\n\n');
}

function labelFor(role: unknown): string {
  return role === 'assistant' ? ASSISTANT_LABEL : HUMAN_LABEL;
}

function renderMessage(message: unknown, labelled: boolean): string {
  if (!isPlainObject(message)) return '';
  const body = renderContent(message['content']);
  if (body === '') return '';
  if (!labelled) return body;
  return `${labelFor(message['role'])} ${body}`;
}

/**
 * Flattens the conversation into the single prompt the CLI accepts. Empty
 * messages drop out so a label never introduces nothing.
 */
export function flattenPrompt(body: ProviderRequestBody): string {
  const messages = body['messages'];
  if (!Array.isArray(messages)) return '';
  const labelled = messages.length > 1;
  return messages
    .map((message) => renderMessage(message, labelled))
    .filter((part) => part !== '')
    .join('\n\n');
}

/** The request's system prompt, flattened to the string the CLI flag takes. */
export function flattenSystem(body: ProviderRequestBody): string {
  return renderContent(body['system']);
}
