import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import net from 'node:net';
import path from 'node:path';
import { expect, test } from '@playwright/test';
import { Client, expectStatus } from '../../_shared/http';
import { expectNoHorizontalScroll } from '../../_shared/mobile';
import { freshIP, seed } from '../../_shared/seed';
import { guardDialogs } from './dialogs';
import { addPerson, createGroup, launchDraw, mailOf, personalPath, targetsVia, visibleText, type TestGroup } from './support';

// Parcours 06 : sauvegarde de la base puis restauration dans un conteneur neuf, selon la
// procédure du README (docs/guide/index.md, « Sauvegarde et restauration ») : arrêter
// l'application, archiver le volume /data avec un conteneur d'aide, vider (volume neuf),
// extraire l'archive dans /data avec le propriétaire 65532, redémarrer avec le même
// SIGNING_SECRET. Le parcours lance SES conteneurs (noms « ssbk-* », nettoyés en fin de
// test) à partir de l'image construite par scripts/e2e.sh (E2E_IMAGE, ss-test-secretsanta:local
// par défaut) ; Docker se choisit par DOCKER_CONTEXT comme partout ailleurs.
// Le serveur SMTP n'est pas utile ici : les personnes sont ajoutées par l'administration.

const IMAGE = process.env.E2E_IMAGE ?? 'ss-test-secretsanta:local';
const HELPER = process.env.E2E_BACKUP_HELPER_IMAGE ?? 'alpine:3.21';

const docker = (...args: string[]): string => execFileSync('docker', args, { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
const tryDocker = (...args: string[]): void => {
  try {
    docker(...args);
  } catch {
    /* nettoyage au mieux */
  }
};

async function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const s = net.createServer();
    s.once('error', reject);
    s.listen(0, '127.0.0.1', () => {
      const { port } = s.address() as net.AddressInfo;
      s.close(() => resolve(port));
    });
  });
}

async function waitHealthy(url: string): Promise<void> {
  const deadline = Date.now() + 60_000;
  for (;;) {
    try {
      if ((await fetch(`${url}/health`)).ok) return;
    } catch {
      /* pas encore prêt */
    }
    if (Date.now() > deadline) throw new Error(`l'application ne répond pas sur ${url}`);
    await new Promise((r) => setTimeout(r, 300));
  }
}

