// Panneau « Rôles dans le tirage » : envoie le sélecteur d'une ligne dès qu'il
// change et annonce le nombre de personnes cochées. Les textes viennent des
// attributs data-* posés par le gabarit ; sans script, chaque ligne garde son
// bouton.
const root = document.querySelector('[data-roles]');
if (root) {
  root.classList.add('roles-js');
  for (const form of root.querySelectorAll('[data-roles-row]')) {
    const select = form.querySelector('select');
    select?.addEventListener('change', () => form.requestSubmit());
  }
  const status = root.querySelector('[data-roles-count]');
  const picks = [...root.querySelectorAll('[data-roles-pick]')];
  const update = () => {
    const n = picks.filter((p) => p.checked).length;
    if (!status) return;
    if (n === 0) {
      status.textContent = '';
      return;
    }
    const tpl = n === 1 ? root.dataset.selectedOne : root.dataset.selectedOther;
    status.textContent = (tpl ?? '').replace('{n}', String(n));
  };
  for (const p of picks) p.addEventListener('change', update);
}
