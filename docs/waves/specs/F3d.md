---
id: F3d
title: "Création vérifiée, lien et QR code, règles modifiées après tirage"
depends_on: [F3, F2b, F1b]
owns: [internal/enrolment/**, internal/rulechange/**]
reads: [internal/participants/**, internal/rules/**, internal/access/**, internal/shared/**]
risk: []
steps: [F3d.1, F3d.2, F3d.3]
---

# F3d Création vérifiée, lien et QR code, règles modifiées après tirage

## Summary

Cette fonctionnalité ajoute deux services au-dessus des participants : la création vérifiée d'un participant (utilisée par l'inscription par lien) avec son lien personnel et un service générique de QR code, et la modification des règles d'exclusion, de foyer et de forçage après le tirage, qui interroge le port de réparation de F1.

## In scope

- Sans courriel : l'administrateur remet le lien ou un QR code (le QR est généré côté serveur par une bibliothèque Go pure, sans service externe, pour le lien personnel).
- Port « création de participant vérifié » `CreateVerified(groupID, saisie, verifiedAt)` utilisé par F11 : applique exactement les mêmes règles de nettoyage, de longueur et d'unicité du nom que l'ajout manuel (un seul code de règles, exporté sous les formes `NormalizeName` et `NameExists`, que F11 réutilise aussi pour contrôler les inscriptions en attente) et crée le lien et le code personnels via F2. Il accepte un rôle facultatif parmi `normal` (défaut), `reception_seule` et `offre_plus` (transmis par F11 seulement quand l'option de l'invitation est activée) ; `un_seul` et toute valeur inconnue sont refusés à la création vérifiée. Un service générique `QRCodePNG(texte)` (bibliothèque Go pure) est exposé, utilisé pour le lien personnel et par F11 pour le lien d'inscription.
- Règles après le tirage : exclusions manuelles, foyers (création, ajout ou retrait d'un membre) et forçages restent modifiables. Avant d'enregistrer une règle, F3 interroge le port `RuleChangeRepair` de F1 (implémenté par F4) : si aucune paire existante ne la viole, la règle est enregistrée sans autre effet ; si une paire la viole, l'administrateur voit un aperçu SANS noms (nombre de personnes touchées, dont combien avaient déjà découvert leur cible), et sur confirmation F3 appelle `Apply` avec un rappel d'écriture de la règle, de sorte que la règle et la réparation à nombre minimal de paires changées s'enregistrent dans une seule transaction (les personnes concernées sont prévenues par F4) ; si aucune réparation n'existe ou si l'administrateur la refuse, la règle est conservée avec l'indicateur « n'affecte pas le tirage actuel », valable seulement pour les futurs ajustements et les nouveaux tirages, avec une explication en français.
- À brancher par l'orchestrateur : routes dans `internal/app/`, textes dans `web/static/i18n/fr.json`, migrations éventuelles dans le registre commun, branchement du port `RuleChangeRepair` implémenté par F4c.
- Rôle après le tirage : l'administrateur change le rôle d'une personne à tout moment. Avant le tirage, l'écriture est directe. Après le tirage, le service interroge le port `RuleChangeRepair` de F1b (nature « changement de rôle dans le tirage ») : aperçu sans noms (comptes seulement), réparation à nombre minimal de paires changées dans la même transaction que le changement de rôle, avec courriels aux seules personnes concernées, ou repli « s'applique aux futurs ajustements et nouveaux tirages » avec explication ; jamais de nouveau tirage silencieux.

## Out of scope

- Implémentation de la réparation du tirage (F4c), pages d'administration (F10), formulaire d'inscription (F11).

## Acceptance criteria

```yaml criteria
AC1: {text: "Le lien personnel d'un participant sans courriel est remis sous forme de lien et de QR code décodable vers ce même lien", verify: "test:internal/enrolment/qr_link_test.go"}
AC2: {text: "Le port CreateVerified crée le participant avec joined_at égal à l'instant de vérification, applique les mêmes règles de nettoyage, de longueur et d'unicité du nom que l'ajout manuel (insensible à la casse et aux accents) et crée son lien et son code personnels", verify: "test:internal/enrolment/verified_creation_port_test.go"}
AC3: {text: "QRCodePNG renvoie un PNG décodable vers le texte donné, pour un lien personnel comme pour une URL d'inscription", verify: "test:internal/enrolment/qr_generic_test.go"}
AC4: {text: "Une règle (exclusion, foyer, forçage) ajoutée après le tirage qui ne contredit aucune paire est enregistrée sans réparation ni courriel", verify: "test:internal/rulechange/rule_after_draw_no_effect_test.go"}
AC5: {text: "Une règle ajoutée après le tirage qui contredit une paire donne un aperçu sans nom (comptes seulement) et, sur confirmation, s'enregistre avec la réparation par le port RuleChangeRepair (doublure) dans une seule transaction", verify: "test:internal/rulechange/rule_after_draw_repair_test.go"}
AC6: {text: "Quand aucune réparation n'existe ou que l'administrateur la refuse, la règle est conservée avec l'indicateur « n'affecte pas le tirage actuel », explication en français, et vaut pour les futurs ajustements et nouveaux tirages", verify: "test:internal/rulechange/rule_after_draw_future_only_test.go"}
AC7: {text: "CreateVerified fixe le rôle reçu parmi normal, reception_seule et offre_plus (normal si absent) et refuse un_seul et toute valeur inconnue, y compris par un paramètre forgé", verify: "test:internal/enrolment/verified_creation_role_test.go"}
AC8: {text: "Avant le tirage, un changement de rôle s'écrit directement ; après le tirage, un changement de rôle qui ne contredit aucune affectation est enregistré sans réparation ni courriel", verify: "test:internal/rulechange/role_after_draw_no_effect_test.go"}
AC9: {text: "Après le tirage, un changement de rôle qui contredit une affectation donne un aperçu en comptes seulement et, sur confirmation, s'écrit avec la réparation par le port RuleChangeRepair (doublure) dans une seule transaction, sans nouveau tirage", verify: "test:internal/rulechange/role_after_draw_repair_test.go"}
AC10: {text: "Quand aucune réparation n'existe ou qu'elle est refusée, le rôle est conservé avec l'indicateur « n'affecte pas le tirage actuel » et vaut pour les futurs ajustements et tirages, avec une explication en français", verify: "test:internal/rulechange/role_after_draw_future_only_test.go"}
```

## Steps

- F3d.1 Port `CreateVerified` avec lien et code personnels, remise du lien par QR code pour les participants sans courriel et service générique `QRCodePNG`.
- F3d.2 Édition après tirage : champs modifiables du participant et règles (exclusions, foyers, forçages) branchées sur le port `RuleChangeRepair` avec aperçu sans noms et repli « futurs ajustements seulement ».
- F3d.3 Rôle dans le tirage après le tirage branché sur `RuleChangeRepair` (nature « changement de rôle »), et rôle facultatif dans `CreateVerified` sans `un_seul`.
