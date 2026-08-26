import { FolderOpen } from 'lucide-react';
export function EmptyState({ onAdd }: { onAdd: () => void }) {
  return (
    <div className="empty-state">
      <FolderOpen size={35} />
      <h1>Nenhum projeto selecionado</h1>
      <p>Vincule um projeto ou estacione uma pasta para começar.</p>
      <button type="button" onClick={onAdd}>
        Adicionar projeto
      </button>
    </div>
  );
}
