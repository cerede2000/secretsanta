// Cœur de la révélation : trois phases (à toucher, en cours, révélé), choix du
// style, confirmation unique par cible, séquence de plusieurs cibles, repli en
// fondu avec prefers-reduced-motion.
//
// Les données arrivent par une interface injectée (`source`) :
//   targets()          -> Promise<[{id, name, hint?, budget?, deadline?}]>
//   confirmReveal(target) -> Promise<void>  (une seule fois par cible)
//   lastStyle?()       -> string|null     (dernier style vu)
//   rememberStyle?(s)  -> void
// Les textes viennent du gabarit (attributs data-t-*), jamais du script.
//
// Un style de révélation est enregistré dans `styles[nom] = { mount(stage, ctx) }`
// et renvoie { destroy() }. ctx : { index, total, reduced, labels, start(),
// complete() }. Sans module de style, le cœur propose le bouton équivalent
// (toucher simple, clavier) : c'est aussi le repli en fondu.
import { pickStyle, seededRandom } from './styles.js';
import { createEffects } from './effects.js';

const PHASES = ['ready', 'progress', 'revealed'];

function fill(text, values) {
  return String(text || '').replace(/\{(\w+)\}/g, (m, k) => (k in values ? String(values[k]) : m));
}

function readLabels(root) {
  const out = {};
  for (const [k, v] of Object.entries(root.dataset)) {
    if (/^t[A-Z]/.test(k)) out[k[1].toLowerCase() + k.slice(2)] = v;
  }
  return out;
}

