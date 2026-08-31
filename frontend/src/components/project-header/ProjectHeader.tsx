import { Copy, ExternalLink, LoaderCircle, QrCode, RefreshCw, X } from 'lucide-react';
import { QRCodeSVG } from 'qrcode.react';
import { useEffect, useState } from 'react';
import type { ProjectInfo } from '../../types';
import { ContextMenu, ContextMenuItem } from '../context-menu/ContextMenu';

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
  const [qrOpen, setQROpen] = useState(false);
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
        <ContextMenu label={`Ações do endereço ${label}`}>
          <ContextMenuItem onClick={onOpen}>
            <ExternalLink size={14} aria-hidden="true" /> Abrir endereço
          </ContextMenuItem>
          <ContextMenuItem onClick={onCopy}>
            <Copy size={14} aria-hidden="true" /> Copiar endereço
          </ContextMenuItem>
          <ContextMenuItem onClick={() => setQROpen(true)}>
            <QrCode size={14} aria-hidden="true" /> Mostrar QR code
          </ContextMenuItem>
        </ContextMenu>
      </div>
      <div className="endpoint-address">
        <code title={url}>{url}</code>
      </div>
      {qrOpen && <QRCodeDialog label={label} url={url} onClose={() => setQROpen(false)} />}
    </article>
  );
}

function QRCodeDialog({
  label,
  url,
  onClose,
}: {
  label: string;
  url: string;
  onClose: () => void;
}) {
  useEffect(() => {
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', closeOnEscape);
    return () => document.removeEventListener('keydown', closeOnEscape);
  }, [onClose]);

  return (
    <div className="qr-backdrop" role="presentation">
      <div className="qr-dialog" role="dialog" aria-modal="true" aria-labelledby="qr-dialog-title">
        <button
          type="button"
          className="dialog-close"
          onClick={onClose}
          aria-label="Fechar QR code"
        >
          <X size={18} />
        </button>
        <span className="section-label">ENDEREÇO {label}</span>
        <h2 id="qr-dialog-title">Escaneie para abrir</h2>
        <div className="qr-code">
          <QRCodeSVG value={url} size={208} level="M" includeMargin bgColor="#ffffff" />
        </div>
        <code title={url}>{url}</code>
        <p>Abra a câmera do celular e aponte para o código.</p>
      </div>
    </div>
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
          <ContextMenu label={`Ações do projeto ${project.name}`}>
            <ContextMenuItem onClick={onReload} disabled={reloadPending} busy={reloadPending}>
              {reloadPending ? (
                <LoaderCircle className="spin" size={14} aria-hidden="true" />
              ) : (
                <RefreshCw size={14} aria-hidden="true" />
              )}
              {reloadPending ? 'Recarregando infraestrutura…' : 'Recarregar infraestrutura'}
            </ContextMenuItem>
            <ContextMenuItem onClick={onCopyPath}>
              <Copy size={14} aria-hidden="true" /> Copiar caminho do projeto
            </ContextMenuItem>
          </ContextMenu>
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
        <div className="project-path" title={project.path}>
          <code>{project.path}</code>
        </div>
      </div>
    </header>
  );
}
