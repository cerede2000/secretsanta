---
id: F4c
title: Ajustement du tirage et réparation après règle modifiée
depends_on: [F4b, F2e, F3, F1b]
owns: [internal/adjust/**, internal/repair/**]
reads: [internal/draw/**, internal/reveal/**, internal/drawengine/**, internal/participants/**, internal/rules/**, internal/groupsettings/**, internal/shared/**]
risk: [security, data]
steps: [F4c.1, F4c.2, F4c.3]
---

# F4c Ajustement du tirage et réparation après règle modifiée

## Summary

Le tirage reste vivant après coup : retirer ou ajouter une personne modifie un minimum de paires, un aperçu sans aucun nom dit combien de personnes seront touchées, les personnes concernées sont prévenues et l'ancienne lettre est archivée. Une règle ajoutée après le tirage qui contredit une paire est réparée de la même façon, dans la même transaction que la règle.

## In scope

- Mode « Ajuster le tirage » (service serveur, aperçu et application ; la page screen-admin-ajuster est livrée par F10) :
  - Retrait de X : Z, qui offrait à X, offre désormais à Y, que X servait. Si cette paire est interdite, réparation à nombre minimal de paires changées avec les mêmes contraintes.
  - Ajout de W : W est inséré dans une paire A→B qui devient A→W et W→B, avec priorité à un A n'ayant pas encore découvert sa cible.
  - Aperçu pour l'administrateur sans aucun nom (par exemple « 1 personne verra sa cible changer, elle l'avait déjà découverte »).
  - Courriels automatiques aux personnes concernées via le port de courriel (ne nomment jamais d'autre personne que le destinataire).
  - Lettre et messages de l'ancienne cible archivés (via un port fourni par F5, branché par l'orchestrateur), consigné dans l'historique.
  - Si aucune réparation n'existe : explication et proposition de tirage complet.
- Évolutivité après tirage : le tirage reste vivant sans tout casser. (a) Modifiables sans toucher aux paires : budget, date du jour J, consigne globale (F2), indices cadeau, noms, prénoms et adresses mail (F3) ; aucun de ces changements ne modifie une paire, un drapeau de révélation ni un lien personnel. (c) Ajout ou retrait d'un participant : « Ajuster le tirage » (ci-dessus). (d) Exclusions, foyers et forçages modifiables après le tirage (saisie dans F3d, via le port `RuleChangeRepair` de F1b, implémenté ici dans `internal/repair/`) : si aucune paire ne viole la règle, rien ne change ; si une paire la viole, aperçu SANS noms (nombre de personnes touchées, dont déjà découvertes) puis réparation à nombre minimal de paires changées sous toutes les contraintes, écrite dans la même transaction que la règle, avec courriels aux seules personnes concernées (même gabarit que l'ajustement) ; si aucune réparation n'existe ou si l'administrateur la refuse, la règle vaut seulement pour les futurs ajustements et les nouveaux tirages. La date de révélation (b), le tirage complet refait (e) et le slug immuable (f) sont livrés par F4b et F2.
- À brancher par l'orchestrateur : routes dans `internal/app/`, textes dans les catalogues `web/static/i18n/<module>/fr.json` et `en.json` (français et anglais, un dossier par module, propriété de la phase concernée), migrations éventuelles dans le registre commun, branchement du port de courriel (F6), du port d'archivage de lettre (F5b) et du port `RuleChangeRepair` consommé par F3d.
- Ajustement avec capacités et rôles : chaque participant reçoit toujours exactement un cadeau, un `un_seul` n'offre jamais plus d'un cadeau et personne ne dépasse le plafond ; les cadeaux supplémentaires vont d'abord aux `offre_plus` puis aux `normal`. Le retrait d'un tireur qui offrait deux cadeaux reroute ses deux cibles, en priorité vers d'autres `offre_plus` ; le retrait d'un bénéficiaire libère la capacité de son tireur ; l'ajout d'une personne (de n'importe quel rôle) est inséré selon les capacités. Si la capacité devient insuffisante, rien n'est modifié : explication sans noms (trop de personnes ne peuvent ou ne veulent pas offrir plus d'un cadeau, plafond trop bas) et propositions.
- Changement de rôle après le tirage (nature « changement de rôle dans le tirage » de `RuleChangeRepair`, implémentée ici) : aperçu en comptes seulement, réparation minimale (un tireur qui offrait deux cadeaux et devient `un_seul` ou `reception_seule` est remplacé en priorité par un `offre_plus`), courriels aux seules personnes concernées, repli « futurs ajustements seulement » ; jamais de re-tirage silencieux.
- Une cible déjà découverte n'est changée qu'en dernier recours ; les drapeaux de révélation des cibles non touchées ne sont jamais réarmés ; chaque nouvelle cible est à découvrir et sa personne est prévenue par courriel (aperçu : combien avaient déjà découvert).

## Out of scope

- Pages d'ajustement (F10), saisie des règles (F3d), moteur de tirage (F4), persistance (F4b).

## Acceptance criteria

```yaml criteria
AC1: {text: "Le retrait d'une personne reroute le tireur vers la cible du retiré, ou répare avec un nombre minimal de paires changées sous les mêmes contraintes", verify: "test:internal/adjust/adjust_remove_test.go"}
AC2: {text: "L'ajout d'une personne insère W dans une paire A→B avec priorité à un A qui n'a pas découvert sa cible", verify: "test:internal/adjust/adjust_add_test.go"}
AC3: {text: "L'aperçu d'ajustement ne contient aucun nom et indique le nombre de personnes touchées et si elles avaient déjà découvert leur cible", verify: "test:internal/adjust/adjust_preview_no_names_test.go"}
AC4: {text: "Les personnes concernées par un ajustement reçoivent un courriel qui ne nomme que le destinataire, et l'ancienne lettre est archivée, l'ajustement étant consigné dans l'historique", verify: "test:internal/adjust/adjust_effects_test.go"}
AC5: {text: "Quand aucune réparation n'existe, l'ajustement est refusé avec explication et proposition de tirage complet, sans rien modifier", verify: "test:internal/adjust/adjust_impossible_test.go"}
AC6: {text: "Après le tirage, la modification du budget (défini, modifié, retiré puis redéfini), de la date du jour J, de la consigne globale, des indices cadeau, des noms, prénoms et adresses mail ne change aucune paire, aucun drapeau de révélation ni aucun lien personnel", verify: "test:internal/adjust/evolvability_settings_test.go"}
AC7: {text: "Quand une règle (exclusion, foyer, forçage) ajoutée après le tirage contredit une paire existante, l'aperçu ne contient que des comptes (personnes touchées, dont déjà découvertes), la réparation change un nombre minimal de paires sous toutes les contraintes, la règle et la réparation s'écrivent dans une seule transaction et seules les personnes concernées sont prévenues", verify: "test:internal/repair/rule_change_repair_test.go"}
AC8: {text: "Une règle ajoutée après le tirage qui ne contredit aucune paire n'en change aucune ; quand aucune réparation n'existe ou que l'administrateur la refuse, la règle est conservée comme « n'affecte pas le tirage actuel » et vaut pour les futurs ajustements et tirages", verify: "test:internal/repair/rule_change_no_effect_test.go"}
AC9: {text: "Le retrait d'une personne qui offrait deux cadeaux reroute ses deux cibles en priorité vers d'autres offre_plus, avec un nombre minimal de paires changées, sans qu'un un_seul offre plus d'un cadeau", verify: "test:internal/adjust/adjust_remove_double_giver_test.go"}
AC10: {text: "Le retrait d'un bénéficiaire libère la capacité de son tireur sans autre changement et chaque participant restant reçoit toujours exactement un cadeau", verify: "test:internal/adjust/adjust_remove_receiver_capacity_test.go"}
AC11: {text: "L'ajout d'une personne, quel que soit son rôle, respecte les capacités, le plafond et la priorité offre_plus puis normal", verify: "test:internal/adjust/adjust_add_capacity_test.go"}
AC12: {text: "Quand la capacité devient insuffisante après un ajustement, il est refusé avec une explication sans noms et les propositions, sans rien modifier", verify: "test:internal/adjust/adjust_capacity_insufficient_test.go"}
AC13: {text: "Un changement de rôle après le tirage (vers ou depuis reception_seule, offre_plus, un_seul) passe par RuleChangeRepair avec aperçu en comptes, réparation minimale et courriels aux seules personnes concernées, sans nouveau tirage", verify: "test:internal/repair/role_change_test.go"}
AC14: {text: "Un tireur qui offrait deux cadeaux et passe un_seul ou reception_seule est remplacé en priorité par un offre_plus, avec repli futurs ajustements seulement quand aucune réparation n'existe", verify: "test:internal/repair/role_change_priority_test.go"}
AC15: {text: "Aucune réparation ni aucun ajustement ne fait offrir plus d'un cadeau à un un_seul ni plus que le plafond (propriété rapid)", verify: "test:internal/adjust/property_capacity_test.go"}
AC16: {text: "Une cible déjà découverte n'est changée qu'en dernier recours, les drapeaux des cibles non touchées ne sont jamais réarmés et la personne dont la cible change est prévenue", verify: "test:internal/adjust/revealed_targets_lock_test.go"}
```

## Steps

- F4c.1 Service d'ajustement : retrait, ajout, réparation minimale, aperçu sans noms, courriels, archivage et historique.
- F4c.2 Implémentation du port `RuleChangeRepair` de F1 : aperçu sans noms, réparation minimale dans la transaction de la règle, courriels aux seules personnes concernées, repli « futurs ajustements seulement ».
- F4c.3 Ajustements avec capacités et rôles (retrait d'un tireur à deux cadeaux ou d'un bénéficiaire, ajout, capacité insuffisante), nature « changement de rôle » du port `RuleChangeRepair` et verrou des cibles déjà découvertes.
