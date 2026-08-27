import {
  parseDoctorChecks,
  parseGlobalConfig,
  parseMetricsSnapshot,
  parseOverview,
  parsePHPVersions,
  parseProjects,
  parseSystemStatus,
  parseTopology,
} from './contracts';
import type {
  DoctorCheck,
  GlobalConfig,
  MetricsRange,
  MetricsSnapshot,
  Overview,
  PHPVersion,
  ProjectConfigUpdate,
  ProjectInfo,
  SystemStatus,
} from './types';

export { VersionMismatchError } from './contracts';

export interface WailsApp {
  GetOverview?: (filter: string) => Promise<unknown>;
  GetProjects: (filter: string) => Promise<unknown>;
  GetStatus: () => Promise<unknown>;
  GetTopology?: () => Promise<unknown>;
  GetMetrics?: (project: string, range: MetricsRange) => Promise<unknown>;
  GetGlobalConfig: () => Promise<unknown>;
  GetPHPVersions: () => Promise<unknown>;
  InstallPHPVersion: (version: string) => Promise<void>;
  RemovePHPVersion: (version: string) => Promise<void>;
  SetDefaultPHPVersion: (version: string) => Promise<void>;
  SaveGlobalConfig: (cfg: GlobalConfig) => Promise<void>;
  SaveProjectConfig: (update: ProjectConfigUpdate) => Promise<void>;
  LinkProject: (name: string, path: string) => Promise<void>;
  UnlinkProject: (name: string) => Promise<void>;
  HideProject: (name: string) => Promise<void>;
  ParkDir: (path: string) => Promise<void>;
  UnparkDir: (path: string) => Promise<void>;
  StartDev: (name: string) => Promise<void>;
  StopDev: (name: string) => Promise<void>;
  RestartDev: (name: string) => Promise<void>;
  BuildProject: (name: string) => Promise<string>;
  InstallDeps: (name: string) => Promise<string>;
  GetProjectLogs: (name: string, lines: number) => Promise<string>;
  RunDoctor: (name: string) => Promise<unknown>;
  ApplyDoctorFix: (action: string, target: string) => Promise<void>;
  OpenURL: (url: string) => Promise<void>;
  CopyURL: (url: string) => Promise<void>;
  Reload: () => Promise<void>;
  ExportConfigJSON: () => Promise<string>;
  ExportDiagnostic: () => Promise<string>;
  TrustCA: () => Promise<void>;
  GetSecurityAudit: (lines: number) => Promise<string>;
}

export interface WailsRuntime {
  ClipboardSetText?: (text: string) => Promise<boolean>;
  BrowserOpenURL?: (url: string) => void;
}

declare global {
  interface Window {
    __DEVLAN_MOCK__?: boolean;
    go?: { gui?: { App?: WailsApp } };
    runtime?: WailsRuntime;
  }
}

export interface DevLANClient {
  getOverview?: (filter?: string) => Promise<Overview>;
  getProjects(filter?: string): Promise<ProjectInfo[]>;
  getStatus(): Promise<SystemStatus>;
  getTopology(): Promise<Record<string, unknown>>;
  getMetrics(project: string, range: MetricsRange): Promise<MetricsSnapshot | null>;
  getGlobalConfig(): Promise<GlobalConfig>;
  getPHPVersions(): Promise<PHPVersion[]>;
  installPHPVersion(version: string): Promise<void>;
  removePHPVersion(version: string): Promise<void>;
  setDefaultPHPVersion(version: string): Promise<void>;
  saveGlobalConfig(cfg: GlobalConfig): Promise<void>;
  saveProjectConfig(update: ProjectConfigUpdate): Promise<void>;
  linkProject(name: string, path: string): Promise<void>;
  unlinkProject(name: string): Promise<void>;
  hideProject(name: string): Promise<void>;
  parkDir(path: string): Promise<void>;
  unparkDir(path: string): Promise<void>;
  startDev(name: string): Promise<void>;
  stopDev(name: string): Promise<void>;
  restartDev(name: string): Promise<void>;
  buildProject(name: string): Promise<string>;
  installDeps(name: string): Promise<string>;
  getProjectLogs(name: string, lines?: number): Promise<string>;
  runDoctor(name?: string): Promise<DoctorCheck[]>;
  applyDoctorFix(action: string, target: string): Promise<void>;
  openURL(url: string): Promise<void>;
  copyURL(url: string): Promise<void>;
  reload(): Promise<void>;
  exportConfigJSON(): Promise<string>;
  exportDiagnostic(): Promise<string>;
  trustCA(): Promise<void>;
  getSecurityAudit(lines?: number): Promise<string>;
}

