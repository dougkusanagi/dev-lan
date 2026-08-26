import { DEVLAN_PROTOCOL_VERSION } from './generated/api-contract';
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

export { DEVLAN_API_CONTRACT, DEVLAN_PROTOCOL_VERSION } from './generated/api-contract';

export class ContractError extends Error {
  constructor(message: string) {
    super(`Contrato DevLAN inválido: ${message}`);
    this.name = 'ContractError';
  }
}

export class VersionMismatchError extends ContractError {
  readonly expected: number;
  readonly received: number;

  constructor(expected: number, received: number) {
    super(`versão incompatível (esperada ${expected}, recebida ${received})`);
    this.name = 'VersionMismatchError';
    this.expected = expected;
    this.received = received;
  }
}

function fail(path: string, expected: string, value: unknown): never {
  const received = value === null ? 'null' : Array.isArray(value) ? 'array' : typeof value;
  throw new ContractError(`${path} deveria ser ${expected}, recebido ${received}`);
}

function record(value: unknown, path: string): Record<string, unknown> {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) {
    return fail(path, 'um objeto', value);
  }
  return value as Record<string, unknown>;
}

function requiredString(value: Record<string, unknown>, key: string, path: string): string {
  const item = value[key];
  if (typeof item !== 'string') return fail(`${path}.${key}`, 'string', item);
  return item;
}

function optionalString(
  value: Record<string, unknown>,
  key: string,
  path: string,
): string | undefined {
  const item = value[key];
  if (item === undefined) return undefined;
  if (typeof item !== 'string') return fail(`${path}.${key}`, 'string opcional', item);
  return item;
}

function requiredNumber(value: Record<string, unknown>, key: string, path: string): number {
  const item = value[key];
  if (typeof item !== 'number' || !Number.isFinite(item)) {
    return fail(`${path}.${key}`, 'número finito', item);
  }
  return item;
}

function optionalNumber(
  value: Record<string, unknown>,
  key: string,
  path: string,
): number | undefined {
  const item = value[key];
  if (item === undefined) return undefined;
  if (typeof item !== 'number' || !Number.isFinite(item)) {
    return fail(`${path}.${key}`, 'número finito opcional', item);
  }
  return item;
}

function requiredBoolean(value: Record<string, unknown>, key: string, path: string): boolean {
  const item = value[key];
  if (typeof item !== 'boolean') return fail(`${path}.${key}`, 'booleano', item);
  return item;
}

function optionalBoolean(
  value: Record<string, unknown>,
  key: string,
  path: string,
): boolean | undefined {
  const item = value[key];
  if (item === undefined) return undefined;
  if (typeof item !== 'boolean') return fail(`${path}.${key}`, 'booleano opcional', item);
  return item;
}

function stringArray(value: Record<string, unknown>, key: string, path: string): string[] {
  const item = value[key];
  if (!Array.isArray(item) || item.some((entry) => typeof entry !== 'string')) {
    return fail(`${path}.${key}`, 'array de strings', item);
  }
  return item as string[];
}

function arrayOf<T>(value: unknown, path: string, parser: (item: unknown, path: string) => T): T[] {
  if (!Array.isArray(value)) return fail(path, 'array', value);
  return value.map((item, index) => parser(item, `${path}[${index}]`));
}

function oneOf<T extends string>(
  value: Record<string, unknown>,
  key: string,
  path: string,
  choices: readonly T[],
): T {
  const item = requiredString(value, key, path);
  if (!choices.includes(item as T)) return fail(`${path}.${key}`, choices.join(' | '), item);
  return item as T;
}

const projectModes = ['auto', 'php', 'dev', 'static'] as const;
const projectStatuses = ['stopped', 'starting', 'ready', 'degraded', 'error'] as const;
const projectKinds = ['linked', 'parked'] as const;
const localStates = ['active', 'starting', 'stopped', 'available'] as const;
const lanStates = ['ready', 'paused'] as const;
const frameworks = [
  'laravel',
  'symfony',
  'generic',
  'vite',
  'astro',
  'nextjs',
  'nuxt',
  'sveltekit',
  'static',
  'unknown',
] as const;
const ranges = ['15m', '1h', '24h', '7d'] as const;

