// Bouton « Copier le lien » : copie le champ en lecture seule et annonce le
// résultat. Les textes viennent des attributs data-* posés par le gabarit.
for (const root of document.querySelectorAll('[data-regadm-copy]')) {
  const field = root.querySelector('[data-regadm-field]');
  const button = root.querySelector('[data-regadm-button]');
  const status = root.querySelector('[data-regadm-status]');
  if (!field || !button || !status) continue;
  button.addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(field.value);
      status.textContent = root.dataset.copied ?? '';
    } catch {
      field.focus();
      field.select();
      status.textContent = root.dataset.failed ?? '';
    }
  });
}