export class APIError extends Error {
  readonly status: number;
  readonly statusText: string;
  readonly details?: unknown;

  constructor(status: number, statusText: string, message: string, details?: unknown) {
    super(message);
    this.name = 'APIError';
    this.status = status;
    this.statusText = statusText;
    this.details = details;
  }
}

type Decoder<T> = (value: unknown) => T;

export interface HttpClientOptions {
  baseUrl?: string;
  fetchImpl?: typeof fetch;
}

function getCSRFToken(): string {
  if (typeof document === 'undefined') return '';
  const match = document.cookie.match(/(?:^|;\s*)devlan_csrf=([^;]+)/);
  return match ? decodeURIComponent(match[1]) : '';
}

function parseErrorBody(value: unknown): { message?: string; details?: unknown } {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return {};
  const body = value as Record<string, unknown>;
  return {
    message: typeof body.error === 'string' ? body.error : undefined,
    details: body,
  };
}

function objectField(value: unknown, key: string, fallback = ''): string {
  if (value !== null && typeof value === 'object' && !Array.isArray(value)) {
    const field = (value as Record<string, unknown>)[key];
    if (typeof field === 'string') return field;
  }
  return fallback;
}

export class HttpDevLANClient implements DevLANClient {
  private readonly baseUrl?: string;
  private readonly fetchImpl: typeof fetch;

  constructor(options: HttpClientOptions = {}) {
    this.baseUrl = options.baseUrl;
    this.fetchImpl = options.fetchImpl ?? globalThis.fetch.bind(globalThis);
  }

  private url(path: string): string {
    if (!this.baseUrl) return path;
    return new URL(path, this.baseUrl.endsWith('/') ? this.baseUrl : `${this.baseUrl}/`).toString();
  }

  private async request<T>(
    method: string,
    path: string,
    body?: unknown,
    decode?: Decoder<T>,
  ): Promise<T> {
    const headers: Record<string, string> = { Accept: 'application/json' };
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    if (['POST', 'PUT', 'DELETE', 'PATCH'].includes(method)) {
      const csrf = getCSRFToken();
      if (csrf) headers['X-DevLAN-CSRF-Token'] = csrf;
    }

    let response: Response;
    try {
      response = await this.fetchImpl(this.url(path), {
        method,
        headers,
        credentials: 'same-origin',
        body: body === undefined ? undefined : JSON.stringify(body),
      });
    } catch (error) {
      throw new APIError(0, 'Network Error', `API indisponível: ${String(error)}`);
    }

    const contentType = response.headers.get('Content-Type') || '';
    let payload: unknown;
    if (response.status !== 204) {
      const raw = await response.text();
      if (raw !== '') {
        if (contentType.includes('application/json')) {
          try {
            payload = JSON.parse(raw) as unknown;
          } catch {
            throw new APIError(response.status, response.statusText, 'API retornou JSON inválido.');
          }
        } else {
          payload = raw;
        }
      }
    }
    if (!response.ok) {
      const errorBody = parseErrorBody(payload);
      throw new APIError(
        response.status,
        response.statusText,
        errorBody.message || `HTTP ${response.status} ${response.statusText}`,
        errorBody.details,
      );
    }
    if (!decode) return payload as T;
    return decode(payload);
  }

  getProjects(filter = ''): Promise<ProjectInfo[]> {
    const query = filter ? `?filter=${encodeURIComponent(filter)}` : '';
    return this.request('GET', `/api/v1/projects${query}`, undefined, parseProjects);
  }

