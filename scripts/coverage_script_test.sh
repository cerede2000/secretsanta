#!/usr/bin/env bash
# F12b.AC9 : sans --enforce, scripts/coverage.sh imprime le tableau et n'échoue pas
# sous un seuil ; avec --enforce il échoue. Joue le script sur un module jetable.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/ss-covtest.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

mkdir -p "$tmp/mod/internal/bas" "$tmp/mod/internal/haut" "$tmp/mod/internal/app"
cd "$tmp/mod"
cat >go.mod <<'M'
module fixture

go 1.21
M
cat >internal/bas/bas.go <<'M'
package bas

func A(x int) int {
	if x > 0 {
		return 1
	}
	if x < -5 {
		return 2
	}
	if x < -10 {
		return 3
	}
	return 0
}

func B() int { return 7 }
M
cat >internal/bas/bas_test.go <<'M'
package bas

import "testing"

func TestA(t *testing.T) { _ = A(1) }
M
cat >internal/haut/haut.go <<'M'
package haut

func A() int { return 1 }
M
cat >internal/haut/haut_test.go <<'M'
package haut

import "testing"

func TestA(t *testing.T) { _ = A() }
M
cat >internal/app/app.go <<'M'
package app

func A() int { return 1 }
M

export COVERAGE_ROOT="$tmp/mod" COVERAGE_OUT="$tmp/out"
fail() { echo "ECHEC : $*" >&2; exit 1; }

sortie=$(bash "$here/coverage.sh") || fail "sans --enforce le script ne doit pas échouer"
echo "$sortie" | grep -q 'internal/bas' || fail "tableau sans internal/bas"
echo "$sortie" | grep -q 'internal/haut' || fail "tableau sans internal/haut"
echo "$sortie" | grep 'internal/bas' | grep -q 'SOUS LE SEUIL' || fail "internal/bas devrait être sous le seuil"
echo "$sortie" | grep 'internal/haut' | grep -q ' ok' || fail "internal/haut devrait être ok"
echo "$sortie" | grep -q 'internal/app' && fail "internal/app doit être exclu"

if bash "$here/coverage.sh" --enforce >/dev/null 2>&1; then fail "--enforce devrait échouer sous le seuil"; fi
echo "coverage_script_test : ok"
