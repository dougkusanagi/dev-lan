import { Copy, ExternalLink, LoaderCircle, RefreshCw } from 'lucide-react';
import type { ProjectInfo } from '../../types';

function stateLabel(project: ProjectInfo, local: boolean): string {
  if (local) {
    if (project.localDevState === 'active') return 'HMR ativo';
    if (project.localDevState === 'starting') return 'Iniciando HMR';
    return project.localDevState === 'stopped' ? 'HMR parado' : 'Aplicação local';
  }
  return project.lanPreviewState === 'paused' ? 'Pausado durante HMR local' : 'Preview disponível';
}

function projectStatusLabel(status: ProjectInfo['status']): string {
  return (
    {
      ready: 'Pronto',
      starting: 'Iniciando',
      stopped: 'Parado',
      degraded: 'Degradado',
      error: 'Erro',
    } as const
  )[status];
}

function Endpoint({
  label,
  url,
  detail,
  tone,
  active,
  onOpen,
  onCopy,
}: {
  label: string;
  url: string;
  detail: string;
  tone: 'local' | 'lan';
  onOpen: () => void;
  onCopy: () => void;
  active: boolean;
}) {
  const cardTitle =
    tone === 'lan'
      ? 'Acesso pela LAN: porta dedicada. Nota: cookies HTTP não são isolados por porta no mesmo IP.'
      : 'Acesso local (.localhost): cookies, storage e HMR isolados.';

  return (
    <article
      className={`endpoint-card ${tone}${active ? ' active' : ' inactive'}`}
      title={cardTitle}
    >
      <div className="endpoint-heading">
        <span>{label}</span>
        <small>{detail}</small>
      </div>
      <div className="endpoint-address">
        <code title={url}>{url}</code>
        <div className="address-actions">
          <button type="button" title="Abrir endereço" aria-label="Abrir endereço" onClick={onOpen}>
            <ExternalLink size={16} />
          </button>
          <button
            type="button"
            title="Copiar endereço"
            aria-label="Copiar endereço"
            onClick={onCopy}
          >
            <Copy size={16} />
          </button>
        </div>
      </div>
    </article>
  );
}

export function ProjectHeader({
  project,
  tab,
  onTab,
  onOpenLocal,
  onCopyLocal,
  onOpenLAN,
  onCopyLAN,
  onCopyPath,
  onReload,
  reloadPending = false,
}: {
  project: ProjectInfo;
  tab: 'overview' | 'logs';
  onTab: (t: 'overview' | 'logs') => void;
  onOpenLocal: () => void;
  onCopyLocal: () => void;
  onOpenLAN: () => void;
  onCopyLAN: () => void;
  onCopyPath: () => void;
  onToggleTLS?: () => void;
  onReload: () => void;
  reloadPending?: boolean;
}) {
  return (
    <header className="project-header">
      <div className="project-context">
        <div>
          <span>PROJETO</span>
          <h1>{project.name}</h1>
        </div>
        <div className="project-context-actions">
          <span className={`inline-status ${project.status}`}>
            {projectStatusLabel(project.status)}
          </span>
          <button
            type="button"
            title="Recarregar infraestrutura"
            aria-label="Recarregar infraestrutura"
            disabled={reloadPending}
            aria-busy={reloadPending}
            onClick={onReload}
          >
            {reloadPending ? (
              <LoaderCircle className="spin" size={15} aria-hidden="true" />
            ) : (
              <RefreshCw size={15} aria-hidden="true" />
            )}
          </button>
        </div>
      </div>
      <div className="endpoint-grid">
        <Endpoint
          tone="local"
          label="LOCAL"
          url={project.localDevUrl}
          detail={stateLabel(project, true)}
          active={project.localDevState === 'active'}
          onOpen={onOpenLocal}
          onCopy={onCopyLocal}
        />
        <Endpoint
          tone="lan"
          label="LAN · PREVIEW"
          url={project.lanUrl}
          detail={stateLabel(project, false)}
          active={project.lanPreviewState === 'ready'}
          onOpen={onOpenLAN}
          onCopy={onCopyLAN}
        />
      </div>
      <div className="project-tabs">
        <div className="project-tablist" role="tablist" aria-label="Conteúdo do projeto">
          <button
            type="button"
            role="tab"
            aria-selected={tab === 'overview'}
            className={tab === 'overview' ? 'active' : ''}
            onClick={() => onTab('overview')}
          >
            Visão geral
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={tab === 'logs'}
            className={tab === 'logs' ? 'active' : ''}
            onClick={() => onTab('logs')}
          >
            Logs
          </button>
        </div>
        <div className="project-path">
          <code title={project.path}>{project.path}</code>
          <button
            type="button"
            title="Copiar caminho do projeto"
            aria-label="Copiar caminho do projeto"
            onClick={onCopyPath}
          >
            <Copy size={14} />
          </button>
        </div>
      </div>
    </header>
  );
}
