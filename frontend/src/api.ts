import type {
  DoctorCheck,
  GlobalConfig,
  MetricsRange,
  MetricsSnapshot,
  PHPVersion,
  ProjectConfigUpdate,
  ProjectInfo,
  SystemStatus,
} from './types';

declare global {
  interface Window {
    __DEVLAN_MOCK__?: boolean;
    go?: {
      gui?: {
        App?: {
          GetProjects: (filter: string) => Promise<ProjectInfo[]>;
          GetStatus: () => Promise<SystemStatus>;
          GetMetrics?: (project: string, range: MetricsRange) => Promise<MetricsSnapshot | null>;
          GetGlobalConfig: () => Promise<GlobalConfig>;
          GetPHPVersions: () => Promise<PHPVersion[]>;
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
          RunDoctor: (name: string) => Promise<DoctorCheck[]>;
          ApplyDoctorFix: (action: string, target: string) => Promise<void>;
          OpenURL: (url: string) => Promise<void>;
          CopyURL: (url: string) => Promise<void>;
          Reload: () => Promise<void>;
          ExportConfigJSON: () => Promise<string>;
          ExportDiagnostic: () => Promise<string>;
          TrustCA: () => Promise<void>;
          GetSecurityAudit: (lines: number) => Promise<string>;
        };
      };
    };
    runtime?: {
      ClipboardSetText?: (text: string) => Promise<boolean>;
      BrowserOpenURL?: (url: string) => void;
      EventsOn?: (event: string, callback: (...args: unknown[]) => void) => void;
    };
  }
}

function getCSRFToken(): string {
  if (typeof document === 'undefined') return '';
  const match = document.cookie.match(/(?:^|;\s*)devlan_csrf=([^;]+)/);
  return match ? decodeURIComponent(match[1]) : '';
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {
    'Accept': 'application/json',
  };

  if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
  }

  const isMutation = method === 'POST' || method === 'PUT' || method === 'DELETE' || method === 'PATCH';
  if (isMutation) {
    const csrf = getCSRFToken();
    if (csrf) {
      headers['X-DevLAN-CSRF-Token'] = csrf;
    }
  }

  const response = await fetch(path, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });

  if (!response.ok) {
    let errorMsg = `HTTP ${response.status} ${response.statusText}`;
    try {
      const errJson = await response.json();
      if (errJson && errJson.error) {
        errorMsg = errJson.error;
      }
    } catch {
      // Keep default status message
    }
    throw new Error(errorMsg);
  }

  const contentType = response.headers.get('Content-Type') || '';
  if (contentType.includes('application/json')) {
    return response.json() as Promise<T>;
  }
  return response.text() as unknown as Promise<T>;
}

const isWails = typeof window !== 'undefined' && !!window.go?.gui?.App;
const isMock = typeof window !== 'undefined' && (window.__DEVLAN_MOCK__ || (typeof location !== 'undefined' && location.search.includes('mock=true')));

