---
id: F8c
title: Service de suggestions et page du tireur
depends_on: [F8b, F2, F3, F4b, F5, F2c, F1c, F9, F13]
owns: [internal/suggestions/**, internal/suggestionpages/**, web/templates/suggestions/**, web/static/js/suggestions/**, web/static/css/suggestions/**, tests/e2e/suggestions/**, web/static/i18n/suggestions/**]
reads: [internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/sources/**, internal/enrich/**, internal/ssrf/**, internal/groups/**, internal/letter/**, internal/participants/**, internal/draw/**, docs/waves/design/snapshot.html#screen-suggestions]
risk: []
steps: [F8c.1, F8c.2, F8c.3]
---

# F8c Service de suggestions et page du tireur

## Summary

Le tireur reçoit des propositions adaptées au budget du groupe quand il en a un (sinon toutes gammes avec un filtre de prix libre), au thème de Noël et à la lettre de son tiré, sans qu'aucune donnée personnelle ne sorte de l'application. Le service met en cache, ménage les sources et réserve l'accès au tireur de la cible.

## In scope

- Suggestion : titre, prix (quand connu), image copiée en local dans le volume (jamais de lien externe direct affiché), lien d'origine. Résultats filtrés par le budget du groupe quand il est défini (budget facultatif en euros, F2) ; sans budget, toutes les gammes de prix sont proposées avec un filtre de prix libre (minimum et maximum facultatifs saisis par le tireur) ; thème Noël. Un budget modifié, retiré ou redéfini s'applique dès l'affichage suivant (le cache est indexé par budget).
- Les suggestions s'appuient sur la lettre du tiré (mots-clés des idées) quand elle existe, sans jamais la citer à quelqu'un d'autre que son tireur ; accès réservé au tireur de la cible.
- Page du tireur (screen-suggestions) : liste de propositions avec image locale, prix, lien, actualisation, filtre de prix libre (toujours disponible, seul filtre quand le groupe n'a pas de budget) ; message clair quand aucune source n'est disponible.
- Cache et quotas pour ménager les sources externes.
- Aucune donnée personnelle ne sort : les requêtes de recherche ne contiennent que des mots-clés de cadeau, sans adresse courriel, nom ni prénom (les mots-clés tirés de la lettre écartent les jetons correspondant aux noms et prénoms des participants du groupe et à tout motif d'adresse courriel). Les requêtes de récupération d'image ou de page n'envoient ni Referer, ni cookie, ni identifiant. Seules les requêtes de recherche vers SearXNG ou les API configurées et la récupération protégée sortent de l'application.
- À brancher par l'orchestrateur : routes dans `internal/app/`, textes dans les catalogues `web/static/i18n/<module>/fr.json` et `en.json` (français et anglais, un dossier par module, propriété de la phase concernée), migrations (`internal/suggestions/migrations/`) dans le registre commun.
- Deux cadeaux : un tireur à deux cibles choisit le cadeau concerné (« Cadeau 1 », « Cadeau 2 ») sur la page de suggestions ; les suggestions, le cache et les mots-clés sont propres à chaque cible, et chaque cible n'est accessible qu'à son tireur. Une personne de rôle `reception_seule` n'a pas de page de suggestions.
- Internationalisation (F13) : aucun texte brut dans les gabarits, les messages ni les courriels de cette fonctionnalité ; tous passent par les fonctions `t` et `tn` avec des clés préfixées par le module dans `web/static/i18n/<module>/fr.json` et `en.json` (français et anglais, mêmes clés, mêmes paramètres). Les textes affichés par du JavaScript sont fournis par le gabarit en attributs `data-*` produits avec `t`, jamais écrits en dur dans le script.
- Contraintes web héritées de F2c : tout gestionnaire qui modifie l'état est enveloppé par `websec.RequireUnsafeMethod` (une requête GET ne modifie jamais rien) ; aucune page n'utilise d'attribut `style` en ligne ni de ressource `data:` (CSP stricte : classes et CSS dans `web/static/css/**`, images servies par une route de même origine).

## Out of scope

- Achat, affiliation, API Amazon payante, réponse du tireur au tiré, thème visuel final (F9).

## Acceptance criteria

```yaml criteria
AC1: {text: "Quand le groupe a un budget, les suggestions le respectent : aucune proposition dont le prix connu dépasse le budget", verify: "test:internal/suggestions/budget_filter_test.go"}
AC2: {text: "Les suggestions peuvent s'appuyer sur la lettre du tiré mais seul son tireur y a accès", verify: "test:internal/suggestions/letter_and_access_test.go"}
AC3: {text: "Les mots-clés envoyés aux sources écartent noms, prénoms et adresses courriel des participants, même quand la lettre les contient", verify: "test:internal/suggestions/keywords_no_personal_data_test.go"}
AC4: {text: "Sans budget, les suggestions couvrent toutes les gammes de prix et le filtre de prix libre (minimum et maximum facultatifs) restreint les résultats ; un budget défini puis retiré, ou retiré puis redéfini, change le filtrage dès l'affichage suivant sans réutiliser le cache de l'ancien budget", verify: "test:internal/suggestions/optional_budget_test.go"}
AC5: {text: "La page de suggestions n'a aucun défilement horizontal et ses zones tactiles mesurent au moins 44 px en 360×640 et 390×844", verify: "cmd:npx playwright test tests/e2e/suggestions/mobile.spec.ts"}
AC6: {text: "L'écran de suggestions correspond à la maquette approuvée", verify: "design-fidelity:screen-suggestions"}
AC7: {text: "Un parcours complet (création de groupe, participants avec noms et courriels reconnaissables, tirage, lettre, suggestions) avec un serveur factice de recherche et de récupération capture toutes les requêtes sortantes (URL, en-têtes, corps) et n'y trouve aucun courriel, nom ni prénom, les seules requêtes sortantes étant les recherches de mots-clés et la récupération protégée", verify: "test:tests/e2e/suggestions/outbound-privacy.spec.ts"}
AC8: {text: "Un tireur à deux cibles obtient des suggestions et un cache distincts par cible, chaque cible n'est accessible qu'à son tireur et une personne reception_seule est refusée", verify: "test:internal/suggestions/multi_target_test.go"}
AC9: {text: "La page de suggestions avec le sélecteur de cadeau n'a aucun défilement horizontal et ses zones tactiles mesurent au moins 44 px en 360×640 et 390×844", verify: "cmd:npx playwright test tests/e2e/suggestions/two-gifts.spec.ts"}
AC10: {text: "La page des suggestions : tous les textes passent par les catalogues avec les clés fr et en du module, et le rendu avec la langue anglaise ne contient aucune clé brute ni texte français", verify: "test:internal/suggestionpages/i18n_pages_test.go"}
AC11: {text: "La page des suggestions : tout gestionnaire qui modifie l'état est enveloppé par RequireUnsafeMethod (un GET sur une route modifiante renvoie 405 sans rien modifier) et aucun gabarit n'utilise d'attribut style en ligne ni de ressource data:", verify: "test:internal/suggestionpages/csp_unsafe_methods_test.go"}
```

## Steps

- F8c.1 Service de suggestions : budget facultatif, filtre de prix libre, thème, mots-clés tirés de la lettre sans nom ni courriel, cache, quotas et contrôle d'accès du tireur.
- F8c.2 Page screen-suggestions, tests Playwright mobiles et test de parcours complet qui capture les requêtes sortantes.
- F8c.3 Suggestions par cadeau pour un tireur à deux cibles (sélecteur, cache et mots-clés par cible) avec tests Playwright mobiles.
