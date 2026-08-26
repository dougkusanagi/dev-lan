import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { MockDevLANClient } from '../api';
import type { ProjectInfo, SystemStatus } from '../types';
import AppShell from './AppShell';

const project: ProjectInfo = {
  name: 'catalogo',
  path: '/sites/catalogo',
  kind: 'linked',
  mode: 'static',
  effectiveMode: 'static',
  framework: 'static',
  url: 'http://192.168.1.50:8080/',
  lanUrl: 'http://192.168.1.50:8080/',
  localDevUrl: 'https://catalogo.localhost/',
  localDevState: 'available',
  lanPreviewState: 'ready',
  tlsEnabled: true,
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

function clientWith(projects: ProjectInfo[], overrides: Partial<SystemStatus> = {}) {
  return new MockDevLANClient({
    projects,
    status: { ...status, totalProjects: projects.length, ...overrides },
  });
}

describe('AppShell states and keyboard behavior', () => {
  it('shows a deterministic loading state before the first response', async () => {
    let resolveProjects: (items: ProjectInfo[]) => void = () => undefined;
    const pending = new Promise<ProjectInfo[]>((resolve) => {
      resolveProjects = resolve;
    });
    const client = clientWith([project]);
    client.getProjects = () => pending;

    render(<AppShell client={client} pollIntervalMs={0} />);
    expect(screen.getByRole('heading', { name: 'Carregando DevLAN' })).toBeInTheDocument();

    resolveProjects([project]);
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'catalogo' })).toBeInTheDocument(),
    );
  });

  it('renders the empty state when the backend has no projects', async () => {
    render(<AppShell client={clientWith([])} pollIntervalMs={0} />);

    expect(
      await screen.findByRole('heading', { name: 'Nenhum projeto selecionado' }),
    ).toBeInTheDocument();
    expect(screen.getAllByRole('button', { name: 'Adicionar projeto' }).length).toBeGreaterThan(0);
  });

  it('renders a retryable error when the API is unavailable', async () => {
    const client = clientWith([]);
    client.getProjects = async () => {
      throw new Error('connection refused');
    };
    render(<AppShell client={client} pollIntervalMs={0} />);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível carregar a interface',
    );
    expect(screen.getByRole('button', { name: 'Tentar novamente' })).toBeInTheDocument();
  });

  it('keeps degraded infrastructure visible with text and repair action', async () => {
    render(
      <AppShell
        client={clientWith([{ ...project, status: 'degraded' }], {
          windowsCaddyRunning: false,
          wslCaddyRunning: false,
          firewallOk: false,
        })}
        pollIntervalMs={0}
      />,
    );

    expect(await screen.findByText('Degradado')).toBeInTheDocument();
    expect(screen.getAllByText('Indisponível').length).toBeGreaterThan(0);
    expect(screen.getByRole('button', { name: 'Corrigir firewall privado' })).toBeInTheDocument();
  });

  it('supports Ctrl+K and confirms a LAN port override through the client', async () => {
    const user = userEvent.setup();
    const client = clientWith([project]);
    render(<AppShell client={client} pollIntervalMs={0} />);
    await screen.findByRole('heading', { name: 'catalogo' });

    await user.keyboard('{Control>}k{/Control}');
    expect(screen.getByRole('textbox', { name: 'Buscar sites (Ctrl+K)' })).toHaveFocus();

    const port = screen.getByRole('spinbutton', { name: 'Porta LAN' });
    await user.clear(port);
    await user.type(port, '8123');
    await user.click(screen.getByRole('button', { name: 'Aplicar' }));
    await waitFor(() => expect(client.calls).toContain('saveProjectConfig'));
    expect(await screen.findByText('Porta LAN 8123 aplicada.')).toBeInTheDocument();
  });
});
