const DISABLE_VARIABLE = 'DISABLE_LOGS';

const FALSE_VALUES: ReadonlySet<string> = new Set(['0', 'false']);

export type LogSink = (line: string) => void;

export const silentLogSink: LogSink = () => {};

/**
 * On unless `DISABLE_LOGS` says otherwise. Read per call rather than at module
 * load so setting the variable takes effect without reloading the module.
 */
export function isLoggingEnabled(): boolean {
  const configured = process.env[DISABLE_VARIABLE];
  if (configured === undefined) return true;
  const normalized = configured.trim().toLowerCase();
  if (normalized === '') return true;
  return FALSE_VALUES.has(normalized);
}

function writeLine(line: string): void {
  process.stdout.write(`${line}\n`);
}

export function createLogSink(): LogSink {
  return (line) => {
    if (!isLoggingEnabled()) return;
    writeLine(line);
  };
}
