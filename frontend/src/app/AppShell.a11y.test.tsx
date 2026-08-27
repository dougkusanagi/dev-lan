import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { axe } from 'vitest-axe';
import { MockDevLANClient } from '../api';
import AppShell from './AppShell';

describe('acessibilidade da shell', () => {
  it('não introduz violações automáticas no estado vazio', async () => {
    const { container } = render(
      <AppShell client={new MockDevLANClient({ projects: [] })} pollIntervalMs={0} />,
    );
    await screen.findByRole('heading', { name: 'Nenhum projeto selecionado' });
    const results = await axe(container, { rules: { 'color-contrast': { enabled: false } } });
    expect(results.violations).toEqual([]);
  });
});