  getOverview(filter = ''): Promise<Overview> {
    const query = filter ? `?filter=${encodeURIComponent(filter)}` : '';
    return this.request('GET', `/api/v1/overview${query}`, undefined, parseOverview);
  }

  getStatus(): Promise<SystemStatus> {
    return this.request('GET', '/api/v1/status', undefined, parseSystemStatus);
  }

  getTopology(): Promise<Record<string, unknown>> {
    return this.request('GET', '/api/v1/topology', undefined, parseTopology);
  }

  getMetrics(project: string, range: MetricsRange): Promise<MetricsSnapshot | null> {
    return this.request(
      'GET',
      `/api/v1/metrics?project=${encodeURIComponent(project)}&range=${encodeURIComponent(range)}`,
      undefined,
      parseMetricsSnapshot,
    );
  }

  getGlobalConfig(): Promise<GlobalConfig> {
    return this.request('GET', '/api/v1/config', undefined, parseGlobalConfig);
  }

  getPHPVersions(): Promise<PHPVersion[]> {
    return this.request('GET', '/api/v1/php/versions', undefined, parsePHPVersions);
  }

  async installPHPVersion(version: string): Promise<void> {
    await this.request('POST', '/api/v1/php/install', { version });
  }

  async removePHPVersion(version: string): Promise<void> {
    await this.request('POST', '/api/v1/php/remove', { version });
  }

  async setDefaultPHPVersion(version: string): Promise<void> {
    await this.request('POST', '/api/v1/php/default', { version });
  }

  async saveGlobalConfig(cfg: GlobalConfig): Promise<void> {
    await this.request('POST', '/api/v1/config', cfg);
  }

  async saveProjectConfig(update: ProjectConfigUpdate): Promise<void> {
    await this.request('POST', '/api/v1/projects/config', update);
  }

  async linkProject(name: string, path: string): Promise<void> {
    await this.request('POST', '/api/v1/projects/link', { name, path });
  }

  async unlinkProject(name: string): Promise<void> {
    await this.request('POST', '/api/v1/projects/unlink', { name });
  }

  async hideProject(name: string): Promise<void> {
    await this.request('POST', '/api/v1/projects/hide', { name });
  }

  async parkDir(path: string): Promise<void> {
    await this.request('POST', '/api/v1/parks/park', { path });
  }

  async unparkDir(path: string): Promise<void> {
    await this.request('POST', '/api/v1/parks/unpark', { path });
  }

  async startDev(name: string): Promise<void> {
    await this.request('POST', '/api/v1/projects/start', { name });
  }

  async stopDev(name: string): Promise<void> {
    await this.request('POST', '/api/v1/projects/stop', { name });
  }

  async restartDev(name: string): Promise<void> {
    await this.request('POST', '/api/v1/projects/restart', { name });
  }

  async buildProject(name: string): Promise<string> {
    const result = await this.request<unknown>('POST', '/api/v1/projects/build', { name });
    return objectField(result, 'output');
  }

  async installDeps(name: string): Promise<string> {
    const result = await this.request<unknown>('POST', '/api/v1/projects/deps', { name });
    return objectField(result, 'output');
  }

  async getProjectLogs(name: string, lines = 100): Promise<string> {
    const result = await this.request<unknown>(
      'GET',
      `/api/v1/projects/logs?name=${encodeURIComponent(name)}&lines=${Math.max(1, Math.floor(lines))}`,
    );
    return objectField(result, 'logs');
  }

  runDoctor(name = ''): Promise<DoctorCheck[]> {
    const query = name ? `?project=${encodeURIComponent(name)}` : '';
    return this.request('GET', `/api/v1/doctor${query}`, undefined, parseDoctorChecks);
  }

  async applyDoctorFix(action: string, target: string): Promise<void> {
    await this.request('POST', '/api/v1/doctor/fix', { action, target });
  }

