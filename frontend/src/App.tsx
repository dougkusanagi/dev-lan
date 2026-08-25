import React, { useState, useEffect, useMemo, useRef } from 'react';
import {
  Globe,
  Server,
  Play,
  Square,
  RotateCw,
  FileText,
  Settings,
  Activity,
  Plus,
  Search,
  Copy,
  ExternalLink,
  ShieldCheck,
  ShieldAlert,
  Moon,
  Sun,
  Wrench,
  CheckCircle2,
  AlertTriangle,
  XCircle,
  Clock,
  Code2,
  Folder,
  Trash2,
  Lock,
  Unlock,
  RefreshCw,
  Terminal,
  Layers,
  EyeOff
} from 'lucide-react';
import { api } from './api';
import { ProjectInfo, SystemStatus, DoctorCheck, GlobalConfig, ProjectConfigUpdate, ProjectStatus, PHPVersion } from './types';

export default function App() {
  const [darkMode, setDarkMode] = useState<boolean>(() => {
    return localStorage.getItem('devlan_theme') === 'dark' ||
      (!('devlan_theme' in localStorage) && window.matchMedia('(prefers-color-scheme: dark)').matches);
  });

  const [projects, setProjects] = useState<ProjectInfo[]>([]);
  const [systemStatus, setSystemStatus] = useState<SystemStatus | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [filterMode, setFilterMode] = useState<string>('all');
  const [filterStatus, setFilterStatus] = useState<string>('all');
  const [loading, setLoading] = useState(false);
  const [hasLoadedOnce, setHasLoadedOnce] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [toastMessage, setToastMessage] = useState<string | null>(null);
  const [startingDev, setStartingDev] = useState<string | null>(null);
  const [hidingProject, setHidingProject] = useState<string | null>(null);

  // Modals
  const [activeTab, setActiveTab] = useState<'projects' | 'doctor' | 'settings'>('projects');
  const [selectedProjectForLogs, setSelectedProjectForLogs] = useState<string | null>(null);
  const [logContent, setLogContent] = useState<string>('');
  const [loadingLogs, setLoadingLogs] = useState(false);
  const [editingProject, setEditingProject] = useState<ProjectInfo | null>(null);
  const [projectForm, setProjectForm] = useState<ProjectConfigUpdate>({ name: '' });
  const [isAddModalOpen, setIsAddModalOpen] = useState(false);
  const [newProject, setNewProject] = useState({ name: '', path: '', isPark: false });
  const [globalConfig, setGlobalConfig] = useState<GlobalConfig | null>(null);
  const [phpVersions, setPhpVersions] = useState<PHPVersion[]>([]);
  const [phpVersionInput, setPhpVersionInput] = useState('');
  const [doctorChecks, setDoctorChecks] = useState<DoctorCheck[]>([]);
  const [runningDoctor, setRunningDoctor] = useState(false);

  const searchInputRef = useRef<HTMLInputElement>(null);
  const toastTimerRef = useRef<number | null>(null);
  const hidingProjectsRef = useRef<Set<string>>(new Set());

  // Dark mode class on html root
  useEffect(() => {
    if (darkMode) {
      document.documentElement.classList.add('dark');
      localStorage.setItem('devlan_theme', 'dark');
    } else {
      document.documentElement.classList.remove('dark');
      localStorage.setItem('devlan_theme', 'light');
    }
  }, [darkMode]);

  const showToast = (msg: string, duration = 3500) => {
    if (toastTimerRef.current !== null) {
      window.clearTimeout(toastTimerRef.current);
    }
    setToastMessage(msg);
    toastTimerRef.current = window.setTimeout(() => {
      setToastMessage(null);
      toastTimerRef.current = null;
    }, duration);
  };

  const refreshData = async () => {
    try {
      setLoading(true);
      setLoadError(null);
      const [projs, status] = await Promise.all([
        api.getProjects(searchQuery),
        api.getStatus()
      ]);
      // Keep an optimistic hide from being undone by the five-second polling
      // cycle while the backend is applying/reloading the configuration.
      setProjects(projs.filter(project => !hidingProjectsRef.current.has(project.name)));
      setSystemStatus(status);
    } catch (err: any) {
      const message = err?.message || String(err);
      setLoadError(message);
      showToast(`Erro ao carregar dados: ${message}`);
    } finally {
      setHasLoadedOnce(true);
      setLoading(false);
    }
  };

  useEffect(() => {
    refreshData();
    const interval = setInterval(refreshData, 5000);
    return () => clearInterval(interval);
  }, []);

  // Keyboard accessibility
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
        e.preventDefault();
        searchInputRef.current?.focus();
      }
      if (e.key === 'Escape') {
        setSelectedProjectForLogs(null);
        setEditingProject(null);
        setIsAddModalOpen(false);
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, []);

  // Filtered projects
  const filteredProjects = useMemo(() => {
    return projects.filter(p => {
      const matchSearch = p.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
        p.path.toLowerCase().includes(searchQuery.toLowerCase()) ||
        (p.framework && p.framework.toLowerCase().includes(searchQuery.toLowerCase()));

      const matchMode = filterMode === 'all' || p.effectiveMode === filterMode || p.mode === filterMode;
      const matchStatus = filterStatus === 'all' || p.status === filterStatus;

      return matchSearch && matchMode && matchStatus;
    });
  }, [projects, searchQuery, filterMode, filterStatus]);

  // Actions
  const handleCopyUrl = async (url: string) => {
    try {
      await api.copyURL(url);
      showToast(`URL copiada para a área de transferência!`);
    } catch (err: any) {
      showToast(`Erro ao copiar URL: ${err?.message || err}`);
    }
  };

  const handleOpenUrl = async (url: string) => {
    try {
      await api.openURL(url);
    } catch (err: any) {
      showToast(`Erro ao abrir URL: ${err?.message || err}`);
    }
  };

  const handleStartDev = async (name: string) => {
    if (startingDev) return;
    setStartingDev(name);
    try {
      showToast(`Iniciando servidor dev para ${name}...`, 30000);
      await api.startDev(name);
      await refreshData();
      showToast(`Servidor dev iniciado com sucesso!`);
    } catch (err: any) {
      showToast(`Erro ao iniciar servidor: ${err?.message || err}`);
    } finally {
      setStartingDev(null);
    }
  };

  const handleStopDev = async (name: string) => {
    try {
      await api.stopDev(name);
      await refreshData();
      showToast(`Servidor dev parado.`);
    } catch (err: any) {
      showToast(`Erro ao parar servidor: ${err?.message || err}`);
    }
  };

  const handleRestartDev = async (name: string) => {
    try {
      showToast(`Reiniciando servidor dev para ${name}...`);
      await api.restartDev(name);
      await refreshData();
      showToast(`Servidor dev reiniciado.`);
    } catch (err: any) {
      showToast(`Erro ao reiniciar: ${err?.message || err}`);
    }
  };

  const handleOpenLogs = async (name: string) => {
    setSelectedProjectForLogs(name);
    setLoadingLogs(true);
    try {
      const logs = await api.getProjectLogs(name, 120);
      setLogContent(logs || 'Nenhum log registrado para este projeto.');
    } catch (err: any) {
      setLogContent(`Erro ao buscar logs: ${err?.message || err}`);
    } finally {
      setLoadingLogs(false);
    }
  };

  const handleToggleTLS = async (project: ProjectInfo) => {
    try {
      const newTLS = !project.tlsEnabled;
      await api.saveProjectConfig({ name: project.name, tlsEnabled: newTLS });
      await refreshData();
      showToast(`SSL ${newTLS ? 'ativado' : 'desativado'} para ${project.name}.`);
    } catch (err: any) {
      showToast(`Erro ao alterar SSL: ${err?.message || err}`);
    }
  };

  const handleUnlink = async (name: string) => {
    if (!confirm(`Deseja desvincular o projeto "${name}"? Os arquivos do projeto NÃO serão excluídos.`)) return;
    try {
      await api.unlinkProject(name);
      await refreshData();
      showToast(`Projeto ${name} desvinculado.`);
    } catch (err: any) {
      showToast(`Erro ao desvincular: ${err?.message || err}`);
    }
  };

  const handleHide = async (name: string) => {
    if (!confirm(`Ocultar o projeto "${name}" da lista? Os arquivos e o diretório estacionado NÃO serão alterados.`)) return;
    if (hidingProjectsRef.current.has(name)) return;

    hidingProjectsRef.current.add(name);
    setHidingProject(name);
    // The list should respond immediately; Caddy/config reload can take a few
    // seconds and does not need to block this visual change.
    setProjects(current => current.filter(project => project.name !== name));
    showToast(`Ocultando ${name}...`, 30000);

    let applied = false;
    try {
      await api.hideProject(name);
      applied = true;
      await refreshData();
      showToast(`Projeto ${name} ocultado da lista.`);
    } catch (err: any) {
      if (!applied) {
        hidingProjectsRef.current.delete(name);
        try {
          await refreshData();
        } catch {
          // Preserve the original operation error in the toast.
        }
        showToast(`Erro ao ocultar: ${err?.message || err}`);
      } else {
        showToast(`Projeto ${name} ocultado, mas a atualização da lista demorou: ${err?.message || err}`);
      }
    } finally {
      hidingProjectsRef.current.delete(name);
      setHidingProject(null);
    }
  };

  const handleRunDoctor = async () => {
    setRunningDoctor(true);
    try {
      const checks = await api.runDoctor();
      setDoctorChecks(checks);
      showToast('Diagnóstico concluído.');
    } catch (err: any) {
      showToast(`Erro no diagnóstico: ${err?.message || err}`);
    } finally {
      setRunningDoctor(false);
    }
  };

  const handleApplyFix = async (action: string, target: string) => {
    try {
      showToast(`Aplicando correção guiada: ${action}...`);
      await api.applyDoctorFix(action, target);
      await handleRunDoctor();
      await refreshData();
      showToast('Correção aplicada com sucesso!');
    } catch (err: any) {
      showToast(`Erro ao aplicar correção: ${err?.message || err}`);
    }
  };

  const handleExportConfig = async () => {
    try {
      const data = await api.exportConfigJSON();
      await navigator.clipboard?.writeText(data);
      showToast('Configuração sanitizada copiada para a área de transferência.');
    } catch (err: any) {
      showToast(`Erro ao exportar configuração: ${err?.message || err}`);
    }
  };

  const handleExportDiagnostic = async () => {
    try {
      const path = await api.exportDiagnostic();
      showToast(path ? `Diagnóstico exportado para ${path}` : 'Diagnóstico exportado.', 8000);
    } catch (err: any) {
      showToast(`Erro ao exportar diagnóstico: ${err?.message || err}`);
    }
  };

  const handleTrustCA = async () => {
    try {
      await api.trustCA();
      showToast('CA interna instalada como confiável neste computador.');
    } catch (err: any) {
      showToast(`Não foi possível confiar na CA: ${err?.message || err}`);
    }
  };

  const handleOpenEdit = (project: ProjectInfo) => {
    setEditingProject(project);
    setProjectForm({
      name: project.name,
      mode: project.mode,
      phpVersion: project.phpVersion || '',
      tlsEnabled: project.tlsEnabled,
      routeMode: project.routingMode,
      routePort: project.port,
      routeHost: project.host,
      staticDir: project.staticDir || '',
    });
  };

  const handleSaveProject = async () => {
    if (!editingProject) return;
    try {
      await api.saveProjectConfig(projectForm);
      setEditingProject(null);
      await refreshData();
      showToast(`Configurações de ${editingProject.name} salvas com sucesso.`);
    } catch (err: any) {
      showToast(`Erro ao salvar: ${err?.message || err}`);
    }
  };

  const handleAddProject = async () => {
    if (!newProject.path.trim()) {
      showToast('O caminho do diretório é obrigatório.');
      return;
    }
    try {
      if (newProject.isPark) {
        await api.parkDir(newProject.path.trim());
        showToast(`Diretório estacionado com sucesso.`);
      } else {
        if (!newProject.name.trim()) {
          showToast('O nome do projeto é obrigatório para link.');
          return;
        }
        await api.linkProject(newProject.name.trim(), newProject.path.trim());
        showToast(`Projeto ${newProject.name} vinculado com sucesso.`);
      }
      setIsAddModalOpen(false);
      setNewProject({ name: '', path: '', isPark: false });
      await refreshData();
    } catch (err: any) {
      showToast(`Erro ao registrar: ${err?.message || err}`);
    }
  };

  const handleOpenSettings = async () => {
    setActiveTab('settings');
    try {
      const [cfg, versions] = await Promise.all([api.getGlobalConfig(), api.getPHPVersions()]);
      setGlobalConfig(cfg);
      setPhpVersions(versions);
    } catch (err: any) {
      showToast(`Erro ao carregar configurações: ${err?.message || err}`);
    }
  };

  const handleInstallPHP = async () => {
    const version = phpVersionInput.trim();
    if (!version) return showToast('Informe uma versão PHP, por exemplo 8.4.');
    try {
      await api.installPHPVersion(version);
      setPhpVersions(await api.getPHPVersions());
      setPhpVersionInput('');
      showToast(`PHP ${version} instalado e registrado.`);
    } catch (err: any) {
      showToast(`Erro ao instalar PHP: ${err?.message || err}`);
    }
  };

  const handleRemovePHP = async (version: string) => {
    if (!confirm(`Remover a versão PHP ${version}?`)) return;
    try {
      await api.removePHPVersion(version);
      setPhpVersions(await api.getPHPVersions());
      showToast(`PHP ${version} removido.`);
    } catch (err: any) {
      showToast(`Erro ao remover PHP: ${err?.message || err}`);
    }
  };

  const handleSetDefaultPHP = async (version: string) => {
    try {
      await api.setDefaultPHPVersion(version);
      setGlobalConfig(current => current ? { ...current, phpDefaultVersion: version } : current);
      showToast(`PHP ${version} definido como padrão.`);
    } catch (err: any) {
      showToast(`Erro ao definir PHP padrão: ${err?.message || err}`);
    }
  };

  const handleSaveGlobalConfig = async () => {
    if (!globalConfig) return;
    try {
      await api.saveGlobalConfig(globalConfig);
      showToast('Configurações globais salvas com sucesso.');
      await refreshData();
    } catch (err: any) {
      showToast(`Erro ao salvar: ${err?.message || err}`);
    }
  };

  const getStatusBadge = (status: ProjectStatus) => {
    switch (status) {
      case 'ready':
        return (
          <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-semibold bg-emerald-100 text-emerald-800 dark:bg-emerald-950/80 dark:text-emerald-300 border border-emerald-300/40">
            <span className="w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse"></span>
            Pronto
          </span>
        );
      case 'starting':
        return (
          <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-semibold bg-amber-100 text-amber-800 dark:bg-amber-950/80 dark:text-amber-300 border border-amber-300/40">
            <span className="w-1.5 h-1.5 rounded-full bg-amber-500 animate-ping"></span>
            Iniciando
          </span>
        );
      case 'degraded':
        return (
          <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-semibold bg-orange-100 text-orange-800 dark:bg-orange-950/80 dark:text-orange-300 border border-orange-300/40">
            <AlertTriangle className="w-3 h-3 text-orange-600 dark:text-orange-400" />
            Degradado
          </span>
        );
      case 'error':
        return (
          <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-semibold bg-rose-100 text-rose-800 dark:bg-rose-950/80 dark:text-rose-300 border border-rose-300/40">
            <XCircle className="w-3 h-3 text-rose-600 dark:text-rose-400" />
            Erro
          </span>
        );
      case 'stopped':
      default:
        return (
          <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium bg-slate-200 text-slate-700 dark:bg-slate-800 dark:text-slate-300">
            <Clock className="w-3 h-3 text-slate-500" />
            Parado
          </span>
        );
    }
  };

  return (
    <div className="flex flex-col min-h-screen">
      {/* Toast Notification */}
      {toastMessage && (
        <div className="fixed bottom-5 right-5 z-50 flex items-center gap-2 px-4 py-2.5 bg-slate-900 text-white dark:bg-slate-100 dark:text-slate-900 rounded-lg shadow-xl border border-slate-700 text-sm font-medium animate-bounce">
          <Activity className="w-4 h-4 text-sky-400 dark:text-sky-600" />
          {toastMessage}
        </div>
      )}

      {/* Top Header */}
      <header className="sticky top-0 z-40 bg-white/80 dark:bg-slate-900/80 backdrop-blur-md border-b border-slate-200 dark:border-slate-800 px-6 py-3.5 flex items-center justify-between shadow-sm">
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2.5">
            <div className="w-8 h-8 rounded-lg bg-gradient-to-tr from-sky-600 to-indigo-600 flex items-center justify-center text-white font-bold shadow-md shadow-sky-500/20">
              DL
            </div>
            <div>
              <h1 className="text-base font-bold leading-none tracking-tight flex items-center gap-2">
                DevLAN
                <span className="text-[10px] font-semibold uppercase tracking-wider px-1.5 py-0.5 rounded bg-sky-100 text-sky-800 dark:bg-sky-950 dark:text-sky-300">
                  v0.1.0-mvp
                </span>
              </h1>
              <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">
                Publicação e gestão local na LAN
              </p>
            </div>
          </div>

          {/* LAN IP and Caddy Status Badge */}
          {systemStatus && (
            <div className="hidden md:flex items-center gap-2 pl-4 border-l border-slate-200 dark:border-slate-800">
              <div className="flex items-center gap-1.5 px-2.5 py-1 bg-slate-100 dark:bg-slate-800/80 rounded-md text-xs font-mono text-slate-700 dark:text-slate-300">
                <Globe className="w-3.5 h-3.5 text-sky-500" />
                <span>LAN: <strong className="text-slate-900 dark:text-white">{systemStatus.lanIp}</strong></span>
              </div>
              <div className="flex items-center gap-1.5 px-2.5 py-1 bg-slate-100 dark:bg-slate-800/80 rounded-md text-xs text-slate-700 dark:text-slate-300">
                <Server className="w-3.5 h-3.5 text-emerald-500" />
                <span>Borda: <strong>:{systemStatus.windowsPort}</strong></span>
                {systemStatus.tlsEnabled && <Lock className="w-3 h-3 text-emerald-500" />}
              </div>
            </div>
          )}
        </div>

        {/* Navigation Tabs & Top Actions */}
        <div className="flex items-center gap-2">
          <nav className="flex items-center bg-slate-100 dark:bg-slate-800/80 p-1 rounded-lg">
            <button
              onClick={() => setActiveTab('projects')}
              className={`px-3 py-1 text-xs font-medium rounded-md transition-all ${
                activeTab === 'projects'
                  ? 'bg-white dark:bg-slate-700 text-sky-600 dark:text-sky-300 shadow-sm font-semibold'
                  : 'text-slate-600 dark:text-slate-400 hover:text-slate-900 dark:hover:text-slate-200'
              }`}
            >
              Projetos ({hasLoadedOnce ? projects.length : '…'})
            </button>
            <button
              onClick={() => {
                setActiveTab('doctor');
                if (doctorChecks.length === 0) handleRunDoctor();
              }}
              className={`px-3 py-1 text-xs font-medium rounded-md transition-all flex items-center gap-1.5 ${
                activeTab === 'doctor'
                  ? 'bg-white dark:bg-slate-700 text-sky-600 dark:text-sky-300 shadow-sm font-semibold'
                  : 'text-slate-600 dark:text-slate-400 hover:text-slate-900 dark:hover:text-slate-200'
              }`}
            >
              <Wrench className="w-3.5 h-3.5" />
              Doctor
            </button>
            <button
              onClick={handleOpenSettings}
              className={`px-3 py-1 text-xs font-medium rounded-md transition-all flex items-center gap-1.5 ${
                activeTab === 'settings'
                  ? 'bg-white dark:bg-slate-700 text-sky-600 dark:text-sky-300 shadow-sm font-semibold'
                  : 'text-slate-600 dark:text-slate-400 hover:text-slate-900 dark:hover:text-slate-200'
              }`}
            >
              <Settings className="w-3.5 h-3.5" />
              Configurações
            </button>
          </nav>

          <button
            onClick={() => refreshData()}
            title="Recarregar dados (Ctrl+R)"
            className="p-2 rounded-lg text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
          >
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin text-sky-500' : ''}`} />
          </button>

          <button
            onClick={() => setDarkMode(!darkMode)}
            title="Alternar tema claro/escuro"
            className="p-2 rounded-lg text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
          >
            {darkMode ? <Sun className="w-4 h-4 text-amber-400" /> : <Moon className="w-4 h-4 text-slate-600" />}
          </button>

          <button
            onClick={() => setIsAddModalOpen(true)}
            className="flex items-center gap-1.5 px-3 py-1.5 bg-sky-600 hover:bg-sky-700 text-white text-xs font-semibold rounded-lg shadow-sm transition-colors"
          >
            <Plus className="w-3.5 h-3.5" />
            Adicionar
          </button>
        </div>
      </header>

      {/* Main Content Area */}
      <main className="flex-1 max-w-7xl w-full mx-auto p-6 space-y-6">
        {/* PROJECTS TAB */}
        {activeTab === 'projects' && (
          <div className="space-y-4">
            {/* Search & Filter Bar */}
            <div className="flex flex-col sm:flex-row gap-3 items-center justify-between bg-white dark:bg-slate-900 p-3 rounded-xl border border-slate-200 dark:border-slate-800 shadow-sm">
              <div className="relative flex-1 w-full">
                <Search className="w-4 h-4 absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" />
                <input
                  ref={searchInputRef}
                  type="text"
                  placeholder="Buscar por nome, caminho ou framework... (Pressione Ctrl+K para focar)"
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  className="w-full pl-9 pr-4 py-2 bg-slate-50 dark:bg-slate-800/60 border border-slate-200 dark:border-slate-700 rounded-lg text-xs focus:outline-none focus:ring-2 focus:ring-sky-500 transition-all placeholder:text-slate-400"
                />
              </div>

              <div className="flex items-center gap-2 w-full sm:w-auto">
                <select
                  value={filterMode}
                  onChange={(e) => setFilterMode(e.target.value)}
                  className="px-3 py-2 bg-slate-50 dark:bg-slate-800/60 border border-slate-200 dark:border-slate-700 rounded-lg text-xs focus:outline-none focus:ring-2 focus:ring-sky-500"
                >
                  <option value="all">Todos os Modos</option>
                  <option value="php">PHP</option>
                  <option value="dev">Dev Server</option>
                  <option value="static">Estático</option>
                  <option value="auto">Auto</option>
                </select>

                <select
                  value={filterStatus}
                  onChange={(e) => setFilterStatus(e.target.value)}
                  className="px-3 py-2 bg-slate-50 dark:bg-slate-800/60 border border-slate-200 dark:border-slate-700 rounded-lg text-xs focus:outline-none focus:ring-2 focus:ring-sky-500"
                >
                  <option value="all">Todos os Status</option>
                  <option value="ready">Pronto</option>
                  <option value="starting">Iniciando</option>
                  <option value="stopped">Parado</option>
                  <option value="degraded">Degradado</option>
                  <option value="error">Erro</option>
                </select>
              </div>
            </div>

            {/* Project Cards Grid */}
            {loading && !hasLoadedOnce ? (
              <div className="text-center py-16 bg-white dark:bg-slate-900 rounded-xl border border-dashed border-slate-300 dark:border-slate-800 p-8 space-y-3">
                <RefreshCw className="w-10 h-10 text-sky-500 mx-auto animate-spin" />
                <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-300">Buscando projetos...</h3>
                <p className="text-xs text-slate-500 max-w-sm mx-auto">
                  Consultando os projetos registrados e verificando os serviços do DevLAN.
                </p>
              </div>
            ) : loadError && projects.length === 0 ? (
              <div className="text-center py-16 bg-white dark:bg-slate-900 rounded-xl border border-dashed border-red-300 dark:border-red-900/70 p-8 space-y-3">
                <XCircle className="w-10 h-10 text-red-500 mx-auto" />
                <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-300">Não foi possível carregar os projetos</h3>
                <p className="text-xs text-slate-500 max-w-sm mx-auto">{loadError}</p>
                <button
                  onClick={() => refreshData()}
                  className="inline-flex items-center gap-1.5 px-3.5 py-1.5 bg-sky-600 hover:bg-sky-700 text-white text-xs font-semibold rounded-lg shadow-sm transition-colors mt-2"
                >
                  <RefreshCw className="w-3.5 h-3.5" />
                  Tentar novamente
                </button>
              </div>
            ) : filteredProjects.length === 0 ? (
              <div className="text-center py-16 bg-white dark:bg-slate-900 rounded-xl border border-dashed border-slate-300 dark:border-slate-800 p-8 space-y-3">
                <Folder className="w-12 h-12 text-slate-300 dark:text-slate-600 mx-auto" />
                <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-300">Nenhum projeto encontrado</h3>
                <p className="text-xs text-slate-500 max-w-sm mx-auto">
                  {searchQuery || filterMode !== 'all' || filterStatus !== 'all'
                    ? 'Nenhum resultado corresponde aos filtros informados.'
                    : 'Nenhum projeto registrado ou estacionado no DevLAN. Clique em "Adicionar" para vincular seu primeiro projeto.'}
                </p>
                {!searchQuery && (
                  <button
                    onClick={() => setIsAddModalOpen(true)}
                    className="inline-flex items-center gap-1.5 px-3.5 py-1.5 bg-sky-600 hover:bg-sky-700 text-white text-xs font-semibold rounded-lg shadow-sm transition-colors mt-2"
                  >
                    <Plus className="w-3.5 h-3.5" />
                    Vincular Novo Projeto
                  </button>
                )}
              </div>
            ) : (
              <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
                {filteredProjects.map((project) => (
                  <div
                    key={project.name}
                    className="bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-800/90 rounded-xl p-5 shadow-sm hover:shadow-md transition-shadow flex flex-col justify-between space-y-4"
                  >
                    {/* Top Row: Name, Badges & TLS */}
                    <div className="flex items-start justify-between gap-3">
                      <div className="space-y-1 min-w-0">
                        <div className="flex items-center gap-2">
                          <h2 className="text-base font-bold text-slate-900 dark:text-white truncate">
                            {project.name}
                          </h2>
                          {getStatusBadge(project.status)}
                          {project.kind === 'parked' && (
                            <span className="text-[10px] uppercase font-bold tracking-wider px-1.5 py-0.5 rounded bg-purple-100 text-purple-800 dark:bg-purple-950 dark:text-purple-300">
                              Estacionado
                            </span>
                          )}
                        </div>
                         <p className="text-xs font-mono text-slate-500 dark:text-slate-400 truncate" title={project.path}>
                           {project.path}
                         </p>
                         {project.statusDetail && (
                           <p className="text-[11px] text-orange-700 dark:text-orange-300" title={project.statusDetail}>
                             {project.statusDetail}
                           </p>
                         )}
                      </div>

                      {/* SSL Toggle button */}
                      <button
                        onClick={() => handleToggleTLS(project)}
                        title={project.tlsEnabled ? 'SSL Ativo (Clique para desativar)' : 'SSL Desativado (Clique para ativar)'}
                        className={`p-1.5 rounded-lg border text-xs font-medium flex items-center gap-1 transition-colors ${
                          project.tlsEnabled
                            ? 'bg-emerald-50 border-emerald-200 text-emerald-700 dark:bg-emerald-950/40 dark:border-emerald-800 dark:text-emerald-300'
                            : 'bg-slate-50 border-slate-200 text-slate-500 dark:bg-slate-800 dark:border-slate-700 dark:text-slate-400'
                        }`}
                      >
                        {project.tlsEnabled ? <Lock className="w-3.5 h-3.5" /> : <Unlock className="w-3.5 h-3.5" />}
                        <span className="text-[10px]">{project.tlsEnabled ? 'SSL ON' : 'SSL OFF'}</span>
                      </button>
                    </div>

                    {/* Metadata tags: Framework, Mode, PHP, Port */}
                    <div className="flex flex-wrap items-center gap-1.5 text-xs">
                      <span className="px-2 py-0.5 bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-300 rounded font-medium">
                        Modo: <strong>{project.effectiveMode}</strong>
                      </span>
                      {project.framework && (
                        <span className="px-2 py-0.5 bg-indigo-50 dark:bg-indigo-950/60 text-indigo-700 dark:text-indigo-300 rounded font-medium">
                          Framework: <strong>{project.framework}</strong>
                        </span>
                      )}
                      {project.phpVersion && (
                        <span className="px-2 py-0.5 bg-sky-50 dark:bg-sky-950/60 text-sky-700 dark:text-sky-300 rounded font-medium">
                          PHP {project.phpVersion}
                        </span>
                      )}
                      {project.devRunning && (
                        <span className="px-2 py-0.5 bg-amber-50 dark:bg-amber-950/60 text-amber-700 dark:text-amber-300 rounded font-mono">
                          Dev PID: {project.devPid} (:{project.devPort})
                        </span>
                      )}
                      <span className="px-2 py-0.5 bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-400 rounded">
                        Rota: {project.routingMode}
                      </span>
                    </div>

                    {/* URL Bar */}
                    <div className="flex items-center gap-2 p-2 bg-slate-50 dark:bg-slate-800/60 rounded-lg border border-slate-200/70 dark:border-slate-700/60">
                      <Globe className="w-4 h-4 text-sky-500 flex-shrink-0" />
                      <a
                        href={project.url}
                        target="_blank"
                        rel="noreferrer"
                        className="text-xs font-mono font-medium text-sky-600 dark:text-sky-400 truncate flex-1 hover:underline"
                        title={project.url}
                      >
                        {project.url}
                      </a>
                      <button
                        onClick={() => handleCopyUrl(project.url)}
                        title="Copiar URL"
                        className="p-1 rounded hover:bg-slate-200 dark:hover:bg-slate-700 text-slate-500 transition-colors"
                      >
                        <Copy className="w-3.5 h-3.5" />
                      </button>
                      <button
                        onClick={() => handleOpenUrl(project.url)}
                        title="Abrir no Navegador"
                        className="p-1 rounded hover:bg-slate-200 dark:hover:bg-slate-700 text-slate-500 transition-colors"
                      >
                        <ExternalLink className="w-3.5 h-3.5" />
                      </button>
                    </div>

                    {/* Action Bar */}
                    <div className="flex items-center justify-between pt-2 border-t border-slate-100 dark:border-slate-800/80">
                      {/* Left: Dev control buttons if mode is dev or auto */}
                      <div className="flex items-center gap-1.5">
                        {(project.effectiveMode === 'dev' || project.effectiveMode === 'auto') && (project.devRunning ? (
                          <>
                            <button
                              onClick={() => handleStopDev(project.name)}
                              className="px-2.5 py-1 bg-rose-50 hover:bg-rose-100 text-rose-700 dark:bg-rose-950/50 dark:hover:bg-rose-900/60 dark:text-rose-300 rounded text-xs font-semibold flex items-center gap-1 transition-colors"
                              title="Parar Servidor Dev"
                            >
                              <Square className="w-3 h-3" />
                              Parar
                            </button>
                            <button
                              onClick={() => handleRestartDev(project.name)}
                              className="px-2.5 py-1 bg-amber-50 hover:bg-amber-100 text-amber-700 dark:bg-amber-950/50 dark:hover:bg-amber-900/60 dark:text-amber-300 rounded text-xs font-semibold flex items-center gap-1 transition-colors"
                              title="Reiniciar Servidor Dev"
                            >
                              <RotateCw className="w-3 h-3" />
                              Reiniciar
                            </button>
                          </>
                        ) : (
                          <button
                            onClick={() => handleStartDev(project.name)}
                            disabled={startingDev === project.name}
                            className="px-2.5 py-1 bg-emerald-50 hover:bg-emerald-100 disabled:opacity-60 disabled:cursor-wait text-emerald-700 dark:bg-emerald-950/50 dark:hover:bg-emerald-900/60 dark:text-emerald-300 rounded text-xs font-semibold flex items-center gap-1 transition-colors"
                            title="Iniciar Servidor Dev"
                          >
                            {startingDev === project.name ? <RefreshCw className="w-3 h-3 animate-spin" /> : <Play className="w-3 h-3" />}
                            {startingDev === project.name ? 'Iniciando...' : 'Iniciar Dev'}
                          </button>
                        )
                        )}

                        <button
                          onClick={() => handleOpenLogs(project.name)}
                          className="px-2.5 py-1 bg-slate-100 hover:bg-slate-200 text-slate-700 dark:bg-slate-800 dark:hover:bg-slate-700 dark:text-slate-300 rounded text-xs font-medium flex items-center gap-1 transition-colors"
                          title="Ver Logs"
                        >
                          <Terminal className="w-3 h-3" />
                          Logs
                        </button>
                      </div>

                      {/* Right: Edit & Unlink */}
                      <div className="flex items-center gap-1">
                        <button
                          onClick={() => handleOpenEdit(project)}
                          className="p-1.5 rounded hover:bg-slate-100 dark:hover:bg-slate-800 text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 transition-colors"
                          title="Editar Configurações do Projeto"
                        >
                          <Settings className="w-4 h-4" />
                        </button>
                        {project.kind === 'linked' ? (
                          <button
                            onClick={() => handleUnlink(project.name)}
                            className="p-1.5 rounded hover:bg-rose-50 dark:hover:bg-rose-950 text-slate-400 hover:text-rose-600 transition-colors"
                            title="Desvincular Projeto"
                          >
                            <Trash2 className="w-4 h-4" />
                          </button>
                        ) : (
                          <button
                            onClick={() => handleHide(project.name)}
                            disabled={hidingProject === project.name}
                            className="p-1.5 rounded hover:bg-amber-50 dark:hover:bg-amber-950 disabled:opacity-50 text-slate-400 hover:text-amber-600 transition-colors"
                            title="Ocultar Projeto Estacionado"
                          >
                            {hidingProject === project.name ? <RefreshCw className="w-4 h-4 animate-spin" /> : <EyeOff className="w-4 h-4" />}
                          </button>
                        )}
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}

        {/* DOCTOR TAB */}
        {activeTab === 'doctor' && (
          <div className="space-y-4">
            <div className="flex items-center justify-between bg-white dark:bg-slate-900 p-4 rounded-xl border border-slate-200 dark:border-slate-800 shadow-sm">
              <div className="space-y-1">
                <h2 className="text-sm font-bold text-slate-900 dark:text-white flex items-center gap-2">
                  <Wrench className="w-4 h-4 text-sky-500" />
                  Diagnóstico Integrado do DevLAN
                </h2>
                <p className="text-xs text-slate-500">
                  Verifica firewall, portas em conflito, Caddys no Windows e WSL, PHP-FPM, e rotas ativas.
                </p>
              </div>
              <button
                onClick={handleRunDoctor}
                disabled={runningDoctor}
                className="px-3.5 py-1.5 bg-sky-600 hover:bg-sky-700 text-white text-xs font-semibold rounded-lg shadow-sm flex items-center gap-1.5 transition-colors disabled:opacity-50"
              >
                <RefreshCw className={`w-3.5 h-3.5 ${runningDoctor ? 'animate-spin' : ''}`} />
                {runningDoctor ? 'Executando...' : 'Executar Diagnóstico'}
              </button>
            </div>

            <div className="space-y-2">
              {doctorChecks.length === 0 ? (
                <div className="text-center py-12 bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-800 p-6">
                  <p className="text-xs text-slate-500">Clique em "Executar Diagnóstico" para iniciar a verificação de saúde do ambiente.</p>
                </div>
              ) : (
                doctorChecks.map((check, idx) => (
                  <div
                    key={idx}
                    className="flex items-center justify-between p-3.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-800 rounded-xl shadow-sm"
                  >
                    <div className="flex items-center gap-3">
                      {check.status === 'OK' && <CheckCircle2 className="w-5 h-5 text-emerald-500 flex-shrink-0" />}
                      {check.status === 'WARN' && <AlertTriangle className="w-5 h-5 text-amber-500 flex-shrink-0" />}
                      {check.status === 'FAIL' && <XCircle className="w-5 h-5 text-rose-500 flex-shrink-0" />}
                      <div>
                        <h4 className="text-xs font-bold text-slate-900 dark:text-white">{check.name}</h4>
                        <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">{check.detail}</p>
                      </div>
                    </div>

                    {check.fixable && check.fixAction && (
                      <button
                        onClick={() => handleApplyFix(check.fixAction!, check.name)}
                        className="px-2.5 py-1 bg-amber-100 hover:bg-amber-200 text-amber-900 dark:bg-amber-950 dark:hover:bg-amber-900 dark:text-amber-200 text-xs font-semibold rounded-lg transition-colors flex items-center gap-1"
                      >
                        <Wrench className="w-3 h-3" />
                        Corrigir
                      </button>
                    )}
                  </div>
                ))
              )}
            </div>
          </div>
        )}

        {/* SETTINGS TAB */}
        {activeTab === 'settings' && globalConfig && (
          <div className="bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-800 rounded-xl p-6 shadow-sm space-y-6 max-w-2xl mx-auto">
            <div className="space-y-1">
              <h2 className="text-base font-bold text-slate-900 dark:text-white flex items-center gap-2">
                <Settings className="w-4 h-4 text-sky-500" />
                Configurações Globais
              </h2>
              <p className="text-xs text-slate-500">
                Ajuste os parâmetros padrão gravados em <code className="text-sky-600">%LOCALAPPDATA%/DevLAN/config.toml</code>.
              </p>
            </div>

            <div className="space-y-4 text-xs">
              <div className="space-y-1.5">
                <label className="font-semibold text-slate-700 dark:text-slate-300">Modo Padrão Global</label>
                <select
                  value={globalConfig.defaultMode}
                  onChange={(e) => setGlobalConfig({ ...globalConfig, defaultMode: e.target.value })}
                  className="w-full px-3 py-2 bg-slate-50 dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-lg text-xs focus:outline-none focus:ring-2 focus:ring-sky-500"
                >
                  <option value="auto">auto (Detecta automaticamente PHP ou JS)</option>
                  <option value="php">php (PHP-FPM)</option>
                  <option value="dev">dev (Servidor Dev Node/Bun/Vite)</option>
                  <option value="static">static (Arquivos estáticos)</option>
                </select>
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div className="space-y-1.5">
                  <label className="font-semibold text-slate-700 dark:text-slate-300">Porta HTTP Borda (Windows)</label>
                  <input
                    type="number"
                    value={globalConfig.windowsPort}
                    onChange={(e) => setGlobalConfig({ ...globalConfig, windowsPort: parseInt(e.target.value) || 80 })}
                    className="w-full px-3 py-2 bg-slate-50 dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-lg text-xs focus:outline-none focus:ring-2 focus:ring-sky-500"
                  />
                </div>
                <div className="space-y-1.5">
                  <label className="font-semibold text-slate-700 dark:text-slate-300">Porta HTTPS Borda</label>
                  <input
                    type="number"
                    value={globalConfig.httpsPort}
                    onChange={(e) => setGlobalConfig({ ...globalConfig, httpsPort: parseInt(e.target.value) || 443 })}
                    className="w-full px-3 py-2 bg-slate-50 dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-lg text-xs focus:outline-none focus:ring-2 focus:ring-sky-500"
                  />
                </div>
              </div>

              <div className="space-y-1.5">
                <label className="font-semibold text-slate-700 dark:text-slate-300">Versão Padrão do PHP</label>
                <input
                  type="text"
                  placeholder="Ex: 8.3 ou 8.5"
                  value={globalConfig.phpDefaultVersion}
                  onChange={(e) => setGlobalConfig({ ...globalConfig, phpDefaultVersion: e.target.value })}
                  className="w-full px-3 py-2 bg-slate-50 dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-lg text-xs focus:outline-none focus:ring-2 focus:ring-sky-500"
                />
              </div>

              <div className="space-y-1.5">
                <label className="font-semibold text-slate-700 dark:text-slate-300">Sub-redes Permitidas (Allowlist - separadas por vírgula)</label>
                <input
                  type="text"
                  placeholder="192.168.0.0/16, 10.0.0.0/8"
                  value={globalConfig.allowlist.join(', ')}
                  onChange={(e) => setGlobalConfig({
                    ...globalConfig,
                    allowlist: e.target.value.split(',').map(s => s.trim()).filter(Boolean)
                  })}
                  className="w-full px-3 py-2 bg-slate-50 dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-lg text-xs focus:outline-none focus:ring-2 focus:ring-sky-500"
                />
              </div>

              <div className="flex items-center justify-end gap-2 pt-4 border-t border-slate-200 dark:border-slate-800">
                <button
                  onClick={() => setActiveTab('projects')}
                  className="px-4 py-2 bg-slate-100 hover:bg-slate-200 dark:bg-slate-800 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-300 text-xs font-semibold rounded-lg transition-colors"
                >
                  Cancelar
                </button>
                <button
                  onClick={handleSaveGlobalConfig}
                  className="px-4 py-2 bg-sky-600 hover:bg-sky-700 text-white text-xs font-semibold rounded-lg shadow-sm transition-colors"
                >
                  Salvar Alterações
                </button>
              </div>

              <div className="pt-4 border-t border-slate-200 dark:border-slate-800 space-y-2">
                <p className="font-semibold text-slate-700 dark:text-slate-300">Versões PHP-FPM</p>
                <div className="flex gap-2">
                  <input value={phpVersionInput} onChange={(e) => setPhpVersionInput(e.target.value)} placeholder="Ex: 8.4" className="flex-1 px-3 py-1.5 bg-slate-50 dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-lg" />
                  <button onClick={handleInstallPHP} className="px-3 py-1.5 bg-emerald-600 hover:bg-emerald-700 text-white rounded-lg font-semibold">Instalar</button>
                </div>
                {phpVersions.length === 0 ? <p className="text-slate-500">Nenhuma versão registrada.</p> : (
                  <div className="space-y-1">
                    {phpVersions.map(version => (
                      <div key={version.version} className="flex items-center justify-between px-3 py-2 bg-slate-50 dark:bg-slate-800/60 rounded-lg">
                        <span className="font-mono">PHP {version.version} {version.installed ? '· instalado' : '· ausente'}</span>
                        <span className="flex gap-2">
                          <button onClick={() => handleSetDefaultPHP(version.version)} className="text-sky-600 dark:text-sky-300 font-semibold">Usar</button>
                          <button onClick={() => handleRemovePHP(version.version)} className="text-rose-600 dark:text-rose-300 font-semibold">Remover</button>
                        </span>
                      </div>
                    ))}
                  </div>
                )}
              </div>

              <div className="pt-4 border-t border-slate-200 dark:border-slate-800 space-y-2">
                <p className="font-semibold text-slate-700 dark:text-slate-300">Operações de suporte</p>
                <div className="flex flex-wrap gap-2">
                  <button onClick={handleExportConfig} className="px-3 py-1.5 bg-slate-100 hover:bg-slate-200 dark:bg-slate-800 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-300 rounded-lg font-semibold">
                    Copiar configuração sanitizada
                  </button>
                  <button onClick={handleExportDiagnostic} className="px-3 py-1.5 bg-slate-100 hover:bg-slate-200 dark:bg-slate-800 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-300 rounded-lg font-semibold">
                    Exportar diagnóstico
                  </button>
                  <button onClick={handleTrustCA} className="px-3 py-1.5 bg-sky-100 hover:bg-sky-200 dark:bg-sky-950 dark:hover:bg-sky-900 text-sky-800 dark:text-sky-200 rounded-lg font-semibold">
                    Confiar na CA local
                  </button>
                </div>
              </div>
            </div>
          </div>
        )}
      </main>

      {/* MODAL: LOG VIEWER */}
      {selectedProjectForLogs && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-sm">
          <div className="bg-slate-900 border border-slate-800 text-slate-100 rounded-xl max-w-3xl w-full flex flex-col max-h-[80vh] shadow-2xl overflow-hidden animate-in fade-in zoom-in duration-150">
            <div className="px-4 py-3 border-b border-slate-800 flex items-center justify-between">
              <div className="flex items-center gap-2">
                <Terminal className="w-4 h-4 text-sky-400" />
                <h3 className="text-xs font-mono font-bold text-slate-200">
                  Logs do Projeto: <span className="text-sky-400">{selectedProjectForLogs}</span>
                </h3>
              </div>
              <button
                onClick={() => setSelectedProjectForLogs(null)}
                className="text-slate-400 hover:text-white p-1 rounded"
              >
                ✕
              </button>
            </div>

            <div className="p-4 flex-1 overflow-auto bg-slate-950 font-mono text-xs text-slate-300 whitespace-pre-wrap leading-relaxed select-text">
              {loadingLogs ? (
                <div className="flex items-center gap-2 text-slate-500">
                  <RefreshCw className="w-4 h-4 animate-spin" /> Carregando logs...
                </div>
              ) : (
                logContent
              )}
            </div>

            <div className="px-4 py-2.5 border-t border-slate-800 flex items-center justify-between text-xs text-slate-400 bg-slate-900">
              <span>Últimas linhas capturadas</span>
              <button
                onClick={() => handleOpenLogs(selectedProjectForLogs)}
                className="px-2.5 py-1 bg-slate-800 hover:bg-slate-700 text-slate-200 rounded flex items-center gap-1.5 transition-colors"
              >
                <RefreshCw className="w-3 h-3" /> Atualizar
              </button>
            </div>
          </div>
        </div>
      )}

      {/* MODAL: EDIT PROJECT OVERRIDES */}
      {editingProject && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-sm">
          <div className="bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-800 rounded-xl max-w-lg w-full p-6 shadow-2xl space-y-4 animate-in fade-in zoom-in duration-150">
            <div className="flex items-center justify-between border-b border-slate-200 dark:border-slate-800 pb-3">
              <h3 className="text-sm font-bold text-slate-900 dark:text-white flex items-center gap-2">
                <Settings className="w-4 h-4 text-sky-500" />
                Configurar: {editingProject.name}
              </h3>
              <button onClick={() => setEditingProject(null)} className="text-slate-400 hover:text-slate-600 p-1">
                ✕
              </button>
            </div>

            <div className="space-y-3 text-xs">
              <div className="space-y-1">
                <label className="font-semibold text-slate-700 dark:text-slate-300">Modo de Operação</label>
                <select
                  value={projectForm.mode || ''}
                  onChange={(e) => setProjectForm({ ...projectForm, mode: e.target.value })}
                  className="w-full px-3 py-2 bg-slate-50 dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-lg text-xs focus:outline-none focus:ring-2 focus:ring-sky-500"
                >
                  <option value="">(Herda padrão global)</option>
                  <option value="php">PHP-FPM</option>
                  <option value="dev">Dev Server (Vite, Next, Astro, etc.)</option>
                  <option value="static">Estático</option>
                  <option value="auto">Auto detecção</option>
                </select>
              </div>

              <div className="space-y-1">
                <label className="font-semibold text-slate-700 dark:text-slate-300">Modo de Roteamento</label>
                <select
                  value={projectForm.routeMode || 'path'}
                  onChange={(e) => setProjectForm({ ...projectForm, routeMode: e.target.value })}
                  className="w-full px-3 py-2 bg-slate-50 dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-lg text-xs focus:outline-none focus:ring-2 focus:ring-sky-500"
                >
                  <option value="path">Subpath (http://IP/nome)</option>
                  <option value="port">Porta Dedicada (http://IP:PORT)</option>
                  <option value="host">Hostname Dedicado (http://nome.devlan)</option>
                </select>
              </div>

              {projectForm.routeMode === 'port' && (
                <div className="space-y-1">
                  <label className="font-semibold text-slate-700 dark:text-slate-300">Porta Dedicada</label>
                  <input
                    type="number"
                    value={projectForm.routePort || ''}
                    placeholder="Ex: 8080"
                    onChange={(e) => setProjectForm({ ...projectForm, routePort: parseInt(e.target.value) || undefined })}
                    className="w-full px-3 py-2 bg-slate-50 dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-lg text-xs"
                  />
                </div>
              )}

              {projectForm.routeMode === 'host' && (
                <div className="space-y-1">
                  <label className="font-semibold text-slate-700 dark:text-slate-300">Hostname</label>
                  <input
                    type="text"
                    value={projectForm.routeHost || ''}
                    placeholder="Ex: app.local"
                    onChange={(e) => setProjectForm({ ...projectForm, routeHost: e.target.value })}
                    className="w-full px-3 py-2 bg-slate-50 dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-lg text-xs"
                  />
                </div>
              )}

              <div className="space-y-1">
                <label className="font-semibold text-slate-700 dark:text-slate-300">Versão PHP Específica</label>
                <input
                  type="text"
                  placeholder="Ex: 8.3 (deixe em branco para herdar)"
                  value={projectForm.phpVersion || ''}
                  onChange={(e) => setProjectForm({ ...projectForm, phpVersion: e.target.value })}
                  className="w-full px-3 py-2 bg-slate-50 dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-lg text-xs"
                />
              </div>

              <div className="space-y-1">
                <label className="font-semibold text-slate-700 dark:text-slate-300">Diretório Estático de Build</label>
                <input
                  type="text"
                  placeholder="Ex: dist, build ou out"
                  value={projectForm.staticDir || ''}
                  onChange={(e) => setProjectForm({ ...projectForm, staticDir: e.target.value })}
                  className="w-full px-3 py-2 bg-slate-50 dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-lg text-xs"
                />
              </div>
            </div>

            <div className="flex items-center justify-end gap-2 pt-3 border-t border-slate-200 dark:border-slate-800">
              <button
                onClick={() => setEditingProject(null)}
                className="px-3.5 py-1.5 bg-slate-100 hover:bg-slate-200 dark:bg-slate-800 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-300 text-xs font-semibold rounded-lg"
              >
                Cancelar
              </button>
              <button
                onClick={handleSaveProject}
                className="px-3.5 py-1.5 bg-sky-600 hover:bg-sky-700 text-white text-xs font-semibold rounded-lg shadow-sm"
              >
                Salvar Overrides
              </button>
            </div>
          </div>
        </div>
      )}

      {/* MODAL: ADD / PARK PROJECT */}
      {isAddModalOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-sm">
          <div className="bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-800 rounded-xl max-w-md w-full p-6 shadow-2xl space-y-4 animate-in fade-in zoom-in duration-150">
            <div className="flex items-center justify-between border-b border-slate-200 dark:border-slate-800 pb-3">
              <h3 className="text-sm font-bold text-slate-900 dark:text-white flex items-center gap-2">
                <Plus className="w-4 h-4 text-sky-500" />
                Adicionar ao DevLAN
              </h3>
              <button onClick={() => setIsAddModalOpen(false)} className="text-slate-400 hover:text-slate-600 p-1">
                ✕
              </button>
            </div>

            <div className="space-y-3 text-xs">
              <div className="flex items-center gap-3 p-2 bg-slate-50 dark:bg-slate-800/60 rounded-lg">
                <label className="flex items-center gap-2 font-medium cursor-pointer">
                  <input
                    type="radio"
                    name="addType"
                    checked={!newProject.isPark}
                    onChange={() => setNewProject({ ...newProject, isPark: false })}
                    className="text-sky-600"
                  />
                  Vincular Projeto Único (Link)
                </label>
                <label className="flex items-center gap-2 font-medium cursor-pointer">
                  <input
                    type="radio"
                    name="addType"
                    checked={newProject.isPark}
                    onChange={() => setNewProject({ ...newProject, isPark: true })}
                    className="text-sky-600"
                  />
                  Estacionar Diretório (Park)
                </label>
              </div>

              {!newProject.isPark && (
                <div className="space-y-1">
                  <label className="font-semibold text-slate-700 dark:text-slate-300">Nome do Projeto</label>
                  <input
                    type="text"
                    placeholder="Ex: financeiro ou meu-app"
                    value={newProject.name}
                    onChange={(e) => setNewProject({ ...newProject, name: e.target.value })}
                    className="w-full px-3 py-2 bg-slate-50 dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-lg text-xs focus:outline-none focus:ring-2 focus:ring-sky-500"
                  />
                </div>
              )}

              <div className="space-y-1">
                <label className="font-semibold text-slate-700 dark:text-slate-300">
                  {newProject.isPark ? 'Caminho do Diretório de Projetos' : 'Caminho do Projeto'}
                </label>
                <input
                  type="text"
                  placeholder={newProject.isPark ? 'Ex: C:\\Users\\Sites ou ~/Sites' : 'Ex: C:\\Users\\Sites\\meu-app'}
                  value={newProject.path}
                  onChange={(e) => setNewProject({ ...newProject, path: e.target.value })}
                  className="w-full px-3 py-2 bg-slate-50 dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-lg text-xs focus:outline-none focus:ring-2 focus:ring-sky-500"
                />
              </div>
            </div>

            <div className="flex items-center justify-end gap-2 pt-3 border-t border-slate-200 dark:border-slate-800">
              <button
                onClick={() => setIsAddModalOpen(false)}
                className="px-3.5 py-1.5 bg-slate-100 hover:bg-slate-200 dark:bg-slate-800 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-300 text-xs font-semibold rounded-lg"
              >
                Cancelar
              </button>
              <button
                onClick={handleAddProject}
                className="px-3.5 py-1.5 bg-sky-600 hover:bg-sky-700 text-white text-xs font-semibold rounded-lg shadow-sm"
              >
                {newProject.isPark ? 'Estacionar' : 'Vincular'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
