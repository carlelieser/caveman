import type {
  IrContent,
  IrMessage,
  IrRequest,
  IrTextContent,
  IrToolResultContent,
  IrToolUseContent,
} from '../../ir/types.js';
import type { AnthropicRequestBody } from './to-ir.js';
import { applyPassthrough, assignIfPresent } from './passthrough.js';

function fromTextContent(block: IrTextContent): Record<string, unknown> {
  const wire: Record<string, unknown> = { type: 'text', text: block.text };
  assignIfPresent(wire, 'cache_control', block.cacheControl);
  return applyPassthrough(wire, block.passthrough);
}

function fromToolUseContent(block: IrToolUseContent): Record<string, unknown> {
  const wire: Record<string, unknown> = {
    type: 'tool_use',
    id: block.id,
    name: block.name,
    input: block.input,
  };
  assignIfPresent(wire, 'cache_control', block.cacheControl);
  return applyPassthrough(wire, block.passthrough);
}

function fromToolResultContent(block: IrToolResultContent): Record<string, unknown> {
  const wire: Record<string, unknown> = {
    type: 'tool_result',
    tool_use_id: block.toolUseId,
  };
  assignIfPresent(
    wire,
    'content',
    fromContentField(block.content, block.isContentString),
  );
  assignIfPresent(wire, 'is_error', block.isError);
  assignIfPresent(wire, 'cache_control', block.cacheControl);
  return applyPassthrough(wire, block.passthrough);
}

function fromContentBlock(block: IrContent): unknown {
  if (block.kind === 'text') return fromTextContent(block);
  if (block.kind === 'tool_use') return fromToolUseContent(block);
  if (block.kind === 'tool_result') return fromToolResultContent(block);
  return block.raw;
}

/**
 * Reproduces the string-or-array form the content arrived in. A string form is
 * only recoverable from a single text block, which is what `toIR` produced.
 */
function fromContentField(content: IrContent[], isContentString?: boolean): unknown {
  if (isContentString !== true)
    return content.length > 0 ? content.map(fromContentBlock) : undefined;
  const first = content[0];
  if (first !== undefined && first.kind === 'text') return first.text;
  return content.map(fromContentBlock);
}

function fromMessage(message: IrMessage): Record<string, unknown> {
  const wire: Record<string, unknown> = { role: message.role };
  const content = fromContentField(message.content, message.isContentString);
  wire['content'] = content === undefined ? [] : content;
  return applyPassthrough(wire, message.passthrough);
}

function fromSystem(request: IrRequest): unknown {
  const { system } = request;
  if (system === null) return undefined;
  if (request.extensions.isSystemString === true) {
    const first = system[0];
    if (first !== undefined && first.kind === 'text') return first.text;
  }
  return system.map(fromContentBlock);
}

/**
 * Converts the IR back to an Anthropic request body. Passthrough fields are
 * restored last so a modelled key never shadows the value that arrived.
 */
export function fromIR(request: IrRequest): AnthropicRequestBody {
  const body: Record<string, unknown> = {
    model: request.model,
    max_tokens: request.maxTokens,
  };
  assignIfPresent(body, 'system', fromSystem(request));
  body['messages'] = request.messages.map(fromMessage);
  if (request.tools.length > 0) body['tools'] = request.tools.map((tool) => tool.raw);
  return applyPassthrough(body, request.passthrough);
}
