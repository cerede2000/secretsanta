// Style enveloppe à sceau de cire (screen-revelation-b) : toucher le sceau le
// brise et la lettre sort. Le bouton équivalent (classe reveal-open) déclenche
// le même geste au clavier et au toucher simple. La confirmation de révélation
// n'est jamais envoyée d'ici : le cœur la déclenche, une seule fois, quand le
// style appelle ctx.complete(). Les textes viennent du gabarit (data-t-*).
import { registerStyle } from '../core/registry.js';

const NS = 'http://www.w3.org/2000/svg';
const BREAK_MS = 1000;

function svg(tag, attrs = {}, parent) {
  const el = document.createElementNS(NS, tag);
  for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v);
  if (parent) parent.appendChild(el);
  return el;
}

function readLabels() {
  const out = {};
  const holder = document.querySelector('[data-envelope-labels]');
  if (holder) {
    for (const [k, v] of Object.entries(holder.dataset)) {
      if (/^t[A-Z]/.test(k)) out[k[1].toLowerCase() + k.slice(2)] = v;
    }
  }
  return out;
}

function buildEnvelope() {
  const root = svg('svg', { class: 'envelope-svg', 'aria-hidden': 'true', focusable: 'false', width: '290', height: '300', viewBox: '0 0 290 300' });
  svg('ellipse', { cx: '145', cy: '288', rx: '120', ry: '9', fill: '#e6ecff', opacity: '.8' }, root);
  svg('rect', { x: '20', y: '110', width: '250', height: '168', rx: '10', fill: '#eaf4ee' }, root);
  const letter = svg('g', { class: 'envelope-letter' }, root);
  svg('rect', { x: '44', y: '70', width: '202', height: '150', rx: '6', fill: '#fff', stroke: '#c5ddd0', 'stroke-width': '2' }, letter);
  svg('path', { d: 'M62 102h110M62 122h140M62 142h96', stroke: '#c5ddd0', 'stroke-width': '5', 'stroke-linecap': 'round' }, letter);
  const pocket = svg('g', {}, root);
  svg('path', { d: 'M20 278l100-80M270 278L170 198', stroke: '#c5ddd0', 'stroke-width': '2' }, pocket);
  svg('path', { d: 'M20 150l125 86 125-86V278H20z', fill: '#eaf4ee', stroke: '#c5ddd0', 'stroke-width': '2', 'stroke-linejoin': 'round' }, pocket);
  const flap = svg('g', { class: 'envelope-flap' }, root);
  svg('path', { d: 'M20 120l125 86 125-86', fill: '#dcebe2', stroke: '#c5ddd0', 'stroke-width': '2', 'stroke-linejoin': 'round' }, flap);
  const seal = svg('g', { class: 'envelope-seal' }, root);
  const half = { fill: '#c41e3a', stroke: '#8f1329', 'stroke-width': '3' };
  svg('path', { class: 'envelope-seal-l', d: 'M145 178c-18 0-30 12-30 28s12 28 30 28z', ...half }, seal);
  svg('path', { class: 'envelope-seal-r', d: 'M145 178c18 0 30 12 30 28s-12 28-30 28z', ...half }, seal);
  svg('path', { class: 'envelope-star', d: 'M145 194l4 8 9 1-6.500 6 1.500 9-8-4.500-8 4.500 1.500-9-6.500-6 9-1z', fill: '#ffd700' }, seal);
  return root;
}

registerStyle('envelope', {
  mount(stage, ctx) {
    const labels = { ...readLabels(), ...(ctx.labels || {}) };
    const wrap = document.createElement('div');
    wrap.className = 'envelope';
    wrap.dataset.envelope = 'closed';

    const seal = document.createElement('button');
    seal.type = 'button';
    seal.className = 'envelope-seal-btn';
    seal.setAttribute('aria-label', labels.seal || '');
    seal.dataset.role = 'seal';
    seal.appendChild(buildEnvelope());

    const hint = document.createElement('div');
    hint.className = 'envelope-hint';
    hint.textContent = labels.envelopeHint || '';

    // Bouton équivalent : même geste, clavier et toucher simple.
    const equivalent = document.createElement('button');
    equivalent.type = 'button';
    equivalent.className = 'btn btn-gold reveal-open';
    equivalent.textContent = labels.open || '';

    wrap.append(seal, hint, equivalent);
    stage.appendChild(wrap);

    let timer = null;
    let broken = false;
    function breakSeal() {
      if (broken) return;
      broken = true;
      seal.disabled = true;
      equivalent.disabled = true;
      ctx.start();
      wrap.dataset.envelope = 'breaking';
      wrap.classList.add('is-breaking');
      ctx.effects?.vibrate?.([40, 30, 40]);
      timer = setTimeout(() => {
        timer = null;
        wrap.dataset.envelope = 'open';
        ctx.complete();
      }, BREAK_MS);
    }
    seal.addEventListener('click', breakSeal);
    equivalent.addEventListener('click', breakSeal);

    return {
      destroy() {
        if (timer) clearTimeout(timer);
        timer = null;
      },
    };
  },
});
