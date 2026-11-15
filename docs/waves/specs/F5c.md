---
id: F5c
title: Pages de la lettre et boîte aux lettres animée
depends_on: [F5, F5b, F2c, F1c, F9, F13]
owns: [internal/letterpages/**, internal/mailboxpage/**, web/templates/letter/**, web/templates/mailbox/**, web/static/js/letter/**, web/static/js/mailbox/**, web/static/css/letter/**, web/static/css/mailbox/**, tests/e2e/letter/**, tests/e2e/mailbox/**, web/static/i18n/letter/**, web/static/i18n/mailbox/**]
reads: [internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/letter/**, internal/letterread/**, internal/groups/**, docs/waves/design/snapshot.html#screen-lettre, docs/waves/design/snapshot.html#screen-boite-lettres, docs/waves/design/snapshot.html#screen-lettre-recue]
risk: []
steps: [F5c.1, F5c.2, F5c.3]
---

# F5c Pages de la lettre et boîte aux lettres animée

## Summary

Cette fonctionnalité donne à la lettre son interface : la page d'écriture pour le tiré (avec l'arrivée de bienvenue après inscription), la page de lecture seule pour le tireur (budget rappelé seulement s'il est défini) et la boîte aux lettres animée qui sert de fil conducteur, coupée par prefers-reduced-motion.

## In scope

- Budget facultatif : en tête de la lettre reçue (screen-lettre-recue), le budget du groupe (lu depuis F2) n'est rappelé que s'il est défini ; sans budget, ni mention ni espace réservé. Un changement ou un retrait de budget met l'affichage à jour sans toucher à la lettre ni à sa version.
- Boîte aux lettres animée (screen-boite-lettres) comme fil conducteur : états vide, brouillon, envoyée, lue ; animation en CSS et JavaScript natif, coupée par prefers-reduced-motion.
- Page d'écriture de la lettre (screen-lettre) pour le tiré et page de lecture seule (screen-lettre-recue) pour le tireur ; formulaires protégés par le jeton CSRF de F2.
- Arrivée depuis l'inscription par lien (F11) : la page d'écriture accepte le paramètre `bienvenue=1`, qui affiche un bandeau d'accueil incitant à écrire sa lettre et place le focus sur le texte d'introduction ; le paramètre ne change aucun droit d'accès et la lettre reste écrivable avant le tirage.
- À brancher par l'orchestrateur : routes dans `internal/app/`, textes dans les catalogues `web/static/i18n/<module>/fr.json` et `en.json` (français et anglais, un dossier par module, propriété de la phase concernée).
- Deux lettres reçues : la boîte aux lettres liste une lettre reçue par cible déjà découverte (« Cadeau 1 », « Cadeau 2 »), sans autre identité que la cible elle-même une fois découverte, et la page de lecture sait afficher l'une ou l'autre. Une personne de rôle `reception_seule` voit sa boîte avec « Ma lettre » seulement, sans entrée de lettre reçue.
- Internationalisation (F13) : aucun texte brut dans les gabarits, les messages ni les courriels de cette fonctionnalité ; tous passent par les fonctions `t` et `tn` avec des clés préfixées par le module dans `web/static/i18n/<module>/fr.json` et `en.json` (français et anglais, mêmes clés, mêmes paramètres). Les textes affichés par du JavaScript sont fournis par le gabarit en attributs `data-*` produits avec `t`, jamais écrits en dur dans le script.
- Contraintes web héritées de F2c : tout gestionnaire qui modifie l'état est enveloppé par `websec.RequireUnsafeMethod` (une requête GET ne modifie jamais rien) ; aucune page n'utilise d'attribut `style` en ligne ni de ressource `data:` (CSP stricte : classes et CSS dans `web/static/css/**`, images servies par une route de même origine).

## Out of scope

- Services d'écriture et de lecture (F5, F5b), illustrations finales (F9).

## Acceptance criteria

```yaml criteria
AC1: {text: "Les pages screen-lettre, screen-lettre-recue et screen-boite-lettres n'ont aucun défilement horizontal et leurs zones tactiles mesurent au moins 44 px en 360×640 et 390×844", verify: "cmd:npx playwright test tests/e2e/letter tests/e2e/mailbox"}
AC2: {text: "L'écran de lettre correspond à la maquette approuvée", verify: "design-fidelity:screen-lettre"}
AC3: {text: "L'écran de boîte aux lettres animée correspond à la maquette approuvée", verify: "design-fidelity:screen-boite-lettres"}
AC4: {text: "L'animation de la boîte aux lettres est désactivée quand prefers-reduced-motion est actif", verify: "test:tests/e2e/mailbox/reduced-motion.spec.ts"}
AC5: {text: "L'écran de lecture de la lettre reçue correspond à la maquette approuvée", verify: "design-fidelity:screen-lettre-recue"}
AC6: {text: "Avec bienvenue=1 la page d'écriture affiche le bandeau d'accueil et place le focus sur l'introduction sans changer aucun droit d'accès : un tireur ou un étranger n'accède pas à la lettre d'un autre", verify: "test:internal/letterpages/welcome_arrival_test.go"}
AC7: {text: "La lettre reçue n'affiche aucune mention de budget quand il est vide, l'affiche quand il est défini et suit un changement ou un retrait de budget sans modifier la lettre ni sa version", verify: "test:internal/letterpages/budget_display_test.go"}
AC8: {text: "La boîte aux lettres d'un tireur à deux cibles liste une lettre reçue par cible découverte et la page de lecture affiche chacune sans mélanger les deux", verify: "test:internal/mailboxpage/two_received_letters_test.go"}
AC9: {text: "La boîte aux lettres d'une personne reception_seule n'affiche que « Ma lettre » et aucune lettre reçue", verify: "test:internal/mailboxpage/reception_only_mailbox_test.go"}
AC10: {text: "Les pages de deux lettres reçues n'ont aucun défilement horizontal et leurs zones tactiles mesurent au moins 44 px en 360×640 et 390×844", verify: "cmd:npx playwright test tests/e2e/mailbox/two-letters.spec.ts"}
AC11: {text: "Les pages de la lettre au Père Noël : tous les textes passent par les catalogues avec les clés fr et en du module, et le rendu avec la langue anglaise ne contient aucune clé brute ni texte français", verify: "test:internal/letterpages/i18n_pages_test.go"}
AC12: {text: "Les pages de la lettre au Père Noël : tout gestionnaire qui modifie l'état est enveloppé par RequireUnsafeMethod (un GET sur une route modifiante renvoie 405 sans rien modifier) et aucun gabarit n'utilise d'attribut style en ligne ni de ressource data:", verify: "test:internal/letterpages/csp_unsafe_methods_test.go"}
AC13: {text: "La page de la boîte aux lettres : tous les textes passent par les catalogues avec les clés fr et en du module, et le rendu avec la langue anglaise ne contient aucune clé brute ni texte français", verify: "test:internal/mailboxpage/i18n_pages_test.go"}
AC14: {text: "La page de la boîte aux lettres : tout gestionnaire qui modifie l'état est enveloppé par RequireUnsafeMethod (un GET sur une route modifiante renvoie 405 sans rien modifier) et aucun gabarit n'utilise d'attribut style en ligne ni de ressource data:", verify: "test:internal/mailboxpage/csp_unsafe_methods_test.go"}
```

## Steps

- F5c.1 Pages screen-lettre (écriture, avec arrivée `bienvenue=1`) et screen-lettre-recue (lecture seule, budget affiché seulement s'il est défini) avec messages intégrés à l'interface.
- F5c.2 Page screen-boite-lettres animée, repli prefers-reduced-motion et tests Playwright mobiles.
- F5c.3 Boîte aux lettres et lecture pour un tireur à deux cibles, boîte d'une personne `reception_seule`, tests Playwright mobiles.
