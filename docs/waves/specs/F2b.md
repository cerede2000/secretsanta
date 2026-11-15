---
id: F2b
title: "Accès participant : jetons, codes courts et lien magique"
depends_on: [F1b]
owns: [internal/access/**]
reads: [internal/shared/**, internal/db/**]
risk: [security]
steps: [F2b.1, F2b.2, F2b.3, F2b.4]
---

# F2b Accès participant : jetons, codes courts et lien magique

## Summary

Les participants entrent par un lien personnel à jeton aléatoire de 128 bits, par un code court sans caractères ambigus, ou par un lien magique envoyé par courriel, à usage unique et valable 15 minutes. Cette fonctionnalité fournit aussi les briques d'accès que F11 réutilise : générateur de code à longueur paramétrable, comparaison à temps constant, limiteur de requêtes à seaux et réponse neutre du chemin d'invitation.

## In scope

- Accès participant (screen-connexion) : jeton de lien personnel de 128 bits ; code court de 8 caractères sans caractères ambigus (ni 0/O, 1/I/L), unique dans le groupe ; lien magique valable 15 minutes, réellement à usage unique (consommation atomique), réponse identique que l'adresse existe ou non ; limitation de requêtes de 5 demandes de lien magique et 10 tentatives de code par fenêtre de 15 minutes, par adresse IP et par cible. L'envoi du courriel passe par le port de courriel de F1 ; l'implémentation réelle est branchée par l'orchestrateur.
- Services d'accès pour F11 (inscription par lien) : alphabet et générateur de code court à longueur paramétrable (8 caractères pour la connexion, 6 pour la vérification d'inscription), comparaison de jetons à temps constant et limiteur de requêtes à seaux (par adresse IP et par cible). La page publique `/<slug>` et le chemin `/<slug>/rejoindre/<jeton>` ne révèlent ni l'existence ni l'état du mode d'inscription : un slug inconnu, un jeton absent, invalide ou renouvelé et un mode désactivé donnent la même réponse neutre ; le formulaire d'inscription lui-même est livré par F11.
- Jeton et code stockés sous forme hachée par HMAC-SHA-256 avec la clé de signature de l'instance (valeur de SIGNING_SECRET, fournie en paramètre au service, séparation de domaine conservée) : un code court de 8 caractères n'est pas retrouvable hors ligne si la base fuit ; le jeton en clair n'existe que dans le lien remis.
- Persistance (F2b.3) : migrations dans `internal/access/migrations/` (go:embed) et magasin SQL dans `internal/access/sqlstore/` (implémentation de `token.Store`) pour les jetons et les codes courts ; unicité (groupe, nature, empreinte) et (groupe, nature, participant) avec remplacement du secret précédent, `ErrDuplicate` sur collision d'empreinte. Les liens magiques ont déjà leur migration (`internal/access/magic/migrations/`) et leur magasin SQL à consommation atomique, livrés par F2b.2.
- À brancher par l'orchestrateur : enregistrement des migrations dans le registre commun, construction du service avec la clé de signature, abonnement du port de courriel pour l'envoi du lien magique.

## Out of scope

- Pages de connexion (F2e), sessions, CSRF et en-têtes (F2c), création des participants (F3).

## Acceptance criteria

```yaml criteria
AC1: {text: "Les jetons de lien personnel font 128 bits et les codes courts excluent les caractères ambigus avec une unicité vérifiée par propriété", verify: "test:internal/access/token/token_and_code_test.go"}
AC2: {text: "Les isolations entre groupes tiennent : un jeton d'un groupe ne donne jamais accès à un autre groupe", verify: "test:internal/access/token/group_isolation_test.go"}
AC3: {text: "Le générateur de code court produit des codes de 8 ou de 6 caractères selon le paramètre, tous dans l'alphabet sans caractères ambigus, et la comparaison de jetons est à temps constant", verify: "test:internal/access/token/code_generator_length_test.go"}
AC4: {text: "Un lien magique expire après 15 minutes et ne s'utilise qu'une fois, y compris lors de deux requêtes simultanées", verify: "test:internal/access/magic/magic_link_single_use_test.go"}
AC5: {text: "La demande de lien magique renvoie la même réponse, le même statut et un temps comparable que l'adresse existe ou non", verify: "test:internal/access/magic/magic_link_uniform_test.go"}
AC6: {text: "Le chemin /<slug>/rejoindre/<jeton> et la page /<slug> donnent exactement la même réponse neutre (statut, corps, en-têtes) pour un slug inconnu, un jeton absent ou invalide et un mode d'inscription désactivé, et la page publique est identique que le mode soit actif ou non", verify: "test:internal/access/magic/invitation_path_uniform_test.go"}
AC7: {text: "La limitation de requêtes bloque la 6e demande de lien magique et la 11e tentative de code dans la fenêtre de 15 minutes avec horloge simulée, puis réautorise après la fenêtre", verify: "test:internal/access/limiter/rate_limit_test.go"}
AC8: {text: "Le limiteur de requêtes à seaux exposé à F11 compte séparément par adresse IP et par cible, ouvre une nouvelle fenêtre après le délai avec l'horloge simulée et ne partage aucun compteur entre groupes", verify: "test:internal/access/limiter/rate_limiter_buckets_test.go"}
AC9: {text: "Les migrations d'accès créent les tables des jetons et des codes courts avec l'unicité (groupe, nature, empreinte) et (groupe, nature, participant), s'appliquent dans l'ordre sur une base vide et se rejouent sans effet", verify: "test:internal/access/sqlstore/migrations_test.go"}
AC10: {text: "Le magasin SQL des jetons et codes remplace le secret précédent d'un participant, renvoie ErrDuplicate sur une collision d'empreinte et ne résout jamais un secret d'un autre groupe ni d'une autre nature", verify: "test:internal/access/sqlstore/credential_store_test.go"}
AC11: {text: "Après émission d'un jeton et d'un code, aucune valeur en clair (jeton, code, ni leur forme normalisée) n'apparaît dans aucune colonne de la base", verify: "test:internal/access/sqlstore/no_plaintext_test.go"}
AC12: {text: "Le hachage des jetons et codes est un HMAC-SHA-256 avec la clé de signature : un code de 8 caractères n'est pas retrouvable depuis les empreintes sans la clé, une autre clé ne résout rien et la séparation de domaine (groupe, nature) est conservée", verify: "test:internal/access/token/keyed_hash_test.go"}
```

## Steps

- F2b.1 Jetons de lien personnel, codes courts (alphabet sans ambiguïté, longueur paramétrable), stockage haché, comparaison à temps constant et isolation entre groupes.
- F2b.2 Lien magique à usage unique et réponse uniforme, limiteur de requêtes à seaux par adresse IP et par cible, réponse neutre du chemin d'invitation.
- F2b.3 Migrations d'accès et magasin SQL des jetons et codes courts (`token.Store`) : unicité, remplacement du secret précédent, `ErrDuplicate`, aucune valeur en clair.
- F2b.4 Hachage à clé (HMAC-SHA-256 avec la clé de signature) des jetons et codes, séparation de domaine conservée, tests mis à jour.
