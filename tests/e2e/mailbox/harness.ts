import fs from 'node:fs';
import path from 'node:path';
import type { Page } from '@playwright/test';

const WEB = path.resolve(__dirname, '../../../web');
export const PAGE_PATH = '/__mailbox_harness__';
export const CSP = "default-src 'self'; script-src 'self'; style-src 'self'";

export type Entry = { N: number; Total: number; Written: boolean; URL: string };
export type MailboxData = {
  Lang?: string;
  State?: 'empty' | 'draft' | 'sent' | 'read';
  Received?: Entry[];
  TwoTargets?: boolean;
  ReceptionOnly?: boolean;
};

export const TWO_LETTERS: MailboxData = {
  State: 'sent',
  TwoTargets: true,
  Received: [
    { N: 1, Total: 2, Written: true, URL: '/s/lettre-recue/1' },
    { N: 2, Total: 2, Written: false, URL: '/s/lettre-recue/2' },
  ],
};

export function catalog(lang: string): Record<string, string> {
  const read = (dir: string) => JSON.parse(fs.readFileSync(path.join(WEB, 'static/i18n', dir, `${lang}.json`), 'utf8'));
  return { ...read('layout'), ...read('mailbox') };
}

const esc = (s: unknown) =>
  String(s).replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

type Node = { k: 'text'; v: string } | { k: 'act'; v: string } | { k: 'if'; conds: [string, Node[]][]; other: Node[] } | { k: 'range'; v: string; body: Node[] };

function tokenize(src: string): string[] {
  src = src.replace(/\{\{\/\*[\s\S]*?\*\/\}\}/g, '');
  return src.split(/(\{\{[\s\S]*?\}\})/).filter((s) => s !== '');
}

function parse(tokens: string[], defs: Record<string, Node[]>): Node[] {
  let i = 0;
  const block = (stops: string[]): { nodes: Node[]; stop: string } => {
    const nodes: Node[] = [];
    while (i < tokens.length) {
      const tk = tokens[i++];
      if (!tk.startsWith('{{')) {
        nodes.push({ k: 'text', v: tk });
        continue;
      }
      const a = tk.slice(2, -2).trim();
      const head = a.split(/\s+/)[0];
      if (stops.some((s) => a === s || a.startsWith(s + ' '))) return { nodes, stop: a };
      if (head === 'define') {
        const name = /"([^"]+)"/.exec(a)![1];
        defs[name] = block(['end']).nodes;
      } else if (head === 'if') {
        const n: Node = { k: 'if', conds: [], other: [] };
        let cond = a.slice(3).trim();
        for (;;) {
          const r = block(['else', 'end']);
          n.conds.push([cond, r.nodes]);
          if (r.stop === 'end') break;
          if (r.stop.startsWith('else if ')) {
            cond = r.stop.slice(8).trim();
            continue;
          }
          n.other = block(['end']).nodes;
          break;
        }
        nodes.push(n);
      } else if (head === 'range') {
        nodes.push({ k: 'range', v: a.slice(6).trim(), body: block(['end']).nodes });
      } else nodes.push({ k: 'act', v: a });
    }
    return { nodes, stop: '' };
  };
  return block([]).nodes;
}

function words(expr: string): string[] {
  return [...expr.matchAll(/"[^"]*"|\S+/g)].map((m) => m[0]);
}

/** Rend le vrai gabarit (sous-ensemble de html/template) avec le vrai catalogue. */
export function renderMailbox(data: MailboxData): string {
  const lang = data.Lang ?? 'fr';
  const cat = catalog(lang);
  const root: Record<string, unknown> = {
    Lang: lang,
    State: 'empty',
    Received: [],
    GroupName: 'Noël en famille',
    Year: 2026,
    BackURL: '/s/participant',
    OwnURL: '/s/lettre',
    ...data,
  };
  const defs: Record<string, Node[]> = {};
  parse(tokenize(fs.readFileSync(path.join(WEB, 'templates/mailbox/page.html'), 'utf8')), defs);

  const val = (w: string, dot: any): any => {
    if (w.startsWith('"')) return w.slice(1, -1);
    if (/^-?\d+$/.test(w)) return Number(w);
    if (w === '.') return dot;
    const base = w.startsWith('$.') ? root : dot;
    return w.replace(/^\$?\./, '').split('.').reduce((o: any, k) => (o == null ? o : o[k]), base);
  };
  const evalExpr = (expr: string, dot: any): any => {
    const ws = words(expr);
    if (ws[0] === 'eq') return val(ws[1], dot) === val(ws[2], dot);
    if (ws[0] === 'gt') return val(ws[1], dot) > val(ws[2], dot);
    return val(ws[0], dot);
  };
  const run = (nodes: Node[], dot: any): string =>
    nodes
      .map((n): string => {
        if (n.k === 'text') return n.v;
        if (n.k === 'if') {
          for (const [c, body] of n.conds) if (evalExpr(c, dot)) return run(body, dot);
          return run(n.other, dot);
        }
        if (n.k === 'range') return ((evalExpr(n.v, dot) as any[]) ?? []).map((item) => run(n.body, item)).join('');
        const ws = words(n.v);
        if (ws[0] === 't') {
          let text = cat[val(ws[1], dot)] ?? (() => { throw new Error(`clé absente du catalogue ${lang} : ${ws[1]}`); })();
          for (let j = 2; j + 1 < ws.length; j += 2) text = text.split(`{${val(ws[j], dot)}}`).join(String(val(ws[j + 1], dot)));
          return esc(text);
        }
        if (ws[0] === 'template') {
          const name = val(ws[1], dot);
          if (name === 'layout/head') return `<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><link rel="stylesheet" href="/static/css/base/base.css"><link rel="stylesheet" href="/static/css/theme/theme.css"></head>`;
          if (name === 'layout/footer') return '';
          if (!defs[name]) throw new Error('gabarit inconnu : ' + name);
          return run(defs[name], ws[2] ? val(ws[2], dot) : undefined);
        }
        return esc(evalExpr(n.v, dot) ?? '');
      })
      .join('');
  const out = run(defs['mailbox/page'], root);
  if (/\{\{/.test(out)) throw new Error('gabarit : action non rendue : ' + out.match(/\{\{[^}]*\}\}/)![0]);
  return out;
}

export async function openMailbox(page: Page, data: MailboxData = {}): Promise<void> {
  const html = renderMailbox(data);
  await page.route(`**${PAGE_PATH}`, (route) =>
    route.fulfill({ status: 200, contentType: 'text/html; charset=utf-8', headers: { 'Content-Security-Policy': CSP }, body: html }),
  );
  await page.goto(PAGE_PATH);
  await page.waitForLoadState('networkidle');
}

export const root = (page: Page) => page.locator('[data-mailbox]');
