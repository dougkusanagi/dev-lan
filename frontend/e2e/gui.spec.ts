import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

const origins = [process.env.DEVLAN_E2E_LOCAL_ORIGIN, process.env.DEVLAN_E2E_DIRECT_ORIGIN].filter(
  (value): value is string => Boolean(value),
);

test.describe('GUI browser-first nas duas origens administrativas', () => {
  test('mostra projetos, origens, override de porta e confirmação operacional', async ({ page }) => {
    test.skip(origins.length < 2, 'Defina DEVLAN_E2E_LOCAL_ORIGIN e DEVLAN_E2E_DIRECT_ORIGIN para executar o E2E real.');
    page.on('dialog', (dialog) => void dialog.accept());

    for (const origin of origins) {
      await page.goto(`${origin}/?mock=true`);
      await expect(page.getByRole('heading', { name: 'financeiro' })).toBeVisible();
      const accessibility = await new AxeBuilder({ page })
        .disableRules(['color-contrast'])
        .analyze();
      expect(accessibility.violations).toEqual([]);
      await expect(page.getByText('https://financeiro.localhost/')).toBeVisible();
      await expect(page.getByText('http://192.168.1.100:8080/')).toBeVisible();
      await test.info().attach(`gui-${new URL(origin).hostname}`, {
        body: await page.screenshot({ animations: 'disabled', caret: 'hide', fullPage: true }),
        contentType: 'image/png',
      });

      const port = page.getByRole('spinbutton', { name: 'Porta LAN' });
      await port.fill('8123');
      await page.getByRole('button', { name: 'Aplicar' }).click();
      await expect(page.getByText('Porta LAN 8123 aplicada.')).toBeVisible();
      await expect(page.getByText('http://192.168.1.100:8123/')).toBeVisible();

      await page.goto(`${origin}/?mock=true&rollback=true`);
      await expect(page.getByRole('heading', { name: 'financeiro' })).toBeVisible();
      await page.getByRole('spinbutton', { name: 'Porta LAN' }).fill('8124');
      await page.getByRole('button', { name: 'Aplicar' }).click();
      await expect(page.getByText(/configuração anterior foi restaurada/i)).toBeVisible();
    }
  });

  test('history fallback e API continuam separadas', async ({ request }) => {
    test.skip(origins.length < 2, 'Defina as duas origens administrativas para executar o E2E real.');
    for (const origin of origins) {
      const pageResponse = await request.get(`${origin}/projects/catalogo`);
      expect(pageResponse.status()).toBe(200);
      expect(pageResponse.headers()['content-type']).toContain('text/html');

      const apiResponse = await request.get(`${origin}/api/v1/unknown`);
      expect(apiResponse.status()).toBe(404);
      expect(apiResponse.headers()['content-type']).toContain('application/json');
    }
  });
});
