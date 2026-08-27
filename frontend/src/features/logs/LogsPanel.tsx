import { RefreshCw } from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';
import type { DevLANClient } from '../../api';
import { api } from '../../api';

type LoadSignal = { active: boolean };

export function LogsPanel({ project, client = api }: { project: string; client?: DevLANClient }) {
  const [logs, setLogs] = useState('');
  const [loading, setLoading] = useState(true);

  const load = useCallback(
    async (signal?: LoadSignal) => {
      setLoading(true);
      try {
        const value = await client.getProjectLogs(project, 160);
        if (!signal || signal.active) setLogs(value);
      } catch (error) {
        if (!signal || signal.active) {
          setLogs(`Erro ao carregar logs: ${String(error)}`);
        }
      } finally {
        if (!signal || signal.active) setLoading(false);
      }
    },
    [client, project],
  );

  useEffect(() => {
    const signal: LoadSignal = { active: true };
    void Promise.resolve().then(() => load(signal));
    return () => {
      signal.active = false;
    };
  }, [load]);

  return (
    <section className="logs-panel">
      <div className="panel-toolbar">
        <div>
          <span className="section-label">LOGS</span>
          <h2>{project}</h2>
        </div>
        <button type="button" onClick={() => void load()} disabled={loading}>
          <RefreshCw size={15} aria-hidden="true" /> {loading ? 'Atualizando…' : 'Atualizar logs'}
        </button>
      </div>
      <pre aria-live="polite">
        {loading ? 'Carregando logs…' : logs || 'Nenhum log registrado.'}
      </pre>
    </section>
  );
}
