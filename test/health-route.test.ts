import { describe, expect, it } from 'vitest';
import { createApp } from '../src/http/server.js';
import { HEALTH_PATH } from '../src/http/health.js';

async function get(app: ReturnType<typeof createApp>, path: string): Promise<Response> {
  return await app.fetch(new Request(`http://caveman.test${path}`));
}

describe('health route', () => {
  it('answers 200', async () => {
    const response = await get(createApp(), HEALTH_PATH);
    expect(response.status).toBe(200);
  });

  it('names the service so a caller can tell caveman from another server', async () => {
    const response = await get(createApp(), HEALTH_PATH);
    expect(await response.json()).toEqual({ service: 'caveman', status: 'ok' });
  });

  it('answers even with no adapters, so readiness does not depend on providers', async () => {
    const response = await get(createApp(undefined, []), HEALTH_PATH);
    expect(response.status).toBe(200);
  });

  it('leaves an unknown path a 404 rather than becoming a catch-all', async () => {
    const response = await get(createApp(), '/not-a-route');
    expect(response.status).toBe(404);
  });

  it('does not answer a POST to the health path', async () => {
    const response = await createApp().fetch(
      new Request(`http://caveman.test${HEALTH_PATH}`, { method: 'POST' }),
    );
    expect(response.status).toBe(404);
  });
});
