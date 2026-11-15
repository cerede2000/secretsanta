// Helper de lecture des courriels capturés par Mailpit (serveur SMTP factice du
// banc de recette). Lecture seule côté application : on interroge uniquement
// l'API HTTP de Mailpit, jamais la base de l'application.
// L'adresse de l'API vient de E2E_MAIL_API (défaut http://127.0.0.1:18025).

export const MAIL_API = process.env.E2E_MAIL_API ?? 'http://127.0.0.1:18025';

export interface MailSummary {
  ID: string;
  Subject: string;
  To: { Address: string; Name: string }[];
}

export interface Mail extends MailSummary {
  Text: string;
  HTML: string;
}

async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${MAIL_API}${path}`, init);
  if (!res.ok) throw new Error(`Mailpit ${path} : HTTP ${res.status}`);
  return (await res.json()) as T;
}

/** Messages capturés, du plus récent au plus ancien. */
export async function listMails(): Promise<MailSummary[]> {
  return (await api<{ messages: MailSummary[] }>('/api/v1/messages?limit=200')).messages ?? [];
}

/** Message complet (texte et HTML). */
export function getMail(id: string): Promise<Mail> {
  return api<Mail>(`/api/v1/message/${encodeURIComponent(id)}`);
}

/** Message brut (.eml), par exemple pour scripts/spamcheck.sh. */
export async function getRawMail(id: string): Promise<string> {
  const res = await fetch(`${MAIL_API}/api/v1/message/${encodeURIComponent(id)}/raw`);
  if (!res.ok) throw new Error(`Mailpit raw ${id} : HTTP ${res.status}`);
  return res.text();
}

/** Vide la boîte du serveur factice (n'écrit jamais dans la base de l'application). */
export async function clearMails(): Promise<void> {
  const res = await fetch(`${MAIL_API}/api/v1/messages`, { method: 'DELETE' });
  if (!res.ok) throw new Error(`Mailpit effacement : HTTP ${res.status}`);
}

/** Attend (sondage) le premier message destiné à `to`, ou lève une erreur au délai. */
export async function waitForMail(to: string, timeoutMs = 15000): Promise<Mail> {
  const deadline = Date.now() + timeoutMs;
  const wanted = to.toLowerCase();
  for (;;) {
    const hit = (await listMails()).find((m) => m.To.some((t) => t.Address.toLowerCase() === wanted));
    if (hit) return getMail(hit.ID);
    if (Date.now() > deadline) throw new Error(`aucun courriel pour ${to} après ${timeoutMs} ms`);
    await new Promise((r) => setTimeout(r, 300));
  }
}

/** Premier code de vérification (suite de chiffres isolée, 4 à 8 chiffres) du texte. */
export function extractCode(mail: Pick<Mail, 'Text'>): string | null {
  return /(?<![\d])(\d{4,8})(?![\d])/.exec(mail.Text)?.[1] ?? null;
}

/** Liens http(s) du texte du message (liens personnels, désinscription...). */
export function extractLinks(mail: Pick<Mail, 'Text'>): string[] {
  return [...mail.Text.matchAll(/https?:\/\/[^\s<>")]+/g)].map((m) => m[0]);
}
