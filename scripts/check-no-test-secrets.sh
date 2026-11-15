#!/usr/bin/env bash
# F12a.AC2 : les valeurs de secrets de test n'existent que dans
# docker-compose.test.yml ; docker-compose.yml, .env.example et le Dockerfile
# n'en contiennent aucune.
set -euo pipefail
cd "$(dirname "$0")/.."
test_file=docker-compose.test.yml
others=(docker-compose.yml .env.example Dockerfile)

[ -f "$test_file" ] || { echo "$test_file absent" >&2; exit 1; }

# Valeurs par défaut des variables SS_TEST_*SECRET/PASSWORD du fichier de recette.
values=$(sed -nE 's/.*\$\{SS_TEST_[A-Z_]*(PASSWORD|SECRET)[A-Z_]*:-([^}]+)\}.*/\2/p' "$test_file")
[ -n "$values" ] || { echo "aucune valeur de secret de test trouvée dans $test_file" >&2; exit 1; }

status=0
for f in "${others[@]}"; do
  [ -f "$f" ] || { echo "$f absent" >&2; status=1; continue; }
  while IFS= read -r v; do
    if grep -Fq -- "$v" "$f"; then
      echo "valeur de secret de test trouvée dans $f : $v" >&2; status=1
    fi
  done <<<"$values"
  if grep -Eq 'ss-test-|SS_TEST_' "$f"; then
    echo "marqueur de recette (ss-test-/SS_TEST_) trouvé dans $f" >&2; status=1
  fi
done
[ "$status" = 0 ] || exit 1
echo "OK : les valeurs de secrets de test n'existent que dans $test_file"
