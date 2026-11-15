// Active la neige CSS (aucune animation par flocon ici). Sans effet si
// l'utilisateur demande de réduire les animations.
const reduce = window.matchMedia('(prefers-reduced-motion: reduce)');

function ensureLayer() {
  let snow = document.querySelector('.snow');
  if (!snow) {
    snow = document.createElement('div');
    snow.className = 'snow';
    snow.setAttribute('aria-hidden', 'true');
    for (let i = 1; i <= 3; i++) {
      const layer = document.createElement('div');
      layer.className = 'snow-layer snow-layer-' + i;
      snow.appendChild(layer);
    }
    document.body.prepend(snow);
  }
}

function apply() {
  if (reduce.matches) {
    document.documentElement.removeAttribute('data-snow');
    return;
  }
  ensureLayer();
  document.documentElement.setAttribute('data-snow', 'on');
}

apply();
reduce.addEventListener('change', apply);
