#!/usr/bin/env bash
# F12a.AC5 : note des messages .eml bruts avec SpamAssassin (conteneur du banc).
#
#   scripts/spamcheck.sh DOSSIER     note chaque fichier *.eml du dossier
#   scripts/spamcheck.sh --mailpit   note les messages capturés par Mailpit
#   scripts/spamcheck.sh --self-test vérifie le scanner (sain < seuil, pourri >= seuil)
#
# Seuil : un message est accepté si son score est inférieur à 3,0.
# Honnêteté : le score mesure le CONTENU et la forme du message (SpamAssassin en
# mode local, sans aucun test réseau). Il ne dit RIEN de la réputation de
# l'adresse IP ni des listes noires, et SpamAssassin n'a ici aucun contexte
# DKIM/SPF/DMARC : ces mécanismes ne sont pas évalués.
#
# Honore DOCKER_CONTEXT / DOCKER_HOST s'ils sont définis (sinon contexte courant).
# Variable : SS_TEST_MAIL_API_PORT (défaut 18025) pour --mailpit.
set -euo pipefail
cd "$(dirname "$0")/.."

THRESHOLD=3.0
SAMPLES=tests/e2e/spamassassin
COMPOSE_FILE=docker-compose.test.yml
API="http://127.0.0.1:${SS_TEST_MAIL_API_PORT:-18025}"

image=$(sed -nE 's/^[[:space:]]*image:[[:space:]]*(instantlinux\/spamassassin:[^[:space:]]+).*/\1/p' "$COMPOSE_FILE" | head -1)
[ -n "$image" ] || { echo "image SpamAssassin introuvable dans $COMPOSE_FILE" >&2; exit 2; }
case "$image" in *:latest*|*:) echo "tag SpamAssassin non épinglé : $image" >&2; exit 2 ;; esac

banner() {
  cat <<MSG
SpamAssassin ($image), seuil : score < $THRESHOLD
Attention : ce score mesure le CONTENU et la forme des messages, pas la réputation
de l'adresse IP ni les listes noires ; aucun test réseau n'est fait, et il n'y a ici
aucun contexte DKIM/SPF/DMARC (ces mécanismes ne sont pas évalués).
MSG
}

# score FICHIER : écrit le score sur la sortie standard.
score() {
  local out s
  out=$(docker run --rm -i --network none --entrypoint spamassassin "$image" -L -t <"$1") \
    || { echo "échec de SpamAssassin sur $1" >&2; return 1; }
  s=$(printf '%s\n' "$out" | sed -nE 's/^X-Spam-Status:.*score=(-?[0-9]+(\.[0-9]+)?).*/\1/p' | head -1)
  [ -n "$s" ] || { echo "score illisible pour $1" >&2; return 1; }
  printf '%s\n' "$s"
}

below() { awk -v s="$1" -v t="$THRESHOLD" 'BEGIN{exit !(s<t)}'; }

check_files() {
  local bad=0 f s
  for f in "$@"; do
    s=$(score "$f") || return 2
    if below "$s"; then printf 'OK      %6s  %s\n' "$s" "$f"
    else printf 'TROP HAUT %4s  %s\n' "$s" "$f"; bad=1; fi
  done
  return $bad
}

mode=${1:-}
case "$mode" in
  --self-test)
    banner
    good=$(score "$SAMPLES/sain.eml") || exit 2
    bad=$(score "$SAMPLES/pourri.eml") || exit 2
    echo "échantillon sain   : $good"
    echo "échantillon pourri : $bad"
    below "$good" || { echo "ECHEC : l'échantillon sain n'est pas sous $THRESHOLD" >&2; exit 1; }
    below "$bad" && { echo "ECHEC : l'échantillon pourri n'est pas au-dessus de $THRESHOLD" >&2; exit 1; }
    echo "OK : le scanner fonctionne (sain < $THRESHOLD <= pourri)"
    ;;
  --mailpit)
    banner
    tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
    ids=$(curl -fsS "$API/api/v1/messages?limit=500" | jq -r '.messages[].ID')
    [ -n "$ids" ] || { echo "aucun message capturé par Mailpit ($API)" >&2; exit 2; }
    for id in $ids; do curl -fsS "$API/api/v1/message/$id/raw" >"$tmp/$id.eml"; done
    check_files "$tmp"/*.eml
    ;;
  ""|-h|--help)
    sed -n '2,8p' "$0"; [ -n "$mode" ] || exit 2 ;;
  -*) echo "option inconnue : $mode" >&2; exit 2 ;;
  *)
    [ -d "$mode" ] || { echo "dossier introuvable : $mode" >&2; exit 2; }
    shopt -s nullglob; files=("$mode"/*.eml)
    [ ${#files[@]} -gt 0 ] || { echo "aucun .eml dans $mode" >&2; exit 2; }
    banner
    check_files "${files[@]}"
    ;;
esac