export const api = {
  async getProjects(filter = ''): Promise<ProjectInfo[]> {
    if (isMock) {
      return [
        {
          name: 'financeiro',
          path: '/mnt/c/Users/dev/Sites/financeiro',
          kind: 'linked',
          mode: 'php',
          effectiveMode: 'php',
          framework: 'laravel',
          url: 'http://192.168.1.100:8080/',
          lanUrl: 'http://192.168.1.100:8080/',
          localDevUrl: 'https://financeiro.localhost',
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
          localDevUrl: 'https://portal-vite.localhost',
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
    }
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.GetProjects(filter);
    }
    const query = filter ? `?filter=${encodeURIComponent(filter)}` : '';
    return request<ProjectInfo[]>('GET', `/api/v1/projects${query}`);
  },

  async getStatus(): Promise<SystemStatus> {
    if (isMock) {
      return {
        lanIp: '192.168.1.100',
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
        phpVersions: ['8.3', '8.5'],
        totalProjects: 2,
      };
    }
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.GetStatus();
    }
    return request<SystemStatus>('GET', '/api/v1/status');
  },

  async getMetrics(project: string, range: MetricsRange): Promise<MetricsSnapshot | null> {
    if (isMock) {
      return null;
    }
    if (isWails && window.go?.gui?.App?.GetMetrics) {
      return window.go.gui.App.GetMetrics(project, range);
    }
    try {
      return await request<MetricsSnapshot | null>('GET', `/api/v1/metrics?project=${encodeURIComponent(project)}&range=${encodeURIComponent(range)}`);
    } catch {
      return null;
    }
  },

  async getGlobalConfig(): Promise<GlobalConfig> {
    if (isMock) {
      return {
        defaultMode: 'auto',
        windowsPort: 80,
        httpsPort: 443,
        tlsEnabled: false,
        phpDefaultVersion: '8.3',
        allowlist: ['192.168.0.0/16', '10.0.0.0/8'],
      };
    }
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.GetGlobalConfig();
    }
    return request<GlobalConfig>('GET', '/api/v1/config');
  },

  async getPHPVersions(): Promise<PHPVersion[]> {
    if (isMock) {
      return [
        { version: '8.3', installed: true, configured: true },
        { version: '8.5', installed: true, configured: false },
      ];
    }
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.GetPHPVersions();
    }
    return request<PHPVersion[]>('GET', '/api/v1/php/versions');
  },

  async installPHPVersion(version: string): Promise<void> {
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.InstallPHPVersion(version);
    }
    await request<void>('POST', '/api/v1/php/install', { version });
  },

  async removePHPVersion(version: string): Promise<void> {
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.RemovePHPVersion(version);
    }
    await request<void>('POST', '/api/v1/php/remove', { version });
  },

  async setDefaultPHPVersion(version: string): Promise<void> {
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.SetDefaultPHPVersion(version);
    }
    await request<void>('POST', '/api/v1/php/default', { version });
  },

  async saveGlobalConfig(cfg: GlobalConfig): Promise<void> {
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.SaveGlobalConfig(cfg);
    }
    await request<void>('POST', '/api/v1/config', cfg);
  },

  async saveProjectConfig(update: ProjectConfigUpdate): Promise<void> {
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.SaveProjectConfig(update);
    }
    await request<void>('POST', '/api/v1/projects/config', update);
  },

  async linkProject(name: string, path: string): Promise<void> {
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.LinkProject(name, path);
    }
    await request<void>('POST', '/api/v1/projects/link', { name, path });
  },

  async unlinkProject(name: string): Promise<void> {
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.UnlinkProject(name);
    }
    await request<void>('POST', '/api/v1/projects/unlink', { name });
  },

  async hideProject(name: string): Promise<void> {
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.HideProject(name);
    }
    await request<void>('POST', '/api/v1/projects/hide', { name });
  },

  async parkDir(path: string): Promise<void> {
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.ParkDir(path);
    }
    await request<void>('POST', '/api/v1/parks/park', { path });
  },

  async unparkDir(path: string): Promise<void> {
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.UnparkDir(path);
    }
    await request<void>('POST', '/api/v1/parks/unpark', { path });
  },

  async startDev(name: string): Promise<void> {
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.StartDev(name);
    }
    await request<void>('POST', '/api/v1/projects/start', { name });
  },

  async stopDev(name: string): Promise<void> {
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.StopDev(name);
    }
    await request<void>('POST', '/api/v1/projects/stop', { name });
  },

  async restartDev(name: string): Promise<void> {
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.RestartDev(name);
    }
    await request<void>('POST', '/api/v1/projects/restart', { name });
  },

  async buildProject(name: string): Promise<string> {
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.BuildProject(name);
    }
    const res = await request<{ output: string }>('POST', '/api/v1/projects/build', { name });
    return res.output || '';
  },

  async installDeps(name: string): Promise<string> {
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.InstallDeps(name);
    }
    const res = await request<{ output: string }>('POST', '/api/v1/projects/deps', { name });
    return res.output || '';
  },

  async getProjectLogs(name: string, lines = 100): Promise<string> {
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.GetProjectLogs(name, lines);
    }
    const res = await request<{ logs: string }>('GET', `/api/v1/projects/logs?name=${encodeURIComponent(name)}&lines=${lines}`);
    return res.logs || '';
  },

  async runDoctor(name = ''): Promise<DoctorCheck[]> {
    if (isMock) {
      return [
        {
          name: 'Firewall',
          status: 'OK',
          detail: 'Porta 80 e 443 abertas para rede privada',
          fixable: false,
        },
        {
          name: 'Caddy Windows',
          status: 'OK',
          detail: 'Executando e respondendo em 127.0.0.1:2019',
          fixable: false,
        },
        {
          name: 'Caddy WSL',
          status: 'OK',
          detail: 'Executando e respondendo em 127.0.0.1:2020',
          fixable: false,
        },
        {
          name: 'PHP-FPM',
          status: 'OK',
          detail: 'PHP 8.3 mestre ativo com pm=ondemand',
          fixable: false,
        },
      ];
    }
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.RunDoctor(name);
    }
    const query = name ? `?project=${encodeURIComponent(name)}` : '';
    return request<DoctorCheck[]>('GET', `/api/v1/doctor${query}`);
  },

  async applyDoctorFix(action: string, target: string): Promise<void> {
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.ApplyDoctorFix(action, target);
    }
    await request<void>('POST', '/api/v1/doctor/fix', { action, target });
  },

  async openURL(url: string): Promise<void> {
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.OpenURL(url);
    }
    if (window.runtime?.BrowserOpenURL) {
      window.runtime.BrowserOpenURL(url);
      return;
    }
    window.open(url, '_blank');
  },

  async copyURL(url: string): Promise<void> {
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.CopyURL(url);
    }
    if (window.runtime?.ClipboardSetText) {
      await window.runtime.ClipboardSetText(url);
      return;
    }
    await navigator.clipboard.writeText(url);
  },

  async reload(): Promise<void> {
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.Reload();
    }
    await request<void>('POST', '/api/v1/reload');
  },

  async exportConfigJSON(): Promise<string> {
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.ExportConfigJSON();
    }
    return request<string>('POST', '/api/v1/config/export');
  },

  async exportDiagnostic(): Promise<string> {
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.ExportDiagnostic();
    }
    return '';
  },

  async trustCA(): Promise<void> {
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.TrustCA();
    }
    await request<void>('POST', '/api/v1/security/trust');
  },

  async getSecurityAudit(lines = 100): Promise<string> {
    if (isWails && window.go?.gui?.App) {
      return window.go.gui.App.GetSecurityAudit(lines);
    }
    const res = await request<{ logs: string }>('GET', `/api/v1/security/audit?lines=${lines}`);
    return res.logs || '';
  },
};
