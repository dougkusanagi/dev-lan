import {
  AlertTriangle,
  Box,
  CircleCheck,
  Code2,
  LoaderCircle,
  Play,
  RotateCw,
  Square,
  Terminal,
  Trash2,
} from 'lucide-react';
import type { FormEvent } from 'react';
import { useEffect, useMemo, useState } from 'react';
import type { DevLANClient } from '../../api';
import { api } from '../../api';
import type {
  MetricsRange,
  MetricsSnapshot,
  OperationKey,
  PendingOperation,
  PHPVersion,
  ProjectInfo,
  SystemStatus,
} from '../../types';

function ServiceCard({ name, up, detail }: { name: string; up: boolean; detail: string }) {
  return (
    <article className="service-card">
      <div>
        <Box size={17} />
        <strong>{name}</strong>
      </div>
      <span className={up ? 'service-ok' : 'service-down'} role="status">
        {up ? (
          <CircleCheck size={14} aria-hidden="true" />
        ) : (
          <AlertTriangle size={14} aria-hidden="true" />
        )}{' '}
        {up ? 'Disponível' : 'Indisponível'}
      </span>
      <small>{detail}</small>
    </article>
  );
}

export function Overview({
  project,
  system,
  operations = [],
  phpVersions,
  onPHPVersion,
  onRoutePort = () => undefined,
  onTrustCA = () => undefined,
  onRepairFirewall = () => undefined,
  onRemove,
  onAction,
  client = api,
}: {
  project: ProjectInfo;
  system: SystemStatus | null;
  operations?: PendingOperation[];
  phpVersions: PHPVersion[];
  onPHPVersion: (version: string) => void;
  onRoutePort?: (port: number | null) => void;
  onTrustCA?: () => void;
  onRepairFirewall?: () => void;
  onRemove: () => void;
  onAction: (action: 'start' | 'stop' | 'restart' | 'build' | 'deps' | 'doctor') => void;
  client?: DevLANClient;
}) {
  const [range, setRange] = useState<MetricsRange>('1h');
  const [metrics, setMetrics] = useState<MetricsSnapshot | null>(null);
  const [metricsLoading, setMetricsLoading] = useState(false);
  const [metricsRefresh, setMetricsRefresh] = useState(0);
  const canRunDev = project.effectiveMode === 'dev' || project.framework === 'laravel';
  const installedPHP = phpVersions.filter((version) => version.installed);
  const projectOperations = operations.filter(
    (operation) => operation.projectName === project.name,
  );
  const projectBusy = projectOperations.length > 0;
  const isBusy = (...keys: OperationKey[]) =>
    operations.some(
      (operation) =>
        (operation.projectName === undefined || operation.projectName === project.name) &&
        (keys.length === 0 || keys.includes(operation.key)),
    );
  const hmrStarting = isBusy('start', 'restart');
  const hmrStopping = isBusy('stop');
  useEffect(() => {
    void metricsRefresh;
    let current = true;
    void Promise.resolve()
      .then(() => {
        if (current) setMetricsLoading(true);
        return client.getMetrics(project.name, range);
      })
      .then((value) => {
        if (current) setMetrics(value);
      })
      .catch(() => {
        if (current) setMetrics(null);
      })
      .finally(() => {
        if (current) setMetricsLoading(false);
      });
    return () => {
      current = false;
    };
  }, [client, project.name, range, metricsRefresh]);
  const removeLabel = project.kind === 'linked' ? 'Desvincular projeto' : 'Ocultar projeto';
  return (
    <div className="overview-content">
      <section>
        <h2 className="section-label">RUNTIME E WORKERS</h2>
        <div className="runtime-toolbar">
          {project.effectiveMode === 'php' ? (
            <label className="runtime-select">
              <Code2 size={16} aria-hidden="true" />
              <span>PHP</span>
              <select
                aria-label="Versão do PHP"
                value={project.phpVersion || ''}
                onChange={(e) => onPHPVersion(e.target.value)}
                disabled={projectBusy || installedPHP.length === 0}
              >
                <option value="">
                  {installedPHP.length ? 'Selecionar versão' : 'Nenhuma instalada'}
                </option>
                {installedPHP.map((version) => (
                  <option key={version.version} value={version.version}>
                    {version.version}
                    {version.configured ? ' (padrão)' : ''}
                  </option>
                ))}
              </select>
            </label>
          ) : (
            <div>
              <Code2 size={16} aria-hidden="true" />
              <span>{project.effectiveMode}</span>
            </div>
          )}
          {canRunDev && (
            <div>
              <Terminal size={16} aria-hidden="true" />
              <span>
                {project.packageManager || 'npm'} {project.devPort ? `:${project.devPort}` : ''}
              </span>
            </div>
          )}
          <RoutePortControl project={project} busy={projectBusy} onChange={onRoutePort} />
          <span
            className={`process-pill ${canRunDev ? (project.devRunning ? 'active' : 'stopped') : project.status}`}
            role="status"
          >
            <i />{' '}
            {canRunDev
              ? project.devRunning
                ? hmrStarting
                  ? 'Iniciando HMR local'
                  : 'HMR local ativo'
                : hmrStopping
                  ? 'Parando HMR local'
                  : 'HMR local parado'
              : 'Em execução'}
          </span>
          <div className="quick-actions">
            {canRunDev &&
              (project.devRunning ? (
                <button
                  type="button"
                  disabled={projectBusy}
                  aria-busy={hmrStarting || hmrStopping}
                  onClick={() => onAction('stop')}
                >
                  {hmrStarting || hmrStopping ? (
                    <LoaderCircle className="spin" size={14} aria-hidden="true" />
                  ) : (
                    <Square size={14} aria-hidden="true" />
                  )}{' '}
                  {hmrStarting
                    ? 'Iniciando HMR…'
                    : hmrStopping
                      ? 'Parando HMR…'
                      : 'Parar HMR local'}
                </button>
              ) : (
                <button
                  type="button"
                  disabled={projectBusy}
                  aria-busy={hmrStarting || hmrStopping}
                  onClick={() => onAction('start')}
                >
                  {hmrStarting || hmrStopping ? (
                    <LoaderCircle className="spin" size={14} aria-hidden="true" />
                  ) : (
                    <Play size={14} aria-hidden="true" />
                  )}{' '}
                  {hmrStarting
                    ? 'Iniciando HMR…'
                    : hmrStopping
                      ? 'Parando HMR…'
                      : 'Iniciar HMR local'}
                </button>
              ))}
            <button
              type="button"
              disabled={projectBusy}
              aria-busy={isBusy('build')}
              onClick={() => onAction('build')}
              title="Para o HMR local, gera o build e publica o preview na LAN"
            >
              {isBusy('build') && <LoaderCircle className="spin" size={14} aria-hidden="true" />}
              {isBusy('build') ? 'Publicando preview…' : 'Publicar preview LAN'}
            </button>
            <button
              type="button"
              disabled={projectBusy}
              aria-busy={isBusy('deps')}
              onClick={() => onAction('deps')}
              title="Instala as dependências encontradas em package.json e composer.json"
            >
              {isBusy('deps') && <LoaderCircle className="spin" size={14} aria-hidden="true" />}
              {isBusy('deps') ? 'Instalando dependências…' : 'Instalar dependências'}
            </button>
            <button
              type="button"
              className="danger-action"
              disabled={projectBusy}
              onClick={onRemove}
            >
              {projectBusy ? (
                <LoaderCircle className="spin" size={14} aria-hidden="true" />
              ) : (
                <Trash2 size={14} aria-hidden="true" />
              )}{' '}
              {isBusy('remove') ? `${removeLabel}…` : removeLabel}
            </button>
          </div>
        </div>
      </section>
      <section>
        <h2 className="section-label">SERVIÇOS</h2>
        <div className="services-grid">
          <ServiceCard
            name="Caddy WSL único"
            up={!!(system?.caddyRunning ?? system?.wslCaddyRunning)}
            detail={
              system?.caddySystemd === false
                ? 'systemd indisponível'
                : `Borda, TLS e rotas LAN${system?.caddyTopology ? ` · ${system.caddyTopology}` : ''}`
            }
          />
          <ServiceCard
            name="WSL mirrored"
            up={system?.mirroredNetworking ?? false}
            detail="Loopback Windows↔WSL e acesso direto à LAN"
          />
          <ServiceCard
            name="Hyper-V Firewall"
            up={system?.hypervFirewallOk ?? false}
            detail="Private / LocalSubnet · portas gerenciadas"
          />
          {project.effectiveMode === 'php' && (
            <ServiceCard
              name="PHP-FPM"
              up={project.status === 'ready'}
              detail={`PHP ${project.phpVersion || 'padrão'}`}
            />
          )}{' '}
          {canRunDev && (
            <ServiceCard
              name="HMR local"
              up={project.devRunning}
              detail="Servidor de desenvolvimento"
            />
          )}
          <ServiceCard
            name="Preview LAN"
            up={project.lanPreviewState === 'ready'}
            detail={
              project.lanPreviewState === 'paused'
                ? 'Pausado durante HMR local'
                : 'Manifest compilado'
            }
          />
        </div>
        <div className="infrastructure-actions">
          {project.tlsEnabled && (
            <button type="button" disabled={isBusy('ca', 'firewall', 'reload')} onClick={onTrustCA}>
              Confiar na CA local
            </button>
          )}
          {system && !system.firewallOk && (
            <button
              type="button"
              disabled={isBusy('ca', 'firewall', 'reload')}
              onClick={onRepairFirewall}
            >
              Corrigir firewall privado
            </button>
          )}
          {system && (system.hypervFirewallOk === false || system.mirroredNetworking === false) && (
            <button
              type="button"
              disabled={isBusy('ca', 'firewall', 'reload')}
              onClick={onRepairFirewall}
            >
              Corrigir rede WSL espelhada
            </button>
          )}
        </div>
      </section>
      {project.lanPreviewState === 'paused' && (
        <p className="endpoint-notice">
          O preview LAN está pausado enquanto o HMR local está ativo. Use “Publicar preview LAN”
          para parar o dev, gerar o build e publicar uma versão estável na rede.
        </p>
      )}
      <MetricsSection
        metrics={metrics}
        range={range}
        onRange={setRange}
        loading={metricsLoading}
        onRefresh={() => setMetricsRefresh((value) => value + 1)}
      />
    </div>
  );
}

