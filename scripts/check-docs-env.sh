#!/usr/bin/env bash
# F12a.AC4 : variables de .env.example <-> guide, et exemple Compose valide sans secret.
set -euo pipefail
cd "$(dirname "$0")/.."
fail=0
ko() { echo "KO : $*" >&2; fail=1; }

env_vars=$(grep -oE '^#? ?[A-Z][A-Z0-9_]+=' .env.example | tr -d '# =' | sort -u)
doc_vars=$(grep -ohE '^\| `[A-Z][A-Z0-9_]+`' docs/guide/*.md | tr -d '|` ' | sort -u)
for v in $env_vars; do
  echo "$doc_vars" | grep -qx "$v" || ko "$v : dans .env.example, absent du guide"
done
for v in $doc_vars; do
  echo "$env_vars" | grep -qx "$v" || ko "$v : dans le guide, absent de .env.example"
done

ex=docs/guide/docker-compose.example.yml
[ -f "$ex" ] || ko "$ex absent"
if [ -f "$ex" ]; then
  # Aucun secret renseigné : toute valeur d'une variable secrète est une référence ${...}.
  if grep -E '^[[:space:]]*(INSTANCE_PASSWORD|SIGNING_SECRET|SMTP_PASSWORD|BRAVE_API_KEY|SERPER_API_KEY)[:=]' "$ex" \
     | grep -vE '\$\{[A-Z_]+(:\?[^}]*)?\}[[:space:]]*$' | grep -q .; then
    ko "$ex contient un secret renseigné"
  fi
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    INSTANCE_PASSWORD=x SIGNING_SECRET=x docker compose -f "$ex" config -q >/dev/null 2>&1 || ko "$ex : docker compose config échoue"
  elif command -v python3 >/dev/null 2>&1 && python3 -c 'import yaml' 2>/dev/null; then
    python3 -c 'import sys,yaml;d=yaml.safe_load(open(sys.argv[1]));assert "services" in d' "$ex" || ko "$ex : YAML invalide"
  else
    grep -q '^services:' "$ex" || ko "$ex : pas de clé services"
  fi
fi
[ "$fail" -eq 0 ] && echo "OK"
exit "$fail"
