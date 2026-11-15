#!/usr/bin/env bash
# F12a.1 : banc de recette en conteneurs.
#
#   scripts/e2e.sh --smoke     construit, démarre, joue le test de fumée, détruit tout
#   scripts/e2e.sh [spec...]   idem avec tous les tests Playwright (ou les specs données)
#
# Étapes : docker build de l'image du dépôt, docker compose up --wait (application +
# Mailpit), Playwright dans les deux profils mobiles (360x640 et 390x844), puis
# nettoyage systématique : down -v, suppression de l'image de recette, et contrôle
# qu'aucun conteneur ni volume « ss-test » ne subsiste.
#
# Docker : honore DOCKER_CONTEXT et DOCKER_HOST s'ils sont définis, sinon utilise le
# contexte courant. Les secrets de test sont générés à chaque exécution.
# Variables facultatives : SS_TEST_APP_PORT (18080), SS_TEST_MAIL_API_PORT (18025).
set -euo pipefail
cd "$(dirname "$0")/.."
root=$(pwd)

smoke=0
specs=()
for a in "$@"; do
  case "$a" in
    --smoke) smoke=1 ;;
    -h|--help) sed -n '2,13p' "$0"; exit 0 ;;
    -*) echo "option inconnue : $a" >&2; exit 2 ;;
    *) specs+=("$a") ;;
  esac
done

image=ss-test-secretsanta:local
if docker compose version >/dev/null 2>&1; then compose=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then compose=(docker-compose)
else echo "ni « docker compose » ni « docker-compose » n'est disponible" >&2; exit 2; fi
compose+=(-f "$root/docker-compose.test.yml")

app_port=${SS_TEST_APP_PORT:-18080}
mail_port=${SS_TEST_MAIL_API_PORT:-18025}
export SS_TEST_APP_PORT=$app_port SS_TEST_MAIL_API_PORT=$mail_port
export SS_TEST_INSTANCE_PASSWORD="t-$(openssl rand -hex 12)"
export SS_TEST_SIGNING_SECRET="t-$(openssl rand -hex 32)"
export E2E_BASE_URL="http://127.0.0.1:$app_port"
export E2E_MAIL_API="http://127.0.0.1:$mail_port"
export E2E_INSTANCE_PASSWORD=$SS_TEST_INSTANCE_PASSWORD

leftovers() {
  { docker ps -a --filter name='^ss-test' --format 'conteneur {{.Names}}'
    docker volume ls --filter name='^ss-test' --format 'volume {{.Name}}'; } 2>/dev/null
}

cleanup() {
  code=$?
  trap - EXIT
  if [ "$code" -ne 0 ]; then "${compose[@]}" logs --no-color --tail=40 2>&1 || true; fi
  "${compose[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
  docker rmi -f "$image" >/dev/null 2>&1 || true
  left=$(leftovers)
  if [ -n "$left" ]; then
    echo "reste de recette non nettoyé :" >&2; echo "$left" >&2
    [ "$code" -ne 0 ] || code=1
  else
    echo "nettoyage : aucun conteneur ni volume ss-test restant"
  fi
  exit "$code"
}
trap cleanup EXIT

# Le Dockerfile du dépôt prend son architecture de TARGETARCH (la CI construit
# linux/amd64). Pour la recette locale on construit pour l'architecture du
# démon Docker (arm64 sous colima) avec --platform ; le Dockerfile n'est pas
# modifié.
case "$(docker info --format '{{.Architecture}}')" in
  aarch64|arm64) goarch=arm64 ;;
  x86_64|amd64) goarch=amd64 ;;
  *) echo "architecture du démon non prise en charge" >&2; exit 2 ;;
esac
echo "== construction de l'image ($image, linux/$goarch ; la recette locale ne vérifie pas l'image amd64 de la CI)"
docker build --platform "linux/$goarch" --build-arg "VERSION=$(cat VERSION)-recette" -t "$image" .

echo "== démarrage (application + serveur SMTP factice), attente du healthcheck"
"${compose[@]}" up -d --wait --wait-timeout 120

echo "== contrôles de base"
curl -fsS -o /dev/null "$E2E_BASE_URL/" || { echo "l'application ne répond pas sur $E2E_BASE_URL" >&2; exit 1; }
curl -fsS -o /dev/null "$E2E_MAIL_API/api/v1/info" || { echo "l'API Mailpit ne répond pas sur $E2E_MAIL_API" >&2; exit 1; }

echo "== Playwright (profils mobile-360x640 et mobile-390x844)"
cd tests/e2e
[ -d node_modules ] || npm ci
if [ "$smoke" = 1 ]; then
  npx playwright test foundation/home.spec.ts
elif [ ${#specs[@]} -gt 0 ]; then
  npx playwright test "${specs[@]}"
else
  npx playwright test
fi
echo "== recette terminée"