export function createReveal({
  root,
  source,
  styles = {},
  rand,
  seed,
  reduceMotion,
  effects,
  labels,
  win = window,
}) {
  const $ = (role) => root.querySelector(`[data-role="${role}"]`);
  const L = labels || readLabels(root);
  const random = rand || (seed !== undefined && seed !== null && seed !== '' ? seededRandom(seed) : Math.random);
  const reduced = reduceMotion || (() => win.matchMedia('(prefers-reduced-motion: reduce)').matches);
  const fx = effects || createEffects({ layer: $('confetti'), win });

  const stage = $('stage');
  const result = $('result');
  let targets = [];
  let index = 0;
  let last = null;
  let handle = null;
  let seq = 0;
  let confirmed = new Set();
  let pending = false;

  function setText(role, text) {
    const el = $(role);
    if (el) el.textContent = text || '';
  }
  function show(role, on) {
    const el = $(role);
    if (el) el.hidden = !on;
  }

  function setPhase(phase) {
    root.dataset.phase = phase;
    const n = PHASES.indexOf(phase);
    setText('cap', fill(L['phase' + (phase[0].toUpperCase() + phase.slice(1))], {}));
    setText('title', L['title' + (phase[0].toUpperCase() + phase.slice(1))]);
    setText('lead', phase === 'revealed' ? L.leadRevealed : L['lead' + (phase[0].toUpperCase() + phase.slice(1))]);
    root.dataset.step = String(n + 1);
  }

  function updatePill() {
    const pill = $('pill');
    if (!pill) return;
    if (targets.length < 2) {
      pill.hidden = true;
      return;
    }
    pill.hidden = false;
    pill.textContent = fill(L.pill, { n: index + 1, total: targets.length });
  }

  function destroyStyle() {
    if (handle && typeof handle.destroy === 'function') handle.destroy();
    handle = null;
    while (stage.firstChild) stage.removeChild(stage.firstChild);
  }

  // Bouton équivalent : toucher simple et clavier. Sert aussi de repli en fondu.
  function fadeStyle() {
    return {
      mount(el, ctx) {
        const b = win.document.createElement('button');
        b.type = 'button';
        b.className = 'btn btn-gold reveal-open';
        b.textContent = ctx.labels.open;
        b.addEventListener('click', () => {
          ctx.start();
          ctx.complete();
        });
        el.appendChild(b);
        return { destroy() {} };
      },
    };
  }

  async function confirmCurrent() {
    const target = targets[index];
    if (!target || confirmed.has(target.id) || pending) return;
    pending = true;
    const mine = seq;
    show('error', false);
    try {
      await source.confirmReveal(target);
      confirmed.add(target.id);
    } catch {
      pending = false;
      if (mine === seq) show('error', true);
      return;
    }
    pending = false;
    if (mine !== seq) return;
    show('retry', false);
    showActions();
  }

  function showActions() {
    const more = index + 1 < targets.length;
    show('next', more);
    const letter = $('letter');
    if (letter) letter.hidden = more || !letter.getAttribute('href');
    show('actions', true);
  }

  function reveal() {
    const target = targets[index];
    const mine = seq;
    destroyStyle();
    show('stage', false);
    setPhase('revealed');
    setText('name', target.name);
    setText('hint', target.hint);
    show('hintBox', Boolean(target.hint));
    setText('budget', target.budget);
    show('budgetBox', Boolean(target.budget));
    setText('deadline', target.deadline);
    show('deadlineBox', Boolean(target.deadline));
    show('result', true);
    // Le fondu : opacité seule, un tour de rendu après l'affichage.
    win.requestAnimationFrame(() => {
      if (mine === seq) result.classList.add('is-visible');
    });
    setText('status', fill(L.announce, { name: target.name }));
    if (!reduced()) {
      fx.confetti();
      fx.vibrate();
    }
    fx.chime();
    confirmCurrent();
  }

  function begin(i) {
    seq += 1;
    const mine = seq;
    index = i;
    pending = false;
    destroyStyle();
    result.classList.remove('is-visible');
    show('result', false);
    show('actions', false);
    show('error', false);
    show('stage', true);
    const style = pickStyle(last, random);
    last = style;
    if (typeof source.rememberStyle === 'function') {
      try {
        source.rememberStyle(style);
      } catch {
        /* sans conséquence */
      }
    }
    root.dataset.style = style;
    root.dataset.index = String(i);
    root.dataset.total = String(targets.length);
    root.dataset.motion = reduced() ? 'reduced' : 'full';
    setPhase('ready');
    updatePill();
    let started = false;
    let done = false;
    const ctx = {
      index: i,
      total: targets.length,
      reduced: reduced(),
      labels: L,
      effects: fx,
      start() {
        if (mine !== seq || started || done) return;
        started = true;
        setPhase('progress');
      },
      complete() {
        if (mine !== seq || done) return;
        done = true;
        reveal();
      },
    };
    const factory = !ctx.reduced && styles[style] ? styles[style] : fadeStyle();
    handle = factory.mount(stage, ctx) || null;
  }

  async function start() {
    root.dataset.phase = 'loading';
    try {
      targets = await source.targets();
      last = typeof source.lastStyle === 'function' ? source.lastStyle() || null : null;
    } catch {
      root.dataset.phase = 'error';
      setText('title', L.loadError);
      setText('lead', '');
      setText('cap', '');
      return;
    }
    if (!Array.isArray(targets) || targets.length === 0) {
      root.dataset.phase = 'empty';
      setText('title', L.empty);
      setText('lead', '');
      setText('cap', '');
      show('stage', false);
      return;
    }
    begin(0);
  }

  const next = $('next');
  if (next) {
    next.addEventListener('click', () => {
      // La seconde cible ne se lance qu'après la confirmation de la première.
      if (targets[index] && confirmed.has(targets[index].id) && index + 1 < targets.length) begin(index + 1);
    });
  }
  const retry = $('retry');
  if (retry) retry.addEventListener('click', () => confirmCurrent());
  const toggle = $('sound');
  if (toggle) {
    toggle.addEventListener('click', () => {
      const on = toggle.getAttribute('aria-checked') !== 'true';
      toggle.setAttribute('aria-checked', String(on));
      fx.setSound(on);
    });
  }

  return {
    start,
    // Révélation réarmée : nouveau style (différent du dernier vu). Avec de
    // nouvelles cibles, la séquence repart de la première ; sinon la cible
    // courante rejoue son animation, sans nouvelle confirmation.
    rearm(newTargets) {
      if (Array.isArray(newTargets)) {
        targets = newTargets;
        confirmed = new Set();
        begin(0);
      } else if (targets.length) {
        begin(index);
      }
    },
    state: () => ({ index, style: last, phase: root.dataset.phase, confirmed: [...confirmed] }),
  };
}
