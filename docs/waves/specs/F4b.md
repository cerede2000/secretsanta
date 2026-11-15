---
id: F4b
title: "Tirage persistant, visibilité des cibles et révélation"
depends_on: [F3, F4, F2, F1b]
owns: [internal/draw/**, internal/reveal/**]
reads: [internal/drawengine/**, internal/participants/**, internal/rules/**, internal/groups/**, internal/shared/**, internal/db/**]
risk: [security, data]
steps: [F4b.1, F4b.2, F4b.3]
---

# F4b Tirage persistant, visibilité des cibles et révélation

## Summary

Cette fonctionnalité fait vivre le tirage : exécution atomique en une seule transaction, historique par année, tirage complet refait avec archivage, verrou des inscriptions en attente, et surtout la visibilité des cibles (un participant ne voit que sa propre cible, jamais l'administrateur par défaut). La date de révélation, le drapeau serveur de découverte (un par cible : un tireur peut avoir deux cibles), « ouvrir maintenant » et les faits exposés à l'administrateur sans les paires y sont aussi, ainsi que le contrôle de capacité qui lit les rôles de F3 et le plafond du groupe.

## In scope

- Tirage persistant : une seule transaction SQLite (aucun état partiel visible), lecture des contraintes (`BuildConstraints` de F3 et liste des participants), confirmation avant tirage ou nouveau tirage (le service exige l'indicateur de confirmation ; la page est dans F10).
- Historique par année consultable : tirages passés, y compris les ajustements consignés ; les paires ne sont visibles que de la personne concernée pour sa propre ligne (un participant ne voit que sa cible, jamais l'administrateur par défaut).
- Date de révélation (définie dans F2) : sans date, la cible est visible dès le tirage ; avec date, ouverture automatique à la date par une tâche planifiée (planificateur et horloge de F1) avec envoi du courriel « ton tirage est prêt » par le port de courriel ; bouton administrateur « ouvrir maintenant » avec confirmation. La découverte est enregistrée une seule fois côté serveur (drapeau par cible) et les drapeaux sont réarmés si le tirage est refait. La date est modifiable tant que la révélation n'est pas ouverte (la tâche est replanifiée) ; une fois ouverte, ou dès qu'une cible a été découverte, la modification est refusée avec un message français et la découverte ne revient jamais en arrière. Avant la date, le serveur ne renvoie jamais la cible.
- Inscriptions par lien (F11) : via le port `RegistrationState` de F1, le tirage est refusé tant qu'il reste des inscriptions en attente non traitées, avec un message français qui en donne le nombre ; l'administrateur peut explicitement les « ignorer » (conservées côté administration mais fermées, sans code valable) ou les « supprimer » avant de relancer le tirage. F4 implémente le port `DrawState` : dès que le tirage est fait, F11 ferme les inscriptions (les arrivées tardives passent par « Ajuster le tirage »).
- Faits exposés à l'administrateur sans les paires : tirage fait, nombre d'ouvertures, révélation faite (consommés par F10), rapport de contrôle vert/rouge.
- Tirage complet refait possible avec confirmation : l'ancien tirage est archivé dans l'historique, les drapeaux de révélation sont réarmés et les réglages du groupe ne changent pas. Le slug du groupe est immuable (garanti par F2).
- À brancher par l'orchestrateur : routes dans `internal/app/`, textes dans les catalogues `web/static/i18n/<module>/fr.json` et `en.json` (français et anglais, un dossier par module, propriété de la phase concernée), migrations (`internal/draw/migrations/`, `internal/reveal/migrations/`) dans le registre commun, branchement du port de courriel (F6), des ports `DrawState` et `RegistrationState`.
- Cibles multiples : l'affectation persistée associe à chaque tireur une ou deux cibles (jamais plus que le plafond) ; chaque participant reçoit exactement un cadeau ; une personne de rôle `reception_seule` n'a aucune cible. Un participant ne voit que ses propres cibles. Découvrir la première cible ne révèle pas la seconde ; chaque cible a son propre drapeau serveur, posé une seule fois, réarmé par un nouveau tirage.
- Contrôle de capacité avant tirage : le service lit les rôles (F3) et le plafond du groupe (F2, `max_gifts_per_giver`), appelle le moteur de F4 et, si la capacité est insuffisante, refuse avec l'explication sans noms et les propositions (passer des personnes en « J'offre plus » ou « normal », relever le plafond) ; une confirmation explicite est exigée si le plafond ou la répartition dépasse 2.
- Décomptes du contrôle du tirage (consommés par F10, jamais de nom) : bénéficiaires, tireurs à un cadeau, tireurs à deux cadeaux, personnes par rôle (`reception_seule`, `offre_plus`, `normal`, `un_seul`), ligne de capacité (cadeaux supplémentaires nécessaires et capacité des personnes prêtes à offrir plus) et rappel que chaque participant reçoit exactement un cadeau. Données fournies à F6 pour le courriel « tirage prêt » : nombre de cibles (0, 1 ou 2) par destinataire, sans aucun nom.

## Out of scope

- Algorithme de tirage (F4), ajustement et réparation (F4c), pages de la cible et de l'attente (F4d), pages d'administration du tirage (F10), animation d'ouverture (F9c).

## Acceptance criteria

```yaml criteria
AC1: {text: "Le tirage est atomique : une erreur au milieu n'écrit aucune paire et n'altère pas le tirage précédent", verify: "test:internal/draw/atomic_test.go"}
AC2: {text: "Un participant ne reçoit que sa propre cible, jamais celle d'un autre, et l'historique ne divulgue pas les paires des autres", verify: "test:internal/draw/target_visibility_test.go"}
AC3: {text: "Avec des inscriptions en attente (doublure de RegistrationState), le tirage est refusé avec leur nombre ; après le choix explicite « ignorer » ou « supprimer » il s'exécute, et sans inscription en attente il s'exécute directement", verify: "test:internal/draw/pending_registrations_gate_test.go"}
AC4: {text: "Le port DrawState renvoie « tirage non fait » avant le tirage, « tirage fait » juste après, et reflète un nouveau tirage ou un ajustement", verify: "test:internal/draw/draw_state_port_test.go"}
AC5: {text: "Le tirage complet refait exige une confirmation, archive l'ancien tirage dans l'historique, réarme les drapeaux de révélation et ne modifie pas les réglages du groupe", verify: "test:internal/draw/full_redraw_test.go"}
AC6: {text: "Avant la date de révélation, la cible n'est pas renvoyée par le serveur ; à la date, le courriel « tirage prêt » est mis en file avec l'horloge simulée ; sans date, la cible est immédiate", verify: "test:internal/reveal/reveal_date_test.go"}
AC7: {text: "Le drapeau de révélation n'est posé qu'une fois côté serveur, reste posé ensuite et est réarmé quand le tirage est refait", verify: "test:internal/reveal/reveal_flag_test.go"}
AC8: {text: "Le bouton « ouvrir maintenant » exige une confirmation et fait passer la révélation à l'état ouvert pour tous", verify: "test:internal/reveal/open_now_test.go"}
AC9: {text: "La date de révélation est modifiable tant que la révélation n'est pas ouverte, avec replanification de la tâche ; une fois la révélation ouverte ou une cible découverte, la modification est refusée avec un message français et aucune découverte ne revient en arrière", verify: "test:internal/reveal/reveal_date_lock_test.go"}
AC10: {text: "Un tireur peut avoir deux cibles dont chacune porte son drapeau de révélation, posé une seule fois ; découvrir l'une ne révèle pas l'autre ; un nouveau tirage réarme tous les drapeaux", verify: "test:internal/reveal/multi_target_reveal_flags_test.go"}
AC11: {text: "Un participant ne voit que ses propres cibles (une ou deux, jamais celles d'un autre) et une personne reception_seule n'en voit aucune", verify: "test:internal/draw/multi_target_visibility_test.go"}
AC12: {text: "Le tirage lit le plafond du groupe et les rôles de F3 ; avec une capacité insuffisante il est refusé avec explication sans noms et propositions, et une confirmation est exigée au-delà de 2 cadeaux par tireur", verify: "test:internal/draw/capacity_gate_test.go"}
AC13: {text: "Les décomptes du contrôle ne contiennent que des comptes (bénéficiaires, tireurs à un et à deux cadeaux, personnes par rôle, ligne de capacité) et le rapport vérifie que chaque participant reçoit exactement un cadeau, sans aucun nom", verify: "test:internal/draw/check_report_counts_test.go"}
AC14: {text: "L'historique par année et l'archivage d'un tirage refait conservent les cibles multiples sans divulguer de paire hors de la personne concernée", verify: "test:internal/draw/history_multi_target_test.go"}
AC15: {text: "La date de révélation reste verrouillée dès qu'une cible quelconque a été découverte et la tâche planifiée n'ouvre que les cibles non encore ouvertes", verify: "test:internal/reveal/reveal_date_lock_multi_test.go"}
AC16: {text: "Les données du courriel « tirage prêt » portent le nombre de cibles (zéro pour reception_seule, une ou deux) sans aucun nom", verify: "test:internal/reveal/ready_mail_variants_test.go"}
```

## Steps

- F4b.1 Modèle de données et migrations SQL (tirages par année, paires, historique), lecture des contraintes, tirage persistant atomique, visibilité des cibles, verrou des inscriptions en attente, port `DrawState`, tirage complet refait avec archivage.
- F4b.2 Révélation : drapeau serveur, date de révélation modifiable avec verrou, tâche planifiée et courriel « ton tirage est prêt », « ouvrir maintenant » et faits exposés à l'administrateur sans les paires.
- F4b.3 Cibles multiples, drapeaux de révélation par cible, contrôle de capacité (rôles de F3, plafond de F2, confirmation), décomptes du rapport et données du courriel « tirage prêt » sans noms.
