// Point d'entrée de la page de révélation. Les modules de style s'enregistrent
// dans le registre ; le démarrage attend la fin du chargement pour qu'ils
// soient tous prêts.
import { createReveal } from './reveal.js';
import { httpSource } from './http-source.js';
import { styles } from './registry.js';

const root = document.querySelector('[data-reveal]');
if (root) {
  if (document.readyState !== 'complete') {
    await new Promise((resolve) => window.addEventListener('load', resolve, { once: true }));
  }
  createReveal({ root, source: httpSource(root), styles, seed: root.dataset.seed }).start();
}
