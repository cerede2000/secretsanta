// Puits SMTP maison pour le lancement local sans Docker (scripts/e2e-local.sh).
//
//   node tests/e2e/mail-sink/smtp-sink.mts <port SMTP> <port HTTP>
//
// Il capture les messages reçus et en expose la lecture par un sous-ensemble de
// l'API HTTP de Mailpit (celle qu'utilise mail-sink/index.ts) : le code de
// l'amorçage et des specs est donc identique avec le banc officiel (Mailpit,
// docker-compose.test.yml) et en local. Uniquement la syntaxe effaçable de
// TypeScript (Node la lit sans compilation).
import http from 'node:http';
import net from 'node:net';

interface Stored {
  ID: string;
  Subject: string;
  To: { Address: string; Name: string }[];
  Text: string;
  HTML: string;
  raw: string;
}

const messages: Stored[] = [];
let counter = 0;

function qpDecode(s: string): string {
  const bytes: number[] = [];
  const joined = s.replace(/=\r?\n/g, '');
  for (let i = 0; i < joined.length; i++) {
    const c = joined[i];
    if (c === '=' && /^[0-9A-Fa-f]{2}$/.test(joined.slice(i + 1, i + 3))) {
      bytes.push(parseInt(joined.slice(i + 1, i + 3), 16));
      i += 2;
    } else {
      bytes.push(...Buffer.from(c, 'utf8'));
    }
  }
  return Buffer.from(bytes).toString('utf8');
}

function decodeBody(body: string, encoding: string): string {
  switch (encoding.toLowerCase()) {
    case 'quoted-printable':
      return qpDecode(body);
    case 'base64':
      return Buffer.from(body.replace(/\s+/g, ''), 'base64').toString('utf8');
    default:
      return body;
  }
}

/** Décode les mots encodés RFC 2047 d'un en-tête. */
function decodeWords(v: string): string {
  return v
    .replace(/\?=\s+=\?/g, '?==?')
    .replace(/=\?([^?]+)\?([bBqQ])\?([^?]*)\?=/g, (_m, _cs: string, enc: string, text: string) =>
      enc.toLowerCase() === 'b'
        ? Buffer.from(text, 'base64').toString('utf8')
        : qpDecode(text.replace(/_/g, ' ')),
    );
}

function splitMessage(raw: string): { headers: Map<string, string>; body: string } {
  const at = raw.search(/\r?\n\r?\n/);
  const head = at < 0 ? raw : raw.slice(0, at);
  const body = at < 0 ? '' : raw.slice(at).replace(/^\r?\n\r?\n/, '');
  const headers = new Map<string, string>();
  for (const line of head.replace(/\r?\n[ \t]+/g, ' ').split(/\r?\n/)) {
    const i = line.indexOf(':');
    if (i > 0) headers.set(line.slice(0, i).trim().toLowerCase(), line.slice(i + 1).trim());
  }
  return { headers, body };
}

function collectParts(raw: string, out: { text: string; html: string }): void {
  const { headers, body } = splitMessage(raw);
  const type = headers.get('content-type') ?? 'text/plain';
  const boundary = /boundary="?([^";]+)"?/i.exec(type)?.[1];
  if (/^multipart\//i.test(type) && boundary) {
    for (const part of body.split(`--${boundary}`).slice(1)) {
      if (part.startsWith('--')) break;
      collectParts(part.replace(/^\r?\n/, ''), out);
    }
    return;
  }
  const text = decodeBody(body, headers.get('content-transfer-encoding') ?? '7bit');
  if (/^text\/html/i.test(type)) out.html += text;
  else if (/^text\/plain/i.test(type) && !out.text) out.text = text;
}

function store(raw: string, rcpts: string[]): void {
  const { headers } = splitMessage(raw);
  const parts = { text: '', html: '' };
  collectParts(raw, parts);
  messages.unshift({
    ID: `m${++counter}`,
    Subject: decodeWords(headers.get('subject') ?? ''),
    To: rcpts.map((a) => ({ Address: a, Name: '' })),
    Text: parts.text,
    HTML: parts.html,
    raw,
  });
}

const smtp = net.createServer((sock) => {
  sock.setEncoding('latin1');
  const send = (s: string) => sock.write(`${s}\r\n`);
  let buf = '';
  let inData = false;
  let data = '';
  let rcpts: string[] = [];
  send('220 ss-test-sink ESMTP');
  sock.on('data', (chunk: string) => {
    buf += chunk;
    for (;;) {
      if (inData) {
        const end = buf.indexOf('\r\n.\r\n');
        if (end < 0) {
          if (buf === '.\r\n') {
            buf = '';
            inData = false;
            store(Buffer.from(data, 'latin1').toString('utf8'), rcpts);
            data = '';
            send('250 OK queued');
          }
          return;
        }
        data += buf.slice(0, end);
        buf = buf.slice(end + 5);
        inData = false;
        store(Buffer.from(data.replace(/^\.\./gm, '.'), 'latin1').toString('utf8'), rcpts);
        data = '';
        rcpts = [];
        send('250 OK queued');
        continue;
      }
      const nl = buf.indexOf('\r\n');
      if (nl < 0) return;
      const line = buf.slice(0, nl);
      buf = buf.slice(nl + 2);
      const u = line.toUpperCase();
      if (u.startsWith('EHLO')) sock.write('250-ss-test-sink\r\n250 SIZE 10485760\r\n');
      else if (u.startsWith('RCPT')) {
        rcpts.push(/<([^>]*)>/.exec(line)?.[1] ?? '');
        send('250 OK');
      } else if (u === 'DATA') {
        inData = true;
        data = '';
        send('354 go');
      } else if (u === 'QUIT') {
        send('221 bye');
        sock.end();
        return;
      } else send('250 OK');
    }
  });
  sock.on('error', () => {});
});

const api = http.createServer((req, res) => {
  const url = new URL(req.url ?? '/', 'http://sink');
  const json = (code: number, body: unknown) => {
    res.writeHead(code, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify(body));
  };
  if (url.pathname === '/api/v1/info') return json(200, { sink: true });
  if (url.pathname === '/api/v1/messages') {
    if (req.method === 'DELETE') {
      messages.length = 0;
      return json(200, 'ok');
    }
    const limit = Number(url.searchParams.get('limit') ?? 50);
    return json(200, {
      total: messages.length,
      messages: messages.slice(0, limit).map(({ ID, Subject, To }) => ({ ID, Subject, To })),
    });
  }
  const m = /^\/api\/v1\/message\/([^/]+)(\/raw)?$/.exec(url.pathname);
  if (m) {
    const hit = messages.find((x) => x.ID === decodeURIComponent(m[1]));
    if (!hit) return json(404, { error: 'not found' });
    if (m[2]) {
      res.writeHead(200, { 'Content-Type': 'message/rfc822' });
      return void res.end(hit.raw);
    }
    const { raw: _raw, ...rest } = hit;
    return json(200, rest);
  }
  json(404, { error: 'not found' });
});

const [smtpPort, httpPort] = process.argv.slice(2).map(Number);
if (!smtpPort || !httpPort) {
  console.error('usage : node smtp-sink.mts <port SMTP> <port HTTP>');
  process.exit(2);
}
smtp.listen(smtpPort, '127.0.0.1');
api.listen(httpPort, '127.0.0.1', () => console.log(`puits SMTP :${smtpPort}, API :${httpPort}`));
