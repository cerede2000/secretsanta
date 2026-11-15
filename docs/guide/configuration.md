# Configuration

Toute la configuration passe par des variables d'environnement. Le modèle complet est `.env.example` ; les lignes commentées sont facultatives.

## Variables d'environnement

| Variable | Rôle et valeur par défaut |
|---|---|
| `PUBLIC_URL` | Adresse publique (liens des courriels). Défaut `http://localhost:8080`. |
| `LISTEN_ADDR` | Adresse d'écoute HTTP. Défaut `:8080`. |
| `DATA_DIR` | Dossier des données, à monter en volume. Défaut `/data`. |
| `APP_TIMEZONE` | Fuseau horaire IANA (relances, dates). Défaut `Europe/Paris`. |
| `APP_DEFAULT_LANG` | Langue par défaut de l'interface et des courriels. Défaut `fr`. |
| `I18N_DIR` | Dossier facultatif de traductions, voir [traduire](traduire.md). |
| `INSTANCE_PASSWORD` | Obligatoire. Mot de passe d'instance pour créer un groupe. |
| `SIGNING_SECRET` | Obligatoire. Secret de signature (liens, désinscription). |
| `SMTP_HOST` | Serveur SMTP ; sans lui, l'envoi est désactivé. |
| `SMTP_PORT` | Port SMTP (par exemple 587). |
| `SMTP_USER` | Identifiant SMTP. |
| `SMTP_PASSWORD` | Mot de passe SMTP. |
| `MAIL_FROM` | Adresse d'expédition, obligatoire dès que `SMTP_HOST` est défini. |
| `MAIL_FROM_NAME` | Nom d'expéditeur affiché. |
| `MAIL_EHLO_HOSTNAME` | Nom d'hôte annoncé à EHLO. |
| `MAIL_ENVELOPE_FROM` | Expéditeur d'enveloppe (défaut `MAIL_FROM`). |
| `MAIL_REPLY_TO` | Adresse de réponse. |
| `MAIL_TLS_MODE` | `starttls` (défaut), `tls` ou `none`. |
| `MAIL_ALLOW_INSECURE` | `true` requis pour accepter `none`. Défaut `false`. |
| `MAIL_MAX_PER_MINUTE` | Débit maximal d'envois par minute. Défaut 30. |
| `MAIL_DKIM_DOMAIN` | Domaine de la signature DKIM (tout ou rien). |
| `MAIL_DKIM_SELECTOR` | Sélecteur DKIM. |
| `MAIL_DKIM_KEY_FILE` | Chemin du fichier de clé privée DKIM. |
| `MAIL_DKIM_ALGORITHM` | `rsa-sha256` ou `ed25519-sha256`. |
| `MAIL_DKIM_HEADERS` | En-têtes signés, séparés par des virgules. |
| `REMINDERS_ENABLED` | Interrupteur des relances automatiques. Défaut `true`. |
| `TRUSTED_PROXIES` | Adresses ou CIDR dont l'en-tête est cru ; `none` pour aucun. |
| `CLIENT_IP_HEADER` | `X-Forwarded-For` (défaut), `X-Real-IP` ou `CF-Connecting-IP`. |
| `SEARXNG_URL` | Adresse d'un SearXNG auto-hébergé (prioritaire). |
| `BRAVE_API_KEY` | Clé d'API Brave Search. |
| `SERPER_API_KEY` | Clé d'API Serper. |

## Configuration SMTP

Renseignez `SMTP_HOST`, `SMTP_PORT`, `MAIL_FROM` et, si le relais l'exige, `SMTP_USER` et `SMTP_PASSWORD`. Le chiffrement se règle avec `MAIL_TLS_MODE` :

- `starttls` (défaut, port 587) : connexion en clair puis chiffrement ;
- `tls` (port 465) : TLS implicite ;
- `none` : sans chiffrement, par exemple un relais interne de confiance ; il faut en plus `MAIL_ALLOW_INSECURE=true`.

Exemple de relais interne sans TLS : `SMTP_HOST=relais.interne`, `SMTP_PORT=25`, `MAIL_TLS_MODE=none`, `MAIL_ALLOW_INSECURE=true`. Pour les enregistrements DNS et la réputation, voir [délivrabilité](deliverability.md).

## Sources de suggestions

Les idées de cadeaux peuvent être suggérées à partir de plusieurs sources, essayées dans cet ordre :

1. SearXNG auto-hébergé, via `SEARXNG_URL` ;
2. les services à clé d'API `BRAVE_API_KEY` et `SERPER_API_KEY` ;
3. le catalogue de secours embarqué dans l'application, utilisé quand aucune source n'est configurée ou n'est joignable.

Les clés d'API sont des secrets : gardez-les dans `.env`.
