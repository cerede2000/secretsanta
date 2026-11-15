// Onglets « Mot de passe » / « Jeton » de la connexion administrateur.
// Sans JavaScript, le champ unique accepte les deux ; les textes viennent des
// attributs data-* du formulaire (jamais écrits ici).
const form = document.querySelector('form[data-access-admin]');
if (form) {
  const tabs = form.querySelector('[role="tablist"]');
  const label = form.querySelector('[data-access-label]');
  const buttons = Array.from(form.querySelectorAll('[role="tab"]'));
  if (tabs && label && buttons.length) {
    tabs.hidden = false;
    const labels = { password: form.dataset.labelPassword, token: form.dataset.labelToken };
    const select = (kind) => {
      for (const b of buttons) b.setAttribute('aria-selected', String(b.dataset.kind === kind));
      if (labels[kind]) label.textContent = labels[kind];
    };
    for (const b of buttons) b.addEventListener('click', () => select(b.dataset.kind));
    select('password');
  }
}
