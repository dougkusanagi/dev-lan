import { X } from 'lucide-react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { DevLANClient } from '../api';
import { APIError, api } from '../api';
import { EmptyState } from '../components/feedback/EmptyState';
import { ErrorState } from '../components/feedback/ErrorState';
import { LoadingState } from '../components/feedback/LoadingState';
import { Overview } from '../components/metrics/Overview';
import { ProjectHeader } from '../components/project-header/ProjectHeader';
import { ActivityRail } from '../components/rail/ActivityRail';
import { ProjectSidebar } from '../components/sidebar/ProjectSidebar';
import { LogsPanel } from '../features/logs/LogsPanel';
import type {
  DoctorCheck,
  OperationKey,
  PendingOperation,
  PHPVersion,
  ProjectInfo,
  SystemStatus,
} from '../types';

type View = 'sites' | 'doctor' | 'settings';
export interface AppShellProps {
  client?: DevLANClient;
  pollIntervalMs?: number;
}

export default function AppShell({ client = api, pollIntervalMs = 5000 }: AppShellProps = {}) {
  const [projects, setProjects] = useState<ProjectInfo[]>([]);
  const [system, setSystem] = useState<SystemStatus | null>(null);
  const [phpVersions, setPHPVersions] = useState<PHPVersion[]>([]);
  const [selected, setSelected] = useState<string>(
    () => localStorage.getItem('devlan_project') || '',
  );
  const [query, setQuery] = useState('');
  const [tab, setTab] = useState<'overview' | 'logs'>(
    () => (localStorage.getItem('devlan_tab') as 'overview' | 'logs') || 'overview',
  );
  const [view, setView] = useState<View>('sites');
  const [dark, setDark] = useState(() => localStorage.getItem('devlan_theme') !== 'light');
  const [toast, setToast] = useState('');
  const [operations, setOperations] = useState<PendingOperation[]>([]);
  const operationsRef = useRef<PendingOperation[]>([]);
  const operationSequence = useRef(0);
  const [addOpen, setAddOpen] = useState(false);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [newProject, setNewProject] = useState({ name: '', path: '', park: false });
  const [doctor, setDoctor] = useState<DoctorCheck[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const searchRef = useRef<HTMLInputElement>(null);
  const refreshVersion = useRef(0);
  const initialLoad = useRef(true);
  const mutationVersion = useRef(0);
  const refreshInFlight = useRef<Promise<void> | null>(null);
  const refreshQueued = useRef(false);
  const optimisticProjects = useRef<Record<string, Partial<ProjectInfo>>>({});
  const notify = useCallback((m: string) => {
    setToast(m);
    window.setTimeout(() => setToast((current) => (current === m ? '' : current)), 3800);
  }, []);
  const refresh = useCallback(
    async (priority = false) => {
      if (refreshInFlight.current) {
        if (priority) refreshQueued.current = true;
        const current = refreshInFlight.current;
        await current;
        // A priority refresh requested during an existing poll is included by
        // the current run. This await also makes mutation handlers deterministic.
        return;
      }

      const run = async () => {
        const version = ++refreshVersion.current;
        const mutationAtStart = mutationVersion.current;
        if (initialLoad.current) setLoading(true);
        try {
          const overview = client.getOverview
            ? await client.getOverview()
            : await Promise.all([
                client.getProjects(),
                client.getStatus(),
                client.getPHPVersions(),
              ]).then(([projects, status, phpVersions]) => ({ projects, status, phpVersions }));
          // A poll that started before a mutation must never put the old
          // snapshot back over the optimistic/current state.
          if (version !== refreshVersion.current || mutationAtStart !== mutationVersion.current)
            return;
          const mergedProjects = overview.projects.map((item) => ({
            ...item,
            ...(optimisticProjects.current[item.name] ?? {}),
          }));
          setProjects(mergedProjects);
          setSystem(overview.status);
          setPHPVersions(overview.phpVersions);
          setLoadError('');
          setSelected((current) =>
            mergedProjects.some((x) => x.name === current)
              ? current
              : mergedProjects[0]?.name || '',
          );
        } catch (e) {
          if (version === refreshVersion.current && mutationAtStart === mutationVersion.current) {
            const message =
              e instanceof APIError && e.status === 0 ? 'API indisponível.' : String(e);
            setLoadError(message);
            notify(`Erro ao carregar dados: ${message}`);
          }
        } finally {
          if (version === refreshVersion.current) {
            setLoading(false);
            initialLoad.current = false;
          }
        }
      };

      const promise = run();
      refreshInFlight.current = promise;
      try {
        await promise;
      } finally {
        refreshInFlight.current = null;
        // A mutation can arrive during the request. Run exactly one fresh read
        // before releasing its loading state instead of starting a request per
        // event/poll tick.
        if (refreshQueued.current) {
          refreshQueued.current = false;
          await refresh();
        }
      }
    },
    [client, notify],
  );
  const beginOperation = useCallback(
    (key: OperationKey, projectName?: string, targetState?: boolean) => {
      const current = operationsRef.current;
      const conflicts = current.some((operation) => {
        if (!operation.projectName || !projectName) return true;
        return operation.projectName === projectName;
      });
      if (conflicts) return undefined;
      const operation: PendingOperation = {
        id: ++operationSequence.current,
        key,
        projectName,
        targetState,
      };
      operationsRef.current = [...current, operation];
      setOperations(operationsRef.current);
      mutationVersion.current += 1;
      refreshVersion.current += 1;
      return operation;
    },
    [],
  );
  const endOperation = useCallback((operation: PendingOperation) => {
    operationsRef.current = operationsRef.current.filter((item) => item.id !== operation.id);
    setOperations(operationsRef.current);
  }, []);
  const patchProject = useCallback((name: string, patch: Partial<ProjectInfo>) => {
    optimisticProjects.current[name] = { ...(optimisticProjects.current[name] ?? {}), ...patch };
    setProjects((current) =>
      current.map((item) => (item.name === name ? { ...item, ...patch } : item)),
    );
  }, []);
  const setProjectFields = useCallback((name: string, patch: Partial<ProjectInfo>) => {
    setProjects((current) =>
      current.map((item) => (item.name === name ? { ...item, ...patch } : item)),
    );
  }, []);
  const clearProjectPatch = useCallback((name: string) => {
    delete optimisticProjects.current[name];
  }, []);
  const reconcileProject = useCallback(
    async (name: string, finalPatch: Partial<ProjectInfo> = {}) => {
      await refresh(true);
      // Keep the optimistic fields in place until the priority read has
      // completed. An older poll may still be finishing when the mutation
      // response arrives.
      clearProjectPatch(name);
      if (Object.keys(finalPatch).length) setProjectFields(name, finalPatch);
    },
    [clearProjectPatch, refresh, setProjectFields],
  );
  useEffect(() => {
    let running = false;
    const tick = () => {
      if (running) return;
      running = true;
      void refresh().finally(() => {
        running = false;
      });
    };
    tick();
    if (pollIntervalMs <= 0) return undefined;
    const id = window.setInterval(tick, pollIntervalMs);
    return () => clearInterval(id);
  }, [pollIntervalMs, refresh]);
  useEffect(() => {
    document.documentElement.classList.toggle('dark', dark);
    localStorage.setItem('devlan_theme', dark ? 'dark' : 'light');
  }, [dark]);
  useEffect(() => {
    localStorage.setItem('devlan_project', selected);
  }, [selected]);
  useEffect(() => {
    localStorage.setItem('devlan_tab', tab);
  }, [tab]);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        searchRef.current?.focus();
      }
      if (e.key === 'Escape') {
        setAddOpen(false);
        setSidebarOpen(false);
      }
    };
    addEventListener('keydown', onKey);
    return () => removeEventListener('keydown', onKey);
  }, []);
  const visible = useMemo(
    () =>
      projects.filter((p) =>
        [p.name, p.path, p.framework].join(' ').toLowerCase().includes(query.toLowerCase()),
      ),
    [projects, query],
  );
  const project = projects.find((p) => p.name === selected);
  const operate = async (action: 'start' | 'stop' | 'restart' | 'build' | 'deps' | 'doctor') => {
    if (!project) return;
    const operation = beginOperation(action, project.name);
    if (!operation) return;
    const healthyStatus = project.status === 'degraded' ? 'degraded' : 'ready';
    const devStoppedStatus = project.effectiveMode === 'dev' ? 'stopped' : healthyStatus;
    const expectedPatch: Partial<ProjectInfo> =
      action === 'start' || action === 'restart'
        ? {
            devRunning: true,
            localDevState: 'starting',
            status: project.status === 'degraded' ? 'degraded' : 'starting',
            lanPreviewState: 'paused',
          }
        : action === 'stop'
          ? {
              devRunning: false,
              localDevState: 'stopped',
              status: devStoppedStatus,
              lanPreviewState: 'ready',
            }
          : {};
    const successPatch: Partial<ProjectInfo> =
      action === 'start' || action === 'restart'
        ? {
            devRunning: true,
            localDevState: 'active',
            status: healthyStatus,
            lanPreviewState: 'paused',
          }
        : action === 'stop'
          ? {
              devRunning: false,
              localDevState: 'stopped',
              status: devStoppedStatus,
              lanPreviewState: 'ready',
            }
          : {};
    if (Object.keys(expectedPatch).length) patchProject(project.name, expectedPatch);
    try {
      if (action === 'start') await client.startDev(project.name);
      else if (action === 'stop') await client.stopDev(project.name);
      else if (action === 'restart') await client.restartDev(project.name);
      else if (action === 'build') await client.buildProject(project.name);
      else if (action === 'deps') await client.installDeps(project.name);
      else {
        setView('doctor');
        setDoctor(await client.runDoctor(project.name));
      }
      if (Object.keys(successPatch).length) setProjectFields(project.name, successPatch);
      void reconcileProject(project.name, successPatch);
      notify(action === 'doctor' ? 'Diagnóstico concluído.' : 'Operação concluída.');
    } catch (e) {
      clearProjectPatch(project.name);
      // A timeout can happen after the backend committed. Revalidate once in
      // the background so an ambiguous result does not require F5.
      void refresh(true);
      notify(`Falha na operação: ${String(e)}`);
    } finally {
      endOperation(operation);
    }
  };
  const changePHPVersion = async (version: string) => {
    if (!project || !version || version === project.phpVersion) return;
    const operation = beginOperation('php', project.name);
    if (!operation) return;
    try {
      await client.saveProjectConfig({ name: project.name, phpVersion: version });
      await refresh(true);
      notify(`PHP ${version} selecionado.`);
    } catch (e) {
      void refresh(true);
      notify(`Não foi possível selecionar o PHP: ${String(e)}`);
    } finally {
      endOperation(operation);
    }
  };
  const toggleTLS = async (target: ProjectInfo) => {
    const tlsEnabled = !target.tlsEnabled;
    const operation = beginOperation('tls', target.name, tlsEnabled);
    if (!operation) return;
    const protocol = tlsEnabled ? 'https:' : 'http:';
    patchProject(target.name, {
      tlsEnabled,
      url: target.url.replace(/^https?:/, protocol),
      lanUrl: target.lanUrl.replace(/^https?:/, protocol),
    });
    try {
      await client.saveProjectConfig({ name: target.name, tlsEnabled });
      setProjectFields(target.name, {
        tlsEnabled,
        url: target.url.replace(/^https?:/, protocol),
        lanUrl: target.lanUrl.replace(/^https?:/, protocol),
      });
      void reconcileProject(target.name, {
        tlsEnabled,
        url: target.url.replace(/^https?:/, protocol),
        lanUrl: target.lanUrl.replace(/^https?:/, protocol),
      });
      notify(`TLS ${tlsEnabled ? 'ativado' : 'desativado'} em ${target.name}.`);
    } catch (e) {
      clearProjectPatch(target.name);
      void refresh(true);
      notify(`Não foi possível alterar o TLS: ${String(e)}`);
    } finally {
      endOperation(operation);
    }
  };
  const changeRoutePort = async (port: number | null) => {
    if (!project) return;
    const description =
      port === null ? 'restaurar a alocação automática' : `usar a porta LAN ${port}`;
    if (
      !window.confirm(
        `Deseja ${description} para ${project.name}? A infraestrutura será recarregada.`,
      )
    )
      return;
    const operation = beginOperation('route-port', project.name);
    if (!operation) return;
    try {
      await client.saveProjectConfig({
        name: project.name,
        ...(port === null ? { routePortAuto: true } : { routePort: port }),
      });
      await refresh(true);
      notify(port === null ? 'Porta LAN automática restaurada.' : `Porta LAN ${port} aplicada.`);
    } catch (e) {
      const message = String(e);
      const details =
        e instanceof APIError && e.details && typeof e.details === 'object'
          ? (e.details as Record<string, unknown>)
          : undefined;
      const rolledBack =
        /rolled_back|rollback|restaurad/i.test(message) || details?.status === 'rolled_back';
      notify(
        rolledBack
          ? 'Falha ao recarregar; a configuração anterior foi restaurada.'
          : `Não foi possível alterar a porta LAN: ${message}`,
      );
    } finally {
      endOperation(operation);
    }
  };
  const retry = () => {
    initialLoad.current = true;
    setLoading(true);
    setLoadError('');
    void refresh();
  };
  const trustCA = async () => {
    const operation = beginOperation('ca');
    if (!operation) return;
    try {
      await client.trustCA();
      await refresh(true);
      notify('CA local confiada neste computador.');
    } catch (e) {
      void refresh(true);
      notify(`Não foi possível confiar na CA: ${String(e)}`);
    } finally {
      endOperation(operation);
    }
  };
  const repairFirewall = async () => {
    const operation = beginOperation('firewall');
    if (!operation) return;
    try {
      await client.applyDoctorFix('firewall', '');
      await refresh(true);
      notify('Firewall reconciliado.');
    } catch (e) {
      void refresh(true);
      notify(`Não foi possível reconciliar o firewall: ${String(e)}`);
    } finally {
      endOperation(operation);
    }
  };
  const removeProject = async (target: ProjectInfo) => {
    const action = target.kind === 'linked' ? 'desvincular' : 'ocultar';
    if (
      !window.confirm(
        `Deseja ${action} o projeto "${target.name}"? Os arquivos do projeto não serão excluídos.`,
      )
    )
      return;
    const operation = beginOperation('remove', target.name);
    if (!operation) return;
    try {
      if (target.kind === 'linked') await client.unlinkProject(target.name);
      else await client.hideProject(target.name);
      await refresh(true);
      notify(`Projeto ${target.name} ${action === 'ocultar' ? 'ocultado' : 'desvinculado'}.`);
    } catch (e) {
      notify(`Não foi possível ${action} o projeto: ${String(e)}`);
    } finally {
      endOperation(operation);
    }
  };
  const add = async () => {
    if (!newProject.path.trim() || (!newProject.park && !newProject.name.trim()))
      return notify('Informe o caminho e, para vínculo, o nome do projeto.');
    try {
      if (newProject.park) await client.parkDir(newProject.path.trim());
      else await client.linkProject(newProject.name.trim(), newProject.path.trim());
      setAddOpen(false);
      setNewProject({ name: '', path: '', park: false });
      await refresh();
      notify('Projeto registrado.');
    } catch (e) {
      notify(`Não foi possível registrar: ${String(e)}`);
    }
  };
  const changeView = async (next: View) => {
    setView(next);
    setSidebarOpen(false);
    if (next === 'doctor')
      try {
        setDoctor(await client.runDoctor(project?.name || ''));
      } catch (e) {
        notify(`Erro no diagnóstico: ${String(e)}`);
      }
  };
  const reloadInfrastructure = async () => {
    const operation = beginOperation('reload');
    if (!operation) return;
    try {
      await client.reload();
      await refresh(true);
      notify('Infraestrutura recarregada.');
    } catch (e) {
      void refresh(true);
      notify(String(e));
    } finally {
      endOperation(operation);
    }
  };
  return (
    <main className="app-shell">
      <ActivityRail
        active={view}
        onNavigate={(v) => void changeView(v)}
        dark={dark}
        onTheme={() => setDark((x) => !x)}
        onMenu={() => setSidebarOpen((x) => !x)}
      />
      <ProjectSidebar
        open={sidebarOpen}
        projects={visible}
        selected={selected}
        query={query}
        onQuery={setQuery}
        onSelect={(p) => {
          setSelected(p.name);
          setView('sites');
          setSidebarOpen(false);
        }}
        onToggleTLS={(target) => {
          if (
            window.confirm(`${target.tlsEnabled ? 'Desativar' : 'Ativar'} TLS para ${target.name}?`)
          )
            void toggleTLS(target);
        }}
        pendingTLS={Object.fromEntries(
          operations
            .filter((operation) => operation.key === 'tls' && operation.projectName !== undefined)
            .map((operation) => [operation.projectName, operation.targetState ?? false]),
        )}
        onAdd={() => {
          setAddOpen(true);
          setSidebarOpen(false);
        }}
        searchRef={searchRef}
      />
      <div className="workspace">
        {view === 'sites' && loading && projects.length === 0 && <LoadingState />}
        {view === 'sites' && !loading && loadError && projects.length === 0 && (
          <ErrorState message={loadError} onRetry={retry} />
        )}
        {view === 'sites' && !loading && !loadError && project && (
          <>
            <ProjectHeader
              project={project}
              tab={tab}
              onTab={setTab}
              onOpenLocal={() =>
                void client.openURL(project.localDevUrl).catch((e) => notify(String(e)))
              }
              onCopyLocal={() =>
                void client
                  .copyURL(project.localDevUrl)
                  .then(() => notify('URL local copiada.'))
                  .catch((e) => notify(String(e)))
              }
              onOpenLAN={() => void client.openURL(project.lanUrl).catch((e) => notify(String(e)))}
              onCopyLAN={() =>
                void client
                  .copyURL(project.lanUrl)
                  .then(() =>
                    notify(
                      'URL LAN copiada. Nota: cookies HTTP não são isolados por porta no mesmo IP.',
                    ),
                  )
                  .catch((e) => notify(String(e)))
              }
              onCopyPath={() =>
                void client
                  .copyURL(project.path)
                  .then(() => notify('Caminho copiado.'))
                  .catch((e) => notify(String(e)))
              }
              onReload={() => void reloadInfrastructure()}
              reloadPending={operations.some((operation) => operation.key === 'reload')}
            />
            <div className="tab-body">
              {tab === 'overview' ? (
                <Overview
                  project={project}
                  client={client}
                  system={system}
                  phpVersions={phpVersions}
                  operations={operations}
                  onPHPVersion={changePHPVersion}
                  onRoutePort={changeRoutePort}
                  onTrustCA={trustCA}
                  onRepairFirewall={repairFirewall}
                  onRemove={() => void removeProject(project)}
                  onAction={operate}
                />
              ) : (
                <LogsPanel project={project.name} client={client} />
              )}
            </div>
          </>
        )}
        {view === 'sites' && !loading && !loadError && !project && (
          <EmptyState onAdd={() => setAddOpen(true)} />
        )}{' '}
        {view === 'doctor' && (
          <Auxiliary title="Diagnóstico">
            {doctor.length ? (
              doctor.map((c) => (
                <article className="check-row" key={c.name}>
                  <strong>{c.name}</strong>
                  <span className={c.status === 'OK' ? 'service-ok' : 'service-down'}>
                    {c.status}
                  </span>
                  <p>{c.detail}</p>
                </article>
              ))
            ) : (
              <p>Carregando diagnóstico…</p>
            )}
          </Auxiliary>
        )}
        {view === 'settings' && (
          <Auxiliary title="Configurações">
            <p>
              As configurações globais, PHP, segurança e exportação continuam disponíveis pela CLI
              nesta versão da nova interface.
            </p>
            <button
              type="button"
              onClick={() =>
                void client
                  .exportConfigJSON()
                  .then((data) => navigator.clipboard.writeText(data))
                  .then(() => notify('Configuração sanitizada copiada.'))
                  .catch((e) => notify(String(e)))
              }
            >
              Copiar configuração sanitizada
            </button>
          </Auxiliary>
        )}
      </div>
      {toast && (
        <div className="toast" role="status">
          {toast}
        </div>
      )}
      {addOpen && (
        <div className="modal-backdrop" role="presentation">
          <form
            className="dialog"
            role="dialog"
            aria-modal="true"
            aria-labelledby="add-project-title"
            onSubmit={(e) => {
              e.preventDefault();
              void add();
            }}
          >
            <button
              className="dialog-close"
              type="button"
              onClick={() => setAddOpen(false)}
              aria-label="Fechar"
            >
              <X size={18} />
            </button>
            <h2 id="add-project-title">Adicionar projeto</h2>
            <label>
              <input
                type="checkbox"
                checked={newProject.park}
                onChange={(e) => setNewProject((x) => ({ ...x, park: e.target.checked }))}
              />{' '}
              Estacionar uma pasta
            </label>
            {!newProject.park && (
              <>
                <label htmlFor="project-name">Nome do projeto</label>
                <input
                  id="project-name"
                  placeholder="Nome do projeto"
                  value={newProject.name}
                  onChange={(e) => setNewProject((x) => ({ ...x, name: e.target.value }))}
                />
              </>
            )}
            <label htmlFor="project-path">Caminho da pasta</label>
            <input
              id="project-path"
              placeholder="Caminho da pasta"
              value={newProject.path}
              onChange={(e) => setNewProject((x) => ({ ...x, path: e.target.value }))}
            />
            <button type="submit">Adicionar projeto</button>
          </form>
        </div>
      )}
    </main>
  );
}
function Auxiliary({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="auxiliary">
      <span className="section-label">DEVLAN</span>
      <h1>{title}</h1>
      <div className="auxiliary-panel">{children}</div>
    </div>
  );
}
