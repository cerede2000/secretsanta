import { test } from '@playwright/test';

// Chemin du lien d'inscription (/<slug>/rejoindre/<jeton>) d'un groupe dont
// l'inscription par lien est activee. Fourni par la mise en place du serveur
// teste : le lien n'est renvoye qu'une fois, a l'activation.
export const INVITE_PATH = process.env.E2E_INVITE_PATH ?? '';

export function requireInvite(): void {
  test.skip(INVITE_PATH === '', 'E2E_INVITE_PATH (lien d\'inscription actif) non fourni');
}