  async openURL(url: string): Promise<void> {
    if (typeof window !== 'undefined' && window.runtime?.BrowserOpenURL) {
      window.runtime.BrowserOpenURL(url);
      return;
    }
    if (typeof window !== 'undefined') window.open(url, '_blank', 'noopener,noreferrer');
  }

  async copyURL(url: string): Promise<void> {
    if (typeof window !== 'undefined' && window.runtime?.ClipboardSetText) {
      await window.runtime.ClipboardSetText(url);
      return;
    }
    if (typeof navigator !== 'undefined' && navigator.clipboard) {
      await navigator.clipboard.writeText(url);
      return;
    }
    throw new Error('Área de transferência indisponível.');
  }

  async reload(): Promise<void> {
    await this.request('POST', '/api/v1/reload');
  }

  async exportConfigJSON(): Promise<string> {
    const result = await this.request<unknown>('POST', '/api/v1/config/export');
    if (typeof result === 'string') return result;
    return JSON.stringify(result, null, 2);
  }

  async exportDiagnostic(): Promise<string> {
    throw new Error('Diagnóstico ZIP ainda não possui download no navegador.');
  }

  async trustCA(): Promise<void> {
    await this.request('POST', '/api/v1/security/trust');
  }

  async getSecurityAudit(lines = 100): Promise<string> {
    const result = await this.request<unknown>(
      'GET',
      `/api/v1/security/audit?lines=${Math.max(1, Math.floor(lines))}`,
    );
    return objectField(result, 'logs');
  }
}

export class WailsDevLANClient implements DevLANClient {
  constructor(
    private readonly app: WailsApp,
    private readonly runtime?: WailsRuntime,
  ) {}

  async getOverview(filter = ''): Promise<Overview> {
    if (this.app.GetOverview) return parseOverview(await this.app.GetOverview(filter));
    const [projects, status, phpVersions] = await Promise.all([
      this.getProjects(filter),
      this.getStatus(),
      this.getPHPVersions(),
    ]);
    return { projects, status, phpVersions };
  }

  async getProjects(filter = ''): Promise<ProjectInfo[]> {
    return parseProjects(await this.app.GetProjects(filter));
  }

  async getStatus(): Promise<SystemStatus> {
    return parseSystemStatus(await this.app.GetStatus());
  }

  async getTopology(): Promise<Record<string, unknown>> {
    if (!this.app.GetTopology) return {};
    return parseTopology(await this.app.GetTopology());
  }

  async getMetrics(project: string, range: MetricsRange): Promise<MetricsSnapshot | null> {
    if (!this.app.GetMetrics) return null;
    return parseMetricsSnapshot(await this.app.GetMetrics(project, range));
  }

  async getGlobalConfig(): Promise<GlobalConfig> {
    return parseGlobalConfig(await this.app.GetGlobalConfig());
  }

  async getPHPVersions(): Promise<PHPVersion[]> {
    return parsePHPVersions(await this.app.GetPHPVersions());
  }

  installPHPVersion(version: string) {
    return this.app.InstallPHPVersion(version);
  }
  removePHPVersion(version: string) {
    return this.app.RemovePHPVersion(version);
  }
  setDefaultPHPVersion(version: string) {
    return this.app.SetDefaultPHPVersion(version);
  }
  saveGlobalConfig(cfg: GlobalConfig) {
    return this.app.SaveGlobalConfig(cfg);
  }
  saveProjectConfig(update: ProjectConfigUpdate) {
    return this.app.SaveProjectConfig(update);
  }
  linkProject(name: string, path: string) {
    return this.app.LinkProject(name, path);
  }
  unlinkProject(name: string) {
    return this.app.UnlinkProject(name);
  }
  hideProject(name: string) {
    return this.app.HideProject(name);
  }
  parkDir(path: string) {
    return this.app.ParkDir(path);
  }
  unparkDir(path: string) {
    return this.app.UnparkDir(path);
  }
  startDev(name: string) {
    return this.app.StartDev(name);
  }
  stopDev(name: string) {
    return this.app.StopDev(name);
  }
  restartDev(name: string) {
    return this.app.RestartDev(name);
  }
  buildProject(name: string) {
    return this.app.BuildProject(name);
  }
  installDeps(name: string) {
    return this.app.InstallDeps(name);
  }
  getProjectLogs(name: string, lines = 100) {
    return this.app.GetProjectLogs(name, lines);
  }

