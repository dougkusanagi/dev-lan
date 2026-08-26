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
import type { DoctorCheck, PHPVersion, ProjectInfo, SystemStatus } from '../types';

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
  const [busy, setBusy] = useState<string>();
  const [addOpen, setAddOpen] = useState(false);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [newProject, setNewProject] = useState({ name: '', path: '', park: false });
  const [doctor, setDoctor] = useState<DoctorCheck[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const searchRef = useRef<HTMLInputElement>(null);
  const refreshVersion = useRef(0);
  const initialLoad = useRef(true);
  const notify = useCallback((m: string) => {
    setToast(m);
    window.setTimeout(() => setToast((current) => (current === m ? '' : current)), 3800);
  }, []);
  const refresh = useCallback(async () => {
    const version = ++refreshVersion.current;
    if (initialLoad.current) setLoading(true);
    try {
      const [p, s, versions] = await Promise.all([
        client.getProjects(),
        client.getStatus(),
        client.getPHPVersions(),
      ]);
      if (version !== refreshVersion.current) return;
      setProjects(p);
      setSystem(s);
      setPHPVersions(versions);
      setLoadError('');
      setSelected((current) => (p.some((x) => x.name === current) ? current : p[0]?.name || ''));
    } catch (e) {
      if (version === refreshVersion.current) {
        const message = e instanceof APIError && e.status === 0 ? 'API indisponível.' : String(e);
        setLoadError(message);
        notify(`Erro ao carregar dados: ${message}`);
      }
    } finally {
      if (version === refreshVersion.current) {
        setLoading(false);
        initialLoad.current = false;
      }
    }
  }, [client, notify]);
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
    if (!project || busy) return;
    setBusy(action);
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
      notify(action === 'doctor' ? 'Diagnóstico concluído.' : 'Operação concluída.');
      await refresh();
    } catch (e) {
      notify(`Falha na operação: ${String(e)}`);
    } finally {
      setBusy(undefined);
    }
  };
  const changePHPVersion = async (version: string) => {
    if (!project || busy || !version || version === project.phpVersion) return;
    setBusy('php');
    try {
      await client.saveProjectConfig({ name: project.name, phpVersion: version });
      await refresh();
      notify(`PHP ${version} selecionado.`);
    } catch (e) {
      notify(`Não foi possível selecionar o PHP: ${String(e)}`);
    } finally {
      setBusy(undefined);
    }
  };
  const toggleTLS = async (target: ProjectInfo) => {
    if (busy) return;
    setBusy(`tls:${target.name}`);
    try {
      await client.saveProjectConfig({ name: target.name, tlsEnabled: !target.tlsEnabled });
      await refresh();
      notify(`TLS ${target.tlsEnabled ? 'desativado' : 'ativado'} em ${target.name}.`);
    } catch (e) {
      notify(`Não foi possível alterar o TLS: ${String(e)}`);
    } finally {
      setBusy(undefined);
    }
  };
  const changeRoutePort = async (port: number | null) => {
    if (!project || busy) return;
    const description =
      port === null ? 'restaurar a alocação automática' : `usar a porta LAN ${port}`;
    if (
      !window.confirm(
        `Deseja ${description} para ${project.name}? A infraestrutura será recarregada.`,
      )
    )
      return;
    setBusy('route-port');
    try {
      await client.saveProjectConfig({
        name: project.name,
        ...(port === null ? { routePortAuto: true } : { routePort: port }),
      });
      await refresh();
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
      setBusy(undefined);
    }
  };
  const retry = () => {
    initialLoad.current = true;
    setLoading(true);
    setLoadError('');
    void refresh();
  };
  const trustCA = async () => {
    if (busy) return;
    setBusy('ca');
    try {
      await client.trustCA();
      notify('CA local confiada neste computador.');
    } catch (e) {
      notify(`Não foi possível confiar na CA: ${String(e)}`);
    } finally {
      setBusy(undefined);
    }
  };
  const repairFirewall = async () => {
    if (busy) return;
    setBusy('firewall');
    try {
      await client.applyDoctorFix('firewall', '');
      await refresh();
      notify('Firewall reconciliado.');
    } catch (e) {
      notify(`Não foi possível reconciliar o firewall: ${String(e)}`);
    } finally {
      setBusy(undefined);
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
    setBusy(`remove:${target.name}`);
    try {
      if (target.kind === 'linked') await client.unlinkProject(target.name);
      else await client.hideProject(target.name);
      await refresh();
      notify(`Projeto ${target.name} ${action === 'ocultar' ? 'ocultado' : 'desvinculado'}.`);
    } catch (e) {
      notify(`Não foi possível ${action} o projeto: ${String(e)}`);
    } finally {
      setBusy(undefined);
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
              onToggleTLS={() => {
                if (
                  window.confirm(
                    `${project.tlsEnabled ? 'Desativar' : 'Ativar'} TLS para ${project.name}?`,
                  )
                )
                  void toggleTLS(project);
              }}
              onReload={() =>
                void client
                  .reload()
                  .then(() => notify('Infraestrutura recarregada.'))
                  .catch((e) => notify(String(e)))
              }
            />
            <div className="tab-body">
              {tab === 'overview' ? (
                <Overview
                  project={project}
                  client={client}
                  system={system}
                  phpVersions={phpVersions}
                  busy={busy}
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
