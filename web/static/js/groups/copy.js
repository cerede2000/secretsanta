// Bouton Copier : copie le texte de l'élément visé par data-copy-target et
// affiche le résultat dans l'élément data-copy-status. Les textes viennent du
// gabarit (data-copied, data-failed), jamais de ce script.
async function copyText(text) {
  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(text);
    return;
  }
  const area = document.createElement('textarea');
  area.value = text;
  area.setAttribute('readonly', '');
  area.className = 'groups-offscreen';
  document.body.appendChild(area);
  area.select();
  const ok = document.execCommand('copy');
  area.remove();
  if (!ok) throw new Error('copy');
}

function setup(button) {
  const target = document.getElementById(button.dataset.copyTarget);
  const status = document.getElementById(button.dataset.copyStatus);
  if (!target) return;
  button.addEventListener('click', async () => {
    let msg = button.dataset.copied;
    let failed = false;
    try {
      await copyText(target.textContent.trim());
    } catch {
      msg = button.dataset.failed;
      failed = true;
    }
    if (status) {
      status.textContent = msg;
      status.classList.toggle('groups-status-error', failed);
      status.hidden = false;
    }
  });
}

document.querySelectorAll('[data-copy-target]').forEach(setup);
