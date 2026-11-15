#!/usr/bin/env bash
# F12b.3 : couverture de test par paquet internal/* (statements).
#
#   scripts/coverage.sh             imprime le tableau, n'échoue pas sous les seuils
#   scripts/coverage.sh --enforce   échoue si un paquet est sous son seuil
#
# Seuils : 80 % par paquet internal/* (hors internal/app, câblage couvert par les
# parcours) et 85 % pour internal/draw. Un paquet sans test compte pour 0 % ; un
# paquet sans instruction est ignoré. Un échec de test fait toujours échouer le script.
# Variables : COVERAGE_ROOT (module à mesurer, défaut : le dépôt), COVERAGE_OUT
# (dossier du profil coverage.out et du tableau coverage.txt, défaut : le dossier
# temporaire du système).
set -euo pipefail

enforce=0
for a in "$@"; do
  case "$a" in
    --enforce) enforce=1 ;;
    -h|--help) sed -n '2,/^set -/p' "$0" | sed '$d'; exit 0 ;;
    *) echo "option inconnue : $a" >&2; exit 2 ;;
  esac
done

root=${COVERAGE_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}
out=${COVERAGE_OUT:-${TMPDIR:-/tmp}}
mkdir -p "$out"
cd "$root"
mod=$(go list -m)

raw=$(mktemp "${TMPDIR:-/tmp}/ss-cover.XXXXXX")
trap 'rm -f "$raw"' EXIT

status=0
go test -covermode=set -coverprofile="$out/coverage.out" ./internal/... >"$raw" 2>&1 || status=$?

threshold_for() {
  case "$1" in
    "$mod/internal/draw") echo 85 ;;
    *) echo 80 ;;
  esac
}

table="$out/coverage.txt"
{
  printf '%-60s %9s %7s  %s\n' PAQUET COUVERTURE SEUIL ETAT
} >"$table"

below=0
while IFS= read -r line; do
  pkg=$(printf '%s\n' "$line" | awk '{ for (i = 1; i <= NF; i++) if ($i ~ /\/internal\//) { print $i; exit } }')
  [ -n "$pkg" ] || continue
  [ "$pkg" != "$mod/internal/app" ] || continue
  case "$pkg" in "$mod/internal/app/"*) continue ;; esac
  case "$line" in
    *"[no statements]"*) continue ;;
    *"coverage: "*"% of statements"*)
      pct=$(printf '%s\n' "$line" | sed -E 's/.*coverage: ([0-9.]+)% of statements.*/\1/') ;;
    *"[no test files]"*) pct=0.0 ;;
    *) continue ;;
  esac
  min=$(threshold_for "$pkg")
  if awk -v p="$pct" -v m="$min" 'BEGIN { exit !(p + 0 >= m + 0) }'; then etat=ok; else etat="SOUS LE SEUIL"; below=$((below + 1)); fi
  printf '%-60s %8s%% %6s%%  %s\n' "${pkg#"$mod/"}" "$pct" "$min" "$etat" >>"$table"
done < <(grep -E '^(ok|FAIL|\?)?[[:space:]]*[^[:space:]]*/internal/' "$raw" | grep -vE '^(---|===)' || true)

cat "$table"

if [ "$status" -ne 0 ]; then
  echo "go test a échoué :" >&2
  grep -E '^(FAIL|--- FAIL|panic)' "$raw" >&2 || tail -n 30 "$raw" >&2
  exit 1
fi
echo "profil : $out/coverage.out ; tableau : $table"
if [ "$below" -gt 0 ]; then
  if [ "$enforce" = 1 ]; then
    echo "$below paquet(s) sous le seuil de couverture" >&2
    exit 1
  fi
  echo "$below paquet(s) sous le seuil (non bloquant sans --enforce)"
fi
exit 0
