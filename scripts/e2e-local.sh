#!/usr/bin/env bash
# Recette Playwright locale, sans Docker (le banc officiel reste scripts/e2e.sh).
#
#   scripts/e2e-local.sh [arguments de « playwright test »...]
#
# Serveur neuf à chaque exécution : binaire construit, répertoire de données
# temporaire, ports libres, puits SMTP maison (tests/e2e/mail-sink/smtp-sink.mts,
# même API de lecture que Mailpit), arrêt et nettoyage à la sortie. L'amorçage
# (tests/e2e/global-setup.ts) crée ses données par l'application elle-même.
# La garde du banc (aucun test sauté) s'applique comme dans scripts/e2e.sh.
# SEARXNG_URL vise le serveur de recherche factice des specs de suggestions
# (http://127.0.0.1:${E2E_FAKE_SEARCH_PORT:-18090}), démarré par Playwright.
set -euo pipefail
cd "$(dirname "$0")/.."
free_port() { python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])'; }

work=$(mktemp -d "${TMPDIR:-/tmp}/ss-e2e-local.XXXXXX")
pids=()
cleanup() {
  code=$?
  trap - EXIT
  for p in "${pids[@]:-}"; do if [ -n "$p" ]; then kill "$p" 2>/dev/null || true; fi; done
  wait 2>/dev/null || true
  if [ "$code" -ne 0 ] && [ -f "$work/server.log" ]; then tail -30 "$work/server.log" >&2 || true; fi
  rm -rf "$work"
  exit "$code"
}
trap cleanup EXIT

CGO_ENABLED=0 go build -o "$work/secretsanta" ./cmd/secretsanta
app_port=$(free_port); smtp_port=$(free_port); mail_port=$(free_port)
instance_password="t-$(openssl rand -hex 12)"
signing_secret="t-$(openssl rand -hex 32)"
mkdir "$work/data"

node tests/e2e/mail-sink/smtp-sink.mts "$smtp_port" "$mail_port" >"$work/sink.log" 2>&1 &
pids+=($!)
PUBLIC_URL="http://127.0.0.1:$app_port" LISTEN_ADDR="127.0.0.1:$app_port" DATA_DIR="$work/data" \
  INSTANCE_PASSWORD="$instance_password" SIGNING_SECRET="$signing_secret" \
  SMTP_HOST=127.0.0.1 SMTP_PORT="$smtp_port" MAIL_FROM=noel@ss-test.invalid \
  MAIL_EHLO_HOSTNAME=ss-test.invalid MAIL_TLS_MODE=none MAIL_ALLOW_INSECURE=true MAIL_MAX_PER_MINUTE=10000 \
  SEARXNG_URL="http://127.0.0.1:${E2E_FAKE_SEARCH_PORT:-18090}" \
  "$work/secretsanta" >"$work/server.log" 2>&1 &
pids+=($!)
for _ in $(seq 1 100); do
  if curl -fsS -o /dev/null "http://127.0.0.1:$app_port/health" 2>/dev/null &&
    curl -fsS -o /dev/null "http://127.0.0.1:$mail_port/api/v1/info" 2>/dev/null; then break; fi
  sleep 0.2
done
curl -fsS -o /dev/null "http://127.0.0.1:$app_port/health" || { echo "le serveur ne démarre pas" >&2; exit 1; }

export E2E_BASE_URL="http://127.0.0.1:$app_port"
export E2E_MAIL_API="http://127.0.0.1:$mail_port"
export E2E_INSTANCE_PASSWORD=$instance_password
export E2E_SIGNING_SECRET=$signing_secret
export E2E_REPORT_FILE="$work/report.json"
cd tests/e2e
[ -d node_modules ] || npm ci
set +e
npx playwright test --forbid-only "$@"
code=$?
set -e
if [ "$code" -eq 0 ]; then node _shared/check-report.mts "$E2E_REPORT_FILE" || code=$?; fi
exit "$code"
