#!/usr/bin/env bash
# F12b.3 : recherche de vulnérabilités (F12b.AC11). govulncheck ne sort en erreur
# que pour une vulnérabilité atteignable depuis le code ; les alertes seulement
# présentes dans les dépendances sont rapportées (mode -show verbose) sans échec.
set -euo pipefail
cd "$(dirname "$0")/.."

gvc=$(command -v govulncheck || true)
[ -n "$gvc" ] || gvc="$(go env GOPATH)/bin/govulncheck"
if [ ! -x "$gvc" ]; then
  echo "govulncheck absent : installation" >&2
  go install golang.org/x/vuln/cmd/govulncheck@latest
  gvc="$(go env GOPATH)/bin/govulncheck"
fi

echo "== govulncheck ./..."
set +e
"$gvc" -show verbose ./... 2>&1 | tee "${TMPDIR:-/tmp}/ss-vuln.$$.txt"
code=${PIPESTATUS[0]}
set -e
rm -f "${TMPDIR:-/tmp}/ss-vuln.$$.txt"
if [ "$code" -ne 0 ]; then
  echo "ECHEC : vulnérabilité atteignable trouvée (ou govulncheck en erreur, code $code)" >&2
  exit 1
fi
echo "== aucune vulnérabilité atteignable"
