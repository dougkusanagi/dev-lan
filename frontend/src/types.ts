// The browser contract is temporarily generated from internal/api/contract.json
// while the canonical HTTP contract lives in api/openapi.yaml. Keep
// application imports pointed at this stable module until R-07d replaces the
// generated source without changing component code.
export type {
  DoctorCheck,
  GlobalConfig,
  LanPreviewState,
  LatencyBucket,
  LocalDevState,
  MetricsRange,
  MetricsSnapshot,
  MutationResult,
  Overview,
  OverviewMeta,
  PHPVersion,
  ProjectConfigUpdate,
  ProjectFramework,
  ProjectInfo,
  ProjectKind,
  ProjectMode,
  ProjectStatus,
  RouteSnapshot,
  SystemStatus,
  TrafficPoint,
} from './generated/api-contract';

export type MutationPhase =
  | 'accepted'
  | 'applying'
  | 'starting'
  | 'ready'
  | 'stopping'
  | 'stopped'
  | 'failed'
  | 'rolled_back';

export type OperationKey =
  | 'tls'
  | 'start'
  | 'stop'
  | 'restart'
  | 'build'
  | 'deps'
  | 'doctor'
  | 'php'
  | 'route-port'
  | 'ca'
  | 'firewall'
  | 'remove'
  | 'reload';

export interface PendingOperation {
  id: number;
  operationId: string;
  key: OperationKey;
  projectName?: string;
  targetState?: boolean;
}