export function parseProjectInfo(value: unknown, path = 'projects[]'): ProjectInfo {
  const item = record(value, path);
  return {
    name: requiredString(item, 'name', path),
    path: requiredString(item, 'path', path),
    kind: oneOf(item, 'kind', path, projectKinds),
    mode: (() => {
      const mode = requiredString(item, 'mode', path);
      if (mode !== '' && !projectModes.includes(mode as (typeof projectModes)[number])) {
        return fail(`${path}.mode`, "'', auto | php | dev | static", mode);
      }
      return mode as ProjectInfo['mode'];
    })(),
    effectiveMode: oneOf(item, 'effectiveMode', path, projectModes),
    framework: oneOf(item, 'framework', path, frameworks),
    url: requiredString(item, 'url', path),
    lanUrl: requiredString(item, 'lanUrl', path),
    localDevUrl: requiredString(item, 'localDevUrl', path),
    localDevState: oneOf(item, 'localDevState', path, localStates),
    lanPreviewState: oneOf(item, 'lanPreviewState', path, lanStates),
    tlsEnabled: requiredBoolean(item, 'tlsEnabled', path),
    port: optionalNumber(item, 'port', path),
    routePortOverride: optionalNumber(item, 'routePortOverride', path),
    status: oneOf(item, 'status', path, projectStatuses),
    statusDetail: optionalString(item, 'statusDetail', path),
    phpVersion: optionalString(item, 'phpVersion', path),
    packageManager: optionalString(item, 'packageManager', path),
    staticDir: optionalString(item, 'staticDir', path),
    devRunning: requiredBoolean(item, 'devRunning', path),
    devPid: optionalNumber(item, 'devPid', path),
    devPort: optionalNumber(item, 'devPort', path),
  };
}

export function parseProjects(value: unknown): ProjectInfo[] {
  return arrayOf(value, 'projects', parseProjectInfo);
}

export function parseOverview(value: unknown, expectedVersion = DEVLAN_PROTOCOL_VERSION): Overview {
  const item = record(value, 'overview');
  return {
    projects: parseProjects(item.projects),
    status: parseSystemStatus(item.status, expectedVersion),
    phpVersions: parsePHPVersions(item.phpVersions),
  };
}

export function parseSystemStatus(
  value: unknown,
  expectedVersion = DEVLAN_PROTOCOL_VERSION,
): SystemStatus {
  const item = record(value, 'status');
  const protocolVersion = requiredNumber(item, 'protocolVersion', 'status');
  if (protocolVersion !== expectedVersion) {
    throw new VersionMismatchError(expectedVersion, protocolVersion);
  }
  return {
    lanIp: requiredString(item, 'lanIp', 'status'),
    windowsPort: requiredNumber(item, 'windowsPort', 'status'),
    httpsPort: requiredNumber(item, 'httpsPort', 'status'),
    routeBasePort: requiredNumber(item, 'routeBasePort', 'status'),
    routePortCount: requiredNumber(item, 'routePortCount', 'status'),
    uiPort: requiredNumber(item, 'uiPort', 'status'),
    tlsEnabled: requiredBoolean(item, 'tlsEnabled', 'status'),
    defaultMode: oneOf(item, 'defaultMode', 'status', projectModes),
    phpDefaultVersion: requiredString(item, 'phpDefaultVersion', 'status'),
    windowsCaddyRunning: requiredBoolean(item, 'windowsCaddyRunning', 'status'),
    wslCaddyRunning: requiredBoolean(item, 'wslCaddyRunning', 'status'),
    wslAvailable: requiredBoolean(item, 'wslAvailable', 'status'),
    firewallOk: requiredBoolean(item, 'firewallOk', 'status'),
    phpVersions: stringArray(item, 'phpVersions', 'status'),
    totalProjects: requiredNumber(item, 'totalProjects', 'status'),
    protocolVersion,
  };
}

export function parsePHPVersion(value: unknown, path = 'phpVersions[]'): PHPVersion {
  const item = record(value, path);
  const extensions = item.extensions;
  if (
    extensions !== undefined &&
    (!Array.isArray(extensions) || extensions.some((entry) => typeof entry !== 'string'))
  ) {
    return fail(`${path}.extensions`, 'array de strings opcional', extensions);
  }
  return {
    version: requiredString(item, 'version', path),
    installed: requiredBoolean(item, 'installed', path),
    configured: requiredBoolean(item, 'configured', path),
    ...(extensions === undefined ? {} : { extensions: extensions as string[] }),
  };
}

export function parsePHPVersions(value: unknown): PHPVersion[] {
  return arrayOf(value, 'phpVersions', parsePHPVersion);
}

export function parseDoctorCheck(value: unknown, path = 'doctor[]'): DoctorCheck {
  const item = record(value, path);
  return {
    name: requiredString(item, 'name', path),
    status: oneOf(item, 'status', path, ['OK', 'WARN', 'FAIL'] as const),
    detail: requiredString(item, 'detail', path),
    fixable: requiredBoolean(item, 'fixable', path),
    fixAction: optionalString(item, 'fixAction', path),
  };
}

