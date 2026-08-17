import type { IrRequest } from '../ir/types.js';

export type ProviderRequestBody = Record<string, unknown>;

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
};
