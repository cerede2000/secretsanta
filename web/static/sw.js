// Service worker minimal : requis pour l'installation, sans aucune mise en
// cache (ni données personnelles ni réponses dynamiques). Pas de gestionnaire
// fetch : le navigateur traite toutes les requêtes normalement.
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (event) => {
  event.waitUntil(
    (async () => {
      // Purge d'éventuels caches hérités : cette application n'en crée aucun.
      for (const name of await caches.keys()) await caches.delete(name);
      await self.clients.claim();
    })(),
  );
});
