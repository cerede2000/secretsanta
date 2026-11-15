// Choix de rôle volontaire : deux cases exclusives. Cocher l'une décoche
// l'autre ; la décocher revient au rôle normal (aucune valeur envoyée).
for (const group of document.querySelectorAll('[data-reg-roles]')) {
  const boxes = [...group.querySelectorAll('input[type="checkbox"]')];
  for (const box of boxes) {
    box.addEventListener('change', () => {
      if (!box.checked) return;
      for (const other of boxes) if (other !== box) other.checked = false;
    });
  }
}
