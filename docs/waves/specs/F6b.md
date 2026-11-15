---
id: F6b
title: "Messages conformes et signature DKIM"
depends_on: [F1, F1b]
owns: [internal/mailmsg/**, internal/maildkim/**]
reads: [internal/config/**, internal/shared/**]
risk: [security]
steps: [F6b.1, F6b.2]
---

# F6b Messages conformes et signature DKIM

## Summary

Cette fonctionnalité fabrique les messages comme les filtres antispam les aiment : multipart/alternative avec un texte et un HTML équivalents, en-têtes complets et corrects, objets sobres, liens sûrs, protection contre l'injection d'en-têtes, et une signature DKIM facultative faite par l'application quand le relais SMTP ne signe pas. Elle ne décide ni du contenu des modèles (F6) ni de l'envoi (F6). La réputation de l'adresse IP et les listes noires échappent à l'application et sont traitées honnêtement dans le diagnostic (F6c).

## In scope

- Constructeur de message (`internal/mailmsg/`) : multipart/alternative (text/plain d'abord, puis text/html valide), UTF-8 en quoted-printable avec lignes d'au plus 76 caractères, HTML léger sans image distante, sans pixel de suivi, sans script ni feuille de style externe, attribut alt obligatoire sur toute image, langue fr ; `ParityCheck` vérifie que les liens et les informations du HTML se retrouvent dans le texte.
- En-têtes : From avec nom d'affichage et adresse alignée sur l'expéditeur d'enveloppe, Reply-To facultatif, Date au format RFC 5322, Message-ID de la forme `<identifiant@domaine-expéditeur>` unique, MIME-Version, Content-Language fr, Auto-Submitted: auto-generated, X-Auto-Response-Suppress, List-Unsubscribe (https et mailto) avec List-Unsubscribe-Post sur les seuls messages non essentiels qui portent une adresse de désinscription. Date, Message-ID et aléa viennent de l'horloge et de l'aléa injectés (F1b) pour des tests déterministes.
- Objet : encodé RFC 2047 en UTF-8, vérifié par le constructeur (60 caractères au plus, aucun mot en majuscules intégrales, aucun « !!! »).
- Liens : https uniquement vers l'hôte de `PUBLIC_URL` (le lien de désinscription et le mailto sont les seules exceptions déclarées), aucun raccourcisseur d'adresse.
- Protection contre l'injection : tout retour à la ligne dans un nom, un objet ou une adresse est refusé.
- Signature DKIM (`internal/maildkim/`) : rsa-sha256 et ed25519-sha256, canonicalisation relaxed/relaxed, en-têtes signés listés par la configuration (au minimum From, To, Subject, Date, Message-ID, MIME-Version, List-Unsubscribe), clé privée lue seulement depuis un chemin de fichier (jamais d'une variable en clair), jamais journalisée ; le domaine de signature doit s'aligner sur le domaine de l'adresse d'expédition sinon la signature est refusée avec une erreur explicite ; sans configuration DKIM le module ne fait rien. Bibliothèque Go pure maintenue au choix du développeur ; vérification indépendante en test avec un vérificateur DKIM distinct du signataire.
- À brancher par l'orchestrateur : F6 appelle le constructeur et le signataire ; aucune route, aucune migration.

## Out of scope

- Contenu et rendu des modèles français, envoi, file, désinscription (F6), diagnostic DNS et TLS et liste de suppression (F6c), variables d'environnement (F1).

## Acceptance criteria

```yaml criteria
AC1: {text: "Le constructeur produit un multipart/alternative avec le texte d'abord puis le HTML, en UTF-8 quoted-printable avec des lignes d'au plus 76 caractères, et le message se décode sans perte", verify: "test:internal/mailmsg/mime_structure_test.go"}
AC2: {text: "Les en-têtes obligatoires sont présents et corrects : From avec nom d'affichage aligné sur l'expéditeur d'enveloppe, Date RFC 5322, Message-ID unique au domaine de l'expéditeur, MIME-Version, Content-Language fr, Auto-Submitted et X-Auto-Response-Suppress, Reply-To seulement s'il est configuré", verify: "test:internal/mailmsg/headers_test.go"}
AC3: {text: "L'objet est encodé RFC 2047 et le constructeur refuse un objet de plus de 60 caractères, contenant un mot en majuscules intégrales ou « !!! »", verify: "test:internal/mailmsg/subject_lint_test.go"}
AC4: {text: "Tout retour à la ligne dans un nom, un objet ou une adresse est refusé, aucun en-tête supplémentaire ne peut être injecté", verify: "test:internal/mailmsg/header_injection_test.go"}
AC5: {text: "List-Unsubscribe (https et mailto) et List-Unsubscribe-Post n'existent que sur les messages non essentiels qui portent une adresse de désinscription et jamais sur un message essentiel", verify: "test:internal/mailmsg/list_unsubscribe_test.go"}
AC6: {text: "Le contrôle du HTML refuse une image distante, un pixel de suivi, un script, une feuille de style externe, une image sans attribut alt, un lien non https, un lien hors de l'hôte de PUBLIC_URL (hors désinscription) et un raccourcisseur d'adresse", verify: "test:internal/mailmsg/html_lint_test.go"}
AC7: {text: "ParityCheck signale tout lien ou toute information présente dans le HTML et absente du texte et réciproquement", verify: "test:internal/mailmsg/parity_test.go"}
AC8: {text: "Avec une horloge et un aléa injectés, la construction d'un même message est déterministe (Date, Message-ID et frontières identiques)", verify: "test:internal/mailmsg/deterministic_test.go"}
AC9: {text: "La signature DKIM rsa-sha256 et ed25519-sha256 en canonicalisation relaxed/relaxed est vérifiable par un vérificateur indépendant avec une clé publique de test, les en-têtes signés suivent la liste configurée et le hachage du corps est correct", verify: "test:internal/maildkim/sign_verify_test.go"}
AC10: {text: "La clé privée DKIM est lue seulement depuis un chemin de fichier, un fichier illisible, invalide ou d'un type incompatible avec l'algorithme est refusé, et la clé n'est jamais journalisée ni renvoyée dans une erreur", verify: "test:internal/maildkim/key_loading_test.go"}
AC11: {text: "Le domaine de signature doit s'aligner sur le domaine de l'adresse d'expédition sinon la signature est refusée avec une erreur explicite, et sans configuration DKIM le module laisse le message intact", verify: "test:internal/maildkim/alignment_and_disabled_test.go"}
```

## Steps

- F6b.1 Constructeur de messages (multipart/alternative, quoted-printable, en-têtes complets, objet RFC 2047 contrôlé, liens et HTML contrôlés, protection contre l'injection, parité texte et HTML, déterminisme) avec leurs tests.
- F6b.2 Signature DKIM (rsa-sha256 et ed25519-sha256, relaxed/relaxed, clé par fichier, alignement du domaine, désactivée sans configuration) vérifiée par un vérificateur indépendant.
