---
id: F1c
title: "Point d'entrée, squelette web et livraison"
depends_on: [F1, F1b]
owns: [cmd/**, internal/health/**, web/embed.go, web/templates/layout/**, web/static/css/base/**, tests/e2e/package.json, tests/e2e/package-lock.json, tests/e2e/playwright.config.ts, tests/e2e/_shared/**, tests/e2e/foundation/**, tests/e2e/docker/**, Dockerfile, .dockerignore, docker-compose.yml, .github/workflows/ci.yml, VERSION, Makefile, web/static/i18n/layout/**, web/embed_test.go]
reads: [internal/config/**, internal/db/**, internal/shared/**]
risk: []
steps: [F1c.1, F1c.2, F1c.3, F1c.4, F1c.5]
---

# F1c Point d'entrée, squelette web et livraison

## Summary

Cette fonctionnalité assemble le socle exécutable : le point d'entrée `cmd/secretsanta` avec /health et sa sous-commande healthcheck, le squelette web (gabarits, CSS de base, gabarit d'administration à navigation basse, configuration Playwright mobile), l'image Docker minimale non-root, le docker-compose d'exemple et la CI GitHub qui publie l'image amd64 sur GHCR avec versions propres et rétention de 4 versions.

## In scope

- Route /health (base joignable, version) ; arrêt propre sur SIGTERM. Sous-commande `secretsanta healthcheck` du même binaire (appel local de /health, code de sortie 0 ou 1), car l'image finale ne contient pas de shell.
- Squelette web : gabarit de mise en page `web/templates/layout/` (html/template), CSS de base `web/static/css/base/` mobile d'abord, `web/embed.go` (go:embed des gabarits et des assets statiques), helpers de tests mobile (`tests/e2e/_shared/`) vérifiant l'absence de défilement horizontal et les zones tactiles de 44 px minimum, configuration Playwright avec deux projets 360×640 et 390×844.
- Gabarit d'administration `web/templates/layout/admin.html` et feuille `web/static/css/base/admin-nav.css` : sur téléphone, barre de navigation basse persistante à 5 entrées dans l'ordre Tableau, Participants, Tirage, Inscriptions, Plus (icônes SVG de trait et libellés, cibles d'au moins 44 px, accessible au pouce, entrée active marquée par un trait et du gras en plus de la couleur) ; à partir de 1024 px, barre latérale à la place de la barre basse. Les entrées viennent du registre `internal/shared/adminnav/` de F1b.
- Dockerfile multi-étapes (compilation avec CGO_ENABLED=0, puis image finale minimale distroless static ou scratch, utilisateur non root, HEALTHCHECK par `secretsanta healthcheck`), docker-compose d'exemple lisant ses secrets depuis l'environnement sans aucune valeur par défaut, volume de données.
- Chaîne de contrôle : `go vet ./...`, `gofmt -l .` (doit être vide), `go test ./...`, `CGO_ENABLED=0 go build ./cmd/secretsanta`, `govulncheck ./...`, `npx playwright test` (dans `tests/e2e/`). Linter : go vet et gofmt seuls (choix le plus simple, `.golangci.yml` seulement si utile). Un Makefile facultatif regroupe ces commandes.
- CI et publication (dépôt public https://github.com/cerede2000/secretsanta, vide ; le premier commit est fait par l'orchestrateur avant les vagues). Workflow dans `.github/workflows/`, permissions minimales (contents: write, packages: write). Contrôles avant toute publication : gofmt vide, go vet, go test, build statique sans CGO, govulncheck. Sur pull request : contrôles seulement, aucune publication. Image x64 uniquement (linux/amd64, ni ARM ni multi-arch), publiée sur ghcr.io/cerede2000/secretsanta depuis la branche principale.
- Versions : fichier `VERSION` à la racine contenant « 0.1 » (majeure.mineure, modifié à la main). À chaque publication le workflow calcule le patch : 0.1.0 au premier build, puis 0.1.1, 0.1.2… (nombre de tags Git existants de la ligne 0.1 ; repart à 0 quand VERSION passe à 0.2). Tags d'image : `<version>` et `latest`, jamais de hash de commit. Le workflow crée aussi le tag Git `v<version>`.
- Rétention : à chaque publication, ne garder que les 4 dernières versions de l'image (suppression des versions de paquet GHCR plus anciennes et des versions sans tag ou orphelines, sans jamais supprimer `latest` ni les 4 plus récentes) ; suppression des tags Git `v<version>` et des releases correspondants hors des 4 plus récents. Le calcul de version et le calcul de rétention sont des outils Go isolés et testables (`cmd/ci/version/`, `cmd/ci/retention/`), appelés par le workflow, testés avec des jeux de versions factices.
- Fichiers d'intégration à brancher par l'orchestrateur (hors owns de toute phase) : `internal/app/` (assemblage, routes, registre des migrations), les catalogues de textes `web/static/i18n/<module>/<langue>.json` (un dossier par module, français et anglais, propriété de chaque phase ; le module `core` et le chargeur `internal/i18n` relèvent de F13), `go.mod` et `go.sum`. L'orchestrateur les crée au début, dans le premier commit du dépôt vide, avec `.gitignore`, la route /health, une page d'accueil vide et **toutes les dépendances Go prévues** (modernc.org/sqlite, go-mail, pgregory.net/rapid, une bibliothèque de QR code Go pure, golang.org/x/text, golang.org/x/net/html) afin que les phases en parallèle n'aient jamais à modifier ces fichiers ; une phase qui a besoin d'une dépendance supplémentaire le signale à l'orchestrateur.
- Internationalisation (F13) : aucun texte brut dans les gabarits, les messages ni les courriels de cette fonctionnalité ; tous passent par les fonctions `t` et `tn` avec des clés préfixées par le module dans `web/static/i18n/<module>/fr.json` et `en.json` (français et anglais, mêmes clés, mêmes paramètres). Les textes affichés par du JavaScript sont fournis par le gabarit en attributs `data-*` produits avec `t`, jamais écrits en dur dans le script.
- Contraintes web héritées de F2c : tout gestionnaire qui modifie l'état est enveloppé par `websec.RequireUnsafeMethod` (une requête GET ne modifie jamais rien) ; aucune page n'utilise d'attribut `style` en ligne ni de ressource `data:` (CSP stricte : classes et CSS dans `web/static/css/**`, images servies par une route de même origine).
- Le gabarit de mise en page inclut le sélecteur de langue discret de F13 (pied de page) et ses textes `layout.*` ; `web/embed.go` embarque les gabarits, les assets et tous les catalogues `web/static/i18n/**/<langue>.json`, y compris les sous-dossiers à plusieurs niveaux (`web/embed_test.go` énumère le système de fichiers embarqué).
- docker-compose.yml montre en commentaire le volume optionnel `./i18n:/i18n` et la variable `I18N_DIR` pour ajouter une langue sans reconstruire l'image.

## Out of scope

- Toute logique métier (groupes, tirage, courriels réels, thème visuel).
- Implémentation SMTP (F6), thème et manifeste web (F9), banc de recette complet (F12a).

## Acceptance criteria

```yaml criteria
AC1: {text: "Aucune valeur par défaut n'existe pour les secrets : .env.example, docker-compose.yml et le Dockerfile ne contiennent aucun mot de passe renseigné", verify: "cmd:bash tests/e2e/docker/no-default-secrets.sh"}
AC2: {text: "GET /health renvoie 200 avec l'état de la base quand le serveur tourne, et la sous-commande healthcheck renvoie le code 0 quand il répond et 1 sinon", verify: "test:internal/health/health_test.go"}
AC3: {text: "La page d'accueil du squelette web n'a aucun défilement horizontal et ses boutons mesurent au moins 44 px en 360×640 et 390×844", verify: "cmd:npx playwright test tests/e2e/foundation"}
AC4: {text: "go vet, gofmt, go test et la compilation statique sans CGO s'exécutent sans erreur", verify: "cmd:go vet ./... && test -z \"$(gofmt -l .)\" && go test ./... && CGO_ENABLED=0 go build ./cmd/secretsanta"}
AC5: {text: "L'image Docker se construit en binaire statique non-root sans shell, son healthcheck (sous-commande healthcheck) passe au vert quand les secrets sont fournis, et le conteneur refuse de démarrer sans eux", verify: "cmd:bash tests/e2e/docker/docker-smoke.sh"}
AC6: {text: "Le workflow CI exécute les contrôles (dont govulncheck) puis publie l'image sur GHCR uniquement depuis la branche principale", verify: "test:cmd/ci/workflow/ci_workflow_test.go"}
AC7: {text: "Le calcul de rétention sur 6 versions factices en garde exactement 4, préserve latest et supprime les versions orphelines sans tag ainsi que les tags Git et releases hors des 4 plus récents", verify: "test:cmd/ci/retention/retention_test.go"}
AC8: {text: "Le calcul de version donne 0.1.0 sans tag, 0.1.2 quand 0.1.0 et 0.1.1 existent, et 0.2.0 quand VERSION vaut 0.2 sans tag 0.2.x", verify: "test:cmd/ci/version/version_test.go"}
AC9: {text: "Le workflow ne cible que linux/amd64 (aucun arm64) et aucun tag d'image n'est dérivé de github.sha", verify: "cmd:! grep -rniE 'arm64|github\\.sha' .github/workflows"}
AC10: {text: "Le workflow est syntaxiquement valide et ne publie pas sur pull request (publication conditionnée à la branche principale, aucune publication déclenchée par pull_request)", verify: "test:cmd/ci/workflow/ci_workflow_test.go"}
AC11: {text: "Le gabarit d'administration affiche en 360×640 et 390×844 une barre de navigation basse à 5 entrées placée dans le tiers inférieur de l'écran, aux cibles d'au moins 44 px, sans défilement horizontal et avec l'entrée active marquée autrement que par la couleur seule ; en 1024 px une barre latérale la remplace", verify: "cmd:npx playwright test tests/e2e/foundation/admin-nav.spec.ts"}
AC12: {text: "Le gabarit de mise en page n'affiche aucun texte brut et aucune clé : ses textes passent par t avec les clés du module layout en français et en anglais, vérifié sur la page d'accueil et sur le gabarit d'administration (le sélecteur de langue et la négociation par cookie relèvent de F13)", verify: "cmd:npx playwright test tests/e2e/foundation/i18n.spec.ts --grep 'aucun texte brut'"}
AC13: {text: "Les pages du squelette respectent la CSP stricte : aucun attribut style en ligne ni ressource data: et aucune violation de politique relevée par le navigateur", verify: "cmd:npx playwright test tests/e2e/foundation/csp.spec.ts"}
AC14: {text: "web/embed.go embarque les gabarits, les assets et tous les catalogues de textes, y compris les sous-dossiers de profondeur deux, ce qu'un test vérifie en énumérant le système de fichiers embarqué", verify: "test:web/embed_test.go"}
AC15: {text: "docker-compose.yml montre en commentaire le volume optionnel ./i18n:/i18n et la variable I18N_DIR sans rendre aucun des deux obligatoires", verify: "cmd:bash tests/e2e/docker/compose-i18n-volume.sh"}
```

## Steps

- F1c.1 Écrire le point d'entrée `cmd/secretsanta`, la route /health et la sous-commande healthcheck avec leurs tests.
- F1c.2 Mettre en place la configuration Playwright 360×640 et 390×844 (`package.json` confiné à `tests/e2e/`) et les helpers de tests mobiles.
- F1c.3 Créer le gabarit de mise en page, le CSS de base, `web/embed.go`, le gabarit d'administration (barre basse sur téléphone, barre latérale à partir de 1024 px) et leurs tests Playwright mobiles.
- F1c.4 Écrire le Dockerfile multi-étapes (binaire statique, image minimale non-root), le docker-compose d'exemple, le test de fumée du conteneur et le contrôle d'absence de secret par défaut.
- F1c.5 Écrire le workflow CI (contrôles, calcul de version, publication amd64 sur GHCR, rétention, tags Git), les outils `cmd/ci/version` et `cmd/ci/retention` et les tests de structure.
