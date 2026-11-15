import type { Page } from '@playwright/test';

/**
 * F12b.AC7 : aucune boîte du navigateur (alert, confirm, prompt) pendant un parcours.
 * L'écouteur compte les boîtes ouvertes (et les refuse pour ne pas bloquer la page) ;
 * `stop()` le retire en fin de test (page.off), `expectNone()` échoue s'il en a vu une.
 */
export function guardDialogs(page: Page): { count: () => number; expectNone: () => void; stop: () => void } {
  let n = 0;
  const onDialog = async (d: { dismiss(): Promise<void> }) => {
    n += 1;
    await d.dismiss();
  };
  page.on('dialog', onDialog);
  return {
    count: () => n,
    expectNone: () => {
      if (n !== 0) throw new Error(`${n} boîte(s) du navigateur ouverte(s) : alert, confirm et prompt sont interdits`);
    },
    stop: () => {
      page.off('dialog', onDialog);
    },
  };
}
