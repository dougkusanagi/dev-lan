import { MetricsRange, MetricsSnapshot, ProjectInfo, SystemStatus, DoctorCheck, GlobalConfig, ProjectConfigUpdate, PHPVersion } from './types';

declare global {
  interface Window {
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
      EventsOn?: (event: string, callback: (...args: any[]) => void) => void;
    };
  }
}

const isWails = typeof window !== 'undefined' && !!window.go?.gui?.App;
const mockDevProjects = new Set(['portal-vite']);

export const api = {
  async getProjects(filter = ''): Promise<ProjectInfo[]> {
    if (isWails) return window.go!.gui!.App!.GetProjects(filter);
    return [
      {
        name: 'financeiro',
        path: '/mnt/c/Users/dev/Sites/financeiro',
        kind: 'linked',
        mode: 'php',
        effectiveMode: 'php',
        framework: 'laravel',
        url: 'http://192.168.1.100/financeiro',
        lanUrl: 'http://192.168.1.100/financeiro',
        tlsEnabled: false,
        routingMode: 'path',
        status: 'ready',
        phpVersion: '8.3',
        devRunning: mockDevProjects.has('financeiro'),
      },
      {
        name: 'portal-vite',
        path: '/mnt/c/Users/dev/Sites/portal-vite',
        kind: 'linked',
        mode: 'dev',
        effectiveMode: 'dev',
        framework: 'vite',
        url: 'http://192.168.1.100/portal-vite',
        lanUrl: 'http://192.168.1.100/portal-vite',
        tlsEnabled: true,
        routingMode: 'path',
        status: 'ready',
        devRunning: mockDevProjects.has('portal-vite'),
        devPort: 5173,
        devPid: 12450,
      }
    ];
  },

  async getStatus(): Promise<SystemStatus> {
    if (isWails) return window.go!.gui!.App!.GetStatus();
    return {
      lanIp: '192.168.1.100',
      windowsPort: 80,
      httpsPort: 443,
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
  },

  async getMetrics(project: string, range: MetricsRange): Promise<MetricsSnapshot | null> {
    if (isWails) {
      const method = window.go!.gui!.App!.GetMetrics;
      return method ? method(project, range) : null;
    }
    return null;
  },

  async getGlobalConfig(): Promise<GlobalConfig> {
    if (isWails) return window.go!.gui!.App!.GetGlobalConfig();
    return {
      defaultMode: 'auto',
      windowsPort: 80,
      httpsPort: 443,
      tlsEnabled: false,
      phpDefaultVersion: '8.3',
      allowlist: ['192.168.0.0/16', '10.0.0.0/8'],
      defaultRouteMode: 'path',
    };
  },

  async getPHPVersions(): Promise<PHPVersion[]> {
    if (isWails) return window.go!.gui!.App!.GetPHPVersions();
    return [
      { version: '8.3', installed: true, configured: true },
      { version: '8.5', installed: true, configured: false },
    ];
  },

  async installPHPVersion(version: string): Promise<void> {
    if (isWails) return window.go!.gui!.App!.InstallPHPVersion(version);
  },

  async removePHPVersion(version: string): Promise<void> {
    if (isWails) return window.go!.gui!.App!.RemovePHPVersion(version);
  },

  async setDefaultPHPVersion(version: string): Promise<void> {
    if (isWails) return window.go!.gui!.App!.SetDefaultPHPVersion(version);
  },

  async saveGlobalConfig(cfg: GlobalConfig): Promise<void> {
    if (isWails) return window.go!.gui!.App!.SaveGlobalConfig(cfg);
  },

  async saveProjectConfig(update: ProjectConfigUpdate): Promise<void> {
    if (isWails) return window.go!.gui!.App!.SaveProjectConfig(update);
  },

  async linkProject(name: string, path: string): Promise<void> {
    if (isWails) return window.go!.gui!.App!.LinkProject(name, path);
  },

  async unlinkProject(name: string): Promise<void> {
    if (isWails) return window.go!.gui!.App!.UnlinkProject(name);
  },

  async hideProject(name: string): Promise<void> {
    if (isWails) return window.go!.gui!.App!.HideProject(name);
  },

  async parkDir(path: string): Promise<void> {
    if (isWails) return window.go!.gui!.App!.ParkDir(path);
  },

  async unparkDir(path: string): Promise<void> {
    if (isWails) return window.go!.gui!.App!.UnparkDir(path);
  },

  async startDev(name: string): Promise<void> {
    if (isWails) return window.go!.gui!.App!.StartDev(name);
    mockDevProjects.add(name);
  },

  async stopDev(name: string): Promise<void> {
    if (isWails) return window.go!.gui!.App!.StopDev(name);
    mockDevProjects.delete(name);
  },

  async restartDev(name: string): Promise<void> {
    if (isWails) return window.go!.gui!.App!.RestartDev(name);
    mockDevProjects.add(name);
  },

  async buildProject(name: string): Promise<string> {
    if (isWails) return window.go!.gui!.App!.BuildProject(name);
    return 'Build concluído com sucesso (Mock)';
  },

  async installDeps(name: string): Promise<string> {
    if (isWails) return window.go!.gui!.App!.InstallDeps(name);
    return 'Dependências instaladas (Mock)';
  },

  async getProjectLogs(name: string, lines = 100): Promise<string> {
    if (isWails) return window.go!.gui!.App!.GetProjectLogs(name, lines);
    return `[DevLAN Log Mock para ${name}]\nServidor inicializado em http://localhost:5173\nPronto para receber conexões da LAN.`;
  },

  async runDoctor(name = ''): Promise<DoctorCheck[]> {
    if (isWails) return window.go!.gui!.App!.RunDoctor(name);
    return [
      { name: 'Firewall', status: 'OK', detail: 'Porta 80 e 443 abertas para rede privada', fixable: false },
      { name: 'Caddy Windows', status: 'OK', detail: 'Executando e respondendo em 127.0.0.1:2019', fixable: false },
      { name: 'Caddy WSL', status: 'OK', detail: 'Executando e respondendo em 127.0.0.1:2020', fixable: false },
      { name: 'PHP-FPM', status: 'OK', detail: 'PHP 8.3 mestre ativo com pm=ondemand', fixable: false },
    ];
  },

  async applyDoctorFix(action: string, target: string): Promise<void> {
    if (isWails) return window.go!.gui!.App!.ApplyDoctorFix(action, target);
  },

  async openURL(url: string): Promise<void> {
    if (isWails) {
      return window.go!.gui!.App!.OpenURL(url);
    }
    window.open(url, '_blank');
  },

  async copyURL(url: string): Promise<void> {
    if (isWails) {
      return window.go!.gui!.App!.CopyURL(url);
    }
    await navigator.clipboard.writeText(url);
  },

  async reload(): Promise<void> {
    if (isWails) return window.go!.gui!.App!.Reload();
  },

  async exportConfigJSON(): Promise<string> {
    if (isWails) return window.go!.gui!.App!.ExportConfigJSON();
    return JSON.stringify({}, null, 2);
  },

  async exportDiagnostic(): Promise<string> {
    if (isWails) return window.go!.gui!.App!.ExportDiagnostic();
    return '';
  },

  async trustCA(): Promise<void> {
    if (isWails) return window.go!.gui!.App!.TrustCA();
  },

  async getSecurityAudit(lines = 100): Promise<string> {
    if (isWails) return window.go!.gui!.App!.GetSecurityAudit(lines);
    return '';
  }
};
