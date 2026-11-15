---
id: F5b
title: "Lettre au Père Noël : lecture par le tireur, cachet et archivage"
depends_on: [F5, F4b]
owns: [internal/letterread/**]
reads: [internal/letter/**, internal/draw/**, internal/reveal/**, internal/shared/**]
risk: [security, data]
steps: [F5b.1, F5b.2]
---

# F5b Lettre au Père Noël : lecture par le tireur, cachet et archivage

## Summary

Le tireur lit, en lecture seule, uniquement la lettre de sa propre cible et seulement après la révélation. Il ne peut jamais répondre : aucune route d'écriture n'existe pour lui. Le tiré voit un cachet « lue par le Père Noël » sans identité. L'ajustement du tirage archive la lettre de l'ancienne cible.

## In scope

- Lecture par le tireur : uniquement la lettre de sa propre cible, en lecture seule, et seulement après révélation (F4). Aucune réponse, aucune question, aucun fil de discussion, aucun champ de commentaire côté tireur : aucune route d'écriture n'existe pour le tireur.
- Cachet « lue par le Père Noël » : posé la première fois que le tireur ouvre la lettre (par version ; une nouvelle version retire le cachet jusqu'à la prochaine lecture), visible côté tiré, sans date ni identité. L'administrateur voit seulement « lettre lue » (F10).
- Port d'archivage : à l'ajustement de tirage (F4), la lettre de l'ancienne cible est archivée (conservée, plus lisible par le tireur) ; une lettre archivée n'est jamais montrée à un nouveau tireur.
- À brancher par l'orchestrateur : routes dans `internal/app/`, migrations (`internal/letterread/migrations/`) dans le registre commun, branchement du port d'archivage consommé par F4c.
- Tireur à deux cibles : il lit une lettre par cible, chacune en lecture seule et seulement après la révélation de CETTE cible ; le cachet « lue par le Père Noël » est posé par lettre (lire la première ne pose pas le cachet de la seconde) ; l'archivage lors d'un ajustement s'applique par cible. Aucune lettre n'est jamais montrée à quelqu'un qui n'est pas le tireur de sa cible.

## Out of scope

- Écriture de la lettre (F5), pages (F5c), notification et relance du tireur (F7b).

## Acceptance criteria

```yaml criteria
AC1: {text: "Le tireur lit uniquement la lettre de sa cible, uniquement après révélation, et aucun autre participant n'y accède", verify: "test:internal/letterread/read_access_test.go"}
AC2: {text: "Aucune route d'écriture, de réponse ou de commentaire n'existe pour le tireur", verify: "test:internal/letterread/no_reply_routes_test.go"}
AC3: {text: "Le cachet « lue par le Père Noël » apparaît côté tiré après la lecture du tireur sans aucune information d'identité, et disparaît quand la lettre est modifiée", verify: "test:internal/letterread/read_stamp_test.go"}
AC4: {text: "L'archivage lors d'un ajustement retire la lettre à l'ancien tireur et ne la montre pas à un nouveau", verify: "test:internal/letterread/archive_on_adjust_test.go"}
AC5: {text: "Un tireur à deux cibles lit chaque lettre séparément, seulement après la révélation de la cible correspondante, et ne voit jamais la lettre d'une cible non révélée", verify: "test:internal/letterread/two_targets_read_access_test.go"}
AC6: {text: "Le cachet est posé par lettre : lire la première lettre ne pose pas le cachet de la seconde et chaque cachet reste sans aucune identité", verify: "test:internal/letterread/two_targets_stamp_test.go"}
AC7: {text: "L'archivage lors d'un ajustement s'applique à la seule lettre de la cible changée et laisse l'autre lettre lisible par son tireur", verify: "test:internal/letterread/two_targets_archive_test.go"}
```

## Steps

- F5b.1 Lecture en lecture seule par le tireur, cachet « lue par le Père Noël » et archivage à l'ajustement.
- F5b.2 Lecture par cible pour un tireur à deux cibles, cachet par lettre et archivage par cible.
