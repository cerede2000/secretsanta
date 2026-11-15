// Pages des participants : bouton « Copier » du lien régénéré, et fermeture du
// menu « … » ouvert quand un autre s'ouvre. Les textes viennent des attributs
// data-* du gabarit ; sans script, la page reste complète (le lien est dans un
// champ en lecture seule).
const root = document.querySelector('[data-people]');
if (root) {
  const copy = root.querySelector('[data-people-copy]');
  const field = root.querySelector('[data-people-link]');
  const status = root.querySelector('[data-people-copied]');
  copy?.addEventListener('click', async () => {
    if (!field) return;
    field.select();
    try {
      await navigator.clipboard.writeText(field.value);
    } catch {
      document.execCommand('copy');
    }
    if (status) status.textContent = root.dataset.copied ?? '';
  });
  const menus = [...root.querySelectorAll('.ppl-menu')];
  for (const m of menus) {
    m.addEventListener('toggle', () => {
      if (!m.open) return;
      for (const o of menus) if (o !== m) o.open = false;
    });
  }
}
