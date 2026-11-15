// Mailbox animation: picks the motion mode from prefers-reduced-motion.
// The animation itself is CSS; this script only toggles the class that
// triggers it. No displayed text lives here.
const root = document.querySelector('[data-mailbox]');
if (root) {
  const art = root.querySelector('.mb-art');
  const query = window.matchMedia('(prefers-reduced-motion: reduce)');
  const apply = () => {
    const reduced = query.matches;
    root.dataset.motion = reduced ? 'reduced' : 'full';
    if (art) art.classList.toggle('is-animated', !reduced);
  };
  apply();
  query.addEventListener('change', apply);
}