  async runDoctor(name = ''): Promise<DoctorCheck[]> {
    return parseDoctorChecks(await this.app.RunDoctor(name));
  }

  applyDoctorFix(action: string, target: string) {
    return this.app.ApplyDoctorFix(action, target);
  }

  openURL(url: string) {
    if (this.app.OpenURL) return this.app.OpenURL(url);
    this.runtime?.BrowserOpenURL?.(url);
    return Promise.resolve();
  }

  copyURL(url: string) {
    if (this.app.CopyURL) return this.app.CopyURL(url);
    return this.runtime?.ClipboardSetText?.(url).then(() => undefined) ?? Promise.resolve();
  }

  reload() {
    return this.app.Reload();
  }
  exportConfigJSON() {
    return this.app.ExportConfigJSON();
  }
  exportDiagnostic() {
    return this.app.ExportDiagnostic();
  }
  trustCA() {
    return this.app.TrustCA();
  }
  getSecurityAudit(lines = 100) {
    return this.app.GetSecurityAudit(lines);
  }
}

export interface MockClientOptions {
  projects?: ProjectInfo[];
  status?: SystemStatus;
  phpVersions?: PHPVersion[];
  globalConfig?: GlobalConfig;
  metrics?: MetricsSnapshot | null;
  failures?: Partial<Record<keyof DevLANClient, Error>>;
}

export class MockDevLANClient implements DevLANClient {
  readonly calls: string[] = [];
  private projects: ProjectInfo[];
  private status: SystemStatus;
  private phpVersions: PHPVersion[];
  private globalConfig: GlobalConfig;
  private metrics: MetricsSnapshot | null;
  private readonly failures: Partial<Record<keyof DevLANClient, Error>>;

  constructor(options: MockClientOptions = {}) {
    this.projects = structuredClone(options.projects ?? defaultProjects);
    this.status = structuredClone(options.status ?? defaultStatus);
    this.phpVersions = structuredClone(options.phpVersions ?? defaultPHPVersions);
    this.globalConfig = structuredClone(options.globalConfig ?? defaultGlobalConfig);
    this.metrics = structuredClone(options.metrics ?? null);
    this.failures = options.failures ?? {};
  }

  private check<K extends keyof DevLANClient>(name: K) {
    this.calls.push(String(name));
    const error = this.failures[name];
    if (error) throw error;
  }

  async getOverview(filter = '') {
    this.check('getOverview');
    const [projects, status, phpVersions] = await Promise.all([
      this.getProjects(filter),
      this.getStatus(),
      this.getPHPVersions(),
    ]);
    return { projects, status, phpVersions };
  }

