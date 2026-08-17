/**
 * A recorded Anthropic `/v1/messages` wire body. Every fixture must survive
 * `fromIR(toIR(body))` unchanged.
 */
export type RequestFixture = {
  name: string;
  body: Record<string, unknown>;
};
