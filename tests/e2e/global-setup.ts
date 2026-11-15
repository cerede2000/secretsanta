import fs from 'node:fs';
import { buildSeed } from './_shared/seed';

// Amorçage commun à toutes les specs (voir _shared/seed.ts) : crée par l'application
// réelle les groupes et participants de départ, puis les publie aux processus de
// test dans process.env.E2E_SEED (les processus de test héritent de cet
// environnement). E2E_SEED_SKIP=1 le saute (test de fumée de scripts/e2e.sh --smoke,
// qui n'en a pas besoin) ; une spec qui l'utilise alors échoue franchement.
export default async function globalSetup(): Promise<void> {
  if (process.env.E2E_SEED_SKIP === '1') return;
  const started = Date.now();
  const seed = await buildSeed();
  process.env.E2E_SEED = JSON.stringify(seed);
  // Pour inspecter les données à la main (captures, essais) : E2E_SEED_DUMP=<fichier>.
  if (process.env.E2E_SEED_DUMP) fs.writeFileSync(process.env.E2E_SEED_DUMP, JSON.stringify(seed, null, 2));
  const groups = seed.lanes.flatMap((l) => [l.reg, l.people, l.roles, l.draft, l.waiting, l.revealed]);
  const people = groups.reduce((n, g) => n + g.people.length + g.pending.length, 0);
  console.log(`amorçage : ${groups.length} groupes, ${people} participants en ${((Date.now() - started) / 1000).toFixed(1)} s`);
}
