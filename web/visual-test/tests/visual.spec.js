import { test, expect } from '@playwright/test';

const MOCK = '?mock=1';

test.describe('visual regression', () => {
  test('style gallery', async ({ page }) => {
    await page.goto(`/debug/gallery/${MOCK}`);
    await page.waitForSelector('curio-ux', { state: 'attached', timeout: 15000 });
    await page.waitForSelector('style-gallery', { state: 'attached', timeout: 15000 });
    await page.waitForTimeout(800);
    await expect(page).toHaveScreenshot('gallery.png', { fullPage: true });
  });

  test('dashboard overview', async ({ page }) => {
    await page.goto(`/${MOCK}`);
    await page.waitForSelector('curio-ux', { state: 'attached', timeout: 15000 });
    await page.waitForSelector('cluster-machines', { state: 'attached', timeout: 15000 });
    await page.waitForTimeout(800);
    await expect(page).toHaveScreenshot('dashboard.png', { fullPage: true });
  });

  test('alerts page', async ({ page }) => {
    await page.goto(`/pages/alerts/${MOCK}`);
    await page.waitForSelector('curio-ux', { state: 'attached', timeout: 15000 });
    await page.waitForSelector('alerts-manage', { state: 'attached', timeout: 15000 });
    await page.waitForTimeout(800);
    await expect(page).toHaveScreenshot('alerts.png', { fullPage: true });
  });

  test('task page', async ({ page }) => {
    await page.goto(`/pages/task/${MOCK}`);
    await page.waitForSelector('curio-ux', { state: 'attached', timeout: 15000 });
    await page.waitForTimeout(800);
    await expect(page).toHaveScreenshot('task.png', { fullPage: true });
  });
});

test.describe('contrast checks', () => {
  test('body text meets AA contrast on canvas', async ({ page }) => {
    await page.goto(`/debug/gallery/${MOCK}`);
    await page.waitForSelector('curio-ux', { state: 'attached', timeout: 15000 });

    const ratio = await page.evaluate(() => {
      const fg = getComputedStyle(document.body).color;
      const bg = getComputedStyle(document.body).backgroundColor;
      const parse = (str) => {
        const m = str.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)/);
        return m ? [Number(m[1]), Number(m[2]), Number(m[3])] : null;
      };
      const lum = ([r, g, b]) => {
        const [rs, gs, bs] = [r, g, b].map((c) => {
          const s = c / 255;
          return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
        });
        return 0.2126 * rs + 0.7152 * gs + 0.0722 * bs;
      };
      const fgRgb = parse(fg);
      const bgRgb = parse(bg);
      if (!fgRgb || !bgRgb) return 0;
      const l1 = lum(fgRgb);
      const l2 = lum(bgRgb);
      return (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05);
    });

    expect(ratio).toBeGreaterThanOrEqual(4.5);
  });
});