function RoutePortControl({
  project,
  busy,
  onChange,
}: {
  project: ProjectInfo;
  busy: boolean;
  onChange: (port: number | null) => void;
}) {
  const [draft, setDraft] = useState(project.port ? String(project.port) : '');
  const [invalid, setInvalid] = useState(false);
  const hasOverride = project.routePortOverride !== undefined;

  useEffect(() => {
    setDraft(project.port ? String(project.port) : '');
    setInvalid(false);
  }, [project.port]);

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const port = Number(draft);
    if (!Number.isInteger(port) || port < 1024 || port > 65535) {
      setInvalid(true);
      return;
    }
    setInvalid(false);
    onChange(port);
  };

  return (
    <form className="route-port-control" aria-label="Configuração da porta LAN" onSubmit={submit}>
      <label htmlFor="route-port">Porta LAN</label>
      <input
        id="route-port"
        type="number"
        min={1024}
        max={65535}
        inputMode="numeric"
        value={draft}
        onChange={(event) => {
          setDraft(event.target.value);
          setInvalid(false);
        }}
        aria-invalid={invalid}
        aria-describedby={invalid ? 'route-port-help' : 'route-port-state'}
        disabled={busy}
      />
      <span id="route-port-state" className="route-port-state">
        {hasOverride ? 'override' : 'automática'}
      </span>
      <button type="submit" disabled={busy || !draft}>
        Aplicar
      </button>
      {hasOverride && (
        <button type="button" disabled={busy} onClick={() => onChange(null)}>
          Automática
        </button>
      )}
      {invalid && (
        <small id="route-port-help" role="alert">
          Use uma porta entre 1024 e 65535.
        </small>
      )}
    </form>
  );
}

