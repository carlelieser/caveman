/**
 * Raised by the compression stage so the handler can answer with 400 naming the
 * header. A scorer the client asked for and did not get is an error, never a
 * silent substitution. Lives apart from both so neither imports the other.
 */
export class UnknownScorerError extends Error {
  readonly requested: string;
  readonly available: readonly string[];

  constructor(requested: string, available: readonly string[]) {
    super(`resolving X-Caveman-Scorer failed: unknown scorer "${requested}"`);
    this.name = 'UnknownScorerError';
    this.requested = requested;
    this.available = available;
  }
}
