# Ajouter une langue en 5 minutes

Le français et l'anglais sont fournis. Une autre langue s'ajoute sans reconstruire l'image.

1. Exportez le fichier plat à traduire : `secretsanta i18n export de` (ou `-o de.json`). Dans Docker : `docker compose run --rm secretsanta i18n export de > de.json` (ajustez l'entrée du conteneur si besoin).
2. Traduisez les valeurs du fichier ; gardez les clés et les `{paramètres}` tels quels.
3. Déposez le fichier `de.json` dans le dossier désigné par la variable `I18N_DIR`, monté en volume (`./i18n:/i18n`, avec `I18N_DIR=/i18n`).
4. Contrôlez-le : `secretsanta i18n check de.json`.
5. Redémarrez l'application : `docker compose restart secretsanta`. La langue apparaît dans le sélecteur ; `APP_DEFAULT_LANG` peut la choisir par défaut.

## Contribuer une langue au dépôt

Ajoutez les fichiers `<code>.json` dans chaque dossier `web/static/i18n/<module>/`, vérifiez avec `secretsanta i18n check <code>`, puis ouvrez une demande de fusion.

## Limites

Certains messages métier (erreurs du domaine) sont encore en français côté serveur et ne sont pas traduits par ce mécanisme.