test('parcours 06 : sauvegarde du volume puis restauration dans un conteneur neuf', async ({ page }) => {
  test.setTimeout(240_000);
  const dialogs = guardDialogs(page);
  const s = seed();
  const secrets = {
    INSTANCE_PASSWORD: s.instancePassword,
    SIGNING_SECRET: s.signingSecret || process.env.E2E_SIGNING_SECRET || '',
  };
  expect(secrets.SIGNING_SECRET, 'E2E_SIGNING_SECRET fourni par scripts/e2e.sh').not.toBe('');

  const id = Math.random().toString(36).slice(2, 8);
  const names = { src: `ssbk-${id}-src`, dst: `ssbk-${id}-dst` };
  const vols = { src: `ssbk-${id}-vsrc`, dst: `ssbk-${id}-vdst` };
  fs.mkdirSync(path.join(process.cwd(), 'test-results'), { recursive: true });
  const dir = fs.mkdtempSync(path.join(process.cwd(), 'test-results', 'bk-'));

  const run = async (name: string, volume: string): Promise<string> => {
    const port = await freePort();
    const url = `http://127.0.0.1:${port}`;
    docker(
      'run', '-d', '--name', name, '-p', `127.0.0.1:${port}:8080`, '-v', `${volume}:/data`,
      '-e', `PUBLIC_URL=${url}`, '-e', `INSTANCE_PASSWORD=${secrets.INSTANCE_PASSWORD}`,
      '-e', `SIGNING_SECRET=${secrets.SIGNING_SECRET}`, IMAGE,
    );
    await waitHealthy(url);
    return url;
  };

  try {
    // --- Application d'origine : un groupe, quatre personnes, un tirage.
    const urlA = await run(names.src, vols.src);
    const g: TestGroup = await createGroup('bk', urlA, secrets.INSTANCE_PASSWORD);
    for (const f of ['Alix', 'Bastien', 'Cléo', 'Dorian']) await addPerson(g, f, 'Parcours', mailOf(f, g.slug));
    await launchDraw(g);
    const links = new Map<string, string>();
    for (const m of g.people) links.set(m.name, await personalPath(g, m));
    const before = new Map<string, string[]>();
    for (const [who, link] of links) before.set(who, await targetsVia(urlA, g, link));
    for (const [, t] of before) expect(t.length).toBeGreaterThan(0);
    const listBefore = visibleText(expectStatus('liste', await g.admin.get(`/${g.slug}/admin/participants`), 200).body);
    const drawBefore = visibleText(expectStatus('tirage', await g.admin.get(`/${g.slug}/admin/tirage`), 200).body);
    expect(drawBefore).not.toContain('pas encore été lancé');

    // --- Sauvegarde (procédure du README) : arrêt, archive du volume, redémarrage possible.
    docker('stop', names.src);
    docker('run', '--rm', '-v', `${vols.src}:/data`, '-v', `${dir}:/sauvegarde`, HELPER, 'tar', 'czf', '/sauvegarde/secretsanta-data.tgz', '-C', '/data', '.');
    expect(fs.statSync(path.join(dir, 'secretsanta-data.tgz')).size, 'archive non vide').toBeGreaterThan(1000);
    docker('rm', '-f', names.src);
    docker('volume', 'rm', vols.src);

    // --- Restauration dans un conteneur neuf : volume vide, extraction, propriétaire 65532.
    docker('volume', 'create', vols.dst);
    docker(
      'run', '--rm', '-v', `${vols.dst}:/data`, '-v', `${dir}:/sauvegarde`, HELPER, 'sh', '-c',
      'tar xzf /sauvegarde/secretsanta-data.tgz -C /data && chown -R 65532:65532 /data',
    );
    const urlB = await run(names.dst, vols.dst);

    // --- Les participants et le tirage sont retrouvés à l'identique, avec les mêmes liens.
    const admin = new Client(urlB, freshIP());
    expectStatus('connexion administrateur restaurée', await admin.submit(`/${g.slug}/admin/connexion`, `/${g.slug}/admin/connexion`, { secret: g.adminSecret }), 303);
    const listAfter = visibleText(expectStatus('liste restaurée', await admin.get(`/${g.slug}/admin/participants`), 200).body);
    expect(listAfter).toBe(listBefore);
    const drawAfter = visibleText(expectStatus('tirage restauré', await admin.get(`/${g.slug}/admin/tirage`), 200).body);
    expect(drawAfter).toBe(drawBefore);
    for (const [who, link] of links) {
      expect(await targetsVia(urlB, g, link), `cibles de ${who} après restauration`).toEqual(before.get(who));
    }

    // --- Et l'administration restaurée se lit sur mobile.
    await page.goto(`${urlB}/${g.slug}/admin/connexion`);
    await page.locator('input[name="secret"]').fill(g.adminSecret);
    await page.locator('form[data-access-admin] button[type="submit"]').click();
    await page.waitForURL((u) => !u.pathname.endsWith('/admin/connexion'));
    await page.goto(`${urlB}/${g.slug}/admin/participants`);
    for (const m of g.people) await expect(page.locator('main')).toContainText(m.firstName);
    await expectNoHorizontalScroll(page);
    dialogs.expectNone();
  } finally {
    dialogs.stop();
    tryDocker('rm', '-f', names.src);
    tryDocker('rm', '-f', names.dst);
    tryDocker('volume', 'rm', '-f', vols.src);
    tryDocker('volume', 'rm', '-f', vols.dst);
    fs.rmSync(dir, { recursive: true, force: true });
  }
});