export function parseDoctorChecks(value: unknown): DoctorCheck[] {
  return arrayOf(value, 'doctor', parseDoctorCheck);
}

export function parseGlobalConfig(value: unknown): GlobalConfig {
  const item = record(value, 'config');
  return {
    defaultMode: oneOf(item, 'defaultMode', 'config', projectModes),
    windowsPort: requiredNumber(item, 'windowsPort', 'config'),
    httpsPort: requiredNumber(item, 'httpsPort', 'config'),
    tlsEnabled: requiredBoolean(item, 'tlsEnabled', 'config'),
    phpDefaultVersion: requiredString(item, 'phpDefaultVersion', 'config'),
    allowlist: stringArray(item, 'allowlist', 'config'),
  };
}

function nullableNumber(value: Record<string, unknown>, key: string, path: string): number | null {
  const item = value[key];
  if (item === null) return null;
  if (typeof item !== 'number' || !Number.isFinite(item))
    return fail(`${path}.${key}`, 'número ou null', item);
  return item;
}

function parseLatencyBucket(value: unknown, path: string) {
  const item = record(value, path);
  return {
    upperBoundMs: nullableNumber(item, 'upperBoundMs', path),
    count: requiredNumber(item, 'count', path),
  };
}

function parseTrafficPoint(value: unknown, path: string) {
  const item = record(value, path);
  return {
    at: requiredString(item, 'at', path),
    requestsPerMinute: requiredNumber(item, 'requestsPerMinute', path),
  };
}

function parseRouteSnapshot(value: unknown, path: string) {
  const item = record(value, path);
  return {
    method: requiredString(item, 'method', path),
    normalizedPath: requiredString(item, 'normalizedPath', path),
    p50Ms: nullableNumber(item, 'p50Ms', path),
    p95Ms: nullableNumber(item, 'p95Ms', path),
    requests: requiredNumber(item, 'requests', path),
    errors: requiredNumber(item, 'errors', path),
  };
}

export function parseMetricsSnapshot(value: unknown): MetricsSnapshot | null {
  if (value === null) return null;
  const item = record(value, 'metrics');
  return {
    project: requiredString(item, 'project', 'metrics'),
    range: oneOf(item, 'range', 'metrics', ranges) as MetricsRange,
    generatedAt: requiredString(item, 'generatedAt', 'metrics'),
    excludedColdStarts: requiredNumber(item, 'excludedColdStarts', 'metrics'),
    requests: requiredNumber(item, 'requests', 'metrics'),
    requestsPerMinute: requiredNumber(item, 'requestsPerMinute', 'metrics'),
    errorCount: requiredNumber(item, 'errorCount', 'metrics'),
    errorRate: requiredNumber(item, 'errorRate', 'metrics'),
    p50Ms: nullableNumber(item, 'p50Ms', 'metrics'),
    p95Ms: nullableNumber(item, 'p95Ms', 'metrics'),
    latencyBuckets: arrayOf(item.latencyBuckets, 'metrics.latencyBuckets', parseLatencyBucket),
    traffic: arrayOf(item.traffic, 'metrics.traffic', parseTrafficPoint),
    routes: arrayOf(item.routes, 'metrics.routes', parseRouteSnapshot),
  };
}

export function parseString(value: unknown, path = 'response'): string {
  if (typeof value !== 'string') return fail(path, 'string', value);
  return value;
}

export function parseProjectConfigUpdate(value: unknown): ProjectConfigUpdate {
  const item = record(value, 'projectConfig');
  return {
    name: requiredString(item, 'name', 'projectConfig'),
    mode: optionalString(item, 'mode', 'projectConfig') as ProjectConfigUpdate['mode'],
    phpVersion: optionalString(item, 'phpVersion', 'projectConfig'),
    phpPreset: optionalString(item, 'phpPreset', 'projectConfig'),
    tlsEnabled: optionalBoolean(item, 'tlsEnabled', 'projectConfig'),
    routePort: optionalNumber(item, 'routePort', 'projectConfig'),
    routePortAuto: optionalBoolean(item, 'routePortAuto', 'projectConfig'),
    staticDir: optionalString(item, 'staticDir', 'projectConfig'),
    devCommand: optionalString(item, 'devCommand', 'projectConfig'),
    devPort: optionalNumber(item, 'devPort', 'projectConfig'),
  };
}
