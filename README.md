# secretsanta

Application web de Père Noël secret : création de groupes, tirage au sort, listes d'idées et courriels de relance, pensée d'abord pour le mobile. Elle est écrite en Go, avec une base SQLite embarquée, et tourne dans un seul conteneur.

## Installation Docker

```
cp .env.example .env     # renseigner INSTANCE_PASSWORD et SIGNING_SECRET
docker compose up -d
```

Détails, exemple Compose et mise à jour : [guide d'installation](docs/guide/index.md) et [docker-compose.example.yml](docs/guide/docker-compose.example.yml).

## Variables d'environnement

Toutes les variables sont décrites dans [la configuration](docs/guide/configuration.md) et listées dans `.env.example`. Seuls `INSTANCE_PASSWORD` et `SIGNING_SECRET` sont obligatoires.

## SMTP

L'envoi de courriels est facultatif et se règle avec `SMTP_HOST` et ses variables : voir [la configuration SMTP](docs/guide/configuration.md#configuration-smtp) et le [guide de délivrabilité](docs/guide/deliverability.md).

## Sources de suggestions

SearXNG, clés d'API Brave ou Serper, et catalogue de secours embarqué : voir [la configuration](docs/guide/configuration.md#sources-de-suggestions).

## Sauvegarde et restauration

Archivez le volume `/data` application arrêtée : voir [le guide](docs/guide/index.md#sauvegarde-et-restauration).

## Mise à jour

`docker compose pull && docker compose up -d` ; retour arrière possible vers l'une des 4 versions conservées : voir [le guide](docs/guide/index.md#mise-à-jour-et-retour-à-la-version-précédente).

## Sécurité

Secrets hors du dépôt, HTTPS devant un mandataire inverse, données personnelles : voir [le guide](docs/guide/index.md#sécurité).

## Dépannage

Voir [le guide](docs/guide/index.md#dépannage).

## Recette locale

Banc de recette en conteneurs sur Mac via colima : `bash scripts/e2e.sh --smoke`. Voir [le guide](docs/guide/index.md#recette-locale).

## Autres guides

- [Délivrabilité des courriels](docs/guide/deliverability.md)
- [Ajouter une langue](docs/guide/traduire.md)
