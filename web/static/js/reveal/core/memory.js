// Mémoire du dernier style vu, côté navigateur. Ne bloque jamais si le
// stockage est indisponible (mode privé, quota, politique du navigateur).
const KEY = 'reveal.lastStyle';

export function createStyleMemory(win = window) {
  let fallback = null;
  return {
    get() {
      try {
        const v = win.localStorage.getItem(KEY);
        return v || fallback;
      } catch {
        return fallback;
      }
    },
    set(style) {
      fallback = style;
      try {
        win.localStorage.setItem(KEY, style);
      } catch {
        /* stockage indisponible : on garde la valeur en mémoire */
      }
    },
  };
}
