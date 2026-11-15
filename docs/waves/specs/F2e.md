---
id: F2e
title: Réglages du groupe et pages d'accès
depends_on: [F2, F2b, F2c, F1c, F9, F1b]
owns: [internal/groupsettings/**, internal/grouppages/**, internal/accesspages/**, web/templates/groups/**, web/templates/access/**, web/static/js/groups/**, web/static/js/access/**, web/static/css/groups/**, web/static/css/access/**, tests/e2e/groups/**, tests/e2e/access/**]
reads: [internal/shared/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, internal/groups/**, internal/access/**, internal/websec/**, docs/waves/design/snapshot.html#screen-accueil-groupe, docs/waves/design/snapshot.html#screen-creation-groupe, docs/waves/design/snapshot.html#screen-connexion, docs/waves/design/snapshot.html#screen-admin-connexion]
risk: []
steps: [F2e.1, F2e.2, F2e.3]
---

# F2e Réglages du groupe et pages d'accès

## Summary

Cette fonctionnalité donne leur interface aux groupes et à l'accès : les quatre pages (accueil du groupe, création, connexion participant, connexion administrateur) et le service de réglages du groupe modifiable à tout moment, y compris après le tirage (budget facultatif, date du jour J, consigne, événement de budget modifié).

## In scope

- Réglages du groupe modifiables à tout moment, y compris après le tirage : service `UpdateGroupSettings` pour le nom, le budget, la date du jour J et la consigne globale, qui n'écrit que les données du groupe et ne touche à aucune paire. Le plafond de cadeaux par tireur (F2) est aussi réglable ici ; relever le plafond au-dessus de 2 exige l'indicateur de confirmation explicite, et le service n'écrit jamais lui-même de réparation du tirage (la prise en compte d'un nouveau plafond sur un tirage déjà fait passe par « Ajuster le tirage »). Le budget se définit, se modifie ou se retire quand on veut (vide autorisé, montant positif borné sinon). Chaque changement de budget incrémente une `budget_version` et, quand l'administrateur coche « Prévenir les participants », émet l'événement `budget_modifie` (ancien budget, nouveau budget ou « aucun », indicateur « prévenir ») sur le port d'événements de F1 ; F6 y répond par un courriel, F4 lit `budget_version` pour le bandeau « Budget mis à jour » côté participant. L'événement ne porte aucune donnée personnelle.
- Pages d'accès et de groupe : screen-accueil-groupe (`/<slug>`, nom et accueil seulement, jamais la liste des participants), screen-creation-groupe (formulaire protégé par le mot de passe d'instance, budget facultatif vide par défaut, affichage unique du secret avec bouton Copier en module JavaScript natif), screen-connexion (lien personnel, code court, lien magique). Gestionnaires dans `internal/grouppages/` et `internal/accesspages/`, gabarits, CSS et JavaScript natif ; formulaires protégés par le jeton CSRF de F2c ; messages d'erreur intégrés à l'interface, jamais de boîte du navigateur.
- Page de connexion administrateur (screen-admin-connexion) : saisie du mot de passe ou du jeton du groupe, message d'erreur intégré, affichage unique du secret généré avec bouton Copier.
- Connexion administrateur : le gestionnaire vérifie le secret par `VerifyAdminSecret` (F2), ouvre la session administrateur de F2c, refuse le secret d'un autre groupe, limite les tentatives avec le limiteur de F2b et répond de façon uniforme.
- À brancher par l'orchestrateur : routes dans `internal/app/` (`/<slug>`, `/creer`, `/connexion`), textes dans `web/static/i18n/fr.json`.

## Out of scope

- Modèle du groupe et création (F2), jetons et codes (F2b), sessions et CSRF (F2c), page d'administration des réglages (F10).

## Acceptance criteria

```yaml criteria
AC1: {text: "Le budget se définit, se modifie et se retire à tout moment, y compris avec la doublure DrawState réglée sur « tirage fait » ; le nom, la date du jour J et la consigne aussi ; chaque changement de budget incrémente budget_version et seules les données du groupe sont écrites", verify: "test:internal/groupsettings/update_settings_test.go"}
AC2: {text: "Un changement de budget émet l'événement budget_modifie avec l'ancien budget, le nouveau (ou aucun) et l'indicateur « prévenir » ; sans l'indicateur aucun envoi n'est demandé ; l'événement ne contient aucune donnée personnelle de participant", verify: "test:internal/groupsettings/budget_event_test.go"}
AC3: {text: "screen-accueil-groupe, screen-creation-groupe, screen-connexion et screen-admin-connexion n'ont aucun défaut de défilement horizontal et leurs zones tactiles mesurent au moins 44 px en 360×640 et 390×844", verify: "cmd:npx playwright test tests/e2e/groups tests/e2e/access"}
AC4: {text: "L'écran d'accueil de groupe correspond à la maquette approuvée", verify: "design-fidelity:screen-accueil-groupe"}
AC5: {text: "L'écran de création de groupe correspond à la maquette approuvée", verify: "design-fidelity:screen-creation-groupe"}
AC6: {text: "L'écran de connexion correspond à la maquette approuvée", verify: "design-fidelity:screen-connexion"}
AC7: {text: "L'écran de connexion administrateur correspond à la maquette approuvée", verify: "design-fidelity:screen-admin-connexion"}
AC8: {text: "La connexion administrateur ouvre une session pour le bon secret, refuse un mauvais secret ou celui d'un autre groupe avec une réponse uniforme et bloque les tentatives répétées dans la fenêtre du limiteur", verify: "test:internal/accesspages/admin_login_test.go"}
AC9: {text: "UpdateGroupSettings modifie le plafond de cadeaux par tireur à tout moment dans les bornes 2 à 5, exige l'indicateur de confirmation au-delà de 2, n'écrit aucune paire et n'émet aucun événement de budget", verify: "test:internal/groupsettings/max_gifts_setting_test.go"}
```

## Steps

- F2e.1 Service `UpdateGroupSettings` (nom, budget facultatif, date du jour J, consigne ; slug immuable), version du budget et événement `budget_modifie` avec leurs tests.
- F2e.2 Pages screen-accueil-groupe et screen-creation-groupe avec bouton Copier.
- F2e.3 Pages screen-connexion et screen-admin-connexion (gestionnaires de connexion participant et administrateur) avec tests Playwright mobiles.
