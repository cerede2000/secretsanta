---
id: F4d
title: Pages de l'attente et de la cible
depends_on: [F4b, F2e, F2c, F1c, F9, F3, F13]
owns: [internal/targetpage/**, web/templates/draw/**, web/static/js/draw/**, web/static/css/draw/**, tests/e2e/draw/**, web/static/i18n/draw/**]
reads: [internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/draw/**, internal/reveal/**, internal/groups/**, docs/waves/design/snapshot.html#screen-attente-revelation, docs/waves/design/snapshot.html#screen-ma-cible, internal/participants/**, docs/waves/design/snapshot.html#screen-reception-seule]
risk: []
steps: [F4d.1, F4d.2, F4d.3]
---

# F4d Pages de l'attente et de la cible

## Summary

Cette fonctionnalité affiche au participant l'attente de la révélation (compte à rebours), sa préférence « un seul cadeau » avant le tirage, l'accueil des personnes de rôle `reception_seule` et, une fois la cible découverte, sa carte de cible (une ou deux cibles) sans animation, avec le budget et le bandeau « Budget mis à jour » seulement quand ils existent. L'animation d'ouverture est dans F9c.

## In scope

- Pages de l'attente de la révélation (compte à rebours en module JavaScript natif qui améliore une page rendue par le serveur) et de la carte de cible sans animation (screen-attente-revelation, screen-ma-cible), gestionnaires dans `internal/targetpage/`, protégées par la session participant de F2c ; la cible n'est jamais rendue avant l'ouverture de la révélation.
- Budget facultatif : la carte de cible et l'écran d'attente n'affichent le budget du groupe (lu depuis F2) que s'il est défini ; sans budget, aucune mention ni espace réservé. Quand le budget change (`budget_version` de F2), un bandeau « Budget mis à jour » s'affiche côté participant tant qu'il ne l'a pas vu (version vue mémorisée par participant dans les tables de F4), puis disparaît ; un budget retiré fait disparaître la mention, un budget redéfini la fait réapparaître avec le bandeau.
- À brancher par l'orchestrateur : routes dans `internal/app/`, textes dans les catalogues `web/static/i18n/<module>/fr.json` et `en.json` (français et anglais, un dossier par module, propriété de la phase concernée), migrations (`internal/targetpage/migrations/`) dans le registre commun.
- Préférence du participant (sur la page d'attente, écran screen-attente-revelation) : « Je préfère n'offrir qu'un seul cadeau », qui bascule le rôle entre `normal` (défaut) et `un_seul` via le service de F3, modifiable jusqu'au tirage puis affichée en lecture seule avec l'explication que seul l'organisateur peut la modifier. Elle n'est proposée que si le rôle courant est `normal` ou `un_seul` : jamais à une personne `reception_seule` ou `offre_plus` (rôles posés par l'administrateur ou à l'inscription), et jamais d'accès à ces deux rôles par cette page.
- Carte de cible à une ou deux cibles : chaque cible a sa carte, la seconde reste « à découvrir » tant que son drapeau de révélation n'est pas posé, et chaque cible renvoie vers sa lettre (F5c) et ses suggestions (F8c).
- Accueil d'une personne `reception_seule` (screen-reception-seule) : elle reçoit un cadeau et n'en offre aucun ; la page n'affiche ni cible, ni compte à rebours de révélation, ni bouton de découverte, mais invite à écrire sa lettre au Père Noël (lien vers F5c) et rappelle le budget et le jour J seulement s'ils sont définis.
- Internationalisation (F13) : aucun texte brut dans les gabarits, les messages ni les courriels de cette fonctionnalité ; tous passent par les fonctions `t` et `tn` avec des clés préfixées par le module dans `web/static/i18n/<module>/fr.json` et `en.json` (français et anglais, mêmes clés, mêmes paramètres). Les textes affichés par du JavaScript sont fournis par le gabarit en attributs `data-*` produits avec `t`, jamais écrits en dur dans le script.
- Contraintes web héritées de F2c : tout gestionnaire qui modifie l'état est enveloppé par `websec.RequireUnsafeMethod` (une requête GET ne modifie jamais rien) ; aucune page n'utilise d'attribut `style` en ligne ni de ressource `data:` (CSP stricte : classes et CSS dans `web/static/css/**`, images servies par une route de même origine).

## Out of scope

- Données de révélation et visibilité (F4b), animation d'ouverture (F9c), pages d'administration du tirage (F10).

## Acceptance criteria

```yaml criteria
AC1: {text: "La carte de cible et l'écran d'attente n'affichent aucune mention de budget quand il est vide et l'affichent quand il est défini ; le bandeau « Budget mis à jour » apparaît après un changement tant que le participant ne l'a pas vu puis disparaît ; un budget retiré puis redéfini réapparaît avec le bandeau", verify: "test:internal/targetpage/budget_display_test.go"}
AC2: {text: "Les pages de compte à rebours et de carte de cible n'ont aucun défilement horizontal et leurs zones tactiles mesurent au moins 44 px en 360×640 et 390×844", verify: "cmd:cd tests/e2e && npx playwright test draw"}
AC3: {text: "L'écran d'attente de la révélation (compte à rebours) correspond à la maquette approuvée", verify: "design-fidelity:screen-attente-revelation"}
AC4: {text: "L'écran de la carte de cible correspond à la maquette approuvée", verify: "design-fidelity:screen-ma-cible"}
AC5: {text: "La page d'attente propose la préférence « un seul cadeau » qui bascule entre normal et un_seul jusqu'au tirage puis la verrouille avec explication, case non cochée par défaut, et ne la propose jamais à une personne reception_seule ou offre_plus", verify: "test:internal/targetpage/preference_test.go"}
AC6: {text: "La carte de cible affiche une ou deux cibles, la seconde restant à découvrir tant que son drapeau n'est pas posé, chacune avec ses liens vers la lettre et les suggestions", verify: "test:internal/targetpage/multi_target_card_test.go"}
AC7: {text: "Une personne reception_seule voit l'accueil de réception seule sans cible, sans compte à rebours de révélation ni bouton de découverte, avec l'invitation à écrire sa lettre", verify: "test:internal/targetpage/reception_only_page_test.go"}
AC8: {text: "L'écran d'accueil d'une personne en réception seule correspond à la maquette approuvée", verify: "design-fidelity:screen-reception-seule"}
AC9: {text: "Les pages de la préférence, des deux cibles et de la réception seule n'ont aucun défilement horizontal et leurs zones tactiles mesurent au moins 44 px en 360×640 et 390×844", verify: "cmd:cd tests/e2e && npx playwright test draw/reception-only.spec.ts draw/preference.spec.ts"}
AC10: {text: "Les pages d'attente et de cible : tous les textes passent par les catalogues avec les clés fr et en du module, et le rendu avec la langue anglaise ne contient aucune clé brute ni texte français", verify: "test:internal/targetpage/i18n_pages_test.go"}
AC11: {text: "Les pages d'attente et de cible : tout gestionnaire qui modifie l'état est enveloppé par RequireUnsafeMethod (un GET sur une route modifiante renvoie 405 sans rien modifier) et aucun gabarit n'utilise d'attribut style en ligne ni de ressource data:", verify: "test:internal/targetpage/csp_unsafe_methods_test.go"}
```

## Steps

- F4d.1 Date de révélation : drapeau serveur, tâche planifiée, courriel « tirage prêt », « ouvrir maintenant », compte à rebours (JavaScript natif) et carte de cible sans animation, conformes aux écrans screen-attente-revelation et screen-ma-cible.
- F4d.2 Affichage conditionnel du budget et bandeau « Budget mis à jour » (version vue mémorisée par participant) sur la carte de cible et l'écran d'attente.
- F4d.3 Préférence « un seul cadeau » (normal ou un_seul) avant le tirage, carte de cible à une ou deux cibles et accueil des personnes `reception_seule` (screen-reception-seule) avec tests Playwright mobiles.