function MetricsSection({
  metrics,
  range,
  onRange,
  loading,
  onRefresh,
}: {
  metrics: MetricsSnapshot | null;
  range: MetricsRange;
  onRange: (range: MetricsRange) => void;
  loading: boolean;
  onRefresh: () => void;
}) {
  const buckets =
    metrics?.latencyBuckets ??
    [25, 50, 100, 250, 500, 1000, null].map((upperBoundMs) => ({ upperBoundMs, count: 0 }));
  const routes = metrics?.routes ?? [];
  return (
    <section className="metrics-section">
      <div className="metrics-heading">
        <h2 className="section-label">TEMPO DE REQUISIÇÃO</h2>
        <div className="metrics-controls">
          <button type="button" className="metrics-refresh" disabled={loading} onClick={onRefresh}>
            <RotateCw size={13} aria-hidden="true" />{' '}
            {loading ? 'Atualizando…' : 'Atualizar métricas'}
          </button>
          <div className="range-tabs">
            {(['15m', '1h', '24h', '7d'] as MetricsRange[]).map((item) => (
              <button
                type="button"
                key={item}
                className={range === item ? 'active' : ''}
                aria-pressed={range === item}
                onClick={() => onRange(item)}
              >
                {item}
              </button>
            ))}
          </div>
        </div>
      </div>
      {!metrics && (
        <p className="metrics-no-data">
          Nenhuma requisição registrada nos últimos {range}. Os gráficos permanecem prontos para
          novas amostras.
        </p>
      )}
      <div className="metrics-grid">
        <MetricCard
          label="P50 (MEDIANA)"
          value={formatLatency(metrics?.p50Ms)}
          unit="ms"
          caption="metade das respostas fica abaixo deste valor"
        />
        <MetricCard
          label="P95"
          value={formatLatency(metrics?.p95Ms)}
          unit="ms"
          caption="95% das respostas ficam abaixo deste valor"
        />
        <MetricCard
          label="REQUISIÇÕES"
          value={(metrics?.requests ?? 0).toLocaleString('pt-BR')}
          caption={`nos últimos ${range}`}
        />
        <MetricCard
          label="RESPOSTAS 4XX/5XX"
          value={formatPercent(metrics?.errorRate ?? 0)}
          unit="%"
          caption={`${metrics?.errorCount ?? 0} de ${(metrics?.requests ?? 0).toLocaleString('pt-BR')} respostas`}
          warning={(metrics?.errorRate ?? 0) > 1}
        />
      </div>
      {(metrics?.excludedColdStarts ?? 0) > 0 && (
        <p className="metrics-note">
          ✣ {metrics?.excludedColdStarts} inícios a frio excluídos do tempo
        </p>
      )}
      <div className="charts-grid">
        <LatencyHistogram buckets={buckets} />
        <TrafficChart points={metrics?.traffic ?? []} />
      </div>
      <SlowRoutes routes={routes} />
      <RoutesTable routes={routes} />
    </section>
  );
}
function formatLatency(value: number | null | undefined): string | null {
  return value == null ? null : value.toLocaleString('pt-BR', { maximumFractionDigits: 1 });
}
function formatPercent(value: number): string {
  return value.toLocaleString('pt-BR', { maximumFractionDigits: 1 });
}
function MetricCard({
  label,
  value,
  unit,
  caption,
  warning,
}: {
  label: string;
  value: number | string | null;
  unit?: string;
  caption: string;
  warning?: boolean;
}) {
  return (
    <article className="metric-card">
      <span className="metric-label">{label}</span>
      <strong className={warning ? 'warning-value' : ''}>
        {value === null ? 'Sem dados' : value}
        {value !== null && unit && <small>{unit}</small>}
      </strong>
      <span>{caption}</span>
    </article>
  );
}
function LatencyHistogram({ buckets }: { buckets: MetricsSnapshot['latencyBuckets'] }) {
  const total = buckets.reduce((sum, item) => sum + item.count, 0);
  const max = Math.max(...buckets.map((item) => item.count), 1);
  const labels = ['<25ms', '<50ms', '<100ms', '<250ms', '<500ms', '<1s', '>1s'];
  return (
    <article className="chart-card">
      <div className="chart-title">
        <span className="chart-label">TEMPO DE RESPOSTA</span>
      </div>
      {total ? (
        <div className="histogram">
          {buckets.map((item, i) => (
            <div className={`hist-bar bar-${i}`} key={item.upperBoundMs ?? 'over'}>
              <i style={{ height: `${Math.max(3, (item.count / max) * 100)}%` }} />
              <small>{labels[i]}</small>
            </div>
          ))}
        </div>
      ) : (
        <div className="chart-empty">Sem respostas no período</div>
      )}
    </article>
  );
}
function TrafficChart({ points }: { points: MetricsSnapshot['traffic'] }) {
  const max = Math.max(...points.map((point) => point.requestsPerMinute), 1);
  const path = points
    .map(
      (point, i) =>
        `${i ? 'L' : 'M'} ${(i / Math.max(points.length - 1, 1)) * 100} ${100 - (point.requestsPerMinute / max) * 80}`,
    )
    .join(' ');
  const area = path ? `${path} L 100 100 L 0 100 Z` : '';
  return (
    <article className="chart-card">
      <div className="chart-title">
        <span className="chart-label">VOLUME DE REQUISIÇÕES</span>
        <span>req/min</span>
      </div>
      {points.length ? (
        <>
          <svg
            className="traffic-chart"
            viewBox="0 0 100 100"
            preserveAspectRatio="none"
            role="img"
            aria-label="Requisições por minuto"
          >
            <path className="traffic-area" d={area} />
            <path className="traffic-line" d={path} />
          </svg>
          <div className="chart-axis">
            <span>{points[0]?.at || '—'}</span>
            <span>{points[points.length - 1]?.at || '—'}</span>
          </div>
        </>
      ) : (
        <div className="chart-empty">Sem tráfego no período</div>
      )}
    </article>
  );
}
function SlowRoutes({ routes }: { routes: MetricsSnapshot['routes'] }) {
  const rows = routes
    .slice()
    .sort((a, b) => (b.p95Ms || 0) - (a.p95Ms || 0))
    .slice(0, 5);
  const max = Math.max(...rows.map((r) => r.p95Ms || 0), 1);
  return (
    <article className="slow-routes">
      <h2 className="section-label">ROTAS MAIS LENTAS</h2>
      {rows.length ? (
        rows.map((route) => (
          <div className="slow-route" key={`${route.method}-${route.normalizedPath}`}>
            <span className={`method method-${route.method.toLowerCase()}`}>{route.method}</span>
            <code>{route.normalizedPath}</code>
            <div className="latency-bar">
              <i style={{ width: `${((route.p95Ms || 0) / max) * 100}%` }} />
            </div>
            <strong>
              {route.p95Ms === null ? 'Sem dados' : `${formatLatency(route.p95Ms)} ms`}
            </strong>
          </div>
        ))
      ) : (
        <div className="table-empty">Nenhuma rota registrada neste período.</div>
      )}
    </article>
  );
}
function RoutesTable({ routes }: { routes: MetricsSnapshot['routes'] }) {
  const [tab, setTab] = useState<'routes' | 'recent'>('routes');
  const sorted = useMemo(
    () => routes.slice().sort((a, b) => (b.p95Ms || 0) - (a.p95Ms || 0)),
    [routes],
  );
  return (
    <article className="routes-table">
      <div className="table-tabs" role="tablist">
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'routes'}
          className={tab === 'routes' ? 'active' : ''}
          onClick={() => setTab('routes')}
        >
          Rotas
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'recent'}
          className={tab === 'recent' ? 'active' : ''}
          onClick={() => setTab('recent')}
        >
          Requisições recentes
        </button>
      </div>
      {tab === 'recent' ? (
        <div className="table-empty">
          Registros recentes não estão disponíveis sem expor dados sensíveis.
        </div>
      ) : (
        <>
          <div className="table-head">
            <span>ROTA</span>
            <span>P50</span>
            <span>P95</span>
            <span>LATÊNCIA</span>
            <span>REQUISIÇÕES</span>
          </div>
          {sorted.length ? (
            sorted.map((route) => (
              <div className="table-row" key={`${route.method}-${route.normalizedPath}`}>
                <span>
                  <b className={`method method-${route.method.toLowerCase()}`}>{route.method}</b>
                  <code>{route.normalizedPath}</code>
                </span>
                <span>{route.p50Ms === null ? '—' : `${formatLatency(route.p50Ms)} ms`}</span>
                <strong
                  className={
                    (route.p95Ms || 0) > 300
                      ? 'danger-text'
                      : (route.p95Ms || 0) > 150
                        ? 'warning-text'
                        : ''
                  }
                >
                  {route.p95Ms === null ? '—' : `${formatLatency(route.p95Ms)} ms`}
                </strong>
                <span className="mini-latency">
                  <i style={{ width: `${Math.min(100, (route.p95Ms || 0) / 5)}%` }} />
                </span>
                <span>{route.requests.toLocaleString('pt-BR')}</span>
              </div>
            ))
          ) : (
            <div className="table-empty table-empty-grid">
              Nenhuma rota registrada neste período.
            </div>
          )}
        </>
      )}
    </article>
  );
}
