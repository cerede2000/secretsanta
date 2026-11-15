---
id: F2c
title: "Sécurité web : sessions, CSRF, cookies et en-têtes"
depends_on: [F1, F1b]
owns: [internal/websec/**]
reads: [internal/shared/**, internal/config/**]
risk: [security]
steps: [F2c.1, F2c.2]
---

# F2c Sécurité web : sessions, CSRF, cookies et en-têtes

## Summary

Cette fonctionnalité pose les protections qui s'appliquent à toutes les pages : sessions (administrateur de groupe et participant) avec cookies durcis, protection CSRF des requêtes modifiantes, contrôle d'origine, en-têtes de sécurité et CSP stricte sans script en ligne. Elle ne connaît ni les groupes ni les participants : les gestionnaires de connexion de F2e lui confient l'identité à retenir.

## In scope

- Sessions : service générique de création, validation et expiration de sessions pour l'administrateur d'un groupe et pour un participant (cookie HttpOnly, durée bornée, SameSite=Strict pour l'administrateur et SameSite=Lax pour le participant qui arrive par un lien de courriel). Les gestionnaires de connexion sont dans F2e ; ce service ne vérifie aucun secret.
- Protection CSRF sur tous les formulaires et requêtes modifiantes (POST, PUT, PATCH, DELETE) des pages rendues par le serveur : jeton lié à la session ou au cookie, champ caché fourni aux gabarits par une fonction `csrfField` et en-tête pour les appels JavaScript, plus contrôle de l'origine (Origin ou Sec-Fetch-Site contre PUBLIC_URL). Aucune requête GET ne modifie l'état. Seules les routes qui portent leur propre signature HMAC (par exemple le désabonnement en un clic de F6) peuvent être déclarées exemptées par l'orchestrateur. Rejet 403 avec message français.
- Cookies : HttpOnly toujours, Secure quand PUBLIC_URL est en https, SameSite=Lax pour la session participant (arrivée par lien de courriel) et SameSite=Strict pour la session administrateur.
- En-têtes de sécurité sur toutes les réponses : Content-Security-Policy stricte (default-src 'self', script-src 'self' sans script en ligne ni `unsafe-inline`, style-src 'self', form-action 'self', frame-ancestors 'none', base-uri 'none', object-src 'none'), X-Content-Type-Options: nosniff, Referrer-Policy: no-referrer (les liens personnels contiennent un jeton), Permissions-Policy restrictive, HSTS quand PUBLIC_URL est en https. Les gabarits ne contiennent ni script ni gestionnaire d'événement en ligne ; tout le JavaScript est en modules ES servis depuis `/static/`.
- À brancher par l'orchestrateur : middlewares CSRF, sessions et en-têtes dans `internal/app/`, textes dans les catalogues `web/static/i18n/<module>/fr.json` et `en.json` (français et anglais, un dossier par module, propriété de la phase concernée), migrations éventuelles (`internal/websec/migrations/`) dans le registre commun.

## Out of scope

- Vérification des secrets et des jetons (F2, F2b), pages de connexion (F2e).

## Acceptance criteria

```yaml criteria
AC1: {text: "Toute requête modifiante sans jeton CSRF valide ou avec une origine étrangère est rejetée en 403, et la même requête avec jeton et origine valides réussit ; aucune requête GET ne modifie l'état", verify: "test:internal/websec/csrf_test.go"}
AC2: {text: "Les cookies sont HttpOnly, Secure selon PUBLIC_URL, SameSite=Lax (participant) ou Strict (administrateur), et toutes les réponses portent la CSP stricte sans script en ligne et les autres en-têtes de sécurité ; aucun gabarit ne contient de script ni de gestionnaire en ligne", verify: "test:internal/websec/security_headers_test.go"}
AC3: {text: "Une session administrateur ou participant se crée, se valide et expire à l'échéance avec l'horloge simulée, son cookie est HttpOnly, SameSite=Strict (administrateur) ou Lax (participant) et la session d'un groupe n'est jamais valide pour un autre", verify: "test:internal/websec/sessions_test.go"}
```

## Steps

- F2c.1 Protection CSRF, cookies durcis, en-têtes de sécurité et CSP stricte (middlewares fournis à l'orchestrateur) avec leurs tests.
- F2c.2 Service de sessions administrateur et participant (création, validation, expiration, isolation par groupe).
