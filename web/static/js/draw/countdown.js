// Compte à rebours de l'attente (amélioration de la page rendue par le serveur).
// Aucun texte n'est écrit ici : l'étiquette accessible vient de data-t-label,
// avec les marques {days}, {hours} et {minutes}. L'heure du serveur sert de
// référence (data-now), pour ne pas dépendre de l'horloge du téléphone.
const timer = document.querySelector('[data-countdown]');

if (timer) {
  const until = Date.parse(timer.dataset.until);
  const offset = Date.parse(timer.dataset.now) - Date.now();
  const unit = (name) => timer.querySelector('[data-unit="' + name + '"]');
  const days = unit('days');
  const hours = unit('hours');
  const minutes = unit('minutes');
  const done = document.querySelector('[data-countdown-done]');
  const template = timer.dataset.tLabel || '';
  let reloading = false;

  const pad = (n) => String(n).padStart(2, '0');

  function render() {
    const left = Math.max(0, until - (Date.now() + offset));
    const d = Math.floor(left / 86400000);
    const h = Math.floor((left % 86400000) / 3600000);
    const m = Math.floor((left % 3600000) / 60000);
    days.textContent = String(d);
    hours.textContent = pad(h);
    minutes.textContent = pad(m);
    timer.setAttribute(
      'aria-label',
      template.replace('{days}', d).replace('{hours}', h).replace('{minutes}', m),
    );
    if (left > 0) {
      window.setTimeout(render, Math.min(left + 50, 15000));
      return;
    }
    if (!reloading) {
      reloading = true;
      if (done) done.hidden = false;
      // La tâche planifiée ouvre la révélation à la date : on laisse trois
      // secondes au serveur, puis la page se recharge une seule fois.
      window.setTimeout(() => window.location.reload(), 3000);
    }
  }

  render();
}