  async getProjects(filter = '') {
    this.check('getProjects');
    const query = filter.trim().toLowerCase();
    return this.projects.filter(
      (project) =>
        !query ||
        `${project.name} ${project.path} ${project.framework}`.toLowerCase().includes(query),
    );
  }
  async getStatus() {
    this.check('getStatus');
    return structuredClone(this.status);
  }
  async getTopology() {
    this.check('getTopology');
    return {
      topology: { topology: this.status.caddyTopology ?? 'unknown' },
      caddy: {
        running: this.status.caddyRunning ?? this.status.wslCaddyRunning,
        live: this.status.caddyLive ?? this.status.wslCaddyRunning,
      },
    };
  }
  async getMetrics() {
    this.check('getMetrics');
    return structuredClone(this.metrics);
  }
  async getGlobalConfig() {
    this.check('getGlobalConfig');
    return structuredClone(this.globalConfig);
  }
  async getPHPVersions() {
    this.check('getPHPVersions');
    return structuredClone(this.phpVersions);
  }
  async installPHPVersion(version: string) {
    this.check('installPHPVersion');
    this.phpVersions.push({ version, installed: true, configured: false });
  }
  async removePHPVersion(version: string) {
    this.check('removePHPVersion');
    this.phpVersions = this.phpVersions.filter((item) => item.version !== version);
  }
  async setDefaultPHPVersion(version: string) {
    this.check('setDefaultPHPVersion');
    this.globalConfig.phpDefaultVersion = version;
  }
  async saveGlobalConfig(cfg: GlobalConfig) {
    this.check('saveGlobalConfig');
    this.globalConfig = structuredClone(cfg);
  }
  async saveProjectConfig(update: ProjectConfigUpdate) {
    this.check('saveProjectConfig');
    this.projects = this.projects.map((project) => {
      if (project.name !== update.name) return project;
      const next = { ...project };
      if (update.tlsEnabled !== undefined) next.tlsEnabled = update.tlsEnabled;
      if (update.routePortAuto) {
        next.routePortOverride = undefined;
        next.port = defaultProjects.find((item) => item.name === next.name)?.port;
        next.lanUrl = buildLANURL(next);
      } else if (update.routePort !== undefined) {
        next.routePortOverride = update.routePort;
        next.port = update.routePort;
        next.lanUrl = buildLANURL(next);
      }
      return next;
    });
  }
  async linkProject() {
    this.check('linkProject');
  }
  async unlinkProject(name: string) {
    this.check('unlinkProject');
    this.projects = this.projects.filter((project) => project.name !== name);
  }
  async hideProject(name: string) {
    this.check('hideProject');
    this.projects = this.projects.filter((project) => project.name !== name);
  }
  async parkDir() {
    this.check('parkDir');
  }
  async unparkDir() {
    this.check('unparkDir');
  }
  async startDev(name: string) {
    this.check('startDev');
    this.setDev(name, true);
  }
  async stopDev(name: string) {
    this.check('stopDev');
    this.setDev(name, false);
  }
  async restartDev(name: string) {
    this.check('restartDev');
    this.setDev(name, true);
  }
  async buildProject() {
    this.check('buildProject');
    return 'Build concluído (mock).';
  }
  async installDeps() {
    this.check('installDeps');
    return 'Dependências instaladas (mock).';
  }
  async getProjectLogs(name: string) {
    this.check('getProjectLogs');
    return `[Mock] Logs de ${name}`;
  }
  async runDoctor() {
    this.check('runDoctor');
    return defaultDoctorChecks;
  }
  async applyDoctorFix() {
    this.check('applyDoctorFix');
  }
  async openURL() {
    this.check('openURL');
  }
  async copyURL() {
    this.check('copyURL');
  }
  async reload() {
    this.check('reload');
  }
  async exportConfigJSON() {
    this.check('exportConfigJSON');
    return JSON.stringify(this.globalConfig, null, 2);
  }
  async exportDiagnostic() {
    this.check('exportDiagnostic');
    return '';
  }
  async trustCA() {
    this.check('trustCA');
  }
  async getSecurityAudit() {
    this.check('getSecurityAudit');
    return '';
  }

  private setDev(name: string, running: boolean) {
    this.projects = this.projects.map((project) =>
      project.name === name
        ? {
            ...project,
            devRunning: running,
            localDevState: running ? 'active' : 'stopped',
            lanPreviewState: running ? 'paused' : 'ready',
          }
        : project,
    );
  }
}

function buildLANURL(project: ProjectInfo): string {
  try {
    const url = new URL(project.lanUrl);
    if (project.port) url.port = String(project.port);
    return url.toString();
  } catch {
    return project.lanUrl;
  }
}

