import type { ClaudeMode } from '../../policy/headers.js';
import type { ProviderAdapter, ProviderRequestBody, UpstreamCall } from '../provider.js';
import { fromIR } from '../anthropic/from-ir.js';
import { toIR } from '../anthropic/to-ir.js';
import { parseClaudeMode } from '../../policy/headers.js';
import { collectMessage } from './collect.js';
import { ClaudeCliError, invokeClaude } from './invoke.js';
import { toEventStream } from './stream.js';

const MESSAGES_PATH = '/claude/v1/messages';

/**
 * The CLI is a subprocess, so there is no host to reach. The value exists
 * because `ProviderAdapter` requires one, and it is never dialled: the
 * transport below owns the trip upstream.
 */
const BASE_URL = 'http://claude-cli.invalid';

/** The CLI speaks the Anthropic wire format, so it reuses that error shape. */
function errorEnvelope(message: string): ProviderRequestBody {
  return { type: 'error', error: { type: 'invalid_request_error', message } };
}

function jsonResponse(body: ProviderRequestBody, status: number): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  });
}

function isStreaming(body: ProviderRequestBody): boolean {
  return body['stream'] === true;
}

function modelOf(body: ProviderRequestBody): string {
  const model = body['model'];
  return typeof model === 'string' ? model : '';
}

function parseBody(raw: string): ProviderRequestBody {
  const parsed: unknown = JSON.parse(raw);
  return typeof parsed === 'object' && parsed !== null
    ? (parsed as ProviderRequestBody)
    : {};
}

/**
 * Runs the request through the local CLI. The mode header is read here rather
 * than in the handler because it selects how this one provider is invoked and
 * means nothing to the others.
 */
async function transport(call: UpstreamCall): Promise<Response> {
  const modeResult = parseClaudeMode(call.originalHeaders);
  if (!modeResult.ok) {
    const detail = `${modeResult.header}: ${modeResult.reason} (received "${modeResult.value}")`;
    return jsonResponse(errorEnvelope(detail), 400);
  }

  const body = parseBody(call.body);
  const mode: ClaudeMode = modeResult.mode;
  const stream = isStreaming(body);

  const run = invokeClaude({ body, mode, stream }, call.signal);

  // A missing binary surfaces as a spawn error rather than a stdout close, and
  // it is the one failure worth catching before a status has been committed.
  const spawned = await run.spawned.catch((error: unknown) => error);
  if (spawned instanceof ClaudeCliError) {
    return jsonResponse(errorEnvelope(spawned.message), 502);
  }

  if (stream) {
    return new Response(
      toEventStream({ envelopes: run.stdout, completion: run.completion }),
      {
        status: 200,
        headers: { 'content-type': 'text/event-stream' },
      },
    );
  }

  // A non-streaming client gets one body, so the failure is known before the
  // status is chosen and can be reported as an error rather than in-band.
  try {
    const collected = await collectMessage(run.stdout, modelOf(body));
    const failure = collected.error ?? (await run.completion);
    if (failure !== null) {
      return jsonResponse(errorEnvelope(failure), 502);
    }
    return jsonResponse(collected.body, 200);
  } catch (error) {
    if (error instanceof ClaudeCliError) {
      return jsonResponse(errorEnvelope(error.message), 502);
    }
    throw error;
  }
}

export const claudeAdapter: ProviderAdapter = {
  name: 'claude',
  path: MESSAGES_PATH,
  baseUrl: BASE_URL,
  toIR,
  fromIR,
  errorEnvelope,
  transport,
};
