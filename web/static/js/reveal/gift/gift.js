// Style cadeau enveloppé : appui long (1,5 s) ou glissement pour défaire le
// ruban, bouton équivalent (clavier et toucher simple). S'enregistre dans le
// registre du cœur ; les textes viennent du gabarit (attributs data-t-*).
import { registerStyle } from '../core/registry.js';

export const HOLD_MS = 1500;
export const SWIPE_PX = 80;
const OPEN_MS = 550;
const START_AFTER_MS = 300;

function readLabels(doc) {
  const el = doc.querySelector('[data-reveal-gift]');
  const out = { art: el ? el.querySelector('template[data-role="gift-art"]') : null };
  if (el) {
    for (const [k, v] of Object.entries(el.dataset)) {
      if (/^t[A-Z]/.test(k)) out[k[1].toLowerCase() + k.slice(2)] = v;
    }
  }
  return out;
}

export const gift = {
  mount(stage, ctx) {
    const doc = stage.ownerDocument;
    const own = readLabels(doc);
    const timers = new Set();
    const later = (fn, ms) => {
      const id = setTimeout(() => {
        timers.delete(id);
        fn();
      }, ms);
      timers.add(id);
      return id;
    };
    const clear = (id) => {
      clearTimeout(id);
      timers.delete(id);
    };

    const wrap = doc.createElement('div');
    wrap.className = 'gift';
    const art = doc.createElement('button');
    art.type = 'button';
    art.className = 'gift-art';
    art.setAttribute('aria-label', own.label || ctx.labels.open);
    if (own.art) art.appendChild(own.art.content.cloneNode(true));
    const hint = doc.createElement('p');
    hint.className = 'gift-hint';
    hint.textContent = own.hold || '';
    const note = doc.createElement('p');
    note.className = 'gift-note';
    note.textContent = own.note || '';
    const btn = doc.createElement('button');
    btn.type = 'button';
    btn.className = 'btn btn-gold reveal-open';
    btn.textContent = ctx.labels.open;
    wrap.append(art, hint, btn, note);
    stage.appendChild(wrap);

    let opened = false;
    let holdTimer = null;
    let startTimer = null;
    let pointerId = null;
    let originX = 0;
    let originY = 0;

    function open() {
      if (opened) return;
      opened = true;
      cancelHold();
      ctx.start();
      wrap.classList.add('is-open');
      later(() => ctx.complete(), OPEN_MS);
    }
    function cancelHold() {
      if (holdTimer !== null) clear(holdTimer);
      if (startTimer !== null) clear(startTimer);
      holdTimer = startTimer = null;
      if (!opened) {
        wrap.classList.remove('is-holding');
        hint.textContent = own.hold || '';
      }
    }
    function begin(e) {
      if (opened || pointerId !== null) return;
      pointerId = e.pointerId;
      originX = e.clientX;
      originY = e.clientY;
      try {
        art.setPointerCapture(e.pointerId);
      } catch {
        /* sans capture : les événements restent suivis sur l'élément */
      }
      wrap.classList.add('is-holding');
      startTimer = later(() => {
        hint.textContent = own.holding || '';
        ctx.start();
      }, START_AFTER_MS);
      holdTimer = later(open, HOLD_MS);
    }
    function end(e) {
      if (pointerId === null || (e && e.pointerId !== pointerId)) return;
      pointerId = null;
      cancelHold();
    }

    art.addEventListener('pointerdown', (e) => {
      if (e.pointerType === 'mouse' && e.button !== 0) return;
      begin(e);
    });
    art.addEventListener('pointermove', (e) => {
      if (pointerId === null || e.pointerId !== pointerId) return;
      if (Math.hypot(e.clientX - originX, e.clientY - originY) >= SWIPE_PX) open();
    });
    art.addEventListener('pointerup', end);
    art.addEventListener('pointercancel', end);
    // Un clic au clavier (detail 0) équivaut au bouton ; un simple toucher ne suffit pas.
    art.addEventListener('click', (e) => {
      if (e.detail === 0) open();
    });
    art.addEventListener('contextmenu', (e) => e.preventDefault());
    btn.addEventListener('click', open);

    return {
      destroy() {
        for (const id of timers) clearTimeout(id);
        timers.clear();
        opened = true;
      },
    };
  },
};

registerStyle('gift', gift);
