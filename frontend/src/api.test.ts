import { describe, expect, it, vi } from 'vitest';
import {
  type APIError,
  createDevLANClient,
  HttpDevLANClient,
  MockDevLANClient,
  VersionMismatchError,
  type WailsApp,
  WailsDevLANClient,
} from './api';
import type { ProjectInfo, SystemStatus } from './types';

const project: ProjectInfo = {
  name: 'catalogo',
  path: '/sites/catalogo',
  kind: 'linked',
  mode: 'php',
  effectiveMode: 'php',
  framework: 'laravel',
  url: 'http://192.168.1.50:8080/',
  lanUrl: 'http://192.168.1.50:8080/',
  localDevUrl: 'https://catalogo.localhost/',
  localDevState: 'available',
  lanPreviewState: 'ready',
  tlsEnabled: false,
  port: 8080,
  status: 'ready',
  devRunning: false,
};

const status: SystemStatus = {
  lanIp: '192.168.1.50',
  windowsPort: 80,
  httpsPort: 443,
  routeBasePort: 8080,
  routePortCount: 100,
  uiPort: 3210,
  tlsEnabled: true,
  defaultMode: 'auto',
  phpDefaultVersion: '8.3',
  windowsCaddyRunning: true,
  wslCaddyRunning: true,
  wslAvailable: true,
  firewallOk: true,
  phpVersions: ['8.3'],
  totalProjects: 1,
  protocolVersion: 1,
};

function jsonResponse(value: unknown, statusCode = 200) {
  return new Response(JSON.stringify(value), {
    status: statusCode,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('HTTP DevLAN client', () => {
  it('sends same-origin credentials and the CSRF double-submit header for mutations', async () => {
    const fetchImpl = vi.fn<typeof fetch>().mockResolvedValue(jsonResponse({ message: 'ok' }));
    // biome-ignore lint/suspicious/noDocumentCookie: exercise the browser double-submit cookie.
    document.cookie = 'devlan_csrf=csrf%2Ftoken';
    const client = new HttpDevLANClient({ fetchImpl });

    await client.saveProjectConfig({ name: 'catalogo', routePort: 8123 });

    expect(fetchImpl).toHaveBeenCalledWith(
      '/api/v1/projects/config',
      expect.objectContaining({
        method: 'POST',
        credentials: 'same-origin',
        headers: expect.objectContaining({
          Accept: 'application/json',
          'Content-Type': 'application/json',
          'X-DevLAN-CSRF-Token': 'csrf/token',
        }),
        body: JSON.stringify({ name: 'catalogo', routePort: 8123 }),
      }),
    );
  });

  it('validates the Go response and rejects protocol version drift', async () => {
    const fetchImpl = vi
      .fn<typeof fetch>()
      .mockResolvedValue(jsonResponse({ ...status, protocolVersion: 99 }));
    const client = new HttpDevLANClient({ fetchImpl });

    await expect(client.getStatus()).rejects.toBeInstanceOf(VersionMismatchError);
  });

  it('rejects malformed project payloads instead of rendering guessed data', async () => {
    const fetchImpl = vi
      .fn<typeof fetch>()
      .mockResolvedValue(jsonResponse([{ ...project, status: 'unknown' }]));
    const client = new HttpDevLANClient({ fetchImpl });

    await expect(client.getProjects()).rejects.toThrow('Contrato DevLAN inválido');
  });

  it('exposes network and HTTP failures as actionable API errors', async () => {
    const fetchImpl = vi
      .fn<typeof fetch>()
      .mockResolvedValue(jsonResponse({ error: 'CSRF inválido' }, 401));
    const client = new HttpDevLANClient({ fetchImpl });

    await expect(client.reload()).rejects.toMatchObject({
      status: 401,
    } satisfies Partial<APIError>);
  });
});

describe('Wails and mock adapters', () => {
  it('normalizes Wails values through the same response contract', async () => {
    const app = {
      GetProjects: vi.fn().mockResolvedValue([project]),
      GetStatus: vi.fn().mockResolvedValue(status),
    } as unknown as WailsApp;
    const client = new WailsDevLANClient(app);

    await expect(client.getProjects()).resolves.toEqual([project]);
    await expect(client.getStatus()).resolves.toEqual(status);
    expect(app.GetProjects).toHaveBeenCalledWith('');
  });

  it('keeps mock state deterministic and supports injected failures', async () => {
    const client = createDevLANClient({ mode: 'mock', mock: { projects: [project] } });
    await client.saveProjectConfig({ name: 'catalogo', routePort: 8123 });
    await expect(client.getProjects()).resolves.toMatchObject([
      { port: 8123, routePortOverride: 8123 },
    ]);

    const failing = new MockDevLANClient({ failures: { getProjects: new Error('offline') } });
    await expect(failing.getProjects()).rejects.toThrow('offline');
  });
});
