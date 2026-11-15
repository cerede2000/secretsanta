# Plan : Secret Santa famille, application self-hosted en Docker

<!-- Généré par wave-plan à partir de docs/waves/specs/*.md. Les specs possèdent le « quoi » (depends_on, owns, critères) ; ce fichier possède le « quand et comment ». Les vagues sont calculées par script (ordonnancement par priorité de chemin critique : wave = 1 + max des vagues des prérequis, règle de fonctionnalité incluse, largeur maximale respectée), jamais placées à la main. Données lisibles par machine uniquement dans les blocs ci-dessous. -->

## Header

```yaml header
design: docs/waves/design/snapshot.html
bootstrap: "test ! -f go.mod || go mod download; test ! -f tests/e2e/package-lock.json || npm ci --prefix tests/e2e"
gates: {format: "test -z \"$(gofmt -l .)\"", lint: "go vet ./...", typecheck: "go build ./...", unit: "go test ./..."}
max_wave_width: 8
```

Les portes de phase sont les quatre commandes ci-dessus. Les critères d'acceptation (tests Playwright, parcours Docker, fidélité à la maquette) sont gatés par vague, après câblage des fichiers d'intégration par l'orchestrateur.

## Feature index

| Feature | Spec |
|---|---|
| F1 Configuration et secrets | docs/waves/specs/F1.md |
| F10 Interface d'administration | docs/waves/specs/F10.md |
| F10b Consultation de paire optionnelle et outils d'exploitation | docs/waves/specs/F10b.md |
| F11 Inscription par lien invité : lien, formulaire et vérification | docs/waves/specs/F11.md |
| F11c Inscription par lien : pages publiques et administration des inscriptions | docs/waves/specs/F11c.md |
| F12a Banc de recette en conteneurs et documentation | docs/waves/specs/F12a.md |
| F12b Parcours de bout en bout, couverture et contrôles de livraison | docs/waves/specs/F12b.md |
| F1b Base de données, ports partagés et registre de navigation | docs/waves/specs/F1b.md |
| F1c Point d'entrée, squelette web et livraison | docs/waves/specs/F1c.md |
| F2 Groupes : création, slug et accès administrateur | docs/waves/specs/F2.md |
| F2b Accès participant : jetons, codes courts et lien magique | docs/waves/specs/F2b.md |
| F2c Sécurité web : sessions, CSRF, cookies et en-têtes | docs/waves/specs/F2c.md |
| F2e Réglages du groupe et pages d'accès | docs/waves/specs/F2e.md |
| F3 Participants, foyers et exclusions | docs/waves/specs/F3.md |
| F3d Création vérifiée, lien et QR code, règles modifiées après tirage | docs/waves/specs/F3d.md |
| F4 Moteur de tirage | docs/waves/specs/F4.md |
| F4b Tirage persistant, visibilité des cibles et révélation | docs/waves/specs/F4b.md |
| F4c Ajustement du tirage et réparation après règle modifiée | docs/waves/specs/F4c.md |
| F4d Pages de l'attente et de la cible | docs/waves/specs/F4d.md |
| F5 Lettre au Père Noël : écriture, idées et images | docs/waves/specs/F5.md |
| F5b Lettre au Père Noël : lecture par le tireur, cachet et archivage | docs/waves/specs/F5b.md |
| F5c Pages de la lettre et boîte aux lettres animée | docs/waves/specs/F5c.md |
| F6 Courrier SMTP, modèles français, file d'envoi et désinscription | docs/waves/specs/F6.md |
| F6b Messages conformes et signature DKIM | docs/waves/specs/F6b.md |
| F6c Délivrabilité : diagnostic DNS et TLS, liste de suppression et page d'administration | docs/waves/specs/F6c.md |
| F7 Relances manuelles et automatiques | docs/waves/specs/F7.md |
| F7b Notification « nouvelle idée » et relance du tireur | docs/waves/specs/F7b.md |
| F8 Récupération protégée contre le SSRF et copie locale des images | docs/waves/specs/F8.md |
| F8b Sources de suggestions, catalogue de secours et enrichissement | docs/waves/specs/F8b.md |
| F8c Service de suggestions et page du tireur | docs/waves/specs/F8c.md |
| F9 Thème de Noël, illustrations, avatars et application installable | docs/waves/specs/F9.md |
| F9c Animation de découverte de la cible | docs/waves/specs/F9c.md |
| F13 Internationalisation : français, anglais et ajout simple d'une langue | docs/waves/specs/F13.md |
| F13b Relecture et contrôle de qualité des textes anglais | docs/waves/specs/F13b.md |

## Phases

<!-- model = haiku | sonnet | opus, dérivé de score par scripts/routing.py ; override explicite quand la règle risk-not-opus du validateur impose opus à toute phase d'une spec marquée risk. -->

```yaml phase
id: P1a
spec: F1
steps: [F1.1, F1.2]
owns: [internal/config/**, .env.example]
depends_on: []
model: sonnet
score: {scope: 1, ambiguity: 0, risk: 1, reasoning: 1}
rationale: "configuration validée, secrets obligatoires et variables de courrier et de signature DKIM masquées : logique standard"
```

```yaml phase
id: P1b-db
spec: F1b
steps: [F1b.1]
owns: [internal/db/**]
depends_on: []
model: sonnet
score: {scope: 1, ambiguity: 0, risk: 1, reasoning: 1}
rationale: "SQLite pur Go et migrations transactionnelles : logique standard"
```

```yaml phase
id: P1b-sh1
spec: F1b
steps: [F1b.2]
owns: [internal/shared/clock/**, internal/shared/scheduler/**, internal/shared/random/**, internal/shared/sanitize/**, internal/shared/errs/**]
depends_on: []
model: sonnet
score: {scope: 1, ambiguity: 1, risk: 0, reasoning: 1}
rationale: "horloge simulable et planificateur : quelques choix de conception"
```

```yaml phase
id: P1b-sh2
spec: F1b
steps: [F1b.3]
owns: [internal/shared/ports/**, internal/shared/outbound/**, internal/shared/adminnav/**]
depends_on: []
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 0, reasoning: 1}
rationale: "ports partagés et client sortant : plusieurs paquets mais contrats clairs"
```

```yaml phase
id: P1c-ci
spec: F1c
steps: [F1c.5]
owns: [cmd/ci/**, .github/workflows/ci.yml, VERSION, Makefile]
depends_on: []
model: sonnet
score: {scope: 1, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "workflow CI, calcul de version et de rétention en outils Go isolés"
```

```yaml phase
id: P1c-entry
spec: F1c
steps: [F1c.1]
owns: [cmd/secretsanta/**, internal/health/**]
depends_on: [P1a, P1b-db]
model: sonnet
score: {scope: 1, ambiguity: 1, risk: 0, reasoning: 1}
rationale: "point d'entrée, arrêt propre et healthcheck : standard"
```

```yaml phase
id: P1c-infra
spec: F1c
steps: [F1c.2]
owns: [tests/e2e/package.json, tests/e2e/package-lock.json, tests/e2e/playwright.config.ts, tests/e2e/_shared/**]
depends_on: []
model: sonnet
score: {scope: 1, ambiguity: 1, risk: 0, reasoning: 1}
rationale: "configuration Playwright et helpers mobiles"
```

```yaml phase
id: P2-groups
spec: F2
steps: [F2.1, F2.2]
owns: [internal/groups/**]
depends_on: [P1a, P1b-db, P1b-sh1]
model: opus
score: {scope: 1, ambiguity: 0, risk: 2, reasoning: 1}
rationale: "secret d'administration et mot de passe d'instance : sécurité, force opus"
```

```yaml phase
id: P2b-a
spec: F2b
steps: [F2b.1]
owns: [internal/access/token/**]
depends_on: [P1b-db, P1b-sh1]
model: opus
score: {scope: 1, ambiguity: 0, risk: 2, reasoning: 1}
rationale: "jetons, codes courts et isolation entre groupes : sécurité, force opus"
```

```yaml phase
id: P2b-b
spec: F2b
steps: [F2b.2]
owns: [internal/access/magic/**, internal/access/limiter/**]
depends_on: [P1b-db, P1b-sh1, P1b-sh2]
model: opus
score: {scope: 1, ambiguity: 0, risk: 2, reasoning: 1}
rationale: "lien magique à usage unique et limiteur de requêtes : sécurité, force opus"
```

```yaml phase
id: P2c-websec
spec: F2c
steps: [F2c.1, F2c.2]
owns: [internal/websec/**]
depends_on: [P1a, P1b-sh1]
model: opus
score: {scope: 1, ambiguity: 0, risk: 2, reasoning: 1}
rationale: "CSRF, cookies, en-têtes et sessions : sécurité, force opus"
```

```yaml phase
id: P4-engine
spec: F4
steps: [F4.1, F4.2, F4.3, F4.4, F4.5]
owns: [internal/drawengine/**]
depends_on: [P1b-sh1]
model: opus
score: {scope: 1, ambiguity: 1, risk: 2, reasoning: 2}
rationale: "tirage, aléa cryptographique et algorithme : force opus"
```

```yaml phase
id: P1c-docker
spec: F1c
steps: [F1c.4]
owns: [Dockerfile, .dockerignore, docker-compose.yml, tests/e2e/docker/**]
depends_on: [P1c-entry]
model: sonnet
score: {scope: 1, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "image minimale non-root et test de fumée du conteneur"
```

```yaml phase
id: P1c-layout
spec: F1c
steps: [F1c.3]
owns: [web/embed.go, web/templates/layout/**, web/static/css/base/**, tests/e2e/foundation/**, web/static/i18n/layout/**, web/embed_test.go]
depends_on: [P1c-infra, P1b-sh2]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 0, reasoning: 1}
rationale: "gabarits, CSS de base et gabarit d'administration : plusieurs fichiers web"
```

```yaml phase
id: P3-core
spec: F3
steps: [F3.1]
owns: [internal/participants/**]
depends_on: [P2-groups, P1b-db, P1b-sh1]
model: sonnet
score: {scope: 1, ambiguity: 0, risk: 1, reasoning: 1}
rationale: "participants, unicité du nom et suppression : logique standard"
```

```yaml phase
id: P3-rules
spec: F3
steps: [F3.2]
owns: [internal/rules/**]
depends_on: [P1b-db]
model: sonnet
score: {scope: 1, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "foyers, exclusions, forçages et détection de conflits"
```

```yaml phase
id: P6b-msg
spec: F6b
steps: [F6b.1]
owns: [internal/mailmsg/**]
depends_on: [P1a]
model: opus
score: {scope: 2, ambiguity: 1, risk: 2, reasoning: 2}
rationale: "constructeur de messages MIME, en-têtes, objets, protection contre l'injection : sécurité, force opus"
```

```yaml phase
id: P6b-dkim
spec: F6b
steps: [F6b.2]
owns: [internal/maildkim/**]
depends_on: [P1a]
model: opus
score: {scope: 1, ambiguity: 1, risk: 2, reasoning: 2}
rationale: "signature DKIM rsa et ed25519, clé par fichier, vérification indépendante : sécurité, force opus"
```

```yaml phase
id: P2b-store
spec: F2b
steps: [F2b.3, F2b.4]
owns: [internal/access/migrations/**, internal/access/sqlstore/**, internal/access/token/**]
depends_on: [P2b-a, P2b-b, P1b-db]
model: opus
score: {scope: 2, ambiguity: 1, risk: 2, reasoning: 2}
rationale: "migrations et magasin SQL des jetons et codes courts (token.Store), puis hachage à clé HMAC-SHA-256 avec la clé de signature : sécurité, force opus"
```

```yaml phase
id: P13-catalog
spec: F13
steps: [F13.1, F13.2]
owns: [internal/i18n/catalog/**, internal/i18n/cli/**]
depends_on: []
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 0, reasoning: 2}
rationale: "chargement et fusion des catalogues, dossier externe, repli, pluriels et outils export et check : logique standard avec plusieurs cas limites"
```

```yaml phase
id: P5-letter
spec: F5
steps: [F5.1, F5.2, F5.3, F5.4]
owns: [internal/letter/**]
depends_on: [P1b-db, P1b-sh1, P1b-sh2]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "modèle, services d'écriture, image locale, événement de regroupement et lettre des personnes en réception seule : un seul paquet cohérent"
```

```yaml phase
id: P8-ssrf
spec: F8
steps: [F8.1, F8.2]
owns: [internal/ssrf/**]
depends_on: [P1b-sh2]
model: opus
score: {scope: 1, ambiguity: 1, risk: 2, reasoning: 2}
rationale: "garde SSRF : sécurité, force opus"
```

```yaml phase
id: P9-illus
spec: F9
steps: [F9.2]
owns: [web/static/assets/**, web/static/js/avatars/**, tests/e2e/illustrations/**]
depends_on: [P1c-infra]
model: sonnet
score: {scope: 1, ambiguity: 1, risk: 0, reasoning: 1}
rationale: "illustrations SVG et avatars par hash du prénom"
```

```yaml phase
id: P9-theme
spec: F9
steps: [F9.1]
owns: [web/static/css/theme/**, web/static/fonts/**, web/static/js/theme/**, web/static/manifest.webmanifest, web/static/sw.js, tests/e2e/theme/**]
depends_on: [P1c-infra, P1c-layout]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 0, reasoning: 1}
rationale: "thème, neige, police locale et manifeste web"
```

```yaml phase
id: P3-roles
spec: F3
steps: [F3.3]
owns: [internal/participants/roles/**]
depends_on: [P3-core, P3-rules]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "rôle dans le tirage : énuméré exclusif, droits de réglage, port de lecture avec décomptes sans noms"
```

```yaml phase
id: P13-negotiate
spec: F13
steps: [F13.3]
owns: [internal/i18n/negotiate/**, internal/i18n/web/**, web/templates/i18n/**, web/static/css/i18n/**, web/static/js/i18n/**, web/static/i18n/i18n/**, tests/e2e/i18n/**, tests/e2e/foundation/i18n.spec.ts]
depends_on: [P2c-websec]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 0, reasoning: 2}
rationale: "négociation de la langue, fonctions de gabarit, sélecteur de langue et son changement protégé par le jeton CSRF : logique standard"
```

```yaml phase
id: P13-guard
spec: F13
steps: [F13.4]
owns: [internal/i18n/guard/**]
depends_on: []
model: sonnet
score: {scope: 1, ambiguity: 1, risk: 0, reasoning: 2}
rationale: "tests de garde-fou (texte brut, parité, clés utilisées, anglais distinct) : analyse de gabarits, contrats clairs"
```

```yaml phase
id: P13-config
spec: F13
steps: [F13.5, F13.6]
owns: [internal/config/**, .env.example, internal/shared/errs/**, web/static/i18n/core/**]
depends_on: [P1a, P1b-sh1]
model: sonnet
score: {scope: 1, ambiguity: 1, risk: 0, reasoning: 1}
rationale: "deux variables de configuration, champ Code facultatif de errs et textes core : changements petits et bornés"
```

```yaml phase
id: P6-queue
spec: F6
steps: [F6.2]
owns: [internal/mail/queue/**, internal/mail/migrations/**]
depends_on: [P1b-db, P1b-sh1, P1b-sh2]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "file d'envoi durable, reprise et regroupement"
```

```yaml phase
id: P6-templates
spec: F6
steps: [F6.3, F6.6]
owns: [internal/mail/templates/**, web/static/i18n/mail/templates/**]
depends_on: [P1b-sh2, P6b-msg]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "modèles français conformes à l'antispam, variantes à une ou deux cibles et réception seule, fichiers .eml de référence"
```

```yaml phase
id: P3d-enrol
spec: F3d
steps: [F3d.1]
owns: [internal/enrolment/**]
depends_on: [P3-core, P2b-a, P1b-sh2]
model: sonnet
score: {scope: 1, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "création vérifiée, lien personnel et QR code"
```

```yaml phase
id: P3d-rulechange
spec: F3d
steps: [F3d.2]
owns: [internal/rulechange/**]
depends_on: [P3-rules, P1b-sh2]
model: sonnet
score: {scope: 1, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "règles modifiées après tirage via le port de réparation"
```

```yaml phase
id: P4b-core
spec: F4b
steps: [F4b.1]
owns: [internal/draw/**]
depends_on: [P3-core, P3-rules, P4-engine, P1b-db, P1b-sh2, P2-groups]
model: opus
score: {scope: 2, ambiguity: 1, risk: 2, reasoning: 2}
rationale: "tirage atomique et visibilité des cibles (anonymat) : force opus"
```

```yaml phase
id: P2e-pages1
spec: F2e
steps: [F2e.2]
owns: [internal/grouppages/**, web/templates/groups/**, web/static/js/groups/**, web/static/css/groups/**, tests/e2e/groups/**, web/static/i18n/groups/**]
depends_on: [P2-groups, P2c-websec, P1c-layout, P9-theme]
model: sonnet
score: {scope: 1, ambiguity: 1, risk: 0, reasoning: 1}
rationale: "pages d'accueil et de création de groupe à partir de la maquette"
```

```yaml phase
id: P2e-pages2
spec: F2e
steps: [F2e.3]
owns: [internal/accesspages/**, web/templates/access/**, web/static/js/access/**, web/static/css/access/**, tests/e2e/access/**, web/static/i18n/access/**]
depends_on: [P2-groups, P2b-a, P2b-b, P2c-websec, P1c-layout, P9-theme]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "pages de connexion et gestionnaires de connexion branchés sur plusieurs modules"
```

```yaml phase
id: P2e-settings
spec: F2e
steps: [F2e.1]
owns: [internal/groupsettings/**]
depends_on: [P2-groups, P1b-sh2]
model: sonnet
score: {scope: 1, ambiguity: 0, risk: 1, reasoning: 1}
rationale: "réglages de groupe et événement de budget : logique standard"
```

```yaml phase
id: P6-transport
spec: F6
steps: [F6.1, F6.5]
owns: [internal/mail/transport/**]
depends_on: [P1a, P1b-sh1, P6b-msg, P6b-dkim]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "transport SMTP durci (TLS exigé, EHLO, enveloppe, débit, classes d'erreur) et signature DKIM branchée"
```

```yaml phase
id: P6-unsub
spec: F6
steps: [F6.4]
owns: [internal/mailunsub/**, web/templates/mail/**, web/static/css/mail/**, tests/e2e/mail/**, web/static/i18n/mail/unsubscribe/**]
depends_on: [P6-queue, P6-templates, P2c-websec, P1c-layout, P1b-sh1]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "désinscription signée et page de confirmation"
```

```yaml phase
id: P8b-enrich
spec: F8b
steps: [F8b.2]
owns: [internal/enrich/**]
depends_on: [P8-ssrf]
model: sonnet
score: {scope: 1, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "extraction de page en meilleur effort"
```

```yaml phase
id: P12a-bench
spec: F12a
steps: [F12a.1, F12a.3]
owns: [docker-compose.test.yml, scripts/e2e.sh, scripts/check-no-test-secrets.sh, tests/e2e/mail-sink/**, scripts/spamcheck.sh, tests/e2e/spamassassin/**]
depends_on: [P1c-docker, P1c-infra]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "banc de recette Docker avec serveur SMTP factice, SpamAssassin et scripts e2e"
```

```yaml phase
id: P4b-reveal
spec: F4b
steps: [F4b.2]
owns: [internal/reveal/**]
depends_on: [P4b-core, P1b-sh1, P1b-sh2]
model: opus
score: {scope: 1, ambiguity: 1, risk: 2, reasoning: 1}
rationale: "révélation et verrou de date, qui garde la cible cachée : force opus"
```

```yaml phase
id: P8b-sources
spec: F8b
steps: [F8b.1]
owns: [internal/sources/**]
depends_on: [P8-ssrf, P1b-sh2, P1a]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "adaptateurs de sources, repli et catalogue embarqué"
```

```yaml phase
id: P9c-core
spec: F9c
steps: [F9c.1]
owns: [web/static/css/reveal/core/**, web/static/js/reveal/core/**, web/templates/reveal/**, tests/e2e/reveal/core/**, web/static/i18n/reveal/core/**]
depends_on: [P9-theme, P1c-infra, P1c-layout]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 0, reasoning: 1}
rationale: "module commun d'animation et sélecteur aléatoire"
```

```yaml phase
id: P3d-roles
spec: F3d
steps: [F3d.3]
owns: [internal/rulechange/**, internal/enrolment/**]
depends_on: [P3d-enrol, P3d-rulechange, P3-roles]
model: sonnet
score: {scope: 1, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "changement de rôle après tirage branché sur la réparation, rôle facultatif dans CreateVerified"
```

```yaml phase
id: P7-rules
spec: F7
steps: [F7.1]
owns: [internal/reminderrules/**]
depends_on: [P1a, P1b-sh1]
model: sonnet
score: {scope: 1, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "plafond, espacement, fenêtre d'envoi et fuseau"
```

```yaml phase
id: P6c-diag
spec: F6c
steps: [F6c.1]
owns: [internal/maildiag/**]
depends_on: [P1a]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "diagnostic SPF, DKIM, DMARC, MX, TLS et HTTPS avec résolveur injectable"
```

```yaml phase
id: P6c-suppress
spec: F6c
steps: [F6c.2]
owns: [internal/mailsuppress/**]
depends_on: [P1b-db, P1b-sh2]
model: sonnet
score: {scope: 1, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "liste de suppression par groupe, implémentation du port SuppressionList"
```

```yaml phase
id: P11-code
spec: F11
steps: [F11.3]
owns: [internal/registration/code/**]
depends_on: [P2b-a, P2b-b, P1b-sh1, P1b-sh2]
model: opus
score: {scope: 1, ambiguity: 1, risk: 2, reasoning: 1}
rationale: "code de vérification, tentatives et plafonds : sécurité, force opus"
```

```yaml phase
id: P11-invite
spec: F11
steps: [F11.1, F11.2]
owns: [internal/registration/invite/**]
depends_on: [P2-groups, P2b-a, P2b-b, P3-core, P1b-sh1, P1b-sh2]
model: opus
score: {scope: 2, ambiguity: 1, risk: 2, reasoning: 1}
rationale: "jeton d'invitation et formulaire : sécurité, force opus"
```

```yaml phase
id: P9c-envelope
spec: F9c
steps: [F9c.3]
owns: [web/static/css/reveal/envelope/**, web/static/js/reveal/envelope/**, tests/e2e/reveal/envelope/**, web/templates/reveal/envelope/**, web/static/i18n/reveal/envelope/**]
depends_on: [P9c-core]
model: sonnet
score: {scope: 1, ambiguity: 1, risk: 0, reasoning: 2}
rationale: "animation de l'enveloppe à sceau de cire"
```

```yaml phase
id: P9c-gift
spec: F9c
steps: [F9c.2]
owns: [web/static/css/reveal/gift/**, web/static/js/reveal/gift/**, tests/e2e/reveal/gift/**, web/templates/reveal/gift/**, web/static/i18n/reveal/gift/**]
depends_on: [P9c-core]
model: sonnet
score: {scope: 1, ambiguity: 1, risk: 0, reasoning: 2}
rationale: "animation du cadeau enveloppé"
```

```yaml phase
id: P4b-multi
spec: F4b
steps: [F4b.3]
owns: [internal/draw/**, internal/reveal/**]
depends_on: [P4b-core, P4b-reveal, P3-roles, P4-engine]
model: opus
score: {scope: 2, ambiguity: 1, risk: 2, reasoning: 2}
rationale: "cibles multiples, drapeaux de révélation par cible, contrôle de capacité sans noms : sécurité et données, force opus"
```

```yaml phase
id: P7-engine
spec: F7
steps: [F7.2, F7.3, F7.4]
owns: [internal/reminders/**]
depends_on: [P7-rules, P6-queue, P6-unsub, P3-core, P5-letter, P1b-sh1]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "planificateur, relance manuelle, réception seule et adresses supprimées exclues des relances"
```

```yaml phase
id: P9c-scratch
spec: F9c
steps: [F9c.4]
owns: [web/static/css/reveal/scratch/**, web/static/js/reveal/scratch/**, tests/e2e/reveal/scratch/**, web/templates/reveal/scratch/**, web/static/i18n/reveal/scratch/**]
depends_on: [P9c-core]
model: sonnet
score: {scope: 1, ambiguity: 1, risk: 0, reasoning: 2}
rationale: "animation de la carte à gratter"
```

```yaml phase
id: P11-verify
spec: F11
steps: [F11.4]
owns: [internal/registration/verify/**]
depends_on: [P11-invite, P11-code, P3d-enrol, P3-core, P1b-sh1, P1b-sh2]
model: opus
score: {scope: 2, ambiguity: 1, risk: 2, reasoning: 1}
rationale: "vérification, purge et validation sans code : force opus"
```

```yaml phase
id: P4c-adjust
spec: F4c
steps: [F4c.1]
owns: [internal/adjust/**]
depends_on: [P4b-core, P4b-reveal, P3-core, P2e-settings, P1b-sh2]
model: opus
score: {scope: 1, ambiguity: 1, risk: 2, reasoning: 2}
rationale: "ajustement du tirage et aperçu sans noms (anonymat) : force opus"
```

```yaml phase
id: P5b-read
spec: F5b
steps: [F5b.1, F5b.2]
owns: [internal/letterread/**]
depends_on: [P5-letter, P4b-core, P4b-reveal]
model: opus
score: {scope: 1, ambiguity: 1, risk: 2, reasoning: 1}
rationale: "lecture seule par le tireur, par cible pour deux cibles, cachet et archivage sans identité : sécurité, force opus"
```

```yaml phase
id: P4d-pages
spec: F4d
steps: [F4d.1, F4d.2]
owns: [internal/targetpage/**, web/templates/draw/**, web/static/js/draw/**, web/static/css/draw/**, tests/e2e/draw/**, web/static/i18n/draw/**]
depends_on: [P4b-reveal, P2e-settings, P2c-websec, P1c-layout, P9-theme]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "pages de l'attente et de la cible à partir de la maquette"
```

```yaml phase
id: P8c-service
spec: F8c
steps: [F8c.1]
owns: [internal/suggestions/**]
depends_on: [P8b-sources, P8b-enrich, P2-groups, P3-core, P4b-core, P5-letter]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "service de suggestions, cache et filtrage des données personnelles"
```

```yaml phase
id: P11-store
spec: F11
steps: [F11.6]
owns: [internal/registration/migrations/**, internal/registration/sqlstore/**]
depends_on: [P11-invite, P11-code, P1b-db]
model: opus
score: {scope: 2, ambiguity: 1, risk: 2, reasoning: 1}
rationale: "migrations et magasins SQL de l'invitation, des inscriptions en attente et des codes (hachés, purge à 48 h) : sécurité et données, force opus"
```

```yaml phase
id: P12a-docs
spec: F12a
steps: [F12a.2, F12a.4]
owns: [docs/guide/**, README.md, scripts/check-docs.sh, scripts/check-docs-env.sh]
depends_on: [P1a]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 0, reasoning: 1}
rationale: "README et guide en français (dont délivrabilité), exemple Compose et contrôles de documentation"
```

```yaml phase
id: P6c-page
spec: F6c
steps: [F6c.3]
owns: [internal/maildiagpages/**, web/templates/maildiag/**, web/static/css/maildiag/**, tests/e2e/maildiag/**, web/static/i18n/maildiag/**]
depends_on: [P6c-diag, P6c-suppress, P2c-websec, P1c-layout, P9-theme]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "page screen-admin-delivrabilite et tests mobiles"
```

```yaml phase
id: P10b-consult
spec: F10b
steps: [F10b.1]
owns: [internal/adminconsult/**, web/templates/adminconsult/**, tests/e2e/adminconsult/**, web/static/i18n/adminconsult/**]
depends_on: [P4b-core, P4b-reveal, P2-groups, P2c-websec, P1c-layout]
model: opus
score: {scope: 1, ambiguity: 1, risk: 2, reasoning: 1}
rationale: "consultation de paire avec ressaisie et journal public : force opus"
```

```yaml phase
id: P4c-repair
spec: F4c
steps: [F4c.2]
owns: [internal/repair/**]
depends_on: [P4c-adjust, P3-rules, P1b-sh2]
model: opus
score: {scope: 1, ambiguity: 1, risk: 2, reasoning: 2}
rationale: "réparation minimale après règle modifiée : force opus"
```

```yaml phase
id: P5c-mailbox
spec: F5c
steps: [F5c.2]
owns: [internal/mailboxpage/**, web/templates/mailbox/**, web/static/js/mailbox/**, web/static/css/mailbox/**, tests/e2e/mailbox/**, web/static/i18n/mailbox/**]
depends_on: [P1c-layout, P9-theme, P9-illus]
model: sonnet
score: {scope: 1, ambiguity: 1, risk: 0, reasoning: 1}
rationale: "boîte aux lettres animée avec repli prefers-reduced-motion"
```

```yaml phase
id: P5c-pages
spec: F5c
steps: [F5c.1]
owns: [internal/letterpages/**, web/templates/letter/**, web/static/js/letter/**, web/static/css/letter/**, tests/e2e/letter/**, web/static/i18n/letter/**]
depends_on: [P5-letter, P5b-read, P2c-websec, P1c-layout, P9-theme]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "pages d'écriture et de lecture de la lettre"
```

```yaml phase
id: P8c-page
spec: F8c
steps: [F8c.2]
owns: [internal/suggestionpages/**, web/templates/suggestions/**, web/static/js/suggestions/**, web/static/css/suggestions/**, tests/e2e/suggestions/**, web/static/i18n/suggestions/**]
depends_on: [P8c-service, P2c-websec, P1c-layout, P9-theme]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "page des suggestions et test de confidentialité des requêtes sortantes"
```

```yaml phase
id: P11-roleopt
spec: F11
steps: [F11.5]
owns: [internal/registration/invite/**, internal/registration/verify/**]
depends_on: [P11-invite, P11-verify, P3d-roles]
model: opus
score: {scope: 1, ambiguity: 1, risk: 2, reasoning: 1}
rationale: "option de choix de rôle, validation des valeurs, transmission à CreateVerified : sécurité et données, force opus"
```

```yaml phase
id: P10b-ops
spec: F10b
steps: [F10b.2, F10b.3]
owns: [internal/adminops/**, web/templates/adminops/**, tests/e2e/adminops/**, web/static/i18n/adminops/**]
depends_on: [P3-core, P3-rules, P2-groups, P2c-websec, P1b-db, P1c-layout]
model: opus
score: {scope: 2, ambiguity: 1, risk: 2, reasoning: 1}
rationale: "export et import JSON atomiques, sauvegarde, réinitialisation et duplication de groupe avec le rôle dans le tirage : données, force opus"
```

```yaml phase
id: P9c-seq
spec: F9c
steps: [F9c.5]
owns: [web/static/css/reveal/sequence/**, web/static/js/reveal/sequence/**, tests/e2e/reveal/sequence/**, web/templates/reveal/sequence/**, web/static/i18n/reveal/sequence/**]
depends_on: [P9c-gift, P9c-envelope, P9c-scratch]
model: sonnet
score: {scope: 1, ambiguity: 1, risk: 0, reasoning: 2}
rationale: "séquence de deux révélations, styles successifs différents"
```

```yaml phase
id: P7b-notify
spec: F7b
steps: [F7b.1, F7b.2]
owns: [internal/givernotify/**]
depends_on: [P5b-read, P6-queue, P6-templates, P7-rules]
model: sonnet
score: {scope: 1, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "notification regroupée par tireur, deux cibles, réception seule exclue"
```

```yaml phase
id: P4d-roles
spec: F4d
steps: [F4d.3]
owns: [internal/targetpage/**, web/templates/draw/**, web/static/js/draw/**, web/static/css/draw/**, tests/e2e/draw/**, web/static/i18n/draw/**]
depends_on: [P4d-pages, P4b-multi, P3-roles]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "préférence un seul cadeau, carte de cible à une ou deux cibles, accueil réception seule"
```

```yaml phase
id: P11c-public
spec: F11c
steps: [F11c.1]
owns: [internal/registrationpages/public/**, web/templates/registration/public/**, web/static/js/registration/public/**, web/static/css/registration/public/**, tests/e2e/registration/public/**, web/static/i18n/registration/public/**]
depends_on: [P11-invite, P11-code, P11-verify, P2c-websec, P1c-layout, P9-theme]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "pages publiques d'inscription"
```

```yaml phase
id: P4c-roles
spec: F4c
steps: [F4c.3]
owns: [internal/adjust/**, internal/repair/**]
depends_on: [P4c-adjust, P4c-repair, P4b-multi, P3-roles]
model: opus
score: {scope: 2, ambiguity: 1, risk: 2, reasoning: 2}
rationale: "ajustements avec capacités et changement de rôle, verrou des cibles découvertes : sécurité et données, force opus"
```

```yaml phase
id: P5c-roles
spec: F5c
steps: [F5c.3]
owns: [internal/letterpages/**, internal/mailboxpage/**, web/templates/letter/**, web/templates/mailbox/**, web/static/js/letter/**, web/static/js/mailbox/**, web/static/css/letter/**, web/static/css/mailbox/**, tests/e2e/letter/**, tests/e2e/mailbox/**, web/static/i18n/letter/**, web/static/i18n/mailbox/**]
depends_on: [P5c-pages, P5c-mailbox, P5b-read]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "boîte aux lettres et lecture pour deux cibles, boîte de la réception seule"
```

```yaml phase
id: P8c-multi
spec: F8c
steps: [F8c.3]
owns: [internal/suggestions/**, internal/suggestionpages/**, web/templates/suggestions/**, web/static/js/suggestions/**, web/static/css/suggestions/**, tests/e2e/suggestions/**, web/static/i18n/suggestions/**]
depends_on: [P8c-service, P8c-page, P4b-multi]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "suggestions par cadeau pour deux cibles"
```

```yaml phase
id: P10-dash
spec: F10
steps: [F10.1]
owns: [internal/admin/dashboard/**, internal/admin/audit/**, web/templates/admin/dashboard/**, web/static/js/admin/dashboard/**, web/static/css/admin/dashboard/**, tests/e2e/admin/dashboard/**, internal/admin/migrations/**, web/static/i18n/admin/dashboard/**]
depends_on: [P2-groups, P2c-websec, P4b-core, P4b-reveal, P6-queue, P1b-sh2, P1c-layout, P9-theme]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "tableau de bord sans paires et contrôle du tirage"
```

```yaml phase
id: P10-menu
spec: F10
steps: [F10.2]
owns: [internal/admin/menu/**, web/templates/admin/menu/**, web/static/js/admin/menu/**, web/static/css/admin/menu/**, tests/e2e/admin/menu/**, web/static/i18n/admin/menu/**]
depends_on: [P1c-layout, P1b-sh2, P2c-websec, P9-theme]
model: sonnet
score: {scope: 1, ambiguity: 1, risk: 0, reasoning: 1}
rationale: "navigation d'administration et feuille Plus"
```

```yaml phase
id: P10-settings
spec: F10
steps: [F10.5]
owns: [internal/admin/settingspages/**, web/templates/admin/settings/**, web/static/js/admin/settings/**, web/static/css/admin/settings/**, tests/e2e/admin/settings/**, web/static/i18n/admin/settings/**]
depends_on: [P2e-settings, P7-engine, P6-queue, P2c-websec, P1c-layout, P9-theme]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "réglages en sections repliables et relances"
```

```yaml phase
id: P11c-admin
spec: F11c
steps: [F11c.2]
owns: [internal/registrationpages/admin/**, web/templates/registration/admin/**, web/static/js/registration/admin/**, web/static/css/registration/admin/**, tests/e2e/registration/admin/**, tests/e2e/registration/flow/**, web/static/i18n/registration/admin/**]
depends_on: [P11c-public, P11-verify, P2c-websec, P1c-layout, P1b-sh2, P3d-enrol]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "page d'administration des inscriptions et parcours complet"
```

```yaml phase
id: P10-roles
spec: F10
steps: [F10.6]
owns: [internal/admin/roles/**, web/templates/admin/roles/**, web/static/js/admin/roles/**, web/static/css/admin/roles/**, tests/e2e/admin/roles/**, web/static/i18n/admin/roles/**]
depends_on: [P3-roles, P4b-multi]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "sélecteur de rôle, panneau des rôles en lot, récapitulatif en comptes et plafond : fragments inclus par les pages des participants et du tirage"
```

```yaml phase
id: P10-draw
spec: F10
steps: [F10.4]
owns: [internal/admin/drawpages/**, web/templates/admin/draw/**, web/static/js/admin/draw/**, web/static/css/admin/draw/**, tests/e2e/admin/drawpages/**, web/static/i18n/admin/draw/**]
depends_on: [P4b-core, P4b-reveal, P4c-adjust, P2c-websec, P1c-layout, P9-theme, P10-roles]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "pages du tirage et de l'ajustement ; inclut les fragments de rôles de P10-roles"
```

```yaml phase
id: P10-people
spec: F10
steps: [F10.3]
owns: [internal/admin/people/**, web/templates/admin/people/**, web/static/js/admin/people/**, web/static/css/admin/people/**, tests/e2e/admin/people/**, web/static/i18n/admin/people/**]
depends_on: [P3-core, P3-rules, P3d-enrol, P3d-rulechange, P2b-a, P6-unsub, P2c-websec, P1c-layout, P9-theme, P10-roles]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "page des participants et fiche de personne ; inclut les fragments de rôles de P10-roles"
```

```yaml phase
id: P11c-roles
spec: F11c
steps: [F11c.3]
owns: [internal/registrationpages/public/**, web/templates/registration/public/**, web/static/js/registration/public/**, web/static/css/registration/public/**, tests/e2e/registration/public/**, internal/registrationpages/admin/**, web/templates/registration/admin/**, web/static/js/registration/admin/**, web/static/css/registration/admin/**, tests/e2e/registration/admin/**, web/static/i18n/registration/public/**]
depends_on: [P11c-public, P11c-admin, P11-roleopt]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "choix de rôle volontaire dans le formulaire et réglage de l'option côté administration"
```

```yaml phase
id: P12b-cov
spec: F12b
steps: [F12b.3]
owns: [scripts/coverage.sh, scripts/coverage_script_test.sh, scripts/check-image.sh, scripts/check-vuln.sh, scripts/check-workflows.sh, .github/workflows/coverage.yml, .github/workflows/recette.yml]
depends_on: [P12a-bench, P1c-docker, P1c-ci]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "scripts de couverture, d'image et de vulnérabilités, workflows"
```

```yaml phase
id: P12b-jA
spec: F12b
steps: [F12b.1]
owns: [tests/e2e/journey/a/**]
depends_on: [P12a-bench, P2e-pages1, P2e-pages2, P11c-admin, P5c-pages, P5c-mailbox, P10-people, P10-draw, P4d-pages, P9c-gift, P9c-envelope, P9c-scratch, P4b-reveal, P10-menu]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "parcours 01 à 03 contre l'image construite"
```

```yaml phase
id: P12b-jB
spec: F12b
steps: [F12b.2]
owns: [tests/e2e/journey/b/**, scripts/check-no-dialogs.sh]
depends_on: [P12a-bench, P10-draw, P10-settings, P6-unsub, P7-engine, P7b-notify, P10b-consult, P10b-ops, P4c-repair, P8c-page, P5c-pages]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "parcours 04 à 06 : ajustement, désinscription, isolation, sauvegarde et restauration"
```

```yaml phase
id: P12b-jC
spec: F12b
steps: [F12b.6]
owns: [tests/e2e/journey/c/**]
depends_on: [P12a-bench, P10-roles, P11c-roles, P9c-seq, P4d-roles, P5c-roles, P6c-page, P4c-roles, P8c-multi]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 1, reasoning: 1}
rationale: "parcours Playwright 07 (rôles et deux cibles) et 08 (délivrabilité)"
```

```yaml phase
id: P13b-review
spec: F13b
steps: [F13b.1]
owns: [internal/i18n/review/**, web/static/i18n/**]
depends_on: []
model: sonnet
score: {scope: 1, ambiguity: 1, risk: 0, reasoning: 1}
rationale: "relecture des catalogues anglais et tests de qualité : texte et tests simples"
```

```yaml phase
id: P12b-zza
spec: F12b
steps: [F12b.4]
owns: [internal/config/zz_coverage_*_test.go, internal/db/zz_coverage_*_test.go, internal/shared/clock/zz_coverage_*_test.go, internal/shared/scheduler/zz_coverage_*_test.go, internal/shared/random/zz_coverage_*_test.go, internal/shared/sanitize/zz_coverage_*_test.go, internal/shared/errs/zz_coverage_*_test.go, internal/shared/ports/zz_coverage_*_test.go, internal/shared/outbound/zz_coverage_*_test.go, internal/shared/adminnav/zz_coverage_*_test.go, internal/health/zz_coverage_*_test.go, internal/groups/zz_coverage_*_test.go, internal/access/token/zz_coverage_*_test.go, internal/access/magic/zz_coverage_*_test.go, internal/access/limiter/zz_coverage_*_test.go, internal/websec/zz_coverage_*_test.go, internal/groupsettings/zz_coverage_*_test.go, internal/participants/zz_coverage_*_test.go, internal/rules/zz_coverage_*_test.go, internal/enrolment/zz_coverage_*_test.go, internal/rulechange/zz_coverage_*_test.go, internal/mail/transport/zz_coverage_*_test.go, internal/mail/queue/zz_coverage_*_test.go, internal/mail/templates/zz_coverage_*_test.go, internal/mailunsub/zz_coverage_*_test.go, internal/ssrf/zz_coverage_*_test.go, internal/sources/zz_coverage_*_test.go, internal/enrich/zz_coverage_*_test.go, internal/access/sqlstore/zz_coverage_*_test.go]
depends_on: [P12b-cov, P2b-store]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 0, reasoning: 1}
rationale: "tests de comblement de couverture : socle et services"
```

```yaml phase
id: P12b-zzb
spec: F12b
steps: [F12b.5]
owns: [internal/drawengine/zz_coverage_*_test.go, internal/draw/zz_coverage_*_test.go, internal/reveal/zz_coverage_*_test.go, internal/adjust/zz_coverage_*_test.go, internal/repair/zz_coverage_*_test.go, internal/targetpage/zz_coverage_*_test.go, internal/letter/zz_coverage_*_test.go, internal/letterread/zz_coverage_*_test.go, internal/letterpages/zz_coverage_*_test.go, internal/mailboxpage/zz_coverage_*_test.go, internal/reminders/zz_coverage_*_test.go, internal/reminderrules/zz_coverage_*_test.go, internal/givernotify/zz_coverage_*_test.go, internal/suggestions/zz_coverage_*_test.go, internal/suggestionpages/zz_coverage_*_test.go, internal/grouppages/zz_coverage_*_test.go, internal/accesspages/zz_coverage_*_test.go, internal/admin/dashboard/zz_coverage_*_test.go, internal/admin/audit/zz_coverage_*_test.go, internal/admin/menu/zz_coverage_*_test.go, internal/admin/people/zz_coverage_*_test.go, internal/admin/drawpages/zz_coverage_*_test.go, internal/admin/settingspages/zz_coverage_*_test.go, internal/adminconsult/zz_coverage_*_test.go, internal/adminops/zz_coverage_*_test.go, internal/registration/invite/zz_coverage_*_test.go, internal/registration/code/zz_coverage_*_test.go, internal/registration/verify/zz_coverage_*_test.go, internal/registrationpages/public/zz_coverage_*_test.go, internal/registrationpages/admin/zz_coverage_*_test.go, internal/registration/sqlstore/zz_coverage_*_test.go]
depends_on: [P12b-cov, P11-store]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 0, reasoning: 1}
rationale: "tests de comblement de couverture : domaine, pages et administration"
```

```yaml phase
id: P12b-zzc
spec: F12b
steps: [F12b.7]
owns: [internal/mailmsg/zz_coverage_*_test.go, internal/maildkim/zz_coverage_*_test.go, internal/maildiag/zz_coverage_*_test.go, internal/mailsuppress/zz_coverage_*_test.go, internal/maildiagpages/zz_coverage_*_test.go]
depends_on: [P12b-cov, P12a-bench, P6b-msg, P6b-dkim, P6c-diag, P6c-suppress, P6c-page]
model: sonnet
score: {scope: 2, ambiguity: 1, risk: 0, reasoning: 1}
rationale: "score antispam des modèles avec spamcheck et comblement de couverture des paquets de délivrabilité"
```

## Four graphs

### Dependency graph

| Phase | Depends on |
|---|---|
| P1a | none |
| P1b-db | none |
| P1b-sh1 | none |
| P1b-sh2 | none |
| P1c-ci | none (+ toutes les phases de F1, F1b par la règle de fonctionnalité) |
| P1c-entry | P1a, P1b-db (+ toutes les phases de F1, F1b par la règle de fonctionnalité) |
| P1c-infra | none (+ toutes les phases de F1, F1b par la règle de fonctionnalité) |
| P2-groups | P1a, P1b-db, P1b-sh1 (+ toutes les phases de F1, F1b par la règle de fonctionnalité) |
| P2b-a | P1b-db, P1b-sh1 (+ toutes les phases de F1b par la règle de fonctionnalité) |
| P2b-b | P1b-db, P1b-sh1, P1b-sh2 (+ toutes les phases de F1b par la règle de fonctionnalité) |
| P2c-websec | P1a, P1b-sh1 (+ toutes les phases de F1, F1b par la règle de fonctionnalité) |
| P4-engine | P1b-sh1 (+ toutes les phases de F1b par la règle de fonctionnalité) |
| P1c-docker | P1c-entry (+ toutes les phases de F1, F1b par la règle de fonctionnalité) |
| P1c-layout | P1c-infra, P1b-sh2 (+ toutes les phases de F1, F1b par la règle de fonctionnalité) |
| P3-core | P2-groups, P1b-db, P1b-sh1 (+ toutes les phases de F2, F1b par la règle de fonctionnalité) |
| P3-rules | P1b-db (+ toutes les phases de F2, F1b par la règle de fonctionnalité) |
| P6b-msg | P1a (+ toutes les phases de F1, F1b par la règle de fonctionnalité) |
| P6b-dkim | P1a (+ toutes les phases de F1, F1b par la règle de fonctionnalité) |
| P2b-store | P2b-a, P2b-b, P1b-db (+ toutes les phases de F1b par la règle de fonctionnalité) |
| P13-catalog | none (+ toutes les phases de F1, F1b, F2c par la règle de fonctionnalité) |
| P5-letter | P1b-db, P1b-sh1, P1b-sh2 (+ toutes les phases de F1b par la règle de fonctionnalité) |
| P8-ssrf | P1b-sh2 (+ toutes les phases de F1b par la règle de fonctionnalité) |
| P9-illus | P1c-infra (+ toutes les phases de F1c par la règle de fonctionnalité) |
| P9-theme | P1c-infra, P1c-layout (+ toutes les phases de F1c par la règle de fonctionnalité) |
| P3-roles | P3-core, P3-rules (+ toutes les phases de F2, F1b par la règle de fonctionnalité) |
| P13-negotiate | P2c-websec (+ toutes les phases de F1, F1b, F2c par la règle de fonctionnalité) |
| P13-guard | none (+ toutes les phases de F1, F1b, F2c par la règle de fonctionnalité) |
| P13-config | P1a, P1b-sh1 (+ toutes les phases de F1, F1b, F2c par la règle de fonctionnalité) |
| P6-queue | P1b-db, P1b-sh1, P1b-sh2 (+ toutes les phases de F1b, F1c, F1, F2c, F6b, F13 par la règle de fonctionnalité) |
| P6-templates | P1b-sh2, P6b-msg (+ toutes les phases de F1b, F1c, F1, F2c, F6b, F13 par la règle de fonctionnalité) |
| P3d-enrol | P3-core, P2b-a, P1b-sh2 (+ toutes les phases de F3, F2b, F1b par la règle de fonctionnalité) |
| P3d-rulechange | P3-rules, P1b-sh2 (+ toutes les phases de F3, F2b, F1b par la règle de fonctionnalité) |
| P4b-core | P3-core, P3-rules, P4-engine, P1b-db, P1b-sh2, P2-groups (+ toutes les phases de F3, F4, F2, F1b par la règle de fonctionnalité) |
| P2e-pages1 | P2-groups, P2c-websec, P1c-layout, P9-theme (+ toutes les phases de F2, F2b, F2c, F1c, F9, F1b, F13 par la règle de fonctionnalité) |
| P2e-pages2 | P2-groups, P2b-a, P2b-b, P2c-websec, P1c-layout, P9-theme (+ toutes les phases de F2, F2b, F2c, F1c, F9, F1b, F13 par la règle de fonctionnalité) |
| P2e-settings | P2-groups, P1b-sh2 (+ toutes les phases de F2, F2b, F2c, F1c, F9, F1b, F13 par la règle de fonctionnalité) |
| P6-transport | P1a, P1b-sh1, P6b-msg, P6b-dkim (+ toutes les phases de F1b, F1c, F1, F2c, F6b, F13 par la règle de fonctionnalité) |
| P6-unsub | P6-queue, P6-templates, P2c-websec, P1c-layout, P1b-sh1 (+ toutes les phases de F1b, F1c, F1, F2c, F6b, F13 par la règle de fonctionnalité) |
| P8b-enrich | P8-ssrf (+ toutes les phases de F8, F1b, F1 par la règle de fonctionnalité) |
| P12a-bench | P1c-docker, P1c-infra (+ toutes les phases de F1c, F1, F13 par la règle de fonctionnalité) |
| P4b-reveal | P4b-core, P1b-sh1, P1b-sh2 (+ toutes les phases de F3, F4, F2, F1b par la règle de fonctionnalité) |
| P8b-sources | P8-ssrf, P1b-sh2, P1a (+ toutes les phases de F8, F1b, F1 par la règle de fonctionnalité) |
| P9c-core | P9-theme, P1c-infra, P1c-layout (+ toutes les phases de F9, F1c, F13 par la règle de fonctionnalité) |
| P3d-roles | P3d-enrol, P3d-rulechange, P3-roles (+ toutes les phases de F3, F2b, F1b par la règle de fonctionnalité) |
| P7-rules | P1a, P1b-sh1 (+ toutes les phases de F6, F3, F5, F1, F1b par la règle de fonctionnalité) |
| P6c-diag | P1a (+ toutes les phases de F6, F6b, F2c, F1c, F9, F1b, F1, F13 par la règle de fonctionnalité) |
| P6c-suppress | P1b-db, P1b-sh2 (+ toutes les phases de F6, F6b, F2c, F1c, F9, F1b, F1, F13 par la règle de fonctionnalité) |
| P11-code | P2b-a, P2b-b, P1b-sh1, P1b-sh2 (+ toutes les phases de F2, F2b, F3, F3d, F1b par la règle de fonctionnalité) |
| P11-invite | P2-groups, P2b-a, P2b-b, P3-core, P1b-sh1, P1b-sh2 (+ toutes les phases de F2, F2b, F3, F3d, F1b par la règle de fonctionnalité) |
| P9c-envelope | P9c-core (+ toutes les phases de F9, F1c, F13 par la règle de fonctionnalité) |
| P9c-gift | P9c-core (+ toutes les phases de F9, F1c, F13 par la règle de fonctionnalité) |
| P4b-multi | P4b-core, P4b-reveal, P3-roles, P4-engine (+ toutes les phases de F3, F4, F2, F1b par la règle de fonctionnalité) |
| P7-engine | P7-rules, P6-queue, P6-unsub, P3-core, P5-letter, P1b-sh1 (+ toutes les phases de F6, F3, F5, F1, F1b par la règle de fonctionnalité) |
| P9c-scratch | P9c-core (+ toutes les phases de F9, F1c, F13 par la règle de fonctionnalité) |
| P11-verify | P11-invite, P11-code, P3d-enrol, P3-core, P1b-sh1, P1b-sh2 (+ toutes les phases de F2, F2b, F3, F3d, F1b par la règle de fonctionnalité) |
| P4c-adjust | P4b-core, P4b-reveal, P3-core, P2e-settings, P1b-sh2 (+ toutes les phases de F4b, F2e, F3, F1b par la règle de fonctionnalité) |
| P5b-read | P5-letter, P4b-core, P4b-reveal (+ toutes les phases de F5, F4b par la règle de fonctionnalité) |
| P4d-pages | P4b-reveal, P2e-settings, P2c-websec, P1c-layout, P9-theme (+ toutes les phases de F4b, F2e, F2c, F1c, F9, F3, F13 par la règle de fonctionnalité) |
| P8c-service | P8b-sources, P8b-enrich, P2-groups, P3-core, P4b-core, P5-letter (+ toutes les phases de F8b, F2, F3, F4b, F5, F2c, F1c, F9, F13 par la règle de fonctionnalité) |
| P11-store | P11-invite, P11-code, P1b-db (+ toutes les phases de F2, F2b, F3, F3d, F1b par la règle de fonctionnalité) |
| P12a-docs | P1a (+ toutes les phases de F1c, F1, F13 par la règle de fonctionnalité) |
| P6c-page | P6c-diag, P6c-suppress, P2c-websec, P1c-layout, P9-theme (+ toutes les phases de F6, F6b, F2c, F1c, F9, F1b, F1, F13 par la règle de fonctionnalité) |
| P10b-consult | P4b-core, P4b-reveal, P2-groups, P2c-websec, P1c-layout (+ toutes les phases de F2, F2c, F3, F4b, F6, F1c, F1b, F13 par la règle de fonctionnalité) |
| P4c-repair | P4c-adjust, P3-rules, P1b-sh2 (+ toutes les phases de F4b, F2e, F3, F1b par la règle de fonctionnalité) |
| P5c-mailbox | P1c-layout, P9-theme, P9-illus (+ toutes les phases de F5, F5b, F2c, F1c, F9, F13 par la règle de fonctionnalité) |
| P5c-pages | P5-letter, P5b-read, P2c-websec, P1c-layout, P9-theme (+ toutes les phases de F5, F5b, F2c, F1c, F9, F13 par la règle de fonctionnalité) |
| P8c-page | P8c-service, P2c-websec, P1c-layout, P9-theme (+ toutes les phases de F8b, F2, F3, F4b, F5, F2c, F1c, F9, F13 par la règle de fonctionnalité) |
| P11-roleopt | P11-invite, P11-verify, P3d-roles (+ toutes les phases de F2, F2b, F3, F3d, F1b par la règle de fonctionnalité) |
| P10b-ops | P3-core, P3-rules, P2-groups, P2c-websec, P1b-db, P1c-layout (+ toutes les phases de F2, F2c, F3, F4b, F6, F1c, F1b, F13 par la règle de fonctionnalité) |
| P9c-seq | P9c-gift, P9c-envelope, P9c-scratch (+ toutes les phases de F9, F1c, F13 par la règle de fonctionnalité) |
| P7b-notify | P5b-read, P6-queue, P6-templates, P7-rules (+ toutes les phases de F5b, F6, F7 par la règle de fonctionnalité) |
| P4d-roles | P4d-pages, P4b-multi, P3-roles (+ toutes les phases de F4b, F2e, F2c, F1c, F9, F3, F13 par la règle de fonctionnalité) |
| P11c-public | P11-invite, P11-code, P11-verify, P2c-websec, P1c-layout, P9-theme (+ toutes les phases de F11, F2c, F1c, F9, F1b, F3d, F13 par la règle de fonctionnalité) |
| P4c-roles | P4c-adjust, P4c-repair, P4b-multi, P3-roles (+ toutes les phases de F4b, F2e, F3, F1b par la règle de fonctionnalité) |
| P5c-roles | P5c-pages, P5c-mailbox, P5b-read (+ toutes les phases de F5, F5b, F2c, F1c, F9, F13 par la règle de fonctionnalité) |
| P8c-multi | P8c-service, P8c-page, P4b-multi (+ toutes les phases de F8b, F2, F3, F4b, F5, F2c, F1c, F9, F13 par la règle de fonctionnalité) |
| P10-dash | P2-groups, P2c-websec, P4b-core, P4b-reveal, P6-queue, P1b-sh2, P1c-layout, P9-theme (+ toutes les phases de F2, F2b, F2c, F2e, F3, F3d, F4b, F4c, F6, F7, F1c, F9, F1b, F13 par la règle de fonctionnalité) |
| P10-menu | P1c-layout, P1b-sh2, P2c-websec, P9-theme (+ toutes les phases de F2, F2b, F2c, F2e, F3, F3d, F4b, F4c, F6, F7, F1c, F9, F1b, F13 par la règle de fonctionnalité) |
| P10-settings | P2e-settings, P7-engine, P6-queue, P2c-websec, P1c-layout, P9-theme (+ toutes les phases de F2, F2b, F2c, F2e, F3, F3d, F4b, F4c, F6, F7, F1c, F9, F1b, F13 par la règle de fonctionnalité) |
| P11c-admin | P11c-public, P11-verify, P2c-websec, P1c-layout, P1b-sh2, P3d-enrol (+ toutes les phases de F11, F2c, F1c, F9, F1b, F3d, F13 par la règle de fonctionnalité) |
| P10-roles | P3-roles, P4b-multi (+ toutes les phases de F2, F2b, F2c, F2e, F3, F3d, F4b, F4c, F6, F7, F1c, F9, F1b, F13 par la règle de fonctionnalité) |
| P10-draw | P4b-core, P4b-reveal, P4c-adjust, P2c-websec, P1c-layout, P9-theme, P10-roles (+ toutes les phases de F2, F2b, F2c, F2e, F3, F3d, F4b, F4c, F6, F7, F1c, F9, F1b, F13 par la règle de fonctionnalité) |
| P10-people | P3-core, P3-rules, P3d-enrol, P3d-rulechange, P2b-a, P6-unsub, P2c-websec, P1c-layout, P9-theme, P10-roles (+ toutes les phases de F2, F2b, F2c, F2e, F3, F3d, F4b, F4c, F6, F7, F1c, F9, F1b, F13 par la règle de fonctionnalité) |
| P11c-roles | P11c-public, P11c-admin, P11-roleopt (+ toutes les phases de F11, F2c, F1c, F9, F1b, F3d, F13 par la règle de fonctionnalité) |
| P12b-cov | P12a-bench, P1c-docker, P1c-ci (+ toutes les phases de F12a, F2e, F4d, F5c, F6, F7b, F8c, F9c, F10, F10b, F11c, F4b, F7, F4c, F1c, F6b, F6c, F2b, F11, F13 par la règle de fonctionnalité) |
| P12b-jA | P12a-bench, P2e-pages1, P2e-pages2, P11c-admin, P5c-pages, P5c-mailbox, P10-people, P10-draw, P4d-pages, P9c-gift, P9c-envelope, P9c-scratch, P4b-reveal, P10-menu (+ toutes les phases de F12a, F2e, F4d, F5c, F6, F7b, F8c, F9c, F10, F10b, F11c, F4b, F7, F4c, F1c, F6b, F6c, F2b, F11, F13 par la règle de fonctionnalité) |
| P12b-jB | P12a-bench, P10-draw, P10-settings, P6-unsub, P7-engine, P7b-notify, P10b-consult, P10b-ops, P4c-repair, P8c-page, P5c-pages (+ toutes les phases de F12a, F2e, F4d, F5c, F6, F7b, F8c, F9c, F10, F10b, F11c, F4b, F7, F4c, F1c, F6b, F6c, F2b, F11, F13 par la règle de fonctionnalité) |
| P12b-jC | P12a-bench, P10-roles, P11c-roles, P9c-seq, P4d-roles, P5c-roles, P6c-page, P4c-roles, P8c-multi (+ toutes les phases de F12a, F2e, F4d, F5c, F6, F7b, F8c, F9c, F10, F10b, F11c, F4b, F7, F4c, F1c, F6b, F6c, F2b, F11, F13 par la règle de fonctionnalité) |
| P13b-review | none (+ toutes les phases de F13, F1c, F2e, F4d, F5c, F6, F6c, F8c, F9c, F10, F10b, F11c par la règle de fonctionnalité) |
| P12b-zza | P12b-cov, P2b-store (+ toutes les phases de F12a, F2e, F4d, F5c, F6, F7b, F8c, F9c, F10, F10b, F11c, F4b, F7, F4c, F1c, F6b, F6c, F2b, F11, F13 par la règle de fonctionnalité) |
| P12b-zzb | P12b-cov, P11-store (+ toutes les phases de F12a, F2e, F4d, F5c, F6, F7b, F8c, F9c, F10, F10b, F11c, F4b, F7, F4c, F1c, F6b, F6c, F2b, F11, F13 par la règle de fonctionnalité) |
| P12b-zzc | P12b-cov, P12a-bench, P6b-msg, P6b-dkim, P6c-diag, P6c-suppress, P6c-page (+ toutes les phases de F12a, F2e, F4d, F5c, F6, F7b, F8c, F9c, F10, F10b, F11c, F4b, F7, F4c, F1c, F6b, F6c, F2b, F11, F13 par la règle de fonctionnalité) |

### Ownership graph

| Phase | Owns |
|---|---|
| P1a | internal/config/**, .env.example |
| P1b-db | internal/db/** |
| P1b-sh1 | internal/shared/clock/**, internal/shared/scheduler/**, internal/shared/random/**, internal/shared/sanitize/**, internal/shared/errs/** |
| P1b-sh2 | internal/shared/ports/**, internal/shared/outbound/**, internal/shared/adminnav/** |
| P1c-ci | cmd/ci/**, .github/workflows/ci.yml, VERSION, Makefile |
| P1c-entry | cmd/secretsanta/**, internal/health/** |
| P1c-infra | tests/e2e/package.json, tests/e2e/package-lock.json, tests/e2e/playwright.config.ts, tests/e2e/_shared/** |
| P2-groups | internal/groups/** |
| P2b-a | internal/access/token/** |
| P2b-b | internal/access/magic/**, internal/access/limiter/** |
| P2c-websec | internal/websec/** |
| P4-engine | internal/drawengine/** |
| P1c-docker | Dockerfile, .dockerignore, docker-compose.yml, tests/e2e/docker/** |
| P1c-layout | web/embed.go, web/templates/layout/**, web/static/css/base/**, tests/e2e/foundation/**, web/static/i18n/layout/**, web/embed_test.go |
| P3-core | internal/participants/** |
| P3-rules | internal/rules/** |
| P6b-msg | internal/mailmsg/** |
| P6b-dkim | internal/maildkim/** |
| P2b-store | internal/access/migrations/**, internal/access/sqlstore/**, internal/access/token/** |
| P13-catalog | internal/i18n/catalog/**, internal/i18n/cli/** |
| P5-letter | internal/letter/** |
| P8-ssrf | internal/ssrf/** |
| P9-illus | web/static/assets/**, web/static/js/avatars/**, tests/e2e/illustrations/** |
| P9-theme | web/static/css/theme/**, web/static/fonts/**, web/static/js/theme/**, web/static/manifest.webmanifest, web/static/sw.js, tests/e2e/theme/** |
| P3-roles | internal/participants/roles/** |
| P13-negotiate | internal/i18n/negotiate/**, internal/i18n/web/**, web/templates/i18n/**, web/static/css/i18n/**, web/static/js/i18n/**, web/static/i18n/i18n/**, tests/e2e/i18n/**, tests/e2e/foundation/i18n.spec.ts |
| P13-guard | internal/i18n/guard/** |
| P13-config | internal/config/**, .env.example, internal/shared/errs/**, web/static/i18n/core/** |
| P6-queue | internal/mail/queue/**, internal/mail/migrations/** |
| P6-templates | internal/mail/templates/**, web/static/i18n/mail/templates/** |
| P3d-enrol | internal/enrolment/** |
| P3d-rulechange | internal/rulechange/** |
| P4b-core | internal/draw/** |
| P2e-pages1 | internal/grouppages/**, web/templates/groups/**, web/static/js/groups/**, web/static/css/groups/**, tests/e2e/groups/**, web/static/i18n/groups/** |
| P2e-pages2 | internal/accesspages/**, web/templates/access/**, web/static/js/access/**, web/static/css/access/**, tests/e2e/access/**, web/static/i18n/access/** |
| P2e-settings | internal/groupsettings/** |
| P6-transport | internal/mail/transport/** |
| P6-unsub | internal/mailunsub/**, web/templates/mail/**, web/static/css/mail/**, tests/e2e/mail/**, web/static/i18n/mail/unsubscribe/** |
| P8b-enrich | internal/enrich/** |
| P12a-bench | docker-compose.test.yml, scripts/e2e.sh, scripts/check-no-test-secrets.sh, tests/e2e/mail-sink/**, scripts/spamcheck.sh, tests/e2e/spamassassin/** |
| P4b-reveal | internal/reveal/** |
| P8b-sources | internal/sources/** |
| P9c-core | web/static/css/reveal/core/**, web/static/js/reveal/core/**, web/templates/reveal/**, tests/e2e/reveal/core/**, web/static/i18n/reveal/core/** |
| P3d-roles | internal/rulechange/**, internal/enrolment/** |
| P7-rules | internal/reminderrules/** |
| P6c-diag | internal/maildiag/** |
| P6c-suppress | internal/mailsuppress/** |
| P11-code | internal/registration/code/** |
| P11-invite | internal/registration/invite/** |
| P9c-envelope | web/static/css/reveal/envelope/**, web/static/js/reveal/envelope/**, tests/e2e/reveal/envelope/**, web/templates/reveal/envelope/**, web/static/i18n/reveal/envelope/** |
| P9c-gift | web/static/css/reveal/gift/**, web/static/js/reveal/gift/**, tests/e2e/reveal/gift/**, web/templates/reveal/gift/**, web/static/i18n/reveal/gift/** |
| P4b-multi | internal/draw/**, internal/reveal/** |
| P7-engine | internal/reminders/** |
| P9c-scratch | web/static/css/reveal/scratch/**, web/static/js/reveal/scratch/**, tests/e2e/reveal/scratch/**, web/templates/reveal/scratch/**, web/static/i18n/reveal/scratch/** |
| P11-verify | internal/registration/verify/** |
| P4c-adjust | internal/adjust/** |
| P5b-read | internal/letterread/** |
| P4d-pages | internal/targetpage/**, web/templates/draw/**, web/static/js/draw/**, web/static/css/draw/**, tests/e2e/draw/**, web/static/i18n/draw/** |
| P8c-service | internal/suggestions/** |
| P11-store | internal/registration/migrations/**, internal/registration/sqlstore/** |
| P12a-docs | docs/guide/**, README.md, scripts/check-docs.sh, scripts/check-docs-env.sh |
| P6c-page | internal/maildiagpages/**, web/templates/maildiag/**, web/static/css/maildiag/**, tests/e2e/maildiag/**, web/static/i18n/maildiag/** |
| P10b-consult | internal/adminconsult/**, web/templates/adminconsult/**, tests/e2e/adminconsult/**, web/static/i18n/adminconsult/** |
| P4c-repair | internal/repair/** |
| P5c-mailbox | internal/mailboxpage/**, web/templates/mailbox/**, web/static/js/mailbox/**, web/static/css/mailbox/**, tests/e2e/mailbox/**, web/static/i18n/mailbox/** |
| P5c-pages | internal/letterpages/**, web/templates/letter/**, web/static/js/letter/**, web/static/css/letter/**, tests/e2e/letter/**, web/static/i18n/letter/** |
| P8c-page | internal/suggestionpages/**, web/templates/suggestions/**, web/static/js/suggestions/**, web/static/css/suggestions/**, tests/e2e/suggestions/**, web/static/i18n/suggestions/** |
| P11-roleopt | internal/registration/invite/**, internal/registration/verify/** |
| P10b-ops | internal/adminops/**, web/templates/adminops/**, tests/e2e/adminops/**, web/static/i18n/adminops/** |
| P9c-seq | web/static/css/reveal/sequence/**, web/static/js/reveal/sequence/**, tests/e2e/reveal/sequence/**, web/templates/reveal/sequence/**, web/static/i18n/reveal/sequence/** |
| P7b-notify | internal/givernotify/** |
| P4d-roles | internal/targetpage/**, web/templates/draw/**, web/static/js/draw/**, web/static/css/draw/**, tests/e2e/draw/**, web/static/i18n/draw/** |
| P11c-public | internal/registrationpages/public/**, web/templates/registration/public/**, web/static/js/registration/public/**, web/static/css/registration/public/**, tests/e2e/registration/public/**, web/static/i18n/registration/public/** |
| P4c-roles | internal/adjust/**, internal/repair/** |
| P5c-roles | internal/letterpages/**, internal/mailboxpage/**, web/templates/letter/**, web/templates/mailbox/**, web/static/js/letter/**, web/static/js/mailbox/**, web/static/css/letter/**, web/static/css/mailbox/**, tests/e2e/letter/**, tests/e2e/mailbox/**, web/static/i18n/letter/**, web/static/i18n/mailbox/** |
| P8c-multi | internal/suggestions/**, internal/suggestionpages/**, web/templates/suggestions/**, web/static/js/suggestions/**, web/static/css/suggestions/**, tests/e2e/suggestions/**, web/static/i18n/suggestions/** |
| P10-dash | internal/admin/dashboard/**, internal/admin/audit/**, web/templates/admin/dashboard/**, web/static/js/admin/dashboard/**, web/static/css/admin/dashboard/**, tests/e2e/admin/dashboard/**, internal/admin/migrations/**, web/static/i18n/admin/dashboard/** |
| P10-menu | internal/admin/menu/**, web/templates/admin/menu/**, web/static/js/admin/menu/**, web/static/css/admin/menu/**, tests/e2e/admin/menu/**, web/static/i18n/admin/menu/** |
| P10-settings | internal/admin/settingspages/**, web/templates/admin/settings/**, web/static/js/admin/settings/**, web/static/css/admin/settings/**, tests/e2e/admin/settings/**, web/static/i18n/admin/settings/** |
| P11c-admin | internal/registrationpages/admin/**, web/templates/registration/admin/**, web/static/js/registration/admin/**, web/static/css/registration/admin/**, tests/e2e/registration/admin/**, tests/e2e/registration/flow/**, web/static/i18n/registration/admin/** |
| P10-roles | internal/admin/roles/**, web/templates/admin/roles/**, web/static/js/admin/roles/**, web/static/css/admin/roles/**, tests/e2e/admin/roles/**, web/static/i18n/admin/roles/** |
| P10-draw | internal/admin/drawpages/**, web/templates/admin/draw/**, web/static/js/admin/draw/**, web/static/css/admin/draw/**, tests/e2e/admin/drawpages/**, web/static/i18n/admin/draw/** |
| P10-people | internal/admin/people/**, web/templates/admin/people/**, web/static/js/admin/people/**, web/static/css/admin/people/**, tests/e2e/admin/people/**, web/static/i18n/admin/people/** |
| P11c-roles | internal/registrationpages/public/**, web/templates/registration/public/**, web/static/js/registration/public/**, web/static/css/registration/public/**, tests/e2e/registration/public/**, internal/registrationpages/admin/**, web/templates/registration/admin/**, web/static/js/registration/admin/**, web/static/css/registration/admin/**, tests/e2e/registration/admin/**, web/static/i18n/registration/public/** |
| P12b-cov | scripts/coverage.sh, scripts/coverage_script_test.sh, scripts/check-image.sh, scripts/check-vuln.sh, scripts/check-workflows.sh, .github/workflows/coverage.yml, .github/workflows/recette.yml |
| P12b-jA | tests/e2e/journey/a/** |
| P12b-jB | tests/e2e/journey/b/**, scripts/check-no-dialogs.sh |
| P12b-jC | tests/e2e/journey/c/** |
| P13b-review | internal/i18n/review/**, web/static/i18n/** |
| P12b-zza | internal/config/zz_coverage_*_test.go, internal/db/zz_coverage_*_test.go, internal/shared/clock/zz_coverage_*_test.go, internal/shared/scheduler/zz_coverage_*_test.go, internal/shared/random/zz_coverage_*_test.go, internal/shared/sanitize/zz_coverage_*_test.go, internal/shared/errs/zz_coverage_*_test.go, internal/shared/ports/zz_coverage_*_test.go, internal/shared/outbound/zz_coverage_*_test.go, internal/shared/adminnav/zz_coverage_*_test.go, internal/health/zz_coverage_*_test.go, internal/groups/zz_coverage_*_test.go, internal/access/token/zz_coverage_*_test.go, internal/access/magic/zz_coverage_*_test.go, internal/access/limiter/zz_coverage_*_test.go, internal/websec/zz_coverage_*_test.go, internal/groupsettings/zz_coverage_*_test.go, internal/participants/zz_coverage_*_test.go, internal/rules/zz_coverage_*_test.go, internal/enrolment/zz_coverage_*_test.go, internal/rulechange/zz_coverage_*_test.go, internal/mail/transport/zz_coverage_*_test.go, internal/mail/queue/zz_coverage_*_test.go, internal/mail/templates/zz_coverage_*_test.go, internal/mailunsub/zz_coverage_*_test.go, internal/ssrf/zz_coverage_*_test.go, internal/sources/zz_coverage_*_test.go, internal/enrich/zz_coverage_*_test.go, internal/access/sqlstore/zz_coverage_*_test.go |
| P12b-zzb | internal/drawengine/zz_coverage_*_test.go, internal/draw/zz_coverage_*_test.go, internal/reveal/zz_coverage_*_test.go, internal/adjust/zz_coverage_*_test.go, internal/repair/zz_coverage_*_test.go, internal/targetpage/zz_coverage_*_test.go, internal/letter/zz_coverage_*_test.go, internal/letterread/zz_coverage_*_test.go, internal/letterpages/zz_coverage_*_test.go, internal/mailboxpage/zz_coverage_*_test.go, internal/reminders/zz_coverage_*_test.go, internal/reminderrules/zz_coverage_*_test.go, internal/givernotify/zz_coverage_*_test.go, internal/suggestions/zz_coverage_*_test.go, internal/suggestionpages/zz_coverage_*_test.go, internal/grouppages/zz_coverage_*_test.go, internal/accesspages/zz_coverage_*_test.go, internal/admin/dashboard/zz_coverage_*_test.go, internal/admin/audit/zz_coverage_*_test.go, internal/admin/menu/zz_coverage_*_test.go, internal/admin/people/zz_coverage_*_test.go, internal/admin/drawpages/zz_coverage_*_test.go, internal/admin/settingspages/zz_coverage_*_test.go, internal/adminconsult/zz_coverage_*_test.go, internal/adminops/zz_coverage_*_test.go, internal/registration/invite/zz_coverage_*_test.go, internal/registration/code/zz_coverage_*_test.go, internal/registration/verify/zz_coverage_*_test.go, internal/registrationpages/public/zz_coverage_*_test.go, internal/registrationpages/admin/zz_coverage_*_test.go, internal/registration/sqlstore/zz_coverage_*_test.go |
| P12b-zzc | internal/mailmsg/zz_coverage_*_test.go, internal/maildkim/zz_coverage_*_test.go, internal/maildiag/zz_coverage_*_test.go, internal/mailsuppress/zz_coverage_*_test.go, internal/maildiagpages/zz_coverage_*_test.go |

### Context graph

| Phase | Reads |
|---|---|
| P1a | aucun |
| P1b-db | internal/config/** |
| P1b-sh1 | internal/config/** |
| P1b-sh2 | internal/config/** |
| P1c-ci | internal/config/**, internal/db/**, internal/shared/** |
| P1c-entry | internal/config/**, internal/db/**, internal/shared/** |
| P1c-infra | internal/config/**, internal/db/**, internal/shared/** |
| P2-groups | internal/shared/**, internal/config/**, internal/db/** |
| P2b-a | internal/shared/**, internal/db/** |
| P2b-b | internal/shared/**, internal/db/** |
| P2c-websec | internal/shared/**, internal/config/** |
| P4-engine | internal/shared/** |
| P1c-docker | internal/config/**, internal/db/**, internal/shared/** |
| P1c-layout | internal/config/**, internal/db/**, internal/shared/** |
| P3-core | internal/groups/**, internal/shared/**, internal/db/** |
| P3-rules | internal/groups/**, internal/shared/**, internal/db/** |
| P6b-msg | internal/config/**, internal/shared/** |
| P6b-dkim | internal/config/**, internal/shared/** |
| P2b-store | internal/shared/**, internal/db/** |
| P13-catalog | internal/shared/**, internal/websec/**, internal/app/**, web/embed.go, web/templates/layout/**, web/static/i18n/core/**, tests/e2e/_shared/** |
| P5-letter | internal/shared/**, internal/db/** |
| P8-ssrf | internal/shared/** |
| P9-illus | docs/waves/design/snapshot.html#tokens |
| P9-theme | docs/waves/design/snapshot.html#tokens |
| P3-roles | internal/groups/**, internal/shared/**, internal/db/** |
| P13-negotiate | internal/shared/**, internal/websec/**, internal/app/**, web/embed.go, web/templates/layout/**, web/static/i18n/core/**, tests/e2e/_shared/** |
| P13-guard | internal/shared/**, internal/websec/**, internal/app/**, web/embed.go, web/templates/layout/**, web/static/i18n/core/**, tests/e2e/_shared/** |
| P13-config | internal/shared/**, internal/websec/**, internal/app/**, web/embed.go, web/templates/layout/**, web/static/i18n/core/**, tests/e2e/_shared/** |
| P6-queue | internal/config/**, internal/shared/**, internal/db/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, docs/waves/design/snapshot.html#screen-desinscription, internal/mailmsg/**, internal/maildkim/** |
| P6-templates | internal/config/**, internal/shared/**, internal/db/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, docs/waves/design/snapshot.html#screen-desinscription, internal/mailmsg/**, internal/maildkim/** |
| P3d-enrol | internal/participants/**, internal/rules/**, internal/access/**, internal/shared/** |
| P3d-rulechange | internal/participants/**, internal/rules/**, internal/access/**, internal/shared/** |
| P4b-core | internal/drawengine/**, internal/participants/**, internal/rules/**, internal/groups/**, internal/shared/**, internal/db/** |
| P2e-pages1 | internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/groups/**, internal/access/**, internal/websec/**, docs/waves/design/snapshot.html#screen-accueil-groupe, docs/waves/design/snapshot.html#screen-creation-groupe, docs/waves/design/snapshot.html#screen-connexion, docs/waves/design/snapshot.html#screen-admin-connexion |
| P2e-pages2 | internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/groups/**, internal/access/**, internal/websec/**, docs/waves/design/snapshot.html#screen-accueil-groupe, docs/waves/design/snapshot.html#screen-creation-groupe, docs/waves/design/snapshot.html#screen-connexion, docs/waves/design/snapshot.html#screen-admin-connexion |
| P2e-settings | internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/groups/**, internal/access/**, internal/websec/**, docs/waves/design/snapshot.html#screen-accueil-groupe, docs/waves/design/snapshot.html#screen-creation-groupe, docs/waves/design/snapshot.html#screen-connexion, docs/waves/design/snapshot.html#screen-admin-connexion |
| P6-transport | internal/config/**, internal/shared/**, internal/db/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, docs/waves/design/snapshot.html#screen-desinscription, internal/mailmsg/**, internal/maildkim/** |
| P6-unsub | internal/config/**, internal/shared/**, internal/db/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, docs/waves/design/snapshot.html#screen-desinscription, internal/mailmsg/**, internal/maildkim/** |
| P8b-enrich | internal/ssrf/**, internal/shared/**, internal/config/** |
| P12a-bench | Dockerfile, docker-compose.yml, .env.example, VERSION, tests/e2e/_shared/**, tests/e2e/playwright.config.ts, tests/e2e/package.json, docs/waves/design/DESIGN.md |
| P4b-reveal | internal/drawengine/**, internal/participants/**, internal/rules/**, internal/groups/**, internal/shared/**, internal/db/** |
| P8b-sources | internal/ssrf/**, internal/shared/**, internal/config/** |
| P9c-core | web/templates/layout/**, web/static/css/base/**, web/static/css/theme/**, tests/e2e/_shared/**, docs/waves/design/snapshot.html#screen-revelation, docs/waves/design/snapshot.html#screen-revelation-b, docs/waves/design/snapshot.html#screen-revelation-c |
| P3d-roles | internal/participants/**, internal/rules/**, internal/access/**, internal/shared/** |
| P7-rules | internal/mail/**, internal/mailunsub/**, internal/letter/**, internal/participants/**, internal/shared/**, internal/config/** |
| P6c-diag | internal/shared/**, internal/config/**, internal/mail/**, internal/mailmsg/**, internal/maildkim/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, docs/waves/design/snapshot.html#screen-admin-delivrabilite |
| P6c-suppress | internal/shared/**, internal/config/**, internal/mail/**, internal/mailmsg/**, internal/maildkim/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, docs/waves/design/snapshot.html#screen-admin-delivrabilite |
| P11-code | internal/groups/**, internal/access/**, internal/participants/**, internal/enrolment/**, internal/shared/**, internal/config/** |
| P11-invite | internal/groups/**, internal/access/**, internal/participants/**, internal/enrolment/**, internal/shared/**, internal/config/** |
| P9c-envelope | web/templates/layout/**, web/static/css/base/**, web/static/css/theme/**, tests/e2e/_shared/**, docs/waves/design/snapshot.html#screen-revelation, docs/waves/design/snapshot.html#screen-revelation-b, docs/waves/design/snapshot.html#screen-revelation-c |
| P9c-gift | web/templates/layout/**, web/static/css/base/**, web/static/css/theme/**, tests/e2e/_shared/**, docs/waves/design/snapshot.html#screen-revelation, docs/waves/design/snapshot.html#screen-revelation-b, docs/waves/design/snapshot.html#screen-revelation-c |
| P4b-multi | internal/drawengine/**, internal/participants/**, internal/rules/**, internal/groups/**, internal/shared/**, internal/db/** |
| P7-engine | internal/mail/**, internal/mailunsub/**, internal/letter/**, internal/participants/**, internal/shared/**, internal/config/** |
| P9c-scratch | web/templates/layout/**, web/static/css/base/**, web/static/css/theme/**, tests/e2e/_shared/**, docs/waves/design/snapshot.html#screen-revelation, docs/waves/design/snapshot.html#screen-revelation-b, docs/waves/design/snapshot.html#screen-revelation-c |
| P11-verify | internal/groups/**, internal/access/**, internal/participants/**, internal/enrolment/**, internal/shared/**, internal/config/** |
| P4c-adjust | internal/draw/**, internal/reveal/**, internal/drawengine/**, internal/participants/**, internal/rules/**, internal/groupsettings/**, internal/shared/** |
| P5b-read | internal/letter/**, internal/draw/**, internal/reveal/**, internal/shared/** |
| P4d-pages | internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/draw/**, internal/reveal/**, internal/groups/**, docs/waves/design/snapshot.html#screen-attente-revelation, docs/waves/design/snapshot.html#screen-ma-cible, internal/participants/**, docs/waves/design/snapshot.html#screen-reception-seule |
| P8c-service | internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/sources/**, internal/enrich/**, internal/ssrf/**, internal/groups/**, internal/letter/**, internal/participants/**, internal/draw/**, docs/waves/design/snapshot.html#screen-suggestions |
| P11-store | internal/groups/**, internal/access/**, internal/participants/**, internal/enrolment/**, internal/shared/**, internal/config/** |
| P12a-docs | Dockerfile, docker-compose.yml, .env.example, VERSION, tests/e2e/_shared/**, tests/e2e/playwright.config.ts, tests/e2e/package.json, docs/waves/design/DESIGN.md |
| P6c-page | internal/shared/**, internal/config/**, internal/mail/**, internal/mailmsg/**, internal/maildkim/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, docs/waves/design/snapshot.html#screen-admin-delivrabilite |
| P10b-consult | internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/groups/**, internal/access/**, internal/participants/**, internal/rules/**, internal/draw/**, internal/mail/**, internal/db/** |
| P4c-repair | internal/draw/**, internal/reveal/**, internal/drawengine/**, internal/participants/**, internal/rules/**, internal/groupsettings/**, internal/shared/** |
| P5c-mailbox | internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/letter/**, internal/letterread/**, internal/groups/**, docs/waves/design/snapshot.html#screen-lettre, docs/waves/design/snapshot.html#screen-boite-lettres, docs/waves/design/snapshot.html#screen-lettre-recue |
| P5c-pages | internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/letter/**, internal/letterread/**, internal/groups/**, docs/waves/design/snapshot.html#screen-lettre, docs/waves/design/snapshot.html#screen-boite-lettres, docs/waves/design/snapshot.html#screen-lettre-recue |
| P8c-page | internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/sources/**, internal/enrich/**, internal/ssrf/**, internal/groups/**, internal/letter/**, internal/participants/**, internal/draw/**, docs/waves/design/snapshot.html#screen-suggestions |
| P11-roleopt | internal/groups/**, internal/access/**, internal/participants/**, internal/enrolment/**, internal/shared/**, internal/config/** |
| P10b-ops | internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/groups/**, internal/access/**, internal/participants/**, internal/rules/**, internal/draw/**, internal/mail/**, internal/db/** |
| P9c-seq | web/templates/layout/**, web/static/css/base/**, web/static/css/theme/**, tests/e2e/_shared/**, docs/waves/design/snapshot.html#screen-revelation, docs/waves/design/snapshot.html#screen-revelation-b, docs/waves/design/snapshot.html#screen-revelation-c |
| P7b-notify | internal/letter/**, internal/letterread/**, internal/mail/**, internal/mailunsub/**, internal/reveal/**, internal/shared/** |
| P4d-roles | internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/draw/**, internal/reveal/**, internal/groups/**, docs/waves/design/snapshot.html#screen-attente-revelation, docs/waves/design/snapshot.html#screen-ma-cible, internal/participants/**, docs/waves/design/snapshot.html#screen-reception-seule |
| P11c-public | internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/registration/**, internal/groups/**, docs/waves/design/snapshot.html#screen-inscription, docs/waves/design/snapshot.html#screen-inscription-code, docs/waves/design/snapshot.html#screen-inscription-reussie, docs/waves/design/snapshot.html#screen-admin-inscriptions |
| P4c-roles | internal/draw/**, internal/reveal/**, internal/drawengine/**, internal/participants/**, internal/rules/**, internal/groupsettings/**, internal/shared/** |
| P5c-roles | internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/letter/**, internal/letterread/**, internal/groups/**, docs/waves/design/snapshot.html#screen-lettre, docs/waves/design/snapshot.html#screen-boite-lettres, docs/waves/design/snapshot.html#screen-lettre-recue |
| P8c-multi | internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/sources/**, internal/enrich/**, internal/ssrf/**, internal/groups/**, internal/letter/**, internal/participants/**, internal/draw/**, docs/waves/design/snapshot.html#screen-suggestions |
| P10-dash | internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/groups/**, internal/groupsettings/**, internal/access/**, internal/participants/**, internal/rules/**, internal/draw/**, internal/reveal/**, internal/adjust/**, internal/mail/**, internal/mailunsub/**, internal/reminders/**, internal/letter/**, docs/waves/design/snapshot.html#screen-admin-menu, docs/waves/design/snapshot.html#screen-admin-participants, docs/waves/design/snapshot.html#screen-admin-tableau, docs/waves/design/snapshot.html#screen-admin-tableau-bureau, docs/waves/design/snapshot.html#screen-admin-fiche, docs/waves/design/snapshot.html#screen-admin-ajuster, docs/waves/design/snapshot.html#screen-admin-tirage, docs/waves/design/snapshot.html#screen-admin-reglages |
| P10-menu | internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/groups/**, internal/groupsettings/**, internal/access/**, internal/participants/**, internal/rules/**, internal/draw/**, internal/reveal/**, internal/adjust/**, internal/mail/**, internal/mailunsub/**, internal/reminders/**, internal/letter/**, docs/waves/design/snapshot.html#screen-admin-menu, docs/waves/design/snapshot.html#screen-admin-participants, docs/waves/design/snapshot.html#screen-admin-tableau, docs/waves/design/snapshot.html#screen-admin-tableau-bureau, docs/waves/design/snapshot.html#screen-admin-fiche, docs/waves/design/snapshot.html#screen-admin-ajuster, docs/waves/design/snapshot.html#screen-admin-tirage, docs/waves/design/snapshot.html#screen-admin-reglages |
| P10-settings | internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/groups/**, internal/groupsettings/**, internal/access/**, internal/participants/**, internal/rules/**, internal/draw/**, internal/reveal/**, internal/adjust/**, internal/mail/**, internal/mailunsub/**, internal/reminders/**, internal/letter/**, docs/waves/design/snapshot.html#screen-admin-menu, docs/waves/design/snapshot.html#screen-admin-participants, docs/waves/design/snapshot.html#screen-admin-tableau, docs/waves/design/snapshot.html#screen-admin-tableau-bureau, docs/waves/design/snapshot.html#screen-admin-fiche, docs/waves/design/snapshot.html#screen-admin-ajuster, docs/waves/design/snapshot.html#screen-admin-tirage, docs/waves/design/snapshot.html#screen-admin-reglages |
| P11c-admin | internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/registration/**, internal/groups/**, docs/waves/design/snapshot.html#screen-inscription, docs/waves/design/snapshot.html#screen-inscription-code, docs/waves/design/snapshot.html#screen-inscription-reussie, docs/waves/design/snapshot.html#screen-admin-inscriptions |
| P10-roles | internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/groups/**, internal/groupsettings/**, internal/access/**, internal/participants/**, internal/rules/**, internal/draw/**, internal/reveal/**, internal/adjust/**, internal/mail/**, internal/mailunsub/**, internal/reminders/**, internal/letter/**, docs/waves/design/snapshot.html#screen-admin-menu, docs/waves/design/snapshot.html#screen-admin-participants, docs/waves/design/snapshot.html#screen-admin-tableau, docs/waves/design/snapshot.html#screen-admin-tableau-bureau, docs/waves/design/snapshot.html#screen-admin-fiche, docs/waves/design/snapshot.html#screen-admin-ajuster, docs/waves/design/snapshot.html#screen-admin-tirage, docs/waves/design/snapshot.html#screen-admin-reglages |
| P10-draw | internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/groups/**, internal/groupsettings/**, internal/access/**, internal/participants/**, internal/rules/**, internal/draw/**, internal/reveal/**, internal/adjust/**, internal/mail/**, internal/mailunsub/**, internal/reminders/**, internal/letter/**, docs/waves/design/snapshot.html#screen-admin-menu, docs/waves/design/snapshot.html#screen-admin-participants, docs/waves/design/snapshot.html#screen-admin-tableau, docs/waves/design/snapshot.html#screen-admin-tableau-bureau, docs/waves/design/snapshot.html#screen-admin-fiche, docs/waves/design/snapshot.html#screen-admin-ajuster, docs/waves/design/snapshot.html#screen-admin-tirage, docs/waves/design/snapshot.html#screen-admin-reglages |
| P10-people | internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/groups/**, internal/groupsettings/**, internal/access/**, internal/participants/**, internal/rules/**, internal/draw/**, internal/reveal/**, internal/adjust/**, internal/mail/**, internal/mailunsub/**, internal/reminders/**, internal/letter/**, docs/waves/design/snapshot.html#screen-admin-menu, docs/waves/design/snapshot.html#screen-admin-participants, docs/waves/design/snapshot.html#screen-admin-tableau, docs/waves/design/snapshot.html#screen-admin-tableau-bureau, docs/waves/design/snapshot.html#screen-admin-fiche, docs/waves/design/snapshot.html#screen-admin-ajuster, docs/waves/design/snapshot.html#screen-admin-tirage, docs/waves/design/snapshot.html#screen-admin-reglages |
| P11c-roles | internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/registration/**, internal/groups/**, docs/waves/design/snapshot.html#screen-inscription, docs/waves/design/snapshot.html#screen-inscription-code, docs/waves/design/snapshot.html#screen-inscription-reussie, docs/waves/design/snapshot.html#screen-admin-inscriptions |
| P12b-cov | Dockerfile, docker-compose.yml, docker-compose.test.yml, scripts/e2e.sh, .env.example, VERSION, .github/workflows/**, cmd/**, internal/**, web/**, tests/e2e/_shared/**, tests/e2e/mail-sink/**, tests/e2e/playwright.config.ts, tests/e2e/package.json, docs/waves/design/DESIGN.md, scripts/spamcheck.sh, tests/e2e/spamassassin/** |
| P12b-jA | Dockerfile, docker-compose.yml, docker-compose.test.yml, scripts/e2e.sh, .env.example, VERSION, .github/workflows/**, cmd/**, internal/**, web/**, tests/e2e/_shared/**, tests/e2e/mail-sink/**, tests/e2e/playwright.config.ts, tests/e2e/package.json, docs/waves/design/DESIGN.md, scripts/spamcheck.sh, tests/e2e/spamassassin/** |
| P12b-jB | Dockerfile, docker-compose.yml, docker-compose.test.yml, scripts/e2e.sh, .env.example, VERSION, .github/workflows/**, cmd/**, internal/**, web/**, tests/e2e/_shared/**, tests/e2e/mail-sink/**, tests/e2e/playwright.config.ts, tests/e2e/package.json, docs/waves/design/DESIGN.md, scripts/spamcheck.sh, tests/e2e/spamassassin/** |
| P12b-jC | Dockerfile, docker-compose.yml, docker-compose.test.yml, scripts/e2e.sh, .env.example, VERSION, .github/workflows/**, cmd/**, internal/**, web/**, tests/e2e/_shared/**, tests/e2e/mail-sink/**, tests/e2e/playwright.config.ts, tests/e2e/package.json, docs/waves/design/DESIGN.md, scripts/spamcheck.sh, tests/e2e/spamassassin/** |
| P13b-review | internal/i18n/**, web/static/i18n/**, web/templates/** |
| P12b-zza | Dockerfile, docker-compose.yml, docker-compose.test.yml, scripts/e2e.sh, .env.example, VERSION, .github/workflows/**, cmd/**, internal/**, web/**, tests/e2e/_shared/**, tests/e2e/mail-sink/**, tests/e2e/playwright.config.ts, tests/e2e/package.json, docs/waves/design/DESIGN.md, scripts/spamcheck.sh, tests/e2e/spamassassin/** |
| P12b-zzb | Dockerfile, docker-compose.yml, docker-compose.test.yml, scripts/e2e.sh, .env.example, VERSION, .github/workflows/**, cmd/**, internal/**, web/**, tests/e2e/_shared/**, tests/e2e/mail-sink/**, tests/e2e/playwright.config.ts, tests/e2e/package.json, docs/waves/design/DESIGN.md, scripts/spamcheck.sh, tests/e2e/spamassassin/** |
| P12b-zzc | Dockerfile, docker-compose.yml, docker-compose.test.yml, scripts/e2e.sh, .env.example, VERSION, .github/workflows/**, cmd/**, internal/**, web/**, tests/e2e/_shared/**, tests/e2e/mail-sink/**, tests/e2e/playwright.config.ts, tests/e2e/package.json, docs/waves/design/DESIGN.md, scripts/spamcheck.sh, tests/e2e/spamassassin/** |

### Validation graph

| Phase | Shallow gate | Wave gate criteria |
|---|---|---|
| P1a | format, lint, typecheck, unit | F1.AC1–AC6, F1b.AC1–AC10 |
| P1b-db | format, lint, typecheck, unit | F1.AC1–AC6, F1b.AC1–AC10 |
| P1b-sh1 | format, lint, typecheck, unit | F1.AC1–AC6, F1b.AC1–AC10 |
| P1b-sh2 | format, lint, typecheck, unit | F1.AC1–AC6, F1b.AC1–AC10 |
| P1c-ci | format, lint, typecheck, unit | F2.AC1–AC9, F2c.AC1–AC3 |
| P1c-entry | format, lint, typecheck, unit | F2.AC1–AC9, F2c.AC1–AC3 |
| P1c-infra | format, lint, typecheck, unit | F2.AC1–AC9, F2c.AC1–AC3 |
| P2-groups | format, lint, typecheck, unit | F2.AC1–AC9, F2c.AC1–AC3 |
| P2b-a | format, lint, typecheck, unit | F2.AC1–AC9, F2c.AC1–AC3 |
| P2b-b | format, lint, typecheck, unit | F2.AC1–AC9, F2c.AC1–AC3 |
| P2c-websec | format, lint, typecheck, unit | F2.AC1–AC9, F2c.AC1–AC3 |
| P4-engine | format, lint, typecheck, unit | F2.AC1–AC9, F2c.AC1–AC3 |
| P1c-docker | format, lint, typecheck, unit | F1c.AC1–AC15, F2b.AC1–AC12, F4.AC1–AC16, F6b.AC1–AC11 |
| P1c-layout | format, lint, typecheck, unit | F1c.AC1–AC15, F2b.AC1–AC12, F4.AC1–AC16, F6b.AC1–AC11 |
| P3-core | format, lint, typecheck, unit | F1c.AC1–AC15, F2b.AC1–AC12, F4.AC1–AC16, F6b.AC1–AC11 |
| P3-rules | format, lint, typecheck, unit | F1c.AC1–AC15, F2b.AC1–AC12, F4.AC1–AC16, F6b.AC1–AC11 |
| P6b-msg | format, lint, typecheck, unit | F1c.AC1–AC15, F2b.AC1–AC12, F4.AC1–AC16, F6b.AC1–AC11 |
| P6b-dkim | format, lint, typecheck, unit | F1c.AC1–AC15, F2b.AC1–AC12, F4.AC1–AC16, F6b.AC1–AC11 |
| P2b-store | format, lint, typecheck, unit | F1c.AC1–AC15, F2b.AC1–AC12, F4.AC1–AC16, F6b.AC1–AC11 |
| P13-catalog | format, lint, typecheck, unit | F1c.AC1–AC15, F2b.AC1–AC12, F4.AC1–AC16, F6b.AC1–AC11 |
| P5-letter | format, lint, typecheck, unit | F13.AC1–AC18, F3.AC1–AC17, F5.AC1–AC8, F8.AC1–AC4, F9.AC1–AC10 |
| P8-ssrf | format, lint, typecheck, unit | F13.AC1–AC18, F3.AC1–AC17, F5.AC1–AC8, F8.AC1–AC4, F9.AC1–AC10 |
| P9-illus | format, lint, typecheck, unit | F13.AC1–AC18, F3.AC1–AC17, F5.AC1–AC8, F8.AC1–AC4, F9.AC1–AC10 |
| P9-theme | format, lint, typecheck, unit | F13.AC1–AC18, F3.AC1–AC17, F5.AC1–AC8, F8.AC1–AC4, F9.AC1–AC10 |
| P3-roles | format, lint, typecheck, unit | F13.AC1–AC18, F3.AC1–AC17, F5.AC1–AC8, F8.AC1–AC4, F9.AC1–AC10 |
| P13-negotiate | format, lint, typecheck, unit | F13.AC1–AC18, F3.AC1–AC17, F5.AC1–AC8, F8.AC1–AC4, F9.AC1–AC10 |
| P13-guard | format, lint, typecheck, unit | F13.AC1–AC18, F3.AC1–AC17, F5.AC1–AC8, F8.AC1–AC4, F9.AC1–AC10 |
| P13-config | format, lint, typecheck, unit | F13.AC1–AC18, F3.AC1–AC17, F5.AC1–AC8, F8.AC1–AC4, F9.AC1–AC10 |
| P6-queue | format, lint, typecheck, unit | F2e.AC1–AC14 |
| P6-templates | format, lint, typecheck, unit | F2e.AC1–AC14 |
| P3d-enrol | format, lint, typecheck, unit | F2e.AC1–AC14 |
| P3d-rulechange | format, lint, typecheck, unit | F2e.AC1–AC14 |
| P4b-core | format, lint, typecheck, unit | F2e.AC1–AC14 |
| P2e-pages1 | format, lint, typecheck, unit | F2e.AC1–AC14 |
| P2e-pages2 | format, lint, typecheck, unit | F2e.AC1–AC14 |
| P2e-settings | format, lint, typecheck, unit | F2e.AC1–AC14 |
| P6-transport | format, lint, typecheck, unit | F3d.AC1–AC10, F6.AC1–AC37, F8b.AC1–AC4 |
| P6-unsub | format, lint, typecheck, unit | F3d.AC1–AC10, F6.AC1–AC37, F8b.AC1–AC4 |
| P8b-enrich | format, lint, typecheck, unit | F3d.AC1–AC10, F6.AC1–AC37, F8b.AC1–AC4 |
| P12a-bench | format, lint, typecheck, unit | F3d.AC1–AC10, F6.AC1–AC37, F8b.AC1–AC4 |
| P4b-reveal | format, lint, typecheck, unit | F3d.AC1–AC10, F6.AC1–AC37, F8b.AC1–AC4 |
| P8b-sources | format, lint, typecheck, unit | F3d.AC1–AC10, F6.AC1–AC37, F8b.AC1–AC4 |
| P9c-core | format, lint, typecheck, unit | F3d.AC1–AC10, F6.AC1–AC37, F8b.AC1–AC4 |
| P3d-roles | format, lint, typecheck, unit | F3d.AC1–AC10, F6.AC1–AC37, F8b.AC1–AC4 |
| P7-rules | format, lint, typecheck, unit | F4b.AC1–AC16 |
| P6c-diag | format, lint, typecheck, unit | F4b.AC1–AC16 |
| P6c-suppress | format, lint, typecheck, unit | F4b.AC1–AC16 |
| P11-code | format, lint, typecheck, unit | F4b.AC1–AC16 |
| P11-invite | format, lint, typecheck, unit | F4b.AC1–AC16 |
| P9c-envelope | format, lint, typecheck, unit | F4b.AC1–AC16 |
| P9c-gift | format, lint, typecheck, unit | F4b.AC1–AC16 |
| P4b-multi | format, lint, typecheck, unit | F4b.AC1–AC16 |
| P7-engine | format, lint, typecheck, unit | F5b.AC1–AC7, F7.AC1–AC14 |
| P9c-scratch | format, lint, typecheck, unit | F5b.AC1–AC7, F7.AC1–AC14 |
| P11-verify | format, lint, typecheck, unit | F5b.AC1–AC7, F7.AC1–AC14 |
| P4c-adjust | format, lint, typecheck, unit | F5b.AC1–AC7, F7.AC1–AC14 |
| P5b-read | format, lint, typecheck, unit | F5b.AC1–AC7, F7.AC1–AC14 |
| P4d-pages | format, lint, typecheck, unit | F5b.AC1–AC7, F7.AC1–AC14 |
| P8c-service | format, lint, typecheck, unit | F5b.AC1–AC7, F7.AC1–AC14 |
| P11-store | format, lint, typecheck, unit | F5b.AC1–AC7, F7.AC1–AC14 |
| P12a-docs | format, lint, typecheck, unit | F11.AC1–AC25, F12a.AC1–AC7, F6c.AC1–AC16 |
| P6c-page | format, lint, typecheck, unit | F11.AC1–AC25, F12a.AC1–AC7, F6c.AC1–AC16 |
| P10b-consult | format, lint, typecheck, unit | F11.AC1–AC25, F12a.AC1–AC7, F6c.AC1–AC16 |
| P4c-repair | format, lint, typecheck, unit | F11.AC1–AC25, F12a.AC1–AC7, F6c.AC1–AC16 |
| P5c-mailbox | format, lint, typecheck, unit | F11.AC1–AC25, F12a.AC1–AC7, F6c.AC1–AC16 |
| P5c-pages | format, lint, typecheck, unit | F11.AC1–AC25, F12a.AC1–AC7, F6c.AC1–AC16 |
| P8c-page | format, lint, typecheck, unit | F11.AC1–AC25, F12a.AC1–AC7, F6c.AC1–AC16 |
| P11-roleopt | format, lint, typecheck, unit | F11.AC1–AC25, F12a.AC1–AC7, F6c.AC1–AC16 |
| P10b-ops | format, lint, typecheck, unit | F10b.AC1–AC15, F4c.AC1–AC16, F4d.AC1–AC11, F5c.AC1–AC14, F7b.AC1–AC6, F8c.AC1–AC11, F9c.AC1–AC20 |
| P9c-seq | format, lint, typecheck, unit | F10b.AC1–AC15, F4c.AC1–AC16, F4d.AC1–AC11, F5c.AC1–AC14, F7b.AC1–AC6, F8c.AC1–AC11, F9c.AC1–AC20 |
| P7b-notify | format, lint, typecheck, unit | F10b.AC1–AC15, F4c.AC1–AC16, F4d.AC1–AC11, F5c.AC1–AC14, F7b.AC1–AC6, F8c.AC1–AC11, F9c.AC1–AC20 |
| P4d-roles | format, lint, typecheck, unit | F10b.AC1–AC15, F4c.AC1–AC16, F4d.AC1–AC11, F5c.AC1–AC14, F7b.AC1–AC6, F8c.AC1–AC11, F9c.AC1–AC20 |
| P11c-public | format, lint, typecheck, unit | F10b.AC1–AC15, F4c.AC1–AC16, F4d.AC1–AC11, F5c.AC1–AC14, F7b.AC1–AC6, F8c.AC1–AC11, F9c.AC1–AC20 |
| P4c-roles | format, lint, typecheck, unit | F10b.AC1–AC15, F4c.AC1–AC16, F4d.AC1–AC11, F5c.AC1–AC14, F7b.AC1–AC6, F8c.AC1–AC11, F9c.AC1–AC20 |
| P5c-roles | format, lint, typecheck, unit | F10b.AC1–AC15, F4c.AC1–AC16, F4d.AC1–AC11, F5c.AC1–AC14, F7b.AC1–AC6, F8c.AC1–AC11, F9c.AC1–AC20 |
| P8c-multi | format, lint, typecheck, unit | F10b.AC1–AC15, F4c.AC1–AC16, F4d.AC1–AC11, F5c.AC1–AC14, F7b.AC1–AC6, F8c.AC1–AC11, F9c.AC1–AC20 |
| P10-dash | format, lint, typecheck, unit | aucun |
| P10-menu | format, lint, typecheck, unit | aucun |
| P10-settings | format, lint, typecheck, unit | aucun |
| P11c-admin | format, lint, typecheck, unit | aucun |
| P10-roles | format, lint, typecheck, unit | aucun |
| P10-draw | format, lint, typecheck, unit | F10.AC1–AC50, F11c.AC1–AC19 |
| P10-people | format, lint, typecheck, unit | F10.AC1–AC50, F11c.AC1–AC19 |
| P11c-roles | format, lint, typecheck, unit | F10.AC1–AC50, F11c.AC1–AC19 |
| P12b-cov | format, lint, typecheck, unit | F13b.AC1–AC3 |
| P12b-jA | format, lint, typecheck, unit | F13b.AC1–AC3 |
| P12b-jB | format, lint, typecheck, unit | F13b.AC1–AC3 |
| P12b-jC | format, lint, typecheck, unit | F13b.AC1–AC3 |
| P13b-review | format, lint, typecheck, unit | F13b.AC1–AC3 |
| P12b-zza | format, lint, typecheck, unit | F12b.AC1–AC17 |
| P12b-zzb | format, lint, typecheck, unit | F12b.AC1–AC17 |
| P12b-zzc | format, lint, typecheck, unit | F12b.AC1–AC17 |

## Waves

```yaml wave
wave: 1
phases: [P1a, P1b-db, P1b-sh1, P1b-sh2]
integration_owned: [internal/app/**, web/static/i18n/fr.json, go.mod, go.sum]
criteria: [F1.AC1, F1.AC2, F1.AC3, F1.AC4, F1.AC5, F1.AC6, F1b.AC1, F1b.AC2, F1b.AC3, F1b.AC4, F1b.AC5, F1b.AC6, F1b.AC7, F1b.AC8, F1b.AC9, F1b.AC10]
integration_checks: ["cmd:go build ./... && go vet ./... && test -z \"$(gofmt -l .)\"", "cmd:go test ./..."]
```

```yaml wave
wave: 2
phases: [P1c-ci, P1c-entry, P1c-infra, P2-groups, P2b-a, P2b-b, P2c-websec, P4-engine]
integration_owned: [internal/app/**, web/static/i18n/fr.json, go.mod, go.sum]
criteria: [F2.AC1, F2.AC2, F2.AC3, F2.AC4, F2.AC5, F2.AC6, F2.AC7, F2.AC8, F2.AC9, F2c.AC1, F2c.AC2, F2c.AC3]
integration_checks: ["cmd:go build ./... && go vet ./... && test -z \"$(gofmt -l .)\"", "cmd:go test ./..."]
```

```yaml wave
wave: 3
phases: [P1c-docker, P1c-layout, P3-core, P3-rules, P6b-msg, P6b-dkim, P2b-store, P13-catalog]
integration_owned: [internal/app/**, go.mod, go.sum, tests/e2e/package-lock.json]
criteria: [F1c.AC1, F1c.AC2, F1c.AC3, F1c.AC4, F1c.AC5, F1c.AC6, F1c.AC7, F1c.AC8, F1c.AC9, F1c.AC10, F1c.AC11, F1c.AC12, F1c.AC13, F1c.AC14, F1c.AC15, F2b.AC1, F2b.AC2, F2b.AC3, F2b.AC4, F2b.AC5, F2b.AC6, F2b.AC7, F2b.AC8, F2b.AC9, F2b.AC10, F2b.AC11, F2b.AC12, F4.AC1, F4.AC2, F4.AC3, F4.AC4, F4.AC5, F4.AC6, F4.AC7, F4.AC8, F4.AC9, F4.AC10, F4.AC11, F4.AC12, F4.AC13, F4.AC14, F4.AC15, F4.AC16, F6b.AC1, F6b.AC2, F6b.AC3, F6b.AC4, F6b.AC5, F6b.AC6, F6b.AC7, F6b.AC8, F6b.AC9, F6b.AC10, F6b.AC11]
integration_checks: ["cmd:go build ./... && go vet ./... && test -z \"$(gofmt -l .)\"", "cmd:go test ./...", "cmd:bash tests/e2e/docker/docker-smoke.sh"]
```

```yaml wave
wave: 4
phases: [P5-letter, P8-ssrf, P9-illus, P9-theme, P3-roles, P13-negotiate, P13-guard, P13-config]
integration_owned: [internal/app/**, go.mod, go.sum, tests/e2e/package-lock.json, web/templates/layout/**]
criteria: [F13.AC1, F13.AC2, F13.AC3, F13.AC4, F13.AC5, F13.AC6, F13.AC7, F13.AC8, F13.AC9, F13.AC10, F13.AC11, F13.AC12, F13.AC13, F13.AC14, F13.AC15, F13.AC16, F13.AC17, F13.AC18, F3.AC1, F3.AC2, F3.AC3, F3.AC4, F3.AC5, F3.AC6, F3.AC7, F3.AC8, F3.AC9, F3.AC10, F3.AC11, F3.AC12, F3.AC13, F3.AC14, F3.AC15, F3.AC16, F3.AC17, F5.AC1, F5.AC2, F5.AC3, F5.AC4, F5.AC5, F5.AC6, F5.AC7, F5.AC8, F8.AC1, F8.AC2, F8.AC3, F8.AC4, F9.AC1, F9.AC2, F9.AC3, F9.AC4, F9.AC5, F9.AC6, F9.AC7, F9.AC8, F9.AC9, F9.AC10]
integration_checks: ["cmd:go build ./... && go vet ./... && test -z \"$(gofmt -l .)\"", "cmd:go test ./..."]
```

```yaml wave
wave: 5
phases: [P6-queue, P6-templates, P3d-enrol, P3d-rulechange, P4b-core, P2e-pages1, P2e-pages2, P2e-settings]
integration_owned: [internal/app/**, go.mod, go.sum, tests/e2e/package-lock.json, web/templates/layout/**]
criteria: [F2e.AC1, F2e.AC2, F2e.AC3, F2e.AC4, F2e.AC5, F2e.AC6, F2e.AC7, F2e.AC8, F2e.AC9, F2e.AC10, F2e.AC11, F2e.AC12, F2e.AC13, F2e.AC14]
integration_checks: ["cmd:go build ./... && go vet ./... && test -z \"$(gofmt -l .)\"", "cmd:go test ./..."]
```

```yaml wave
wave: 6
phases: [P6-transport, P6-unsub, P8b-enrich, P12a-bench, P4b-reveal, P8b-sources, P9c-core, P3d-roles]
integration_owned: [internal/app/**, go.mod, go.sum, tests/e2e/package-lock.json, web/templates/layout/**]
criteria: [F3d.AC1, F3d.AC2, F3d.AC3, F3d.AC4, F3d.AC5, F3d.AC6, F3d.AC7, F3d.AC8, F3d.AC9, F3d.AC10, F6.AC1, F6.AC2, F6.AC3, F6.AC4, F6.AC5, F6.AC6, F6.AC7, F6.AC8, F6.AC9, F6.AC10, F6.AC11, F6.AC12, F6.AC13, F6.AC14, F6.AC15, F6.AC16, F6.AC17, F6.AC18, F6.AC19, F6.AC20, F6.AC21, F6.AC22, F6.AC23, F6.AC24, F6.AC25, F6.AC26, F6.AC27, F6.AC28, F6.AC29, F6.AC30, F6.AC31, F6.AC32, F6.AC33, F6.AC34, F6.AC35, F6.AC36, F6.AC37, F8b.AC1, F8b.AC2, F8b.AC3, F8b.AC4]
integration_checks: ["cmd:go build ./... && go vet ./... && test -z \"$(gofmt -l .)\"", "cmd:go test ./...", "cmd:bash scripts/e2e.sh --smoke"]
```

```yaml wave
wave: 7
phases: [P7-rules, P6c-diag, P6c-suppress, P11-code, P11-invite, P9c-envelope, P9c-gift, P4b-multi]
integration_owned: [internal/app/**, go.mod, go.sum, tests/e2e/package-lock.json, web/templates/layout/**]
criteria: [F4b.AC1, F4b.AC2, F4b.AC3, F4b.AC4, F4b.AC5, F4b.AC6, F4b.AC7, F4b.AC8, F4b.AC9, F4b.AC10, F4b.AC11, F4b.AC12, F4b.AC13, F4b.AC14, F4b.AC15, F4b.AC16]
integration_checks: ["cmd:go build ./... && go vet ./... && test -z \"$(gofmt -l .)\"", "cmd:go test ./..."]
```

```yaml wave
wave: 8
phases: [P7-engine, P9c-scratch, P11-verify, P4c-adjust, P5b-read, P4d-pages, P8c-service, P11-store]
integration_owned: [internal/app/**, go.mod, go.sum, tests/e2e/package-lock.json, web/templates/layout/**]
criteria: [F5b.AC1, F5b.AC2, F5b.AC3, F5b.AC4, F5b.AC5, F5b.AC6, F5b.AC7, F7.AC1, F7.AC2, F7.AC3, F7.AC4, F7.AC5, F7.AC6, F7.AC7, F7.AC8, F7.AC9, F7.AC10, F7.AC11, F7.AC12, F7.AC13, F7.AC14]
integration_checks: ["cmd:go build ./... && go vet ./... && test -z \"$(gofmt -l .)\"", "cmd:go test ./..."]
```

```yaml wave
wave: 9
phases: [P12a-docs, P6c-page, P10b-consult, P4c-repair, P5c-mailbox, P5c-pages, P8c-page, P11-roleopt]
integration_owned: [internal/app/**, go.mod, go.sum, tests/e2e/package-lock.json, web/templates/layout/**]
criteria: [F11.AC1, F11.AC2, F11.AC3, F11.AC4, F11.AC5, F11.AC6, F11.AC7, F11.AC8, F11.AC9, F11.AC10, F11.AC11, F11.AC12, F11.AC13, F11.AC14, F11.AC15, F11.AC16, F11.AC17, F11.AC18, F11.AC19, F11.AC20, F11.AC21, F11.AC22, F11.AC23, F11.AC24, F11.AC25, F12a.AC1, F12a.AC2, F12a.AC3, F12a.AC4, F12a.AC5, F12a.AC6, F12a.AC7, F6c.AC1, F6c.AC2, F6c.AC3, F6c.AC4, F6c.AC5, F6c.AC6, F6c.AC7, F6c.AC8, F6c.AC9, F6c.AC10, F6c.AC11, F6c.AC12, F6c.AC13, F6c.AC14, F6c.AC15, F6c.AC16]
integration_checks: ["cmd:go build ./... && go vet ./... && test -z \"$(gofmt -l .)\"", "cmd:go test ./..."]
```

```yaml wave
wave: 10
phases: [P10b-ops, P9c-seq, P7b-notify, P4d-roles, P11c-public, P4c-roles, P5c-roles, P8c-multi]
integration_owned: [internal/app/**, go.mod, go.sum, tests/e2e/package-lock.json, web/templates/layout/**]
criteria: [F10b.AC1, F10b.AC2, F10b.AC3, F10b.AC4, F10b.AC5, F10b.AC6, F10b.AC7, F10b.AC8, F10b.AC9, F10b.AC10, F10b.AC11, F10b.AC12, F10b.AC13, F10b.AC14, F10b.AC15, F4c.AC1, F4c.AC2, F4c.AC3, F4c.AC4, F4c.AC5, F4c.AC6, F4c.AC7, F4c.AC8, F4c.AC9, F4c.AC10, F4c.AC11, F4c.AC12, F4c.AC13, F4c.AC14, F4c.AC15, F4c.AC16, F4d.AC1, F4d.AC2, F4d.AC3, F4d.AC4, F4d.AC5, F4d.AC6, F4d.AC7, F4d.AC8, F4d.AC9, F4d.AC10, F4d.AC11, F5c.AC1, F5c.AC2, F5c.AC3, F5c.AC4, F5c.AC5, F5c.AC6, F5c.AC7, F5c.AC8, F5c.AC9, F5c.AC10, F5c.AC11, F5c.AC12, F5c.AC13, F5c.AC14, F7b.AC1, F7b.AC2, F7b.AC3, F7b.AC4, F7b.AC5, F7b.AC6, F8c.AC1, F8c.AC2, F8c.AC3, F8c.AC4, F8c.AC5, F8c.AC6, F8c.AC7, F8c.AC8, F8c.AC9, F8c.AC10, F8c.AC11, F9c.AC1, F9c.AC2, F9c.AC3, F9c.AC4, F9c.AC5, F9c.AC6, F9c.AC7, F9c.AC8, F9c.AC9, F9c.AC10, F9c.AC11, F9c.AC12, F9c.AC13, F9c.AC14, F9c.AC15, F9c.AC16, F9c.AC17, F9c.AC18, F9c.AC19, F9c.AC20]
integration_checks: ["cmd:go build ./... && go vet ./... && test -z \"$(gofmt -l .)\"", "cmd:go test ./..."]
```

```yaml wave
wave: 11
phases: [P10-dash, P10-menu, P10-settings, P11c-admin, P10-roles]
integration_owned: [internal/app/**, go.mod, go.sum, tests/e2e/package-lock.json, web/templates/layout/**]
criteria: []
integration_checks: ["cmd:go build ./... && go vet ./... && test -z \"$(gofmt -l .)\"", "cmd:go test ./..."]
```

```yaml wave
wave: 12
phases: [P10-draw, P10-people, P11c-roles]
integration_owned: [internal/app/**, go.mod, go.sum, tests/e2e/package-lock.json, web/templates/layout/**]
criteria: [F10.AC1, F10.AC2, F10.AC3, F10.AC4, F10.AC5, F10.AC6, F10.AC7, F10.AC8, F10.AC9, F10.AC10, F10.AC11, F10.AC12, F10.AC13, F10.AC14, F10.AC15, F10.AC16, F10.AC17, F10.AC18, F10.AC19, F10.AC20, F10.AC21, F10.AC22, F10.AC23, F10.AC24, F10.AC25, F10.AC26, F10.AC27, F10.AC28, F10.AC29, F10.AC30, F10.AC31, F10.AC32, F10.AC33, F10.AC34, F10.AC35, F10.AC36, F10.AC37, F10.AC38, F10.AC39, F10.AC40, F10.AC41, F10.AC42, F10.AC43, F10.AC44, F10.AC45, F10.AC46, F10.AC47, F10.AC48, F10.AC49, F10.AC50, F11c.AC1, F11c.AC2, F11c.AC3, F11c.AC4, F11c.AC5, F11c.AC6, F11c.AC7, F11c.AC8, F11c.AC9, F11c.AC10, F11c.AC11, F11c.AC12, F11c.AC13, F11c.AC14, F11c.AC15, F11c.AC16, F11c.AC17, F11c.AC18, F11c.AC19]
integration_checks: ["cmd:go build ./... && go vet ./... && test -z \"$(gofmt -l .)\"", "cmd:go test ./..."]
```

```yaml wave
wave: 13
phases: [P12b-cov, P12b-jA, P12b-jB, P12b-jC, P13b-review]
integration_owned: [internal/app/**, go.mod, go.sum, tests/e2e/package-lock.json, web/templates/layout/**]
criteria: [F13b.AC1, F13b.AC2, F13b.AC3]
integration_checks: ["cmd:go build ./... && go vet ./... && test -z \"$(gofmt -l .)\"", "cmd:go test ./..."]
```

```yaml wave
wave: 14
phases: [P12b-zza, P12b-zzb, P12b-zzc]
integration_owned: [internal/app/**, go.mod, go.sum, tests/e2e/package-lock.json, web/templates/layout/**]
criteria: [F12b.AC1, F12b.AC2, F12b.AC3, F12b.AC4, F12b.AC5, F12b.AC6, F12b.AC7, F12b.AC8, F12b.AC9, F12b.AC10, F12b.AC11, F12b.AC12, F12b.AC13, F12b.AC14, F12b.AC15, F12b.AC16, F12b.AC17]
integration_checks: ["cmd:go build ./... && go vet ./... && test -z \"$(gofmt -l .)\"", "cmd:go test ./...", "cmd:bash scripts/e2e.sh", "cmd:bash scripts/coverage.sh --enforce", "cmd:bash scripts/check-image.sh", "cmd:bash scripts/check-vuln.sh", "cmd:bash scripts/spamcheck.sh --self-test"]
```

## Forme du plan

92 phases en 14 vagues, au plus 8 phases par vague (vagues 2, 3, 4, 5, 6, 7, 8, 9, 10). Modèles : opus 23, sonnet 69.

Chemin critique : 14 maillons, jusqu'à la phase P12b-zza. Les services de chaque domaine sont séparés de leurs pages pour que les pages, feuilles du graphe, s'exécutent en parallèle sans allonger le chemin. Toute phase d'une spec marquée risk passe sur opus (règle du validateur).

Replan additif pendant l'exécution (vague 2 en cours) : phases `P2b-store` (migrations et magasin SQL des jetons et codes d'accès, hachage à clé ; les liens magiques ont déjà les leurs) et `P11-store` (migrations et magasins SQL de l'inscription) ajoutées ; `P6-queue` et `P10-dash` possèdent désormais les migrations de `internal/mail` et `internal/admin`.

Replan internationalisation (vague 2 terminée) : fonctionnalité `F13` (catalogues français et anglais par module, dossier externe `I18N_DIR` pour ajouter une langue, négociation de la langue, fonctions `t` et `tn`, sélecteur, outils `i18n export` et `i18n check`, garde-fous contre le texte brut, `APP_DEFAULT_LANG`, champ `Code` de `errs`) en quatre phases parallèles, et `F13b` (relecture des textes anglais) ; chaque phase d'interface ou de courrier possède ses catalogues `web/static/i18n/<module>/` à la place du fichier central `fr.json` ; les fonctionnalités à gabarits dépendent de `F13` ; la phase `P4-engine2` est absorbée par `P4-engine`, qui a livré les étapes F4.4 et F4.5 ; critères de pages sur la CSP stricte et `RequireUnsafeMethod`. Les vagues figées n'ont pas bougé ; seules les vagues restantes sont recalculées.

## Fichiers d'intégration

Écrits seulement par l'orchestrateur, jamais par une phase : `internal/app/**` (assemblage, routes, registre des migrations dans l'ordre des dépendances, enregistrement des entrées de navigation, branchement du catalogue de textes et du middleware de langue), `go.mod` et `go.sum` (créés dans le premier commit avec toutes les dépendances prévues), `tests/e2e/package-lock.json` après la phase `P1c-infra` qui le possède, et `web/templates/layout/**` après la phase `P1c-layout` qui le possède. Les textes de l'interface ne sont plus un fichier central : chaque phase possède ses catalogues `web/static/i18n/<module>/fr.json` et `en.json`.

Structure qui limite les collisions : chaque fonctionnalité expose un enregistreur propre (`Register(...)`) et ses migrations dans son paquet ; `internal/app/` ne contient que la liste d'appels, une ligne par fonctionnalité.

## Execution protocol

Executed by `/wave-planning:wave-execute`. See that skill for the worktree, dispatch, gate and merge procedure.

## Status

```yaml status
base_branch: main
integration_branch: wave/integration
integration_head: efae8ea0678209de2adc9265208b16936a3e925c
W1: done
W2: done
W3: done
W4: running
W5: pending
W6: pending
W7: pending
W8: pending
W9: pending
W10: pending
W11: pending
W12: pending
W13: pending
W14: pending
P1a: done
P1b-db: done
P1b-sh1: done
P1b-sh2: done
P1c-ci: done
P1c-entry: done
P1c-infra: done
P2-groups: done
P2b-a: done
P2b-b: done
P2c-websec: done
P4-engine: done
P1c-docker: done
P1c-layout: done
P3-core: done
P3-rules: done
P6b-msg: done
P6b-dkim: done
P2b-store: done
P13-catalog: done
P5-letter: gated
P8-ssrf: gated
P9-illus: gated
P9-theme: gated
P3-roles: gated
P13-negotiate: gated
P13-guard: gated
P13-config: gated
P6-queue: pending
P6-templates: pending
P3d-enrol: pending
P3d-rulechange: pending
P4b-core: pending
P2e-pages1: pending
P2e-pages2: pending
P2e-settings: pending
P6-transport: pending
P6-unsub: pending
P8b-enrich: pending
P12a-bench: pending
P4b-reveal: pending
P8b-sources: pending
P9c-core: pending
P3d-roles: pending
P7-rules: pending
P6c-diag: pending
P6c-suppress: pending
P11-code: pending
P11-invite: pending
P9c-envelope: pending
P9c-gift: pending
P4b-multi: pending
P7-engine: pending
P9c-scratch: pending
P11-verify: pending
P4c-adjust: pending
P5b-read: pending
P4d-pages: pending
P8c-service: pending
P11-store: pending
P12a-docs: pending
P6c-page: pending
P10b-consult: pending
P4c-repair: pending
P5c-mailbox: pending
P5c-pages: pending
P8c-page: pending
P11-roleopt: pending
P10b-ops: pending
P9c-seq: pending
P7b-notify: pending
P4d-roles: pending
P11c-public: pending
P4c-roles: pending
P5c-roles: pending
P8c-multi: pending
P10-dash: pending
P10-menu: pending
P10-settings: pending
P11c-admin: pending
P10-roles: pending
P10-draw: pending
P10-people: pending
P11c-roles: pending
P12b-cov: pending
P12b-jA: pending
P12b-jB: pending
P12b-jC: pending
P13b-review: pending
P12b-zza: pending
P12b-zzb: pending
P12b-zzc: pending
```
