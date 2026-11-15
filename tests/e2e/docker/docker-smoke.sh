#!/usr/bin/env bash
# F1c.AC5 : image statique non-root sans shell ; healthcheck vert avec les
# secrets ; refus de démarrer sans eux.
set -euo pipefail
cd "$(dirname "$0")/../../.."

tag="secretsanta-smoke:$$"
name="secretsanta-smoke-$$"
cleanup() {
  docker rm -f "$name" "$name-nosecret" >/dev/null 2>&1 || true
  docker rmi -f "$tag" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker build -t "$tag" .

user=$(docker image inspect -f '{{.Config.User}}' "$tag")
case "$user" in ''|0|root|0:*|root:*) echo "image non non-root : '$user'" >&2; exit 1 ;; esac

hc=$(docker image inspect -f '{{json .Config.Healthcheck.Test}}' "$tag")
case "$hc" in *healthcheck*) ;; *) echo "HEALTHCHECK absent : $hc" >&2; exit 1 ;; esac

if docker run --rm --entrypoint /bin/sh "$tag" -c true >/dev/null 2>&1; then
  echo "l'image contient un shell" >&2; exit 1
fi

# Sans secrets : le conteneur doit s'arrêter avec un code non nul.
docker run -d --name "$name-nosecret" "$tag" >/dev/null
code=$(docker wait "$name-nosecret")
if [ "$code" = "0" ]; then echo "démarrage sans secrets accepté" >&2; exit 1; fi

# Avec secrets : le healthcheck passe au vert.
docker run -d --name "$name" \
  -e INSTANCE_PASSWORD=smoke-instance-pass \
  -e SIGNING_SECRET=smoke-signing-secret-0123456789abcdef0123456789 \
  --health-interval=2s --health-start-period=1s --health-retries=10 \
  "$tag" >/dev/null
for _ in $(seq 1 40); do
  status=$(docker inspect -f '{{.State.Health.Status}}' "$name")
  [ "$status" = "healthy" ] && break
  [ "$(docker inspect -f '{{.State.Running}}' "$name")" = "true" ] || { docker logs "$name" >&2; echo "conteneur arrêté" >&2; exit 1; }
  sleep 1
done
if [ "$status" != "healthy" ]; then docker logs "$name" >&2; echo "healthcheck non vert : $status" >&2; exit 1; fi
docker exec "$name" /secretsanta healthcheck
echo "OK : image saine"
