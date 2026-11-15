// Style carte à gratter : glisser le doigt (ou la souris) efface la couche
// argentée ; la cible se révèle dès que 50 % de la surface est grattée. Le
// bouton équivalent (classe reveal-open) révèle tout d'un toucher ou au
// clavier. La confirmation n'est jamais envoyée d'ici : le cœur la déclenche,
// une seule fois, quand le style appelle ctx.complete(). Les textes viennent
// du gabarit (attributs data-t-*).
import { registerStyle } from '../core/registry.js';

export const THRESHOLD = 0.5;
const COLS = 24;
const ROWS = 12;
const BRUSH = 22;
const FINISH_MS = 380;

function readLabels(doc) {
  const out = {};
  const el = doc.querySelector('[data-reveal-scratch]');
  if (el) {
    for (const [k, v] of Object.entries(el.dataset)) {
      if (/^t[A-Z]/.test(k)) out[k[1].toLowerCase() + k.slice(2)] = v;
    }
  }
  return out;
}

export const scratch = {
  mount(stage, ctx) {
    const doc = stage.ownerDocument;
    const own = readLabels(doc);
    const timers = new Set();

    const wrap = doc.createElement('div');
    wrap.className = 'scratch';
    wrap.dataset.scratch = '0';

    const card = doc.createElement('div');
    card.className = 'scratch-card';
    const ribbon = doc.createElement('div');
    ribbon.className = 'scratch-ribbon';
    const title = doc.createElement('div');
    title.className = 'scratch-title';
    title.textContent = own.card || '';
    const zone = doc.createElement('div');
    zone.className = 'scratch-zone';
    const canvas = doc.createElement('canvas');
    canvas.className = 'scratch-canvas';
    canvas.setAttribute('role', 'img');
    canvas.setAttribute('aria-label', own.label || ctx.labels.open || '');
    const prompt = doc.createElement('div');
    prompt.className = 'scratch-prompt';
    prompt.setAttribute('aria-hidden', 'true');
    prompt.textContent = own.scratch || '';
    zone.append(canvas, prompt);
    card.append(ribbon, title, zone);

    const btn = doc.createElement('button');
    btn.type = 'button';
    btn.className = 'btn btn-gold reveal-open';
    btn.textContent = own.all || ctx.labels.open || '';
    const note = doc.createElement('p');
    note.className = 'scratch-note';
    note.textContent = own.note || '';
    wrap.append(card, btn, note);
    stage.appendChild(wrap);

    // Taille en pixels CSS ; le tracé est fait à cette échelle.
    const rect = canvas.getBoundingClientRect();
    const W = Math.max(1, Math.round(rect.width) || 300);
    const H = Math.max(1, Math.round(rect.height) || 150);
    canvas.width = W;
    canvas.height = H;
    const g = canvas.getContext('2d');
    if (g) {
      const grad = g.createLinearGradient(0, 0, W, H);
      grad.addColorStop(0, '#d9dde6');
      grad.addColorStop(0.4, '#f4f6fa');
      grad.addColorStop(0.7, '#bcc2d0');
      grad.addColorStop(1, '#e6e9f0');
      g.fillStyle = grad;
      g.fillRect(0, 0, W, H);
      g.lineCap = 'round';
      g.lineJoin = 'round';
      g.lineWidth = BRUSH * 2;
    }

    // Couverture : grille de cellules dont le centre est passé sous le pinceau.
    const cells = new Uint8Array(COLS * ROWS);
    let covered = 0;
    function mark(x, y) {
      const c0 = Math.max(0, Math.floor(((x - BRUSH) / W) * COLS));
      const c1 = Math.min(COLS - 1, Math.floor(((x + BRUSH) / W) * COLS));
      const r0 = Math.max(0, Math.floor(((y - BRUSH) / H) * ROWS));
      const r1 = Math.min(ROWS - 1, Math.floor(((y + BRUSH) / H) * ROWS));
      for (let r = r0; r <= r1; r++) {
        for (let c = c0; c <= c1; c++) {
          const i = r * COLS + c;
          if (cells[i]) continue;
          const cx = ((c + 0.5) / COLS) * W;
          const cy = ((r + 0.5) / ROWS) * H;
          if (Math.hypot(cx - x, cy - y) <= BRUSH) {
            cells[i] = 1;
            covered += 1;
          }
        }
      }
    }
    const ratio = () => covered / (COLS * ROWS);

    let finished = false;
    let started = false;
    function finish() {
      if (finished) return;
      finished = true;
      pointerId = null;
      btn.disabled = true;
      wrap.classList.add('is-done');
      wrap.dataset.scratch = '100';
      ctx.start();
      ctx.effects?.vibrate?.([40, 30, 40]);
      const id = setTimeout(() => {
        timers.delete(id);
        ctx.complete();
      }, FINISH_MS);
      timers.add(id);
    }

    let pointerId = null;
    let lx = 0;
    let ly = 0;
    function local(e) {
      const r = canvas.getBoundingClientRect();
      const sx = r.width ? W / r.width : 1;
      const sy = r.height ? H / r.height : 1;
      return [(e.clientX - r.left) * sx, (e.clientY - r.top) * sy];
    }
    function stroke(x0, y0, x1, y1) {
      if (g) {
        g.globalCompositeOperation = 'destination-out';
        g.beginPath();
        g.moveTo(x0, y0);
        g.lineTo(x1, y1);
        g.stroke();
      }
      const steps = Math.max(1, Math.ceil(Math.hypot(x1 - x0, y1 - y0) / (BRUSH / 2)));
      for (let s = 0; s <= steps; s++) mark(x0 + ((x1 - x0) * s) / steps, y0 + ((y1 - y0) * s) / steps);
      wrap.dataset.scratch = String(Math.round(ratio() * 100));
      if (ratio() >= THRESHOLD) finish();
    }

    canvas.addEventListener('pointerdown', (e) => {
      if (finished || pointerId !== null) return;
      if (e.pointerType === 'mouse' && e.button !== 0) return;
      pointerId = e.pointerId;
      try {
        canvas.setPointerCapture(e.pointerId);
      } catch {
        /* sans capture : les événements restent suivis sur l'élément */
      }
      [lx, ly] = local(e);
      if (!started) {
        started = true;
        wrap.classList.add('is-scratching');
        ctx.start();
      }
      stroke(lx, ly, lx, ly);
    });
    canvas.addEventListener('pointermove', (e) => {
      if (finished || pointerId === null || e.pointerId !== pointerId) return;
      const [x, y] = local(e);
      stroke(lx, ly, x, y);
      lx = x;
      ly = y;
    });
    const end = (e) => {
      if (pointerId !== null && e.pointerId === pointerId) pointerId = null;
    };
    canvas.addEventListener('pointerup', end);
    canvas.addEventListener('pointercancel', end);
    canvas.addEventListener('contextmenu', (e) => e.preventDefault());
    btn.addEventListener('click', finish);

    return {
      destroy() {
        for (const id of timers) clearTimeout(id);
        timers.clear();
        finished = true;
      },
    };
  },
};

registerStyle('scratch', scratch);
