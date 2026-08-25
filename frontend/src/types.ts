export type ProjectStatus = 'stopped' | 'starting' | 'ready' | 'degraded' | 'error';

export interface ProjectInfo {
  name: string;
  path: string;
  kind: string; // 'linked' | 'parked'
  mode: string; // 'auto' | 'php' | 'dev' | 'static'
  effectiveMode: string;
  framework: string; // 'laravel' | 'symfony' | 'generic' | 'vite' | 'astro' | 'nextjs' | 'nuxt' | 'sveltekit' | 'static' | 'unknown'
  url: string;
  lanUrl: string;
  localDevUrl: string;
  localDevState: 'active' | 'starting' | 'stopped' | 'available';
  lanPreviewState: 'ready' | 'paused';
  tlsEnabled: boolean;
  routingMode: 'path' | 'port' | 'host';
  port?: number;
  host?: string;
  status: ProjectStatus;
  statusDetail?: string;
  phpVersion?: string;
  packageManager?: string;
  staticDir?: string;
  devRunning: boolean;
  devPid?: number;
  devPort?: number;
}

export interface SystemStatus {
  lanIp: string;
  windowsPort: number;
  httpsPort: number;
  tlsEnabled: boolean;
  defaultMode: string;
  phpDefaultVersion: string;
  windowsCaddyRunning: boolean;
  wslCaddyRunning: boolean;
  wslAvailable: boolean;
  firewallOk: boolean;
  phpVersions: string[];
  totalProjects: number;
}

export interface DoctorCheck {
  name: string;
  status: 'OK' | 'WARN' | 'FAIL';
  detail: string;
  fixable: boolean;
  fixAction?: string;
}

export interface GlobalConfig {
  defaultMode: string;
  windowsPort: number;
  httpsPort: number;
  tlsEnabled: boolean;
  phpDefaultVersion: string;
  allowlist: string[];
  defaultRouteMode: string;
}

export interface PHPVersion {
  version: string;
  installed: boolean;
  configured: boolean;
  extensions?: string[];
}

export type MetricsRange = '15m' | '1h' | '24h' | '7d';
export interface MetricsSnapshot {
  project: string;
  range: MetricsRange;
  generatedAt: string;
  excludedColdStarts: number;
  requests: number;
  requestsPerMinute: number;
  errorCount: number;
  errorRate: number;
  p50Ms: number | null;
  p95Ms: number | null;
  latencyBuckets: { upperBoundMs: number | null; count: number }[];
  traffic: { at: string; requestsPerMinute: number }[];
  routes: { method: string; normalizedPath: string; p50Ms: number | null; p95Ms: number | null; requests: number; errors: number }[];
}

export interface ProjectConfigUpdate {
  name: string;
  mode?: string;
  phpVersion?: string;
  phpPreset?: string;
  tlsEnabled?: boolean;
  routeMode?: string;
  routePort?: number;
  routeHost?: string;
  staticDir?: string;
  devCommand?: string;
  devPort?: number;
}
