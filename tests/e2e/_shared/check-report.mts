// Vérifie le rapport JSON de Playwright : aucun test sauté, aucun échec, au moins
// un test joué. Appelé par scripts/e2e.sh et scripts/e2e-local.sh après la
// recette (node _shared/check-report.mts <rapport.json>).
import fs from 'node:fs';

const file = process.argv[2];
if (!file || !fs.existsSync(file)) {
  console.error(`GARDE DU BANC : rapport JSON introuvable (${file ?? 'aucun chemin'})`);
  process.exit(1);
}
const { stats } = JSON.parse(fs.readFileSync(file, 'utf8')) as {
  stats: { expected: number; unexpected: number; flaky: number; skipped: number };
};
console.log(
  `rapport : ${stats.expected} réussis, ${stats.unexpected} échecs, ${stats.flaky} instables, ${stats.skipped} sautés`,
);
const problems: string[] = [];
if (stats.skipped > 0) problems.push(`${stats.skipped} test(s) sauté(s)`);
if (stats.unexpected > 0) problems.push(`${stats.unexpected} échec(s)`);
if (stats.flaky > 0) problems.push(`${stats.flaky} test(s) instable(s)`);
if (stats.expected === 0) problems.push('aucun test joué');
if (problems.length > 0) {
  console.error(`GARDE DU BANC : ${problems.join(', ')}`);
  process.exit(1);
}
