// The browser contract is generated from internal/api/contract.json. Keep
// application imports pointed at this stable module so generated files can be
// replaced without changing component code.
export type {
  DoctorCheck,
  GlobalConfig,
  LanPreviewState,
  LatencyBucket,
  LocalDevState,
  MetricsRange,
  MetricsSnapshot,
  Overview,
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
  key: OperationKey;
  projectName?: string;
  targetState?: boolean;
}
