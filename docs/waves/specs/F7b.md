---
id: F7b
title: Notification « nouvelle idée » et relance du tireur
depends_on: [F5b, F6, F7]
owns: [internal/givernotify/**]
reads: [internal/letter/**, internal/letterread/**, internal/mail/**, internal/mailunsub/**, internal/reveal/**, internal/shared/**]
risk: []
steps: [F7b.1, F7b.2]
---

# F7b Notification « nouvelle idée » et relance du tireur

## Summary

Quand le tiré ajoute ou modifie des idées, son tireur reçoit un seul courriel regroupé, seulement s'il peut déjà lire la lettre, et une relance s'il n'ouvre pas une lettre nouvelle ou modifiée. Aucun courriel ne révèle l'identité du tireur ni ne nomme une autre personne que le destinataire.

## In scope

- Notification « nouvelle idée » au tireur : abonnement à l'événement de lettre nouvelle ou modifiée (F5) pour un tireur qui peut lire la lettre (tirage fait et révélation ouverte). Plusieurs idées ajoutées ou modifiées en moins de 10 minutes produisent un seul courriel (attente de 10 minutes après la dernière modification, plafond de 60 minutes), en s'appuyant sur le regroupement par clé de F6 et l'horloge de F1.
- Relance du tireur : un tireur qui n'a pas ouvert une lettre nouvelle ou modifiée est relancé (version de lettre de F5 et ouverture de F5b), avec les garde-fous de F7 (un courriel par jour, fenêtre d'envoi, plafond) ; le rappel ne contient que la mention qu'une lettre l'attend.
- Tireur à deux cibles : la clé de regroupement est celle du TIREUR, pas de la lettre ; deux lettres nouvelles ou modifiées en moins de 10 minutes, quelles que soient leurs cibles, produisent un seul courriel qui dit seulement qu'une ou deux lettres l'attendent (sans nom de cible) ; la relance du tireur est unique, quel que soit le nombre de lettres non ouvertes. Une personne `reception_seule` n'a aucune lettre à lire et ne reçoit jamais cette notification.

## Out of scope

- Relances des participants (F7), modèles et regroupement (F6), lecture de la lettre (F5b).

## Acceptance criteria

```yaml criteria
AC1: {text: "La relance du tireur est déclenchée par une lettre nouvelle ou modifiée non ouverte et s'arrête à l'ouverture", verify: "test:internal/givernotify/giver_letter_reminder_test.go"}
AC2: {text: "Plusieurs idées ajoutées ou modifiées en moins de 10 minutes produisent un seul courriel au tireur, envoyé 10 minutes après la dernière modification et au plus tard 60 minutes après la première, avec horloge simulée", verify: "test:internal/givernotify/new_idea_coalescing_test.go"}
AC3: {text: "Pour un tireur à deux cibles, des modifications des deux lettres en moins de 10 minutes produisent un seul courriel, avec horloge simulée, sans nom de cible ni de tireur", verify: "test:internal/givernotify/two_targets_coalescing_test.go"}
AC4: {text: "La relance du tireur est unique quel que soit le nombre de lettres non ouvertes et s'arrête quand toutes les lettres nouvelles ont été ouvertes", verify: "test:internal/givernotify/two_targets_reminder_test.go"}
AC5: {text: "Une personne reception_seule ne reçoit jamais la notification de nouvelle idée ni la relance du tireur", verify: "test:internal/givernotify/reception_only_never_notified_test.go"}
```

## Steps

- F7b.1 Notification regroupée « nouvelle idée » au tireur qui peut lire la lettre, relance du tireur et respect des désinscriptions par type.
- F7b.2 Regroupement et relance par tireur pour deux cibles, exclusion des personnes `reception_seule`.
