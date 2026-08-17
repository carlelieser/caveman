import type { ProviderAdapter, ProviderRequestBody } from '../provider.js';
import { fromIR } from './from-ir.js';
import { toIR } from './to-ir.js';

const MESSAGES_PATH = '/v1/messages';
const BASE_URL = 'https://api.anthropic.com';

function errorEnvelope(message: string): ProviderRequestBody {
  return { type: 'error', error: { type: 'invalid_request_error', message } };
}

export const anthropicAdapter: ProviderAdapter = {
  name: 'anthropic',
  path: MESSAGES_PATH,
  baseUrl: BASE_URL,
  toIR,
  fromIR,
  errorEnvelope,
};
