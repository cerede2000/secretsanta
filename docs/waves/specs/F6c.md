---
id: F6c
title: "Délivrabilité : diagnostic DNS et TLS, liste de suppression et page d'administration"
depends_on: [F6, F6b, F2c, F1c, F9, F1b, F1, F13]
owns: [internal/maildiag/**, internal/mailsuppress/**, internal/maildiagpages/**, web/templates/maildiag/**, web/static/css/maildiag/**, tests/e2e/maildiag/**, web/static/i18n/maildiag/**]
reads: [internal/shared/**, internal/config/**, internal/mail/**, internal/mailmsg/**, internal/maildkim/**, web/templates/layout/**, web/static/css/base/**, tests/e2e/_shared/**, docs/waves/design/snapshot.html#screen-admin-delivrabilite]
risk: []
steps: [F6c.1, F6c.2, F6c.3]
---

# F6c Délivrabilité : diagnostic DNS et TLS, liste de suppression et page d'administration

## Summary

Cette fonctionnalité aide l'exploitant à vérifier que ses courriels ont une chance d'arriver : un diagnostic qui interroge le DNS du domaine d'expédition (SPF, DKIM, DMARC, MX, alignement), vérifie le TLS du serveur SMTP et le HTTPS de l'adresse publique, propose pour chaque point à corriger l'enregistrement DNS à copier, et dit honnêtement ce qui n'est pas vérifiable ici. Elle porte aussi la liste de suppression des adresses refusées définitivement.

## In scope

- Diagnostic (`internal/maildiag/`, résolveur DNS injectable, doublure en test) : SPF (un seul enregistrement v=spf1, mécanisme final -all ou ~all signalé, +all ou ?all signalés comme à corriger, plusieurs enregistrements signalés comme erreur) ; DKIM (si activé : enregistrement `<sélecteur>._domainkey.<domaine>` présent et correspondant à la clé publique dérivée de la clé du fichier ; sinon « non activé ») ; DMARC (`_dmarc.<domaine>` : absent, p=none, p=quarantine, p=reject) ; MX du domaine d'expédition ; alignement du domaine de l'adresse d'expédition, de l'expéditeur d'enveloppe et du domaine DKIM ; connexion TLS au serveur SMTP selon le mode configuré (certificat valide, nom d'hôte correct) ; HTTPS de `PUBLIC_URL` (certificat et `/health`).
- Chaque point à corriger propose l'enregistrement DNS à copier (SPF d'exemple à adapter au fournisseur, DMARC d'exemple en p=none, DKIM calculé depuis la clé publique) avec un bouton Copier en JavaScript natif, sans boîte du navigateur.
- Honnêteté : une section « non vérifiable ici » dit explicitement que la réputation de l'adresse IP, les listes noires et le DNS inverse (PTR) ne sont pas contrôlés par l'application et renvoie au guide de délivrabilité du README (F12).
- Les entrées du diagnostic viennent uniquement de la configuration (jamais d'un hôte saisi par l'utilisateur) ; aucune donnée personnelle ne sort ; les requêtes passent par le client sortant de F1b.
- Courriel de test avec affichage des en-têtes bruts du message mis en file (service de test de F6).
- Liste de suppression (`internal/mailsuppress/`) : implémente le port `SuppressionList` de F1b, par groupe, avec l'adresse, la raison (réponse du serveur nettoyée) et la date ; page qui la liste et permet de retirer une adresse ; une adresse corrigée n'est pas supprimée.
- Page `screen-admin-delivrabilite` dans le gabarit d'administration de F1c, atteinte depuis Réglages, section Courrier, par la route `/admin/courrier/delivrabilite` (F10 y renvoie par un lien sans dépendre de F6c) ; session administrateur et jeton CSRF de F2c, isolation entre groupes, mobile d'abord.
- À brancher par l'orchestrateur : routes dans `internal/app/`, textes dans les catalogues `web/static/i18n/<module>/fr.json` et `en.json` (français et anglais, un dossier par module, propriété de la phase concernée), migrations (`internal/mailsuppress/migrations/`) dans le registre commun, branchement du port `SuppressionList`.
- Internationalisation (F13) : aucun texte brut dans les gabarits, les messages ni les courriels de cette fonctionnalité ; tous passent par les fonctions `t` et `tn` avec des clés préfixées par le module dans `web/static/i18n/<module>/fr.json` et `en.json` (français et anglais, mêmes clés, mêmes paramètres). Les textes affichés par du JavaScript sont fournis par le gabarit en attributs `data-*` produits avec `t`, jamais écrits en dur dans le script.
- Contraintes web héritées de F2c : tout gestionnaire qui modifie l'état est enveloppé par `websec.RequireUnsafeMethod` (une requête GET ne modifie jamais rien) ; aucune page n'utilise d'attribut `style` en ligne ni de ressource `data:` (CSP stricte : classes et CSS dans `web/static/css/**`, images servies par une route de même origine).

## Out of scope

- Construction des messages et DKIM (F6b), envoi et file (F6), guide de délivrabilité du README (F12), réglages du courrier eux-mêmes (variables d'environnement, F1).

## Acceptance criteria

```yaml criteria
AC1: {text: "Le contrôle SPF avec un résolveur factice distingue absent, plusieurs enregistrements (erreur), mécanisme final -all ou ~all (correct) et +all ou ?all (à corriger) et propose l'enregistrement à copier", verify: "test:internal/maildiag/spf_test.go"}
AC2: {text: "Le contrôle DKIM indique non activé quand le DKIM est désactivé et, activé, compare l'enregistrement publié du sélecteur à la clé publique dérivée de la clé du fichier avec l'enregistrement à copier en cas d'écart", verify: "test:internal/maildiag/dkim_test.go"}
AC3: {text: "Le contrôle DMARC distingue absent, p=none, p=quarantine et p=reject et propose un enregistrement d'exemple quand il est absent", verify: "test:internal/maildiag/dmarc_test.go"}
AC4: {text: "Le contrôle MX et l'alignement du domaine d'expédition, de l'expéditeur d'enveloppe et du domaine DKIM signalent chaque désalignement", verify: "test:internal/maildiag/mx_alignment_test.go"}
AC5: {text: "Le contrôle TLS du serveur SMTP, avec un serveur de test, distingue connexion chiffrée valide, certificat expiré, nom d'hôte incorrect et absence de chiffrement selon le mode configuré", verify: "test:internal/maildiag/smtp_tls_test.go"}
AC6: {text: "Le contrôle de PUBLIC_URL vérifie le HTTPS (certificat) et la réponse de /health avec un serveur de test", verify: "test:internal/maildiag/public_url_test.go"}
AC7: {text: "Le diagnostic n'utilise que des valeurs de configuration (aucun hôte saisi), n'envoie aucune donnée personnelle et passe par le client sortant de F1b", verify: "test:internal/maildiag/inputs_and_privacy_test.go"}
AC8: {text: "Le rapport déclare explicitement non vérifiables ici la réputation de l'adresse IP, les listes noires et le DNS inverse, avec le renvoi au guide de délivrabilité", verify: "test:internal/maildiag/not_verifiable_test.go"}
AC9: {text: "Le courriel de test est mis en file et la page affiche les en-têtes bruts du message mis en file", verify: "test:internal/maildiagpages/test_mail_headers_test.go"}
AC10: {text: "Un refus définitif 5xx inscrit l'adresse avec sa raison et sa date pour son groupe seulement, la page la liste et permet de la retirer, et une adresse corrigée n'est pas supprimée", verify: "test:internal/mailsuppress/suppression_store_test.go"}
AC11: {text: "La page de diagnostic et la liste de suppression exigent la session administrateur du groupe et le jeton CSRF, et un administrateur ne voit jamais les adresses d'un autre groupe", verify: "test:internal/maildiagpages/admin_access_test.go"}
AC12: {text: "Chaque point à corriger affiche un bouton Copier pour l'enregistrement proposé sans aucune boîte du navigateur", verify: "test:internal/maildiagpages/copy_record_test.go"}
AC13: {text: "La page de diagnostic n'a aucun défilement horizontal et ses zones tactiles mesurent au moins 44 px en 360×640 et 390×844", verify: "cmd:cd tests/e2e && npx playwright test maildiag"}
AC14: {text: "L'écran de diagnostic de délivrabilité correspond à la maquette approuvée", verify: "design-fidelity:screen-admin-delivrabilite"}
AC15: {text: "La page de diagnostic de délivrabilité : tous les textes passent par les catalogues avec les clés fr et en du module, et le rendu avec la langue anglaise ne contient aucune clé brute ni texte français", verify: "test:internal/maildiagpages/i18n_pages_test.go"}
AC16: {text: "La page de diagnostic de délivrabilité : tout gestionnaire qui modifie l'état est enveloppé par RequireUnsafeMethod (un GET sur une route modifiante renvoie 405 sans rien modifier) et aucun gabarit n'utilise d'attribut style en ligne ni de ressource data:", verify: "test:internal/maildiagpages/csp_unsafe_methods_test.go"}
```

## Steps

- F6c.1 Diagnostic de délivrabilité (SPF, DKIM, DMARC, MX, alignement, TLS du SMTP, HTTPS de l'adresse publique, section honnête « non vérifiable ici ») avec résolveur injectable et ses tests.
- F6c.2 Liste de suppression (implémentation du port `SuppressionList`, par groupe, migrations) et ses tests.
- F6c.3 Page `screen-admin-delivrabilite` (rapport, enregistrements à copier, courriel de test avec en-têtes, liste de suppression) dans le gabarit d'administration avec tests Playwright mobiles.
