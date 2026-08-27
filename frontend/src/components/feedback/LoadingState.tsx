export function LoadingState() {
  return (
    <section className="loading-state" aria-busy="true" aria-live="polite">
      <span className="loading-spinner" aria-hidden="true" />
      <h1>Carregando DevLAN</h1>
      <p>Consultando projetos e infraestrutura local…</p>
    </section>
  );
}