const defaultProjects: ProjectInfo[] = [
  {
    name: 'financeiro',
    path: '/mnt/c/Users/dev/Sites/financeiro',
    kind: 'linked',
    mode: 'php',
    effectiveMode: 'php',
    framework: 'laravel',
    url: 'http://192.168.1.100:8080/',
    lanUrl: 'http://192.168.1.100:8080/',
    localDevUrl: 'https://financeiro.localhost/',
    localDevState: 'available',
    lanPreviewState: 'ready',
    tlsEnabled: false,
    port: 8080,
    status: 'ready',
    phpVersion: '8.3',
    devRunning: false,
  },
  {
    name: 'portal-vite',
    path: '/mnt/c/Users/dev/Sites/portal-vite',
    kind: 'linked',
    mode: 'dev',
    effectiveMode: 'dev',
    framework: 'vite',
    url: 'http://192.168.1.100:8081/',
    lanUrl: 'http://192.168.1.100:8081/',
    localDevUrl: 'https://portal-vite.localhost/',
    localDevState: 'active',
    lanPreviewState: 'paused',
    tlsEnabled: true,
    port: 8081,
    status: 'ready',
    devRunning: true,
    devPort: 5173,
    devPid: 12450,
  },
];

const defaultStatus: SystemStatus = {
  lanIp: '192.168.1.100',
  windowsPort: 80,
  httpsPort: 443,
  routeBasePort: 8080,
  routePortCount: 100,
  uiPort: 3210,
  tlsEnabled: true,
  defaultMode: 'auto',
  phpDefaultVersion: '8.3',
  windowsCaddyRunning: false,
  wslCaddyRunning: true,
  caddyRunning: true,
  caddyTopology: 'single-wsl',
  caddySystemd: true,
  caddyLive: true,
  mirroredNetworking: true,
  hypervFirewallOk: true,
  caRootValid: true,
  caRootTrusted: true,
  wslAvailable: true,
  firewallOk: true,
  phpVersions: ['8.3', '8.5'],
  totalProjects: 2,
  protocolVersion: 1,
};

const defaultPHPVersions: PHPVersion[] = [
  { version: '8.3', installed: true, configured: true },
  { version: '8.5', installed: true, configured: false },
];

const defaultGlobalConfig: GlobalConfig = {
  defaultMode: 'auto',
  windowsPort: 80,
  httpsPort: 443,
  tlsEnabled: false,
  phpDefaultVersion: '8.3',
  allowlist: ['192.168.0.0/16', '10.0.0.0/8'],
};

const defaultDoctorChecks: DoctorCheck[] = [
  {
    name: 'Firewall',
    status: 'OK',
    detail: 'Portas LAN reconciliadas para rede privada.',
    fixable: false,
  },
  { name: 'Caddy WSL único', status: 'OK', detail: 'Proxy de borda respondendo.', fixable: false },
  { name: 'WSL mirrored', status: 'OK', detail: 'Rede espelhada ativa.', fixable: false },
];

export type ClientMode = 'auto' | 'http' | 'wails' | 'mock';

export function createDevLANClient(
  options: {
    mode?: ClientMode;
    http?: HttpClientOptions;
    wailsApp?: WailsApp;
    runtime?: WailsRuntime;
    mock?: MockClientOptions;
  } = {},
): DevLANClient {
  if (options.mode === 'mock') return new MockDevLANClient(options.mock);
  if (options.mode === 'wails') {
    if (!options.wailsApp) throw new Error('Wails App não foi fornecido.');
    return new WailsDevLANClient(options.wailsApp, options.runtime);
  }
  if (options.mode === 'http') return new HttpDevLANClient(options.http);

  const isMock =
    typeof window !== 'undefined' &&
    (window.__DEVLAN_MOCK__ || window.location.search.includes('mock=true'));
  if (isMock) {
    const rollback =
      typeof window !== 'undefined' && window.location.search.includes('rollback=true');
    return new MockDevLANClient({
      ...options.mock,
      failures: rollback
        ? { ...options.mock?.failures, saveProjectConfig: new Error('operação rolled_back') }
        : options.mock?.failures,
    });
  }
  const wailsApp = typeof window !== 'undefined' ? window.go?.gui?.App : undefined;
  if (wailsApp) return new WailsDevLANClient(wailsApp, window.runtime);
  return new HttpDevLANClient(options.http);
}

export const api: DevLANClient = createDevLANClient();
