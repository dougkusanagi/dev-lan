import { CirclePlus, LoaderCircle, Lock, Search, Square } from 'lucide-react';
import type { RefObject } from 'react';
import type { ProjectInfo } from '../../types';

export function ProjectSidebar({
  projects,
  selected,
  query,
  onQuery,
  onSelect,
  onToggleTLS,
  onAdd,
  searchRef,
  open,
  pendingTLS = {},
}: {
  projects: ProjectInfo[];
  selected?: string;
  query: string;
  onQuery: (s: string) => void;
  onSelect: (p: ProjectInfo) => void;
  onToggleTLS: (p: ProjectInfo) => void;
  onAdd: () => void;
  searchRef?: RefObject<HTMLInputElement>;
  open?: boolean;
  pendingTLS?: Record<string, boolean>;
}) {
  const groups = [
    ['VINCULADOS', projects.filter((p) => p.kind === 'linked')],
    ['ESTACIONADOS', projects.filter((p) => p.kind !== 'linked')],
  ] as const;
  return (
    <aside className={`project-sidebar${open ? ' open' : ''}`}>
      <div className="sidebar-title">
        <span>SITES</span>
        <button
          type="button"
          onClick={onAdd}
          title="Adicionar projeto"
          aria-label="Adicionar projeto"
        >
          <CirclePlus size={17} />
        </button>
      </div>
      <label className="project-search">
        <Search size={15} />
        <input
          ref={searchRef}
          value={query}
          onChange={(e) => onQuery(e.target.value)}
          placeholder="Buscar sites"
          aria-label="Buscar sites (Ctrl+K)"
        />
      </label>
      <div className="project-groups">
        {groups.map(
          ([name, list]) =>
            list.length > 0 && (
              <section key={name}>
                <h2>
                  {name}
                  <span>{list.length}</span>
                </h2>
                {list.map((project) =>
                  (() => {
                    const tlsTarget = pendingTLS[project.name];
                    const tlsPending = tlsTarget !== undefined;
                    return (
                      <div
                        key={project.name}
                        className={
                          selected === project.name ? 'project-row selected' : 'project-row'
                        }
                      >
                        <button
                          type="button"
                          className="project-select"
                          onClick={() => onSelect(project)}
                        >
                          <span
                            className={`status-dot ${project.status}`}
                            role="img"
                            aria-label={project.status === 'ready' ? 'Ativo' : project.status}
                          />
                          <span className="project-main">
                            <strong>{project.name}</strong>
                            <small>{project.framework || project.effectiveMode}</small>
                          </span>
                        </button>
                        <button
                          type="button"
                          className={`${project.tlsEnabled ? 'tls-toggle tls' : 'tls-toggle tls-off'}${tlsPending ? ' loading' : ''}`}
                          disabled={tlsPending}
                          onClick={() => onToggleTLS(project)}
                          aria-busy={tlsPending}
                          aria-label={
                            tlsPending
                              ? `${tlsTarget ? 'Ativando' : 'Desativando'} TLS`
                              : project.tlsEnabled
                                ? 'Desativar TLS'
                                : 'Ativar TLS'
                          }
                          title={
                            tlsPending
                              ? `${tlsTarget ? 'Ativando' : 'Desativando'} TLS…`
                              : project.tlsEnabled
                                ? 'TLS ativo — desativar'
                                : 'TLS desativado — ativar'
                          }
                        >
                          {tlsPending ? (
                            <LoaderCircle className="spin" size={14} aria-hidden="true" />
                          ) : (
                            <Lock size={14} aria-hidden="true" />
                          )}
                        </button>
                      </div>
                    );
                  })(),
                )}
              </section>
            ),
        )}
      </div>
      <footer>
        <Square size={12} />
        {projects.length} projeto{projects.length === 1 ? '' : 's'}
      </footer>
    </aside>
  );
}
