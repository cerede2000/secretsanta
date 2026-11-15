// Client HTTP minimal pour l'amorçage : il parle à l'application RÉELLE comme un
// navigateur le ferait (cookies, jeton CSRF, redirections lues à la main), sans
// jamais toucher à sa base. Utilisé par seed.ts uniquement.

export interface Reply {
  status: number;
  location: string | null;
  body: string;
}

export class Client {
  private cookies = new Map<string, string>();

  /**
   * @param base adresse de l'application (sans barre finale)
   * @param ip   adresse annoncée par X-Forwarded-For (le bouclage et les réseaux
   *             privés sont de confiance par défaut) : chaque client simule ainsi
   *             une personne distincte pour les limiteurs par adresse IP.
   */
  constructor(
    readonly base: string,
    private readonly ip?: string,
  ) {}

  private headers(extra: Record<string, string> = {}): Record<string, string> {
    const h: Record<string, string> = {
      Origin: this.base,
      'Sec-Fetch-Site': 'same-origin',
      'Accept-Language': 'fr-FR',
      ...extra,
    };
    if (this.ip) h['X-Forwarded-For'] = this.ip;
    if (this.cookies.size) h.Cookie = [...this.cookies].map(([k, v]) => `${k}=${v}`).join('; ');
    return h;
  }

  private async send(path: string, init: RequestInit): Promise<Reply> {
    const res = await fetch(this.base + path, { ...init, redirect: 'manual' });
    for (const line of res.headers.getSetCookie()) {
      const [pair] = line.split(';');
      const eq = pair.indexOf('=');
      const name = pair.slice(0, eq).trim();
      const value = pair.slice(eq + 1).trim();
      if (value === '' || /max-age=0/i.test(line)) this.cookies.delete(name);
      else this.cookies.set(name, value);
    }
    return { status: res.status, location: res.headers.get('location'), body: await res.text() };
  }

  get(path: string): Promise<Reply> {
    return this.send(path, { headers: this.headers() });
  }

  /** POST d'un formulaire ; une valeur tableau répète le champ (cases à cocher). */
  postForm(path: string, fields: Record<string, string | string[]>): Promise<Reply> {
    const body = new URLSearchParams();
    for (const [k, v] of Object.entries(fields)) for (const x of Array.isArray(v) ? v : [v]) body.append(k, x);
    return this.send(path, {
      method: 'POST',
      headers: this.headers({ 'Content-Type': 'application/x-www-form-urlencoded' }),
      body,
    });
  }

  postJSON(path: string, json: unknown, csrf: string): Promise<Reply> {
    return this.send(path, {
      method: 'POST',
      headers: this.headers({ 'Content-Type': 'application/json', 'X-CSRF-Token': csrf }),
      body: JSON.stringify(json),
    });
  }

  /** Lit la page, en extrait le jeton CSRF et poste le formulaire. */
  async submit(
    formPage: string,
    action: string,
    fields: Record<string, string | string[]>,
  ): Promise<Reply> {
    const page = await this.get(formPage);
    if (page.status !== 200) throw new Error(`GET ${formPage} : HTTP ${page.status}`);
    return this.postForm(action, { csrf_token: csrfToken(page.body), ...fields });
  }
}

const unescapeHTML = (s: string): string =>
  s
    .replace(/&#34;/g, '"')
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&amp;/g, '&');

export function csrfToken(html: string): string {
  const m = /name="csrf_token" value="([^"]+)"/.exec(html);
  if (!m) throw new Error('jeton CSRF introuvable dans la page');
  return unescapeHTML(m[1]);
}

export { unescapeHTML };

/** Échoue avec le contexte quand la réponse n'a pas le statut attendu. */
export function expectStatus(what: string, r: Reply, ...ok: number[]): Reply {
  if (!ok.includes(r.status)) {
    throw new Error(`${what} : HTTP ${r.status}${r.location ? ` → ${r.location}` : ''}\n${r.body.slice(0, 300)}`);
  }
  return r;
}
