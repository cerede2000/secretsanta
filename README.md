# secretsanta

Application web de **Père Noël secret** pour une famille ou un groupe d'amis : création de groupes, inscription par lien, tirage au sort, lettres au Père Noël, courriels de relance. Elle est pensée d'abord pour le mobile, disponible en français et en anglais, et tourne dans **un seul conteneur Docker** (Go, base SQLite embarquée, aucune dépendance externe).

## Présentation

- **Plusieurs groupes isolés** sur une même instance, chacun avec son adresse `/<nom-du-groupe>` ; la création est protégée par un mot de passe d'instance.
- **Inscription par lien ou QR code** : l'invité donne son nom et une adresse de courriel, confirme avec un code reçu par courriel ; l'organisateur peut valider à la main.
- **Tirage au sort** avec règles (exclusions, foyers, associations forcées, personne en « réception seule » ou « offre plus »), options anti-échange A↔B et « pas la même cible que l'an dernier », vérification de faisabilité et réparation minimale quand une règle ou un participant change après le tirage (« Ajuster le tirage »).
- **Respect du secret** : l'organisateur ne voit jamais qui offre à qui (sauf option activée, journalisée). Chacun découvre sa cible par un lien personnel, avec une révélation animée.
- **Lettre au Père Noël** à sens unique (jusqu'à 5 idées par lettre), suggestions d'idées selon le budget, budget facultatif et évolutif.
- **Relances par courriel** (écrire sa lettre, lire la lettre reçue), désinscription en un clic, file d'envoi persistante avec reprise.
- Sécurité du web : CSRF, en-têtes stricts, limites anti-abus par adresse IP, jetons à usage unique, journal d'administration sans donnée sensible.

## Installation avec Docker (démarrage rapide)

Prérequis : Docker avec le plugin Compose. L'image publiée est `ghcr.io/cerede2000/secretsanta` (architecture `linux/amd64`).

```bash
git clone https://github.com/cerede2000/secretsanta.git && cd secretsanta
cp docker-compose.example.yml docker-compose.yml
cp .env.example .env
```

Éditez `.env` : renseignez `INSTANCE_PASSWORD` (mot de passe demandé pour créer un groupe) et `SIGNING_SECRET` (longue valeur aléatoire, par exemple `openssl rand -hex 32`), puis protégez le fichier : `chmod 600 .env`. L'application refuse de démarrer sans ces deux secrets.

```bash
docker compose up -d
```

Ouvrez <http://localhost:8080> : le bouton **Commencer** mène à la création d'un groupe. Le secret d'administration du groupe n'est affiché qu'une fois : notez-le.

Le fichier [docker-compose.example.yml](docker-compose.example.yml) est entièrement commenté (SMTP, DKIM/SPF/DMARC, mandataire inverse, volumes, durcissement). Le [guide d'installation](docs/guide/index.md) détaille l'exploitation.

## Variables d'environnement

Toutes sont facultatives sauf `INSTANCE_PASSWORD` et `SIGNING_SECRET`. Le modèle est `.env.example` ; la référence détaillée est [docs/guide/configuration.md](docs/guide/configuration.md).

| Variable | Rôle et valeur par défaut |
|---|---|
| `PUBLIC_URL` | Adresse publique, en `https://` en production (liens des courriels, cookies sécurisés). Défaut `http://localhost:8080`. |
| `LISTEN_ADDR` | Adresse d'écoute HTTP. Défaut `:8080`. |
| `DATA_DIR` | Dossier des données (base SQLite). Défaut `/data`, à monter en volume. |
| `APP_TIMEZONE` | Fuseau horaire IANA (relances, dates). Défaut `Europe/Paris`. |
| `APP_DEFAULT_LANG` | Langue par défaut de l'interface et des courriels. Défaut `fr`. |
| `I18N_DIR` | Dossier facultatif de traductions ([ajouter une langue](docs/guide/traduire.md)). |
| `INSTANCE_PASSWORD` | **Obligatoire.** Mot de passe d'instance pour créer un groupe. |
| `SIGNING_SECRET` | **Obligatoire.** Secret de signature (liens, désinscription, jetons). |
| `SMTP_HOST` | Serveur SMTP ; sans lui, l'envoi de courriels est désactivé. |
| `SMTP_PORT` | Port SMTP (587 en `starttls`, 465 en `tls`). |
| `SMTP_USER` | Identifiant SMTP. |
| `SMTP_PASSWORD` | Mot de passe SMTP. |
| `MAIL_FROM` | Adresse d'expédition, obligatoire dès que `SMTP_HOST` est défini. |
| `MAIL_FROM_NAME` | Nom d'expéditeur affiché. |
| `MAIL_EHLO_HOSTNAME` | Nom d'hôte annoncé à EHLO (doit correspondre au DNS inverse). |
| `MAIL_ENVELOPE_FROM` | Expéditeur d'enveloppe (défaut `MAIL_FROM`). |
| `MAIL_REPLY_TO` | Adresse de réponse. |
| `MAIL_TLS_MODE` | `starttls` (défaut), `tls` ou `none`. |
| `MAIL_ALLOW_INSECURE` | `true` requis pour accepter `none`. Défaut `false`. |
| `MAIL_MAX_PER_MINUTE` | Débit maximal d'envois par minute. Défaut `30`. |
| `MAIL_DKIM_DOMAIN` | Domaine de la signature DKIM par l'application (tout ou rien). |
| `MAIL_DKIM_SELECTOR` | Sélecteur DKIM. |
| `MAIL_DKIM_KEY_FILE` | Chemin du fichier de clé privée DKIM (jamais dans une variable). |
| `MAIL_DKIM_ALGORITHM` | `rsa-sha256` (défaut) ou `ed25519-sha256`. |
| `MAIL_DKIM_HEADERS` | En-têtes signés, séparés par des virgules. |
| `REMINDERS_ENABLED` | Interrupteur général des relances. Défaut `true`. |
| `TRUSTED_PROXIES` | Adresses ou CIDR dont l'en-tête d'adresse client est cru ; `none` pour aucun. Défaut : bouclage et réseaux privés. |
| `CLIENT_IP_HEADER` | `X-Forwarded-For` (défaut), `X-Real-IP` ou `CF-Connecting-IP`. |
| `SEARXNG_URL` | Adresse d'un SearXNG auto-hébergé (suggestions d'idées, prioritaire). |
| `BRAVE_API_KEY` | Clé d'API Brave Search (suggestions). |
| `SERPER_API_KEY` | Clé d'API Serper (suggestions). |

## SMTP, courriel et anti-spam

L'envoi de courriels est **facultatif mais recommandé**. Sans `SMTP_HOST`, l'application fonctionne, mais aucun code de vérification, aucune relance et aucun avis ne partent : l'inscription par lien affiche alors à l'invité « l'envoi de courriel n'est pas configuré, contacte l'organisateur », et un avertissement apparaît sur le tableau de bord et la page des inscriptions de l'organisateur.

Renseignez `SMTP_HOST`, `SMTP_PORT`, `MAIL_FROM` (et `SMTP_USER` / `SMTP_PASSWORD` si le relais l'exige), puis choisissez le chiffrement avec `MAIL_TLS_MODE`.

Pour que les messages n'arrivent pas en indésirables, le domaine de `MAIL_FROM` doit publier :

- **SPF** : `TXT  v=spf1 include:<votre-relais> ~all` (ou `ip4:<IP du serveur>`) ;
- **DKIM** : `TXT  <sélecteur>._domainkey  v=DKIM1; k=rsa; p=<clé publique>`. Soit le relais SMTP signe (cas courant), soit l'application signe avec `MAIL_DKIM_*` et une clé montée en fichier ;
- **DMARC** : `TXT  _dmarc  v=DMARC1; p=none; rua=mailto:dmarc@<domaine>`, à resserrer en `quarantine` puis `reject` une fois les rapports propres ;
- un **DNS inverse (PTR)** de l'adresse d'envoi qui pointe vers `MAIL_EHLO_HOSTNAME`.

L'application n'envoie que des messages utiles (codes, relances mesurées, avis), avec en-têtes `List-Unsubscribe` et désinscription en un clic, et plafonne son débit. Elle ne peut pas garantir la livraison : voir le [guide de délivrabilité](docs/guide/deliverability.md).

## Mandataire inverse et HTTPS

En production, placez l'application derrière un mandataire qui termine le TLS (Traefik, Caddy, Nginx : exemples dans [docker-compose.example.yml](docker-compose.example.yml)) et définissez `PUBLIC_URL=https://noel.example.org`. L'application lit l'adresse réelle du client dans `X-Forwarded-For` quand la connexion vient du bouclage ou d'un réseau privé : un mandataire dans un réseau Docker ne demande aucun réglage. Sinon :

- `TRUSTED_PROXIES` : adresses ou CIDR de confiance (`none` si l'application est exposée seule) ;
- `CLIENT_IP_HEADER` : `X-Real-IP` (Nginx configuré ainsi) ou `CF-Connecting-IP` (Cloudflare).

Un réglage faux se voit à ce symptôme : tous les visiteurs partagent la même adresse et atteignent ensemble les limites anti-abus (erreur 429).

## Sources de suggestions

Les idées de cadeaux suggérées viennent, dans l'ordre : d'un SearXNG auto-hébergé (`SEARXNG_URL`), des services à clé (`BRAVE_API_KEY`, `SERPER_API_KEY`), puis d'un catalogue de secours embarqué quand rien n'est configuré ou joignable. Les adresses sont validées contre les requêtes vers le réseau interne (SSRF). Voir [la configuration](docs/guide/configuration.md#sources-de-suggestions).

## Sauvegarde et restauration

Tout l'état est dans `/data` (base SQLite en mode WAL). Pour une sauvegarde cohérente, arrêtez l'application puis archivez le volume :

```bash
docker compose stop secretsanta
docker run --rm -v secretsanta_secretsanta-data:/data -v "$PWD":/sauvegarde alpine \
  tar czf /sauvegarde/secretsanta-data.tgz -C /data .
docker compose start secretsanta
```

Le nom du volume est `<dossier-du-compose>_secretsanta-data` (vérifiez avec `docker volume ls`). Restauration : arrêter l'application, vider le volume, extraire l'archive dans `/data` (propriétaire `65532`), redémarrer. Conservez aussi `.env` : sans le même `SIGNING_SECRET`, les liens déjà envoyés ne fonctionnent plus. Voir [le guide](docs/guide/index.md#sauvegarde-et-restauration).

## Mise à jour

```bash
docker compose pull && docker compose up -d
```

Les migrations de base s'appliquent au démarrage et ne sont pas réversibles : sauvegardez avant. Le dépôt publie à chaque version une image `0.1.N` et `latest` ; les **4 dernières versions** sont conservées, pour fixer une étiquette précise en cas de retour arrière. Voir [le guide](docs/guide/index.md#mise-à-jour-et-retour-à-la-version-précédente).

## Sécurité

- Les secrets ne sont jamais dans le dépôt ni dans le fichier Compose : ils viennent de `.env` (droits `600`). La clé DKIM est un fichier monté en lecture seule.
- Image « distroless » sans shell, binaire statique, utilisateur non root (`65532`), système de fichiers en lecture seule possible (seul `/data` est écrit).
- L'administrateur d'un groupe ne voit pas les paires par défaut ; la consultation d'une paire, si le groupe l'autorise, est confirmée et journalisée.
- Jetons de lien d'accès et d'invitation stockés sous forme d'empreinte, comparés à temps constant ; réponses neutres pour ne pas révéler l'existence d'un compte ; limites par adresse IP et par groupe.
- Données personnelles : noms, adresses de courriel, listes d'idées. Supprimez-les en conformité avec le RGPD ; désinscription en un clic dans chaque relance.

Voir aussi [le guide](docs/guide/index.md#sécurité).

## Dépannage

- Le conteneur s'arrête au démarrage : `docker compose logs secretsanta` ; la variable manquante ou invalide est nommée.
- Les invités voient « l'envoi de courriel n'est pas configuré » : renseignez `SMTP_HOST` et `MAIL_FROM`, puis redémarrez.
- Les courriels arrivent en indésirables : SPF, DKIM, DMARC, PTR ; voir la [délivrabilité](docs/guide/deliverability.md).
- Liens des courriels vers la mauvaise adresse : corrigez `PUBLIC_URL`.
- Erreur 429 pour tout le monde : mandataire non reconnu, réglez `TRUSTED_PROXIES` / `CLIENT_IP_HEADER`.
- Base inaccessible avec un dossier de l'hôte : `chown -R 65532:65532 ./data`.

Plus de cas dans [le guide](docs/guide/index.md#dépannage).

## Développement, tests et couverture

Go 1.27 ; aucune étape de build du front (pages serveur, JavaScript natif).

```bash
make check                          # gofmt, vet, tests, build statique, govulncheck
go run ./cmd/secretsanta            # lance l'application (INSTANCE_PASSWORD et SIGNING_SECRET requis)
bash scripts/coverage.sh --enforce  # couverture par paquet : 80 % minimum, 85 % pour le moteur de tirage
bash scripts/check-image.sh         # contrôle de l'image Docker (non root, sans secret, santé)
bash scripts/check-vuln.sh          # govulncheck sur le module et le binaire
bash scripts/spamcheck.sh --self-test   # scanner SpamAssassin des courriels
```

### Recette locale

Le banc de recette en conteneurs (application + Mailpit + Playwright, deux profils mobiles 360×640 et 390×844, zéro test sauté) tourne sur Mac via colima :

```bash
bash scripts/e2e.sh --smoke   # test de fumée
bash scripts/e2e.sh           # toute la recette
```

Détails : [le guide](docs/guide/index.md#recette-locale). La CI GitHub (`.github/workflows/ci.yml`) rejoue les contrôles Go, calcule la version (`VERSION` = `0.1`, puis `0.1.N`), construit l'image `linux/amd64`, la publie sur GHCR (`0.1.N` et `latest`), pose le tag Git et ne garde que les 4 dernières versions.

## Autres guides

- [Guide d'installation et d'exploitation](docs/guide/index.md)
- [Configuration](docs/guide/configuration.md)
- [Délivrabilité des courriels](docs/guide/deliverability.md)
- [Ajouter une langue](docs/guide/traduire.md)
