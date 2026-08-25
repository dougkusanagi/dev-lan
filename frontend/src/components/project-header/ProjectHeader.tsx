import { Copy, ExternalLink, Lock, RefreshCw, ShieldAlert } from 'lucide-react';
import type { ProjectInfo } from '../../types';

function stateLabel(project: ProjectInfo, local: boolean): string {
  if (local) {
    if (project.localDevState === 'active') return 'HMR ativo';
    if (project.localDevState === 'starting') return 'Iniciando HMR';
    return project.localDevState === 'stopped' ? 'HMR parado' : 'Aplicação local';
  }
  return project.lanPreviewState === 'paused' ? 'Pausado durante HMR local' : 'Preview disponível';
}

function Endpoint({ label, url, detail, tone, onOpen, onCopy, tls, onToggleTLS }: { label: string; url: string; detail: string; tone: 'local'|'lan'; onOpen: () => void; onCopy: () => void; tls?: boolean; onToggleTLS?: () => void }) {
  return <article className={`endpoint-card ${tone}`}><div className="endpoint-heading"><span>{label}</span><small>{detail}</small></div><div className="endpoint-address">{tone === 'lan' && <button className={tls ? 'address-tls active' : 'address-tls'} title={tls ? 'Desativar SSL/HTTPS' : 'Ativar SSL/HTTPS'} aria-label={tls ? 'Desativar SSL/HTTPS' : 'Ativar SSL/HTTPS'} onClick={onToggleTLS}>{tls ? <Lock size={15}/> : <ShieldAlert size={15}/>}</button>}<code title={url}>{url}</code><div className="address-actions"><button title="Abrir endereço" aria-label="Abrir endereço" onClick={onOpen}><ExternalLink size={16}/></button><button title="Copiar endereço" aria-label="Copiar endereço" onClick={onCopy}><Copy size={16}/></button></div></div></article>;
}

export function ProjectHeader({ project, tab, onTab, onOpenLocal, onCopyLocal, onOpenLAN, onCopyLAN, onCopyPath, onToggleTLS, onReload }: { project: ProjectInfo; tab: 'overview'|'logs'; onTab: (t: 'overview'|'logs') => void; onOpenLocal: () => void; onCopyLocal: () => void; onOpenLAN: () => void; onCopyLAN: () => void; onCopyPath: () => void; onToggleTLS: () => void; onReload: () => void }) {
  return <header className="project-header"><div className="project-context"><div><span>PROJETO</span><h1>{project.name}</h1></div><div className="project-context-actions"><span className={`inline-status ${project.status}`}>{project.status === 'ready' ? 'Pronto' : project.status}</span><button title="Recarregar infraestrutura" aria-label="Recarregar infraestrutura" onClick={onReload}><RefreshCw size={15}/></button></div></div><div className="endpoint-grid"><Endpoint tone="local" label="LOCAL · HMR" url={project.localDevUrl} detail={stateLabel(project, true)} onOpen={onOpenLocal} onCopy={onCopyLocal}/><Endpoint tone="lan" label="LAN · PREVIEW" url={project.lanUrl} detail={stateLabel(project, false)} onOpen={onOpenLAN} onCopy={onCopyLAN} tls={project.tlsEnabled} onToggleTLS={onToggleTLS}/></div><div className="project-tabs"><button className={tab === 'overview' ? 'active' : ''} onClick={() => onTab('overview')}>Visão geral</button><button className={tab === 'logs' ? 'active' : ''} onClick={() => onTab('logs')}>Logs</button><div className="project-path"><code title={project.path}>{project.path}</code><button title="Copiar caminho do projeto" aria-label="Copiar caminho do projeto" onClick={onCopyPath}><Copy size={14}/></button></div></div></header>;
}
