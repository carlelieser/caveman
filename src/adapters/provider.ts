import type { IrRequest } from '../ir/types.js';

export type ProviderRequestBody = Record<string, unknown>;

/** The denormalized request, as it would go on the wire. */
export type UpstreamCall = {
  body: string;
  /** Forwardable headers: Caveman's own have already been stripped. */
  headers: Headers;
  /** The client's headers as they arrived, for adapter-specific Caveman options. */
  originalHeaders: Headers;
  signal: AbortSignal;
};

/**
 * How a provider reaches its upstream. Exists because not every provider is an
 * HTTP host: a local CLI is a subprocess, and only the adapter knows how to
 * speak to it. The result is a `Response` either way, so the handler treats
 * both alike.
 */
export type ProviderTransport = (call: UpstreamCall) => Promise<Response>;

/**
 * Everything the HTTP layer needs to serve one provider. A provider owns its
 * route, its wire format, and the shape of its errors, so adding one is adding
 * an implementation of this type rather than editing the handler.
 */
export type ProviderAdapter = {
  name: string;
  /** Route this provider serves, and the upstream path requests forward to. */
  path: string;
  /** Where this provider's requests are forwarded, absent an env override. */
  baseUrl: string;
  toIR(body: ProviderRequestBody): IrRequest;
  fromIR(request: IrRequest): ProviderRequestBody;
  /** Wraps a Caveman-generated message in the provider's own error shape. */
  errorEnvelope(message: string): ProviderRequestBody;
  /** Sends the request upstream. Absent means an HTTP POST to `baseUrl + path`. */
  transport?: ProviderTransport;
};
