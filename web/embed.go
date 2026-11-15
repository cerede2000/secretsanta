// Package web embarque les ressources statiques de l'interface. Fichier
// d'intégration : les gabarits et feuilles de style y seront ajoutés par F1c.
package web

import "embed"

// I18n contient les fichiers de textes (static/i18n/<module>/<langue>.json).
//
//go:embed static/i18n/*/*.json
var I18n embed.FS
