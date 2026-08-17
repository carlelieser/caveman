export type IrRole = 'user' | 'assistant' | 'system';

/**
 * Wire fields on a block that the IR does not model, kept so the adapter can
 * restore the block exactly as it arrived.
 */
export type BlockPassthrough = Record<string, unknown>;

/**
 * Key order of one wire object, recorded so it can be rebuilt byte-for-byte.
 * Carried separately from the values because the IR models some keys as named
 * fields and leaves the rest in passthrough.
 */
export type KeyOrder = readonly string[];

export type IrTextContent = {
  kind: 'text';
  text: string;
  compressible: true;
  cacheControl?: unknown;
  passthrough?: BlockPassthrough;
  keyOrder?: KeyOrder;
};

export type IrToolResultContent = {
  kind: 'tool_result';
  toolUseId: string;
  content: IrContent[];
  /** The block carried `content` as a bare string rather than a block array. */
  isContentString?: boolean;
  isError?: boolean;
  cacheControl?: unknown;
  passthrough?: BlockPassthrough;
  keyOrder?: KeyOrder;
};

export type IrToolUseContent = {
  kind: 'tool_use';
  id: string;
  name: string;
  input: unknown;
  cacheControl?: unknown;
  passthrough?: BlockPassthrough;
  keyOrder?: KeyOrder;
};

export type IrThinkingContent = {
  kind: 'thinking';
  raw: unknown;
};

export type IrOpaqueContent = {
  kind: 'opaque';
  raw: unknown;
};

export type IrContent =
  | IrTextContent
  | IrToolResultContent
  | IrToolUseContent
  | IrThinkingContent
  | IrOpaqueContent;

export type IrMessage = {
  role: IrRole;
  content: IrContent[];
  /**
   * The message carried `content` as a bare string rather than a block array.
   * Optional because it is a wire-format detail of providers that accept both
   * encodings; an adapter without that duality omits it.
   */
  isContentString?: boolean;
  passthrough?: BlockPassthrough;
  keyOrder?: KeyOrder;
};

/** Tool definitions are never inspected or mutated; they round-trip verbatim. */
export type IrTool = {
  raw: unknown;
};

/**
 * Provider-specific shape the IR must remember to reproduce the wire format,
 * plus modelled provider features. Not compressible, never inspected by the
 * pipeline.
 */
export type ProviderExtensions = {
  /** `system` arrived as a bare string rather than an array of text blocks. */
  isSystemString?: boolean;
  /**
   * Top-level key order as it arrived. Prompt cache lookup matches on the
   * serialized prefix, so a body rebuilt in a different order misses the cache
   * even though it carries identical content.
   */
  keyOrder?: KeyOrder;
};

export type IrRequest = {
  model: string;
  maxTokens: number;
  system: IrContent[] | null;
  messages: IrMessage[];
  tools: IrTool[];
  extensions: ProviderExtensions;
  passthrough: Record<string, unknown>;
};
