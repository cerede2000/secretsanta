#!/usr/bin/env bash
# F1c.AC1 : aucun secret renseigné par défaut dans .env.example,
# docker-compose.example.yml et Dockerfile.
set -euo pipefail
cd "$(dirname "$0")/../../.."

fail=0
secret_re='(PASSWORD|SECRET|API_KEY|TOKEN)'

# .env.example : une ligne active de secret doit avoir une valeur vide.
while IFS= read -r line; do
  case "$line" in \#*|'') continue ;; esac
  name="${line%%=*}"; value="${line#*=}"
  if [[ "$name" =~ $secret_re && -n "$value" ]]; then
    echo "secret renseigné dans .env.example : $name" >&2; fail=1
  fi
done < .env.example

# docker-compose.example.yml : pas de valeur par défaut (${VAR:-x} / ${VAR-x}) ni de
# valeur littérale pour un secret.
while IFS= read -r line; do
  case "$line" in *'#'*) [[ "$line" =~ ^[[:space:]]*# ]] && continue ;; esac
  if [[ "$line" =~ ^[[:space:]]*([A-Z_]*${secret_re}[A-Z_]*):[[:space:]]*(.*)$ ]]; then
    name="${BASH_REMATCH[1]}"; value="${BASH_REMATCH[3]}"
    if [[ -n "$value" && ! "$value" =~ ^\$\{[A-Z_]+(:\?[^}]*|\?[^}]*)?\}$ ]]; then
      echo "secret avec valeur par défaut dans docker-compose.example.yml : $name" >&2; fail=1
    fi
  fi
done < docker-compose.example.yml

# Dockerfile : aucun ENV/ARG de secret avec une valeur.
if grep -E '^[[:space:]]*(ENV|ARG)[[:space:]]+[A-Z_]*(PASSWORD|SECRET|API_KEY|TOKEN)[A-Z_]*[= ][^[:space:]\\]' Dockerfile >&2; then
  echo "secret renseigné dans Dockerfile" >&2; fail=1
fi
if grep -E '^[[:space:]]+[A-Z_]*(PASSWORD|SECRET|API_KEY|TOKEN)[A-Z_]*=[^[:space:]\\]' Dockerfile >&2; then
  echo "secret renseigné dans Dockerfile (ligne ENV continuée)" >&2; fail=1
fi

[ "$fail" -eq 0 ] && echo "OK : aucun secret par défaut"
exit "$fail"
