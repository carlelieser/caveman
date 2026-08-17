export const HEALTH_PATH = '/health';

export type HealthBody = {
  service: string;
  status: string;
};

/**
 * Names the service so a caller holding the port can tell Caveman apart from an
 * unrelated server answering here. The CLI refuses to treat a 200 without this
 * name as its own process.
 */
export function healthBody(): HealthBody {
  return { service: 'caveman', status: 'ok' };
}
