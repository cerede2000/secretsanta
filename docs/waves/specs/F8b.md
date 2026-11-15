---
id: F8b
title: "Sources de suggestions, catalogue de secours et enrichissement"
depends_on: [F8, F1b, F1]
owns: [internal/sources/**, internal/enrich/**]
reads: [internal/ssrf/**, internal/shared/**, internal/config/**]
risk: []
steps: [F8b.1, F8b.2]
---

# F8b Sources de suggestions, catalogue de secours et enrichissement

## Summary

Les propositions de cadeaux viennent de sources configurables : SearXNG auto-hébergé en priorité, clés d'API facultatives (Brave, Serper), catalogue de secours embarqué par tranche de budget (dont « toutes gammes » pour un groupe sans budget). Les liens Amazon trouvés sont enrichis en meilleur effort par extraction de page.

## In scope

- Sources, toutes configurables par variables d'environnement (déclarées dans F1) : SearXNG auto-hébergé (gratuit, prioritaire), clés facultatives Brave et Serper, catalogue de secours embarqué (go:embed) par tranche de budget, avec une tranche « toutes gammes » pour un groupe sans budget, utilisé quand aucune source n'est disponible ou ne répond. Ordre de repli défini et testé ; une source en panne ne bloque pas les suivantes ; aucune clé ne figure dans le code ni dans les journaux.
- Les liens Amazon renvoyés par la recherche sont enrichis (titre, image, prix) par extraction de la page (analyse HTML en Go), en meilleur effort, sans aucune garantie ; échec d'extraction = suggestion conservée avec les données disponibles ; aucune API Amazon payante.

## Out of scope

- Garde SSRF (F8), service de suggestions et page (F8c).

## Acceptance criteria

```yaml criteria
AC1: {text: "L'ordre de repli SearXNG, puis clés d'API, puis catalogue embarqué est respecté et une source en panne ne bloque pas la suivante", verify: "test:internal/sources/source_fallback_test.go"}
AC2: {text: "Le catalogue embarqué fournit des propositions pour chaque tranche de budget et pour un groupe sans budget (toutes gammes) et fonctionne sans aucune variable de source", verify: "test:internal/sources/embedded_catalog_test.go"}
AC3: {text: "Les sources sont lues des variables d'environnement et aucune clé n'apparaît dans les journaux ni dans les réponses", verify: "test:internal/sources/source_config_no_secrets_test.go"}
AC4: {text: "L'enrichissement d'un lien Amazon extrait titre, image et prix quand la page le permet et conserve la suggestion avec les données disponibles quand l'extraction échoue", verify: "test:internal/enrich/amazon_enrichment_test.go"}
```

## Steps

- F8b.1 Adaptateurs de sources (SearXNG, Brave, Serper) configurables, ordre de repli et catalogue de secours par tranche de budget.
- F8b.2 Enrichissement des liens Amazon par extraction de page en meilleur effort, sans API payante.
