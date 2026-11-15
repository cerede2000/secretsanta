---
id: F1b
title: "Base de données, ports partagés et registre de navigation"
depends_on: []
owns: [internal/db/**, internal/shared/**]
reads: [internal/config/**]
risk: []
steps: [F1b.1, F1b.2, F1b.3]
---

# F1b Base de données, ports partagés et registre de navigation

## Summary

Cette fonctionnalité fournit les bibliothèques internes que toutes les autres utilisent : la base SQLite pur Go avec son exécuteur de migrations, l'horloge simulable et le planificateur, l'aléa cryptographique, le nettoyage des saisies, les ports (courriel, événements, coordination entre fonctionnalités), le client HTTP sortant unique et le registre de navigation d'administration. Les sous-paquets sont indépendants pour que leurs phases s'écrivent en parallèle.

## In scope

- Base SQLite (modernc.org/sqlite, sans CGO) dans le volume (DATA_DIR), mode WAL, clés étrangères actives, exécuteur de migrations ordonnées et transactionnelles, table de suivi des versions. Convention : chaque module range ses migrations SQL dans `internal/<module>/migrations/` et les embarque avec go:embed ; leur enregistrement dans le registre commun (`internal/app/`) est fait par l'orchestrateur.
- Ports partagés dans `internal/shared/` : horloge (réelle et simulée), planificateur par « tick » pilotable par l'horloge simulée, port d'envoi de courriel (interface seule, l'implémentation est dans F6) dont le message porte un type, un indicateur « essentiel » et une clé de regroupement facultative (utilisés par F6 et F7), port d'événements (abonnement et émission, utilisé par F5, F6 et F7), nettoyage et validation des saisies (trim, normalisation Unicode, longueurs maximales), erreurs typées avec messages français, aléa cryptographique (jetons, entiers uniformes), client HTTP sortant unique (voir ci-dessous). Sous-paquets indépendants : `internal/shared/clock`, `scheduler`, `random`, `sanitize`, `errs`, `ports` (courriel, événements, ports de coordination), `outbound` et `adminnav`.
- Trois ports de coordination entre fonctionnalités indépendantes, définis dans `internal/shared/` avec des doublures de test : `RegistrationState` (nombre d'inscriptions en attente d'un groupe et résolution de ces inscriptions par « ignorer » ou « supprimer » ; implémenté par F11, consommé par F4 et F10) et `DrawState` (le tirage d'un groupe est-il fait ; implémenté par F4, consommé par F11) et `RuleChangeRepair` (le tirage s'adapte à une règle modifiée après coup ; implémenté par F4, consommé par F3 : `Preview(groupID, règle proposée)` renvoie seulement des comptes, à savoir le nombre de personnes touchées dont combien avaient déjà découvert leur cible, plus un indicateur de réparabilité, jamais un nom ; `Apply(groupID, règle proposée, écrireRègle)` exécute dans une même transaction l'écriture de la règle par F3 et la réparation minimale du tirage par F4). Ils évitent toute dépendance directe entre ces fonctionnalités ; l'orchestrateur les branche. La « règle proposée » de `RuleChangeRepair` couvre quatre natures : exclusion, foyer, forçage et changement de rôle dans le tirage (voir F3) ; l'aperçu reste des comptes, jamais un nom.
- Un quatrième port de coordination `SuppressionList` (`IsSuppressed(groupID, adresse)`, `Add(groupID, adresse, raison)`, `Remove`) : les adresses refusées de façon définitive par le serveur de courrier (code SMTP 5xx) y sont inscrites par F6 et lues par F6, F7 et F10 ; la persistance est implémentée par F6c. Doublure de test fournie.
- Aucune donnée personnelle ne sort vers un tiers : tout appel HTTP sortant de l'application passe par la fabrique de client HTTP sortant de `internal/shared/`, qui fixe un User-Agent générique, n'envoie ni Referer ni cookies et ne transmet jamais de courriel, nom ou prénom. Les seules requêtes sortantes prévues sont les recherches de cadeaux (mots-clés sans identité) vers les sources configurées et la récupération de page ou d'image protégée contre le SSRF (F8). Le SMTP configuré par l'exploitant n'est pas un tiers. Avatars festifs calculés en local (F9), aucune ressource externe.
- Registre de navigation d'administration `internal/shared/adminnav/` (libellé, icône, rang, destination) : les fonctionnalités y déclarent leurs entrées sans dépendre les unes des autres (F10 déclare Tableau, Participants, Tirage et Plus ; F11c déclare Inscriptions), l'ordre est Tableau, Participants, Tirage, Inscriptions, Plus, et l'entrée active est marquée. L'orchestrateur branche les déclarations ; le gabarit qui les affiche est dans F1c.

## Out of scope

- Variables d'environnement (F1), point d'entrée, gabarits web et livraison (F1c).
- Implémentation SMTP (F6) : seul le port est défini ici.

## Acceptance criteria

```yaml criteria
AC1: {text: "Les migrations s'appliquent dans l'ordre sur une base vide, sont rejouables sans effet (idempotentes) et une migration en échec est annulée entièrement", verify: "test:internal/db/migrations_test.go"}
AC2: {text: "L'horloge simulée et le planificateur déclenchent une tâche exactement à l'instant voulu", verify: "test:internal/shared/scheduler/scheduler_test.go"}
AC3: {text: "Le nettoyage des saisies supprime les caractères de contrôle, normalise les espaces et refuse les valeurs dépassant la longueur maximale", verify: "test:internal/shared/sanitize/sanitize_test.go"}
AC4: {text: "Les jetons aléatoires font 128 bits et deux appels successifs ne renvoient jamais la même valeur sur 10 000 tirages", verify: "test:internal/shared/random/random_test.go"}
AC5: {text: "Le client HTTP sortant unique envoie un User-Agent générique, ni Referer ni cookies ni identité, et aucun appel sortant n'existe dans le code en dehors de cette fabrique", verify: "test:internal/shared/outbound/outbound_client_test.go"}
AC6: {text: "Les ports RegistrationState et DrawState existent dans internal/shared avec des doublures de test : un groupe sans tirage ni inscription renvoie 0 inscription en attente et « tirage non fait », et la résolution « ignorer » ou « supprimer » est exposée par le port", verify: "test:internal/shared/ports/coordination_ports_test.go"}
AC7: {text: "Le port RuleChangeRepair existe dans internal/shared avec une doublure de test : l'aperçu ne renvoie que des comptes (personnes touchées, déjà découvertes) et un indicateur de réparabilité, jamais de nom, et l'application exécute l'écriture de la règle et la réparation dans une même transaction (l'échec de l'une annule l'autre)", verify: "test:internal/shared/ports/rule_change_repair_port_test.go"}
AC8: {text: "Le registre de navigation d'administration trie les entrées déclarées par les fonctionnalités dans l'ordre Tableau, Participants, Tirage, Inscriptions, Plus, accepte des entrées déclarées indépendamment par des doublures sans dépendance entre fonctionnalités et marque l'entrée active", verify: "test:internal/shared/adminnav/registry_test.go"}
AC9: {text: "Le port SuppressionList existe dans internal/shared avec une doublure de test : une adresse ajoutée est signalée supprimée pour son groupe seulement, un retrait la réactive et aucune méthode ne renvoie une adresse d'un autre groupe", verify: "test:internal/shared/ports/suppression_list_port_test.go"}
AC10: {text: "La règle proposée de RuleChangeRepair accepte les quatre natures (exclusion, foyer, forçage, changement de rôle dans le tirage) avec la doublure de test, et l'aperçu de chacune ne renvoie que des comptes", verify: "test:internal/shared/ports/rule_change_kinds_test.go"}
```

## Steps

- F1b.1 Mettre en place SQLite pur Go en volume avec l'exécuteur de migrations embarquées, transactionnel et idempotent, et ses tests.
- F1b.2 Écrire l'horloge réelle et simulée, le planificateur par « tick », l'aléa cryptographique, le nettoyage des saisies et les erreurs typées avec leurs tests.
- F1b.3 Écrire les ports (courriel avec type, caractère essentiel et clé de regroupement, événements, `RegistrationState`, `DrawState`, `RuleChangeRepair` à quatre natures de règle, `SuppressionList`), le client HTTP sortant unique et le registre de navigation d'administration avec leurs doublures et tests.
