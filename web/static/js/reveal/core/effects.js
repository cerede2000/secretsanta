// Effets partagés : confettis (éléments animés par transform et opacity),
// vibration, son optionnel (désactivé par défaut). Aucun texte ici.
const PIECES = 28;
const LIFETIME_MS = 4200;

export function createEffects({ layer, win = window, rand = Math.random } = {}) {
  let soundOn = false;
  let audio = null;

  function confetti() {
    if (!layer) return;
    for (let i = 0; i < PIECES; i++) {
      const el = win.document.createElement('span');
      el.className = 'reveal-confetti-piece reveal-confetti-c' + (i % 4);
      el.style.left = Math.round(rand() * 100) + '%';
      el.style.setProperty('--dx', Math.round((rand() - 0.5) * 120) + 'px');
      el.style.setProperty('--dy', Math.round(380 + rand() * 360) + 'px');
      el.style.setProperty('--rot', Math.round((rand() - 0.5) * 900) + 'deg');
      el.style.setProperty('--delay', Math.round(rand() * 400) + 'ms');
      el.style.setProperty('--dur', Math.round(2200 + rand() * 1400) + 'ms');
      layer.appendChild(el);
    }
    win.setTimeout(() => {
      while (layer.firstChild) layer.removeChild(layer.firstChild);
    }, LIFETIME_MS);
  }

  function vibrate(pattern = [60, 40, 90]) {
    try {
      if (typeof win.navigator.vibrate === 'function') win.navigator.vibrate(pattern);
    } catch {
      /* non disponible */
    }
  }

  function chime() {
    if (!soundOn) return;
    try {
      const Ctx = win.AudioContext || win.webkitAudioContext;
      if (!Ctx) return;
      audio = audio || new Ctx();
      const now = audio.currentTime;
      [659.25, 830.61, 987.77].forEach((freq, i) => {
        const osc = audio.createOscillator();
        const gain = audio.createGain();
        osc.type = 'sine';
        osc.frequency.value = freq;
        gain.gain.setValueAtTime(0.0001, now + i * 0.12);
        gain.gain.exponentialRampToValueAtTime(0.15, now + i * 0.12 + 0.03);
        gain.gain.exponentialRampToValueAtTime(0.0001, now + i * 0.12 + 0.5);
        osc.connect(gain);
        gain.connect(audio.destination);
        osc.start(now + i * 0.12);
        osc.stop(now + i * 0.12 + 0.55);
      });
    } catch {
      /* son indisponible : sans conséquence */
    }
  }

  return {
    confetti,
    vibrate,
    chime,
    soundEnabled: () => soundOn,
    // Seule une activation explicite arme le son.
    setSound(on) {
      soundOn = on === true;
    },
  };
}
