#!/usr/bin/env bash
# F1c.AC15 : le volume ./i18n:/i18n et I18N_DIR sont montrés en commentaire
# seulement (jamais actifs, donc jamais obligatoires).
set -euo pipefail
cd "$(dirname "$0")/../../.."
f=docker-compose.example.yml

grep -Eq '^[[:space:]]*#[[:space:]]*-[[:space:]]*\./i18n:/i18n' "$f" \
  || { echo "volume ./i18n:/i18n absent en commentaire" >&2; exit 1; }
grep -Eq '^[[:space:]]*#[[:space:]]*I18N_DIR:' "$f" \
  || { echo "I18N_DIR absent en commentaire" >&2; exit 1; }
if grep -E '^[[:space:]]*[^#[:space:]].*(\./i18n:/i18n|I18N_DIR)' "$f"; then
  echo "i18n actif dans docker-compose.example.yml : doit rester en commentaire" >&2; exit 1
fi
echo "OK : i18n facultatif, en commentaire"
