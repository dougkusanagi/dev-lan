import { AlertTriangle } from 'lucide-react';

export function ErrorState({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <section className="error-state" role="alert">
      <AlertTriangle size={30} aria-hidden="true" />
      <h1>Não foi possível carregar a interface</h1>
      <p>{message}</p>
      <button type="button" onClick={onRetry}>
        Tentar novamente
      </button>
    </section>
  );
}
