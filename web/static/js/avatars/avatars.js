// Avatars festifs deterministes : choisis par hash du prenom, calcules en local.
export const AVATAR_COUNT = 8;
export const AVATAR_BASE = '/static/assets/avatars/';

// Normalise le prenom : NFC, minuscules, espaces reduits.
function normalize(name) {
  return String(name ?? '').normalize('NFC').toLowerCase().trim().replace(/\s+/g, ' ');
}

// Hash FNV-1a 32 bits sur les octets UTF-8.
export function hashName(name) {
  const bytes = new TextEncoder().encode(normalize(name));
  let h = 0x811c9dc5;
  for (const b of bytes) {
    h ^= b;
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return h >>> 0;
}

// Numero d'avatar, de 1 a AVATAR_COUNT.
export function avatarIndex(name) {
  return (hashName(name) % AVATAR_COUNT) + 1;
}

export function avatarUrl(name) {
  return `${AVATAR_BASE}avatar-${avatarIndex(name)}.svg`;
}

// Renseigne les <img data-avatar-name="..."> de la page.
export function applyAvatars(root = document) {
  for (const img of root.querySelectorAll('img[data-avatar-name]')) {
    img.src = avatarUrl(img.dataset.avatarName);
  }
}

if (typeof document !== 'undefined') {
  applyAvatars();
}
