---
id: F9c
title: Animation de découverte de la cible
depends_on: [F9, F1c, F13]
owns: [web/static/css/reveal/**, web/static/js/reveal/**, web/templates/reveal/**, tests/e2e/reveal/**, web/static/i18n/reveal/**]
reads: [web/templates/layout/**, web/static/css/base/**, web/static/css/theme/**, tests/e2e/_shared/**, docs/waves/design/snapshot.html#screen-revelation, docs/waves/design/snapshot.html#screen-revelation-b, docs/waves/design/snapshot.html#screen-revelation-c]
risk: []
steps: [F9c.1, F9c.2, F9c.3, F9c.4, F9c.5]
---

# F9c Animation de découverte de la cible

## Summary

Le moment fort de l'application : trois styles d'animation de découverte (cadeau enveloppé, enveloppe à sceau de cire, carte à gratter), tirés au hasard à chaque première découverte sans jamais répéter le dernier style vu. Neige, confettis, lueur dorée, vibration, son désactivé par défaut, repli en fondu si l'utilisateur réduit les animations.

## In scope

- Animation de découverte, trois styles tous réalisés, modules dans `web/static/js/reveal/` et gabarit dans `web/templates/reveal/` : (1) cadeau enveloppé (screen-revelation) à ouvrir par appui long ou glissement, ruban qui se dénoue ; (2) enveloppe à sceau de cire (screen-revelation-b) : toucher le sceau, il se brise, la lettre sort ; (3) carte à gratter (screen-revelation-c) : glisser pour gratter, la cible se révèle dès que 50 % de la surface est grattée. Dans les trois, neige et confettis, nom de la cible avec lueur dorée, vibration si disponible, son désactivé par défaut (option explicite pour l'activer). Alternatives accessibles pour chaque style : bouton équivalent au clavier et au toucher simple.
- Choix aléatoire du style (décision de l'utilisateur) : à chaque première découverte, un sélecteur `pickStyle(dernierStyle, aléa)` tire au hasard parmi les trois styles et ne renvoie jamais le même style que le dernier vu d'affilée. Le dernier style vu est fourni par l'interface injectée et mémorisé côté navigateur, sans bloquer si le stockage est indisponible. Une graine de test peut forcer le tirage. Quand le tirage est refait (révélation réarmée), un nouveau style est tiré.
- Dégradation : si prefers-reduced-motion est actif, l'animation de chacun des trois styles est remplacée par un fondu ; fluidité visée sur téléphone modeste (animations par transform et opacity uniquement).
- Confettis à la révélation (JavaScript natif, canvas ou éléments animés par transform et opacity). Aucune boîte du navigateur (alert, confirm) : tout passe par l'interface.
- Le module et la page de révélation reçoivent leurs données par une interface injectée (la cible et la fonction « confirmer la révélation ») ; le branchement à l'API de F4 (requête protégée par le jeton CSRF de F2) est fait par l'orchestrateur. Les tests utilisent une fausse source. Le JavaScript est servi en modules ES depuis `/static/`, sans script en ligne (CSP de F2).
- À brancher par l'orchestrateur : route de la page de révélation dans `internal/app/` (branchement à l'API de F4b, protégé par le jeton CSRF de F2c), textes dans les catalogues `web/static/i18n/<module>/fr.json` et `en.json` (français et anglais, un dossier par module, propriété de la phase concernée).
- Deux cibles : quand un tireur a deux cibles, l'interface injectée fournit la liste des cibles ; la première se découvre avec un style tiré au hasard, puis un bouton « Découvrir mon second cadeau » lance la seconde avec un autre style (`pickStyle` reçoit le dernier style vu, donc jamais le même d'affilée, même entre les deux cibles) ; la confirmation de révélation est envoyée une seule fois par cible ; le repli prefers-reduced-motion s'applique aux deux.
- Internationalisation (F13) : aucun texte brut dans les gabarits, les messages ni les courriels de cette fonctionnalité ; tous passent par les fonctions `t` et `tn` avec des clés préfixées par le module dans `web/static/i18n/<module>/fr.json` et `en.json` (français et anglais, mêmes clés, mêmes paramètres). Les textes affichés par du JavaScript sont fournis par le gabarit en attributs `data-*` produits avec `t`, jamais écrits en dur dans le script.
- Contraintes web héritées de F2c : tout gestionnaire qui modifie l'état est enveloppé par `websec.RequireUnsafeMethod` (une requête GET ne modifie jamais rien) ; aucune page n'utilise d'attribut `style` en ligne ni de ressource `data:` (CSP stricte : classes et CSS dans `web/static/css/**`, images servies par une route de même origine).
- Chaque phase de révélation possède son gabarit `web/templates/reveal/<variante>/` et ses catalogues `web/static/i18n/reveal/<variante>/` ; la phase de cœur possède `web/templates/reveal/` pour la page et `web/static/i18n/reveal/core/`.

## Out of scope

- API de révélation et drapeau serveur (F4b), carte statique de cible (F4d), thème (F9).

## Acceptance criteria

```yaml criteria
AC1: {text: "Le son est désactivé par défaut et ne se joue que sur activation explicite", verify: "test:tests/e2e/reveal/core/sound-default-off.spec.ts"}
AC2: {text: "Aucun alert() ni confirm() n'est utilisé dans le code du thème et de la révélation", verify: "cmd:! grep -rnE '\\b(alert|confirm)\\(' web/static/js/theme web/static/js/reveal web/templates/reveal"}
AC3: {text: "Le sélecteur de style, avec une graine forcée sur 300 tirages, renvoie les trois styles au moins une fois et jamais deux fois le même style d'affilée, et un nouveau tirage de révélation en choisit un autre", verify: "test:tests/e2e/reveal/core/style-picker.spec.ts"}
AC4: {text: "Style cadeau : l'appui long et le glissement ouvrent le cadeau et déclenchent une seule fois la confirmation de révélation, et le bouton équivalent produit le même résultat", verify: "test:tests/e2e/reveal/gift/gesture-and-button.spec.ts"}
AC5: {text: "L'écran de révélation en style cadeau correspond à la maquette approuvée", verify: "design-fidelity:screen-revelation"}
AC6: {text: "Style enveloppe à sceau de cire : toucher le sceau le brise et fait sortir la lettre, la confirmation de révélation n'est déclenchée qu'une fois, et le bouton équivalent au clavier produit le même résultat", verify: "test:tests/e2e/reveal/envelope/envelope-seal.spec.ts"}
AC7: {text: "L'écran de révélation en style enveloppe à sceau de cire correspond à la maquette approuvée", verify: "design-fidelity:screen-revelation-b"}
AC8: {text: "Style carte à gratter : la cible se révèle quand 50 % de la surface est grattée (pas avant), la confirmation n'est déclenchée qu'une fois, et le bouton équivalent produit le même résultat", verify: "test:tests/e2e/reveal/scratch/scratch-card.spec.ts"}
AC9: {text: "L'écran de révélation en style carte à gratter correspond à la maquette approuvée", verify: "design-fidelity:screen-revelation-c"}
AC10: {text: "Avec prefers-reduced-motion, les trois styles sont remplacés par un fondu et la cible reste affichée", verify: "cmd:npx playwright test tests/e2e/reveal/gift/reduced-motion.spec.ts tests/e2e/reveal/envelope/reduced-motion.spec.ts tests/e2e/reveal/scratch/reduced-motion.spec.ts"}
AC11: {text: "Les animations des trois styles n'utilisent que transform et opacity et chaque révélation tient à 30 images par seconde ou plus avec ralentissement CPU x4", verify: "cmd:npx playwright test tests/e2e/reveal/gift/performance.spec.ts tests/e2e/reveal/envelope/performance.spec.ts tests/e2e/reveal/scratch/performance.spec.ts"}
AC12: {text: "Les pages de révélation des trois styles n'ont aucun défilement horizontal et leurs zones tactiles mesurent au moins 44 px en 360×640 et 390×844", verify: "cmd:npx playwright test tests/e2e/reveal/core/mobile.spec.ts tests/e2e/reveal/gift/mobile.spec.ts tests/e2e/reveal/envelope/mobile.spec.ts tests/e2e/reveal/scratch/mobile.spec.ts"}
AC13: {text: "Avec deux cibles, la première se révèle avec un style tiré et la seconde avec un style différent, chaque cible déclenche sa propre confirmation une seule fois et la seconde ne se lance qu'après la première", verify: "test:tests/e2e/reveal/core/multi-target.spec.ts"}
AC14: {text: "Avec deux cibles et prefers-reduced-motion actif, les deux révélations sont des fondus et les deux cibles restent affichées", verify: "cmd:npx playwright test tests/e2e/reveal/core/multi-target-reduced-motion.spec.ts"}
AC15: {text: "La page de la seconde révélation n'a aucun défilement horizontal et ses zones tactiles mesurent au moins 44 px en 360×640 et 390×844", verify: "cmd:npx playwright test tests/e2e/reveal/core/multi-target-mobile.spec.ts"}
AC16: {text: "Le cœur de la révélation : aucun texte brut, clés fr et en du module reveal/core et rendu en anglais sans clé brute", verify: "cmd:npx playwright test tests/e2e/reveal/core/i18n.spec.ts"}
AC17: {text: "Le cœur de la révélation respecte la CSP stricte : aucun attribut style en ligne ni data: et aucune violation de politique relevée par le navigateur", verify: "cmd:npx playwright test tests/e2e/reveal/core/csp.spec.ts"}
AC18: {text: "La révélation du cadeau : textes en français et en anglais sans clé brute, fournis par le gabarit en attributs data", verify: "cmd:npx playwright test tests/e2e/reveal/gift/i18n.spec.ts"}
AC19: {text: "La révélation de l'enveloppe : textes en français et en anglais sans clé brute, fournis par le gabarit en attributs data", verify: "cmd:npx playwright test tests/e2e/reveal/envelope/i18n.spec.ts"}
AC20: {text: "La révélation de la carte à gratter : textes en français et en anglais sans clé brute, fournis par le gabarit en attributs data", verify: "cmd:npx playwright test tests/e2e/reveal/scratch/i18n.spec.ts"}
```

## Steps

- F9c.1 Module commun d'animation (interface injectée, confettis, vibration, son optionnel, repli en fondu), page screen-revelation et sélecteur aléatoire `pickStyle` (jamais deux fois le même d'affilée, graine de test).
- F9c.2 Style cadeau enveloppé (screen-revelation) : appui long ou glissement, ruban qui se dénoue, bouton équivalent, repli en fondu, tests de performance et mobiles.
- F9c.3 Style enveloppe à sceau de cire (screen-revelation-b) : toucher le sceau, il se brise, la lettre sort, bouton équivalent, repli en fondu, tests de performance et mobiles.
- F9c.4 Style carte à gratter (screen-revelation-c) : seuil de 50 % de surface grattée, bouton équivalent, repli en fondu, tests de performance et mobiles.
- F9c.5 Séquence de deux révélations (deux cibles, styles successifs différents, une confirmation par cible, repli en fondu) avec tests Playwright mobiles.
