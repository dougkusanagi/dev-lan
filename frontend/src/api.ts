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

const isWails = typeof window !== 'undefined' && !!window.go?.gui?.App;
const mockDevProjects = new Set(['portal-vite']);

function getWailsApp() {
  const app = window.go?.gui?.App;
  if (!app) throw new Error('Wails App indisponível.');
  return app;
}

export const api = {
  async getProjects(filter = ''): Promise<ProjectInfo[]> {
    if (isWails) return getWailsApp().GetProjects(filter);
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
        devRunning: mockDevProjects.has('financeiro'),
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
        devRunning: mockDevProjects.has('portal-vite'),
        devPort: 5173,
        devPid: 12450,
      },
    ];
  },

  async getStatus(): Promise<SystemStatus> {
    if (isWails) return getWailsApp().GetStatus();
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
      const method = getWailsApp().GetMetrics;
      return method ? method(project, range) : null;
    }
    return null;
  },

  async getGlobalConfig(): Promise<GlobalConfig> {
    if (isWails) return getWailsApp().GetGlobalConfig();
    return {
      defaultMode: 'auto',
      windowsPort: 80,
      httpsPort: 443,
      tlsEnabled: false,
      phpDefaultVersion: '8.3',
      allowlist: ['192.168.0.0/16', '10.0.0.0/8'],
    };
  },

  async getPHPVersions(): Promise<PHPVersion[]> {
    if (isWails) return getWailsApp().GetPHPVersions();
    return [
      { version: '8.3', installed: true, configured: true },
      { version: '8.5', installed: true, configured: false },
    ];
  },

  async installPHPVersion(version: string): Promise<void> {
    if (isWails) return getWailsApp().InstallPHPVersion(version);
  },

  async removePHPVersion(version: string): Promise<void> {
    if (isWails) return getWailsApp().RemovePHPVersion(version);
  },

  async setDefaultPHPVersion(version: string): Promise<void> {
    if (isWails) return getWailsApp().SetDefaultPHPVersion(version);
  },

  async saveGlobalConfig(cfg: GlobalConfig): Promise<void> {
    if (isWails) return getWailsApp().SaveGlobalConfig(cfg);
  },

  async saveProjectConfig(update: ProjectConfigUpdate): Promise<void> {
    if (isWails) return getWailsApp().SaveProjectConfig(update);
  },

  async linkProject(name: string, path: string): Promise<void> {
    if (isWails) return getWailsApp().LinkProject(name, path);
  },

  async unlinkProject(name: string): Promise<void> {
    if (isWails) return getWailsApp().UnlinkProject(name);
  },

  async hideProject(name: string): Promise<void> {
    if (isWails) return getWailsApp().HideProject(name);
  },

  async parkDir(path: string): Promise<void> {
    if (isWails) return getWailsApp().ParkDir(path);
  },

  async unparkDir(path: string): Promise<void> {
    if (isWails) return getWailsApp().UnparkDir(path);
  },

  async startDev(name: string): Promise<void> {
    if (isWails) return getWailsApp().StartDev(name);
    mockDevProjects.add(name);
  },

  async stopDev(name: string): Promise<void> {
    if (isWails) return getWailsApp().StopDev(name);
    mockDevProjects.delete(name);
  },

  async restartDev(name: string): Promise<void> {
    if (isWails) return getWailsApp().RestartDev(name);
    mockDevProjects.add(name);
  },

  async buildProject(name: string): Promise<string> {
    if (isWails) return getWailsApp().BuildProject(name);
    return 'Build concluído com sucesso (Mock)';
  },

  async installDeps(name: string): Promise<string> {
    if (isWails) return getWailsApp().InstallDeps(name);
    return 'Dependências instaladas (Mock)';
  },

  async getProjectLogs(name: string, lines = 100): Promise<string> {
    if (isWails) return getWailsApp().GetProjectLogs(name, lines);
    return `[DevLAN Log Mock para ${name}]\nServidor inicializado em http://localhost:5173\nPronto para receber conexões da LAN.`;
  },

  async runDoctor(name = ''): Promise<DoctorCheck[]> {
    if (isWails) return getWailsApp().RunDoctor(name);
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
  },

  async applyDoctorFix(action: string, target: string): Promise<void> {
    if (isWails) return getWailsApp().ApplyDoctorFix(action, target);
  },

  async openURL(url: string): Promise<void> {
    if (isWails) {
      return getWailsApp().OpenURL(url);
    }
    window.open(url, '_blank');
  },

  async copyURL(url: string): Promise<void> {
    if (isWails) {
      return getWailsApp().CopyURL(url);
    }
    await navigator.clipboard.writeText(url);
  },

  async reload(): Promise<void> {
    if (isWails) return getWailsApp().Reload();
  },

  async exportConfigJSON(): Promise<string> {
    if (isWails) return getWailsApp().ExportConfigJSON();
    return JSON.stringify({}, null, 2);
  },

  async exportDiagnostic(): Promise<string> {
    if (isWails) return getWailsApp().ExportDiagnostic();
    return '';
  },

  async trustCA(): Promise<void> {
    if (isWails) return getWailsApp().TrustCA();
  },

  async getSecurityAudit(lines = 100): Promise<string> {
    if (isWails) return getWailsApp().GetSecurityAudit(lines);
    return '';
  },
};
