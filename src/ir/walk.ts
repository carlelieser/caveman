import type { IrContent, IrMessage, IrRequest, IrRole, IrTextContent } from './types.js';

export type WalkScope = 'messages' | 'system' | 'tool_results';

export const ALL_SCOPES: readonly WalkScope[] = ['messages', 'system', 'tool_results'];

/**
 * Address of a text node within an IrRequest, stable for the duration of one
 * walk. `toolResultIndex` is set only for text nested inside a tool_result.
 */
export type TextNodePath = {
  scope: WalkScope;
  messageIndex: number | null;
  blockIndex: number;
  toolResultIndex: number | null;
};

/** A compressible text node plus the context a scorer needs to rank it. */
export type TextNode = {
  text: string;
  role: IrRole | null;
  path: TextNodePath;
  hasCacheControl: boolean;
};

export type TextVisitor = (node: TextNode) => void;
export type TextMapper = (node: TextNode) => string;

type ScopeSet = ReadonlySet<WalkScope>;

function toScopeSet(scopes: readonly WalkScope[]): ScopeSet {
  return new Set(scopes);
}

function systemPath(blockIndex: number): TextNodePath {
  return { scope: 'system', messageIndex: null, blockIndex, toolResultIndex: null };
}

function messagePath(messageIndex: number, blockIndex: number): TextNodePath {
  return { scope: 'messages', messageIndex, blockIndex, toolResultIndex: null };
}

function toolResultPath(
  messageIndex: number,
  blockIndex: number,
  toolResultIndex: number,
): TextNodePath {
  return { scope: 'tool_results', messageIndex, blockIndex, toolResultIndex };
}

function toTextNode(
  block: IrTextContent,
  role: IrRole | null,
  path: TextNodePath,
): TextNode {
  return {
    text: block.text,
    role,
    path,
    hasCacheControl: block.cacheControl !== undefined,
  };
}

function mapTextBlock(
  block: IrContent,
  node: TextNode | null,
  mapper: TextMapper,
): IrContent {
  if (node === null || block.kind !== 'text') return block;
  const text = mapper(node);
  return text === block.text ? block : { ...block, text };
}

type MessageMapper = (message: IrMessage, messageIndex: number) => IrContent[];

/**
 * Rebuilds a message's blocks, applying `mapper` to every text node the scope
 * set admits. Text directly in a message belongs to `messages`; text nested in
 * a tool_result belongs to `tool_results`.
 */
function createMessageBlockMapper(scopes: ScopeSet, mapper: TextMapper): MessageMapper {
  return (message, messageIndex) =>
    message.content.map((block, blockIndex) => {
      if (block.kind === 'text' && scopes.has('messages')) {
        const path = messagePath(messageIndex, blockIndex);
        return mapTextBlock(block, toTextNode(block, message.role, path), mapper);
      }
      if (block.kind !== 'tool_result' || !scopes.has('tool_results')) return block;
      const content = block.content.map((nested, nestedIndex) => {
        if (nested.kind !== 'text') return nested;
        const path = toolResultPath(messageIndex, blockIndex, nestedIndex);
        return mapTextBlock(nested, toTextNode(nested, message.role, path), mapper);
      });
      return { ...block, content };
    });
}

function mapSystem(
  system: IrContent[] | null,
  scopes: ScopeSet,
  mapper: TextMapper,
): IrContent[] | null {
  if (system === null || !scopes.has('system')) return system;
  return system.map((block, blockIndex) => {
    if (block.kind !== 'text') return block;
    return mapTextBlock(
      block,
      toTextNode(block, 'system', systemPath(blockIndex)),
      mapper,
    );
  });
}

/**
 * Returns a new IrRequest with every in-scope text node replaced by the
 * mapper's result. The input is never mutated; untouched nodes keep their
 * original object identity.
 */
export function mapTextNodes(
  request: IrRequest,
  scopes: readonly WalkScope[],
  mapper: TextMapper,
): IrRequest {
  const scopeSet = toScopeSet(scopes);
  const mapBlocks = createMessageBlockMapper(scopeSet, mapper);
  // System is mapped first so a visitor observes nodes in document order.
  const system = mapSystem(request.system, scopeSet, mapper);
  const messages = request.messages.map((message, messageIndex) => ({
    ...message,
    content: mapBlocks(message, messageIndex),
  }));
  return { ...request, system, messages };
}

/** Visits every in-scope text node in document order without modifying anything. */
export function forEachTextNode(
  request: IrRequest,
  scopes: readonly WalkScope[],
  visitor: TextVisitor,
): void {
  mapTextNodes(request, scopes, (node) => {
    visitor(node);
    return node.text;
  });
}

/** Collects every in-scope text node in document order. */
export function collectTextNodes(
  request: IrRequest,
  scopes: readonly WalkScope[],
): TextNode[] {
  const nodes: TextNode[] = [];
  forEachTextNode(request, scopes, (node) => nodes.push(node));
  return nodes;
}
