// Sélecteur du style de révélation. Trois styles : cadeau enveloppé, enveloppe
// à sceau de cire, carte à gratter. Jamais deux fois le même d'affilée.
export const STYLES = Object.freeze(['gift', 'envelope', 'scratch']);

/**
 * Tire un style parmi les trois, différent de `last` (le dernier style vu).
 * `rand` renvoie un nombre dans [0, 1) ; une graine de test peut le forcer.
 */
export function pickStyle(last, rand = Math.random) {
  const pool = STYLES.filter((s) => s !== last);
  let r = Number(rand());
  if (!(r >= 0 && r < 1)) r = 0;
  return pool[Math.min(pool.length - 1, Math.floor(r * pool.length))];
}

/** Générateur pseudo-aléatoire à graine (mulberry32) pour forcer un tirage. */
export function seededRandom(seed) {
  let a = Number(seed) >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}
