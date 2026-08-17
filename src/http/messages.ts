import type { Context } from 'hono';
import type { ProviderAdapter, ProviderRequestBody } from '../adapters/provider.js';
import type { CompressionPolicy, PolicyFailure } from '../policy/headers.js';
import type { IrRequest } from '../ir/types.js';
import type { PipelineStats } from '../compression/pipeline.js';
import type { ResponseDecorator } from './upstream.js';
import type { SavingsReporter } from '../telemetry/savings-log.js';
import { parseCompressionPolicy } from '../policy/headers.js';
import { accountFor, applyAccountingHeaders } from '../telemetry/accounting.js';
import { UnknownScorerError } from './unknown-scorer-error.js';
import {
  forwardableRequestHeaders,
  passthroughResponse,
  sendUpstream,
} from './upstream.js';

/**
 * The seam the compression pipeline plugs into. The stage returns its stats so
 * telemetry can label the response without re-walking the request.
 */
export type StageResult = {
  request: IrRequest;
  stats: PipelineStats | null;
};

export type CompressionStage = (
  request: IrRequest,
  policy: CompressionPolicy,
) => StageResult;

export const identityCompressionStage: CompressionStage = (request) => ({
  request,
  stats: null,
});

class MalformedBodyError extends Error {
  constructor(path: string, cause: unknown) {
    super(`parsing ${path} request body as JSON failed`, { cause });
    this.name = 'MalformedBodyError';
  }
}

function policyErrorMessage(failure: PolicyFailure): string {
  return `${failure.header}: ${failure.reason} (received "${failure.value}")`;
}

function unknownScorerMessage(error: UnknownScorerError): string {
  return `X-Caveman-Scorer: unknown scorer "${error.requested}" (available: ${error.available.join(', ')})`;
}

function parseBody(raw: string, path: string): ProviderRequestBody {
  try {
    return JSON.parse(raw) as ProviderRequestBody;
  } catch (cause) {
    throw new MalformedBodyError(path, cause);
  }
}

/** Absent stats mean the request was never compressed, so nothing is reported. */
function accountingDecorator(stats: PipelineStats | null): ResponseDecorator {
  return (headers) => {
    if (stats === null) return;
    applyAccountingHeaders(headers, accountFor(stats));
  };
}

type Route = {
  adapter: ProviderAdapter;
  stage: CompressionStage;
  reporter: SavingsReporter | null;
};

function reject(context: Context, route: Route, message: string): Response {
  return context.json(route.adapter.errorEnvelope(message), 400);
}

async function readBody(context: Context, route: Route): Promise<ProviderRequestBody> {
  return parseBody(await context.req.text(), route.adapter.path);
}

/**
 * The client's query string, forwarded so upstream sees the request it was
 * addressed to. An unparseable URL yields none rather than failing the request.
 */
function incomingSearch(url: string): string {
  try {
    return new URL(url).search;
  } catch {
    return '';
  }
}

async function forwardMessages(context: Context, route: Route): Promise<Response> {
  const policyResult = parseCompressionPolicy(context.req.raw.headers);
  if (!policyResult.ok) {
    return reject(context, route, policyErrorMessage(policyResult));
  }

  let body: ProviderRequestBody;
  try {
    body = await readBody(context, route);
  } catch (error) {
    if (!(error instanceof MalformedBodyError)) throw error;
    return reject(context, route, 'request body is not valid JSON');
  }

  let staged: StageResult;
  try {
    staged = route.stage(route.adapter.toIR(body), policyResult.policy);
  } catch (error) {
    if (!(error instanceof UnknownScorerError)) throw error;
    return reject(context, route, unknownScorerMessage(error));
  }

  const upstream = await sendUpstream({
    adapter: route.adapter,
    headers: forwardableRequestHeaders(context.req.raw.headers),
    body: JSON.stringify(route.adapter.fromIR(staged.request)),
    search: incomingSearch(context.req.raw.url),
    signal: context.req.raw.signal,
  });
  if (staged.stats !== null) {
    route.reporter?.record(staged.stats);
  }
  return passthroughResponse(upstream, accountingDecorator(staged.stats));
}

/**
 * Builds the handler for one provider. The adapter, the pipeline, and the
 * reporter are injected, so the handler holds no knowledge of any provider's
 * wire format and none of where its output goes.
 */
export function createMessagesHandler(
  adapter: ProviderAdapter,
  stage: CompressionStage = identityCompressionStage,
  reporter: SavingsReporter | null = null,
): (context: Context) => Promise<Response> {
  const route: Route = { adapter, stage, reporter };
  return (context) => forwardMessages(context, route);
}
