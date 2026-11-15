#!/usr/bin/env bash
# F12b.AC7 : aucun parcours n'utilise de boîte du navigateur (alert, confirm, prompt) et
# les écouteurs de dialogue sont retirés en fin de test.
#
#   scripts/check-no-dialogs.sh
#
# Contrôle statique de tests/e2e/journey/** (le garde d'exécution est dans les parcours :
# tests/e2e/journey/b/dialogs.ts compte les boîtes ouvertes et échoue s'il en voit une) :
#   1. aucun appel alert(, confirm(, prompt( ni acceptation de boîte (dialog.accept) ;
#   2. dans chaque fichier, autant de « .off('dialog' » que de « .on('dialog' »
#      (un écouteur posé est retiré) ;
#   3. chaque parcours du dossier b/ écoute les boîtes (guardDialogs ou .on('dialog') :
#      sans écouteur, Playwright les refuserait en silence.
set -euo pipefail
cd "$(dirname "$0")/.."
root=tests/e2e/journey
[ -d "$root" ] || { echo "dossier $root introuvable" >&2; exit 2; }

fail=0
files=$(find "$root" -type f \( -name '*.ts' -o -name '*.mts' \) | sort)
[ -n "$files" ] || { echo "aucun fichier dans $root" >&2; exit 2; }

for f in $files; do
  # Les lignes de commentaire (// ou *) ne comptent pas.
  code=$(grep -vE '^[[:space:]]*(//|\*|/\*)' "$f" || true)
  if printf '%s\n' "$code" | grep -nE '(^|[^A-Za-z0-9_.])(window\.)?(alert|confirm|prompt)[[:space:]]*\(' >/dev/null; then
    echo "$f : appel de alert/confirm/prompt" >&2; fail=1
  fi
  if printf '%s\n' "$code" | grep -nE '\.(accept|acceptDialog)[[:space:]]*\(' | grep -i dialog >/dev/null; then
    echo "$f : acceptation d'une boîte du navigateur" >&2; fail=1
  fi
  on=$(printf '%s\n' "$code" | grep -cE "\.(on|once)\(['\"]dialog['\"]" || true)
  off=$(printf '%s\n' "$code" | grep -cE "\.off\(['\"]dialog['\"]|\.removeListener\(['\"]dialog['\"]" || true)
  if [ "$off" -lt "$on" ]; then
    echo "$f : $on écouteur(s) de dialogue posé(s), $off retiré(s)" >&2; fail=1
  fi
done

for f in $(find "$root/b" -type f -name '*.spec.ts' 2>/dev/null | sort); do
  if ! grep -qE "guardDialogs\(|\.on\(['\"]dialog['\"]" "$f"; then
    echo "$f : le parcours n'écoute pas les boîtes du navigateur" >&2; fail=1
  fi
  if grep -q 'guardDialogs(' "$f" && ! grep -qE '\.stop\(\)' "$f"; then
    echo "$f : guardDialogs sans stop() en fin de test" >&2; fail=1
  fi
done

if [ "$fail" -ne 0 ]; then exit 1; fi
echo "aucune boîte du navigateur dans $root : $(printf '%s\n' "$files" | wc -l | tr -d ' ') fichier(s) contrôlé(s)"
