#!/usr/bin/env bash
# F12b.3 : contrôles de l'image finale (F12b.AC10).
#   - taille <= 40 Mo (valeur mesurée affichée) ;
#   - utilisateur non root ;
#   - aucun shell dans l'image ;
#   - binaire statique (construit sans CGO) ;
#   - healthcheck déclaré et vert ;
#   - refus de démarrer sans les secrets obligatoires.
# Docker : honore DOCKER_CONTEXT / DOCKER_HOST. Les conteneurs et l'image créés
# (préfixe ss-test-image) sont supprimés à la sortie. Aucun prune.
set -euo pipefail
cd "$(dirname "$0")/.."

image=ss-test-image-secretsanta:check
c_ok=ss-test-image-ok
c_nosecret=ss-test-image-nosecret
c_copy=ss-test-image-copy
max_bytes=$((40 * 1000 * 1000))
tmp=$(mktemp -d "${TMPDIR:-/tmp}/ss-check-image.XXXXXX")

cleanup() {
  code=$?
  trap - EXIT
  docker rm -f "$c_ok" "$c_nosecret" "$c_copy" >/dev/null 2>&1 || true
  docker rmi -f "$image" >/dev/null 2>&1 || true
  rm -rf "$tmp"
  exit "$code"
}
trap cleanup EXIT

fail() { echo "ECHEC : $*" >&2; exit 1; }
ok() { echo "ok : $*"; }

case "$(docker info --format '{{.Architecture}}')" in
  aarch64|arm64) goarch=arm64 ;;
  x86_64|amd64) goarch=amd64 ;;
  *) echo "architecture du démon non prise en charge" >&2; exit 2 ;;
esac
echo "== construction de l'image (linux/$goarch)"
docker build --platform "linux/$goarch" --build-arg "VERSION=$(cat VERSION)-controle" -t "$image" . >/dev/null

# Taille
size=$(docker image inspect --format '{{.Size}}' "$image")
mo=$(awk -v s="$size" 'BEGIN { printf "%.1f", s / 1000000 }')
[ "$size" -le "$max_bytes" ] || fail "image de $mo Mo, au-delà de 40 Mo"
ok "taille mesurée $mo Mo (maximum 40 Mo)"

# Utilisateur non root
user=$(docker image inspect --format '{{.Config.User}}' "$image")
case "$user" in
  ""|0|root|0:*|root:*) fail "l'image s'exécute en root (User='$user')" ;;
esac
ok "utilisateur non root ($user)"

# Healthcheck déclaré
hc=$(docker image inspect --format '{{json .Config.Healthcheck.Test}}' "$image")
case "$hc" in
  ""|null|'["NONE"]') fail "aucun healthcheck déclaré" ;;
esac
ok "healthcheck déclaré ($hc)"

# Aucun shell : ni exécutable connu, ni lancement possible
for sh in /bin/sh /bin/bash /busybox/sh /usr/bin/sh /bin/ash; do
  if docker run --rm --entrypoint "$sh" "$image" -c true >/dev/null 2>&1; then
    fail "un shell est présent dans l'image ($sh)"
  fi
done
docker create --name "$c_copy" "$image" >/dev/null
docker export "$c_copy" >"$tmp/rootfs.tar"
if tar -tf "$tmp/rootfs.tar" | grep -E '(^|/)(bash|sh|ash|dash|zsh|busybox)$' >/dev/null; then
  fail "un exécutable de shell figure dans le système de fichiers de l'image"
fi
ok "aucun shell dans l'image"

# Binaire statique : construit sans CGO, et pas d'interpréteur ELF
mkdir "$tmp/bin"
tar -xf "$tmp/rootfs.tar" -C "$tmp/bin" secretsanta
if go version -m "$tmp/bin/secretsanta" | grep -q 'CGO_ENABLED=1'; then fail "binaire construit avec CGO"; fi
if command -v file >/dev/null 2>&1 && file "$tmp/bin/secretsanta" | grep -q 'dynamically linked'; then
  fail "binaire lié dynamiquement"
fi
ok "binaire statique (sans CGO)"

# Démarrage sans secrets : doit s'arrêter avec un code non nul
docker run -d --name "$c_nosecret" "$image" >/dev/null
for _ in $(seq 1 30); do
  [ "$(docker inspect --format '{{.State.Running}}' "$c_nosecret")" = false ] && break
  sleep 1
done
[ "$(docker inspect --format '{{.State.Running}}' "$c_nosecret")" = false ] || fail "le conteneur démarre sans les secrets obligatoires"
[ "$(docker inspect --format '{{.State.ExitCode}}' "$c_nosecret")" != 0 ] || fail "arrêt sans secrets avec le code 0"
ok "refuse de démarrer sans INSTANCE_PASSWORD ni SIGNING_SECRET"

# Démarrage normal : healthcheck vert (secrets jetables générés ici)
docker run -d --name "$c_ok" \
  --health-interval 2s --health-timeout 5s --health-start-period 1s --health-retries 3 \
  -e "INSTANCE_PASSWORD=t-$(openssl rand -hex 12)" \
  -e "SIGNING_SECRET=t-$(openssl rand -hex 32)" \
  -e PUBLIC_URL=http://127.0.0.1:8080 \
  "$image" >/dev/null
state=""
for _ in $(seq 1 60); do
  state=$(docker inspect --format '{{.State.Health.Status}}' "$c_ok")
  [ "$state" = healthy ] && break
  [ "$(docker inspect --format '{{.State.Running}}' "$c_ok")" = true ] || break
  sleep 1
done
if [ "$state" != healthy ]; then docker logs --tail 30 "$c_ok" >&2 || true; fail "healthcheck non vert (état : $state)"; fi
ok "healthcheck vert"
echo "== image conforme ($mo Mo)"
