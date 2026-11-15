// Source de données branchée à l'API de révélation (F4b). Protégée par le
// jeton CSRF (en-tête X-CSRF-Token). Les adresses viennent du gabarit.
import { createStyleMemory } from './memory.js';

export function httpSource(root, win = window) {
  const memory = createStyleMemory(win);
  const csrf = root.dataset.csrf || '';
  return {
    async targets() {
      const res = await win.fetch(root.dataset.targetsUrl, {
        credentials: 'same-origin',
        headers: { Accept: 'application/json' },
      });
      if (!res.ok) throw new Error('targets');
      const body = await res.json();
      return Array.isArray(body) ? body : body.targets || [];
    },
    async confirmReveal(target) {
      const res = await win.fetch(root.dataset.confirmUrl, {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
        body: JSON.stringify({ target_id: target.id }),
      });
      if (!res.ok) throw new Error('confirm');
    },
    lastStyle: () => memory.get(),
    rememberStyle: (s) => memory.set(s),
  };
}
