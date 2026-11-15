# Guide d'installation et d'exploitation

Ce guide explique comment installer, exploiter et mettre à jour secretsanta. Les autres pages : [configuration](configuration.md) (variables, SMTP, suggestions), [délivrabilité](deliverability.md), [ajouter une langue](traduire.md).

## Installation avec Docker

1. Copiez `.env.example` en `.env` et renseignez `INSTANCE_PASSWORD` (mot de passe demandé pour créer un groupe) et `SIGNING_SECRET` (longue valeur aléatoire, par exemple `openssl rand -hex 32`). L'application refuse de démarrer sans ces deux secrets.
2. Reprenez l'exemple [docker-compose.example.yml](docker-compose.example.yml) dans un dossier de votre choix, à côté du fichier `.env`.
3. Définissez `PUBLIC_URL` (l'adresse publique, utilisée dans les liens des courriels).
4. Lancez `docker compose up -d`, puis ouvrez l'adresse : la page d'accueil permet de créer un groupe.

Les données (base SQLite) vivent dans le volume monté sur `/data`. L'image tourne sans shell, avec un utilisateur non root (65532) ; l'état de santé est contrôlé par la commande `secretsanta healthcheck`.

## Sauvegarde et restauration

Tout l'état est dans le dossier `/data` (base SQLite en mode WAL et images). Pour une sauvegarde cohérente, arrêtez l'application puis archivez le volume :

```
docker compose stop secretsanta
docker run --rm -v secretsanta_secretsanta-data:/data -v "$PWD":/sauvegarde alpine \
  tar czf /sauvegarde/secretsanta-data.tgz -C /data .
docker compose start secretsanta
```

Restauration : arrêtez l'application, videz le volume, extrayez l'archive dans `/data` (`tar xzf ... -C /data`, propriétaire 65532), puis redémarrez. Conservez aussi votre `.env` : sans le même `SIGNING_SECRET`, les liens déjà envoyés ne sont plus valables.

## Mise à jour et retour à la version précédente

L'intégration continue publie à chaque version une image numérotée (`ghcr.io/cerede2000/secretsanta:<version>`) et `latest`. Les 4 dernières versions sont conservées.

- Mise à jour : `docker compose pull && docker compose up -d`. Les migrations de base s'appliquent au démarrage.
- Retour arrière : fixez dans le fichier Compose l'étiquette d'une des 4 versions conservées au lieu de `latest`, restaurez la sauvegarde faite avant la mise à jour (les migrations ne sont pas réversibles), puis `docker compose up -d`.

Faites toujours une sauvegarde avant de mettre à jour.

## Sécurité

- Secrets : ne les écrivez jamais dans le fichier Compose ni dans le dépôt ; gardez-les dans `.env` (droits 600) ou dans un gestionnaire de secrets. La clé privée DKIM se place dans un fichier monté (`MAIL_DKIM_KEY_FILE`), jamais dans une variable.
- HTTPS : placez l'application derrière un mandataire inverse (Traefik, Nginx, Caddy) qui termine le TLS et définissez `PUBLIC_URL` en `https://...`. Réglez `TRUSTED_PROXIES` et `CLIENT_IP_HEADER` pour que la limitation de débit voie l'adresse réelle du client.
- Données personnelles : l'application stocke noms, adresses de courriel et listes d'idées. L'administrateur ne voit jamais qui offre à qui. Sauvegardez et supprimez les données en conformité avec vos obligations (RGPD).

## Dépannage

- Le conteneur s'arrête au démarrage : lisez `docker compose logs secretsanta`. Une variable manquante ou invalide est nommée (par exemple `INSTANCE_PASSWORD`).
- Les courriels ne partent pas : vérifiez `SMTP_HOST`, `MAIL_FROM` et le mode `MAIL_TLS_MODE`, puis envoyez un message de test depuis l'administration. Voir [délivrabilité](deliverability.md) si les messages arrivent en indésirables.
- Les liens des courriels pointent vers la mauvaise adresse : corrigez `PUBLIC_URL`.
- Trop de demandes (429) pour tous les utilisateurs : le mandataire n'est pas reconnu, réglez `TRUSTED_PROXIES`.
- Base inaccessible : le dossier `/data` doit appartenir à l'utilisateur 65532.

## Recette locale

La recette complète tourne sur un Mac, en `linux/arm64`, via colima (magasin d'images containerd). L'image `linux/amd64` publiée est construite et contrôlée par la CI GitHub : la recette locale ne prétend pas la vérifier.

1. Démarrez colima (`colima start`) ; le CLI `docker` doit viser son contexte. `DOCKER_CONTEXT` et `DOCKER_HOST` sont honorés s'ils sont définis.
2. `bash scripts/e2e.sh --smoke` construit l'image, démarre l'application et un serveur SMTP factice (Mailpit), attend le healthcheck, joue le test de fumée puis détruit pile et volumes.
3. `bash scripts/e2e.sh` joue tous les tests Playwright dans les deux profils mobiles (360×640 et 390×844).
4. `bash scripts/spamcheck.sh --self-test` vérifie le scanner SpamAssassin ; `--mailpit` note les messages capturés. Le score mesure le contenu et la forme des messages, pas la réputation de l'adresse IP.
5. Sans Docker : `bash scripts/e2e-local.sh`.

Les valeurs de secrets de test n'existent que dans `docker-compose.test.yml`.
