#!/usr/bin/env bash
# F12a : contrôle de la documentation française.
#   check-docs.sh                 sections du README et du guide
#   check-docs.sh --deliverability  guide de délivrabilité
#   check-docs.sh --i18n          guide de traduction et commandes citées
set -euo pipefail
cd "$(dirname "$0")/.."
fail=0
ko() { echo "KO : $*" >&2; fail=1; }
has() { grep -qiE -- "$2" "$1" || ko "$1 : « $3 » absent"; }
french() { # le fichier doit être en français
  [ -f "$1" ] || { ko "$1 absent"; return; }
  n=$(grep -ciE '\b(le|la|les|des|pour|dans|avec|vous)\b' "$1" || true)
  [ "$n" -ge 3 ] || ko "$1 ne semble pas en français"
}

case "${1:-}" in
--deliverability)
  f=docs/guide/deliverability.md
  french "$f"
  has "$f" 'v=spf1' "enregistrement SPF"
  has "$f" 'v=DKIM1' "enregistrement DKIM"
  has "$f" 'v=DMARC1' "enregistrement DMARC"
  has "$f" 'PTR' "DNS inverse (PTR)"
  has "$f" 'DNS inverse' "DNS inverse"
  has "$f" '^#+ .*chauffement' "échauffement progressif"
  has "$f" 'listes noires' "listes noires"
  has "$f" '^#+ .*ce que l.application fait et ne fait pas' "section « ce que l'application fait et ne fait pas »"
  has "$f" 'garantir la livraison' "aveu de non-garantie"
  has README.md 'deliverability\.md' "lien du README"
  ;;
--i18n)
  f=docs/guide/traduire.md
  french "$f"
  has "$f" 'i18n export' "commande export"
  has "$f" 'i18n check' "commande check"
  has "$f" 'I18N_DIR' "I18N_DIR"
  has "$f" 'volume' "volume"
  # Chaque sous-commande citée doit exister dans le binaire.
  while read -r sub; do
    grep -q "case \"$sub\"" internal/i18n/cli/cli.go || ko "sous-commande i18n $sub inexistante"
  done < <(grep -oE 'secretsanta i18n [a-z]+' "$f" | awk '{print $3}' | sort -u)
  grep -q 'os.Args\[1\] == "i18n"' cmd/secretsanta/main.go || ko "sous-commande i18n absente du binaire"
  ;;
"")
  for f in README.md docs/guide/index.md docs/guide/configuration.md; do french "$f"; done
  all="README.md docs/guide/*.md"
  for s in "Installation (avec )?Docker" "Variables d.environnement" "SMTP" "Sources de suggestions" \
           "Sauvegarde et restauration" "Mise à jour" "Sécurité" "Dépannage" "Recette locale"; do
    # shellcheck disable=SC2086
    grep -qiE "^#+ .*($s)" README.md || ko "README.md : section « $s » absente"
    # shellcheck disable=SC2086
    cat $all | grep -qiE "^#+ .*($s)" || ko "guide : section « $s » absente"
  done
  ;;
*) echo "usage : $0 [--deliverability|--i18n]" >&2; exit 2 ;;
esac
[ "$fail" -eq 0 ] && echo "OK"
exit "$fail"
