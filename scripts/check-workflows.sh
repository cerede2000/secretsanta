#!/usr/bin/env bash
# F12b.3 : contrôle des workflows coverage.yml et recette.yml (F12b.AC12).
#   - coverage.yml publie un artefact, ne publie pas l'image, ne bloque pas (autre
#     workflow que ci.yml, jamais de dépendance « needs » vers la publication) ;
#   - recette.yml ne cible que linux/amd64 et ne publie rien (pas de push, pas de
#     connexion au registre, pas de permission d'écriture sur les paquets) ;
#   - aucun des deux ne dérive de tag depuis github.sha.
set -euo pipefail
cd "$(dirname "$0")/.."
wf=.github/workflows
errors=0
fail() { echo "ECHEC : $*" >&2; errors=$((errors + 1)); }

for f in coverage.yml recette.yml; do
  [ -f "$wf/$f" ] || { fail "$wf/$f absent"; continue; }
  grep -qE 'github\.sha|GITHUB_SHA' "$wf/$f" && fail "$f dérive quelque chose de github.sha"
  grep -qE 'docker/(login|build-push)-action|docker push|--push|push: *true' "$wf/$f" && fail "$f publie une image"
  grep -qE '^[[:space:]]*packages:[[:space:]]*write' "$wf/$f" && fail "$f demande packages: write"
  grep -qE '^[[:space:]]*contents:[[:space:]]*write' "$wf/$f" && fail "$f demande contents: write"
  grep -qE 'git (tag|push)|--tag|tags:' "$wf/$f" && fail "$f manipule des tags"
  grep -qE '^[[:space:]]*needs:' "$wf/$f" && grep -qE 'publish' "$wf/$f" && fail "$f dépend de la publication"
done

if [ -f "$wf/coverage.yml" ]; then
  grep -q 'actions/upload-artifact' "$wf/coverage.yml" || fail "coverage.yml ne publie pas d'artefact"
  grep -q 'scripts/coverage.sh' "$wf/coverage.yml" || fail "coverage.yml n'appelle pas scripts/coverage.sh"
  grep -q 'enforce' "$wf/coverage.yml" && fail "coverage.yml ne doit pas bloquer (--enforce)"
  grep -qE 'if: *always\(\)' "$wf/coverage.yml" || fail "l'artefact doit être publié même si la mesure échoue (if: always())"
fi

if [ -f "$wf/recette.yml" ]; then
  grep -q 'linux/amd64' "$wf/recette.yml" || fail "recette.yml ne cible pas linux/amd64"
  grep -qE 'linux/(arm|386|ppc|s390|riscv)' "$wf/recette.yml" && fail "recette.yml cible une autre plateforme"
  grep -q 'scripts/e2e.sh' "$wf/recette.yml" || fail "recette.yml ne rejoue pas la recette (scripts/e2e.sh)"
fi

# Le workflow de publication de F1 reste la seule source des tags : lui seul publie.
for f in "$wf"/*.yml; do
  case "$(basename "$f")" in ci.yml|coverage.yml|recette.yml) ;; *) 
    grep -qE 'docker/build-push-action|git tag' "$f" && fail "$f publie ou crée des tags en dehors de ci.yml" ;;
  esac
done

[ "$errors" -eq 0 ] || exit 1
echo "workflows conformes"
